package commands

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jolehuit/clother/internal/config"
	"github.com/jolehuit/clother/internal/profiles"
	"github.com/jolehuit/clother/internal/providers"
)

const benchDefaultPrompt = "Say hello in one word."

// benchJSONErrorRunes bounds the provider error copied into the JSON envelope.
const benchJSONErrorRunes = 200

type benchResult struct {
	Profile string
	Model   string
	TTFT    time.Duration
	Total   time.Duration
	Preview string
	Err     error
}

func runBench(ctx context.Context, c Context, args []string) (int, error) {
	prompt := benchDefaultPrompt
	var providerFilter []string

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--prompt", "-p":
			if i+1 < len(args) {
				i++
				prompt = args[i]
			}
		default:
			if !strings.HasPrefix(args[i], "-") {
				providerFilter = append(providerFilter, args[i])
			}
		}
	}

	targets := profiles.All(c.Catalog, c.Config)
	var selected []profiles.Target
	for _, t := range targets {
		if t.BaseURL == "" {
			continue // skip native
		}
		if t.Family == providers.FamilyLocal {
			continue // skip local (may not be running)
		}
		if t.AuthMode == providers.AuthSecret && c.Secrets[t.SecretKey] == "" {
			continue // no API key configured
		}
		if benchModel(t) == "" {
			continue // no model to request (e.g. custom provider without a default)
		}
		if len(providerFilter) > 0 {
			found := false
			for _, f := range providerFilter {
				if f == t.Profile {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		selected = append(selected, t)
	}

	if len(selected) == 0 {
		if c.Output.Machine() {
			// JSON mode prints exactly one object on stdout: the envelope.
			return 0, writeBenchJSON(c, prompt, nil)
		}
		fmt.Fprintln(c.Output.Stdout, "No providers available for benchmarking.")
		fmt.Fprintln(c.Output.Stdout, "Configure a provider first: clother config <provider>")
		return 0, nil
	}

	if !c.Output.Machine() {
		fmt.Fprintf(c.Output.Stdout, "Benchmarking %d provider(s) — prompt: %q\n\n", len(selected), prompt)
	}

	results := make([]benchResult, len(selected))
	var wg sync.WaitGroup
	for i, t := range selected {
		wg.Add(1)
		go func(idx int, target profiles.Target) {
			defer wg.Done()
			results[idx] = doBench(ctx, target, c.Secrets, prompt)
		}(i, t)
	}
	wg.Wait()

	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Err != nil && results[j].Err == nil {
			return false
		}
		if results[i].Err == nil && results[j].Err != nil {
			return true
		}
		return results[i].TTFT < results[j].TTFT
	})

	failed := 0
	for _, r := range results {
		if r.Err != nil {
			failed++
		}
	}

	if c.Output.Machine() {
		if err := writeBenchJSON(c, prompt, results); err != nil {
			return 1, err
		}
		if failed == len(results) {
			return 1, nil
		}
		return 0, nil
	}

	fmt.Fprintf(c.Output.Stdout, "  %-18s %-22s %8s %8s   %s\n", "Provider", "Model", "TTFT", "Total", "Preview")
	fmt.Fprintf(c.Output.Stdout, "  %s\n", strings.Repeat("─", 78))
	for _, r := range results {
		if r.Err != nil {
			fmt.Fprintf(c.Output.Stdout, "  %-18s %-22s %8s %8s   ✗ %s\n",
				r.Profile, r.Model, "-", "-", benchShortErr(r.Err, c.Secrets))
		} else {
			fmt.Fprintf(c.Output.Stdout, "  %-18s %-22s %8s %8s   %q\n",
				r.Profile, r.Model,
				benchFmtDur(r.TTFT), benchFmtDur(r.Total),
				r.Preview,
			)
		}
	}
	fmt.Fprintln(c.Output.Stdout)
	if failed == len(results) {
		// Every provider failed: this is a failed benchmark, not a report.
		return 1, nil
	}
	return 0, nil
}

func writeBenchJSON(c Context, prompt string, results []benchResult) error {
	type item struct {
		Profile string `json:"profile"`
		Model   string `json:"model"`
		TTFTms  int64  `json:"ttft_ms,omitempty"`
		TotalMs int64  `json:"total_ms,omitempty"`
		Preview string `json:"preview,omitempty"`
		Error   string `json:"error,omitempty"`
	}
	payload := struct {
		Prompt  string `json:"prompt"`
		Results []item `json:"results"`
	}{Prompt: prompt}
	for _, r := range results {
		entry := item{Profile: r.Profile, Model: r.Model}
		if r.Err != nil {
			// Second belt on the credential, and the only bound on this field:
			// the machine envelope had no length limit at all, so a hostile
			// endpoint could push 256 bytes of anything into a consumer's log.
			entry.Error = truncateRunes(redactSecrets(r.Err.Error(), c.Secrets), benchJSONErrorRunes)
		} else {
			entry.TTFTms = r.TTFT.Milliseconds()
			entry.TotalMs = r.Total.Milliseconds()
			entry.Preview = r.Preview
		}
		payload.Results = append(payload.Results, entry)
	}
	return c.Output.Emit("bench", payload)
}

func doBench(ctx context.Context, target profiles.Target, secrets config.Secrets, prompt string) benchResult {
	model := benchModel(target)
	res := benchResult{Profile: target.Profile, Model: model}

	header, headerValue, configured := authHeader(target, secrets)
	if !configured {
		res.Err = fmt.Errorf("%s not configured", target.SecretKey)
		return res
	}

	endpoint := messagesEndpoint(target.BaseURL)

	body, err := json.Marshal(map[string]interface{}{
		"model":      model,
		"max_tokens": 64,
		"stream":     true,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	})
	if err != nil {
		res.Err = err
		return res
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		res.Err = err
		return res
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("anthropic-version", "2023-06-01")
	if header != "" {
		req.Header.Set(header, headerValue)
	}

	client := credentialClient(30 * time.Second)
	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		res.Err = err
		return res
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		// The body is attacker-controlled and routinely quotes the Authorization
		// header back at us: redact before it becomes an error string, because
		// from there it reaches stdout unbounded in --json.
		res.Err = fmt.Errorf("HTTP %d: %s", resp.StatusCode, redactSecrets(strings.TrimSpace(string(body)), secrets))
		return res
	}

	scanner := bufio.NewScanner(resp.Body)
	ttftDone := false
	var preview strings.Builder

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}
		var event map[string]interface{}
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}
		eventType, _ := event["type"].(string)
		if eventType == "content_block_delta" && !ttftDone {
			res.TTFT = time.Since(start)
			ttftDone = true
		}
		if eventType == "content_block_delta" && preview.Len() < 50 {
			if delta, ok := event["delta"].(map[string]interface{}); ok {
				if text, ok := delta["text"].(string); ok {
					preview.WriteString(text)
				}
			}
		}
		if eventType == "message_stop" {
			break
		}
	}

	res.Total = time.Since(start)
	// Truncate on runes: the catalog is full of providers answering in CJK,
	// where a byte offset lands in the middle of a character. The model's own
	// text is redacted too — a provider that echoes the request headers into the
	// completion would otherwise print the key as a benchmark "preview".
	res.Preview = truncateRunes(redactSecrets(strings.TrimSpace(preview.String()), secrets), 40)
	return res
}

func benchFmtDur(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}

// benchShortErr renders a provider failure for the human table. Truncation is
// not a redaction: where the credential lands in the body decides whether 40
// runes hide it, so the value is masked first and shortened after.
func benchShortErr(err error, secrets config.Secrets) string {
	return truncateRunes(redactSecrets(err.Error(), secrets), 40)
}

// benchModel resolves the model to request for a target: the explicit default
// model when set, otherwise the first configured tier (OpenRouter aliases only
// populate tiers, never Model).
func benchModel(target profiles.Target) string {
	if target.Model != "" {
		return target.Model
	}
	for _, tier := range []string{"sonnet", "opus", "haiku", "small"} {
		if model := target.ModelTiers[tier]; model != "" {
			return model
		}
	}
	return ""
}
