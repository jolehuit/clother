package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/jolehuit/clother/internal/profiles"
	"github.com/jolehuit/clother/internal/providers"
	"github.com/jolehuit/clother/internal/ui"
)

const testTimeout = 10 * time.Second

type testOutcome string

const (
	testOK      testOutcome = "ok"
	testSkipped testOutcome = "skipped"
	testFailed  testOutcome = "failed"
)

type testResult struct {
	Profile string      `json:"profile"`
	Outcome testOutcome `json:"outcome"`
	Status  int         `json:"http_status,omitempty"`
	Detail  string      `json:"detail"`
	// Kind is the stable classification a script should branch on. Detail is
	// for humans and may be reworded; Kind is the contract.
	Kind ui.ErrorKind `json:"kind,omitempty"`
}

func runTest(ctx context.Context, c Context, args []string) (int, error) {
	var targets []profiles.Target
	explicit := len(args) > 0
	if explicit {
		for _, name := range args {
			target, err := resolveTargetArg(c, name)
			if err != nil {
				return 1, err
			}
			targets = append(targets, target)
		}
	} else {
		targets = profiles.All(c.Catalog, c.Config)
	}

	var probed []profiles.Target
	for _, target := range targets {
		// Same criterion as bench: a profile without a base URL (native) has
		// nothing to probe.
		if target.BaseURL == "" {
			continue
		}
		probed = append(probed, target)
	}

	results := make([]testResult, len(probed))
	var wg sync.WaitGroup
	for i, target := range probed {
		wg.Add(1)
		go func(idx int, target profiles.Target) {
			defer wg.Done()
			results[idx] = probeTarget(ctx, c, target, explicit)
		}(i, target)
	}
	wg.Wait()

	okCount, failCount, skipCount := 0, 0, 0
	for _, result := range results {
		switch result.Outcome {
		case testOK:
			okCount++
		case testFailed:
			failCount++
		default:
			skipCount++
		}
	}

	if c.Output.Machine() {
		// The envelope reports that the command ran; the per-provider verdicts
		// live in the data and the exit code carries the overall result.
		payload := struct {
			Results []testResult `json:"results"`
			OK      int          `json:"ok"`
			Failed  int          `json:"failed"`
			Skipped int          `json:"skipped"`
		}{Results: results, OK: okCount, Failed: failCount, Skipped: skipCount}
		if err := c.Output.Emit("test", payload); err != nil {
			return 1, err
		}
	} else {
		for _, result := range results {
			fmt.Fprintf(c.Output.Stdout, "  %-18s %s\n", result.Profile, result.Detail)
		}
		fmt.Fprintf(c.Output.Stdout, "\nResults: %d ok, %d failed, %d skipped\n", okCount, failCount, skipCount)
	}

	if failCount > 0 {
		return 1, nil
	}
	return 0, nil
}

// probeTarget issues the request Claude Code would issue: POST /v1/messages
// with the runtime's authentication header. A bare GET on the base URL used to
// answer 404 or 401 and still be reported as "reachable".
func probeTarget(ctx context.Context, c Context, target profiles.Target, explicit bool) testResult {
	result := testResult{Profile: target.Profile}

	header, value, configured := authHeader(target, c.Secrets)
	if !configured {
		result.Outcome = testSkipped
		result.Kind = ui.KindMissingKey
		result.Detail = fmt.Sprintf("not configured (run `clother config %s`)", target.Profile)
		return result
	}

	model := benchModel(target)
	if model == "" {
		result.Outcome = testSkipped
		result.Detail = "no model configured"
		return result
	}

	body, err := json.Marshal(map[string]any{
		"model":      model,
		"max_tokens": 1,
		"stream":     false,
		"messages": []map[string]string{
			{"role": "user", "content": "ping"},
		},
	})
	if err != nil {
		result.Outcome = testFailed
		result.Kind = ui.KindConfigInvalid
		result.Detail = "invalid request: " + err.Error()
		return result
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, messagesEndpoint(target.BaseURL), bytes.NewReader(body))
	if err != nil {
		// The previous code ignored this error and dereferenced a nil request,
		// panicking the whole command on a single malformed base URL.
		result.Outcome = testFailed
		result.Kind = ui.KindConfigInvalid
		result.Detail = fmt.Sprintf("invalid base URL %q", target.BaseURL)
		return result
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("anthropic-version", "2023-06-01")
	if header != "" {
		req.Header.Set(header, value)
	}

	resp, err := credentialClient(testTimeout).Do(req)
	if err != nil {
		if target.Family == providers.FamilyLocal && !explicit {
			result.Outcome = testSkipped
			result.Detail = "local backend not running"
			return result
		}
		result.Outcome = testFailed
		result.Kind = ui.KindUnreachable
		result.Detail = "unreachable: " + truncateRunes(redactSecrets(shortNetErr(err), c.Secrets), 60)
		return result
	}
	defer resp.Body.Close()
	// The body is attacker-controlled: gateways quote the Authorization header
	// back in their 400s, so it is redacted here, once, before any branch below
	// copies it into a Detail that ends up on stdout.
	preview, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
	safePreview := redactSecrets(compactBody(preview), c.Secrets)

	result.Status = resp.StatusCode
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		result.Outcome = testOK
		result.Detail = fmt.Sprintf("ok (HTTP %d)", resp.StatusCode)
	case resp.StatusCode == http.StatusTooManyRequests:
		result.Outcome = testOK
		result.Detail = "ok (HTTP 429 rate limited, credentials accepted)"
	case resp.StatusCode == http.StatusUnauthorized, resp.StatusCode == http.StatusForbidden:
		result.Outcome = testFailed
		result.Kind = ui.KindAuthRejected
		result.Detail = fmt.Sprintf("invalid or missing key (HTTP %d) — run `clother config %s`", resp.StatusCode, target.Profile)
	case resp.StatusCode == http.StatusNotFound:
		result.Outcome = testFailed
		result.Kind = ui.KindConfigInvalid
		result.Detail = "endpoint not found (HTTP 404) — check the base URL"
	case resp.StatusCode == http.StatusBadRequest:
		result.Outcome = testFailed
		result.Kind = ui.KindModelRejected
		result.Detail = "model rejected (HTTP 400): " + truncateRunes(safePreview, 60)
	default:
		result.Outcome = testFailed
		result.Kind = ui.KindUnknown
		result.Detail = fmt.Sprintf("HTTP %d: %s", resp.StatusCode, truncateRunes(safePreview, 60))
	}
	return result
}

func compactBody(data []byte) string {
	return strings.Join(strings.Fields(string(data)), " ")
}

func shortNetErr(err error) string {
	message := err.Error()
	if idx := strings.LastIndex(message, ": "); idx >= 0 && idx+2 < len(message) {
		message = message[idx+2:]
	}
	return message
}
