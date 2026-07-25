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
		fmt.Fprintln(c.Output.Stdout, "No providers available for benchmarking.")
		fmt.Fprintln(c.Output.Stdout, "Configure a provider first: clother config <provider>")
		return 0, nil
	}

	fmt.Fprintf(c.Output.Stdout, "Benchmarking %d provider(s) — prompt: %q\n\n", len(selected), prompt)

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

	fmt.Fprintf(c.Output.Stdout, "  %-18s %-22s %8s %8s   %s\n", "Provider", "Model", "TTFT", "Total", "Preview")
	fmt.Fprintf(c.Output.Stdout, "  %s\n", strings.Repeat("─", 78))
	for _, r := range results {
		if r.Err != nil {
			fmt.Fprintf(c.Output.Stdout, "  %-18s %-22s %8s %8s   ✗ %s\n",
				r.Profile, r.Model, "-", "-", benchShortErr(r.Err))
		} else {
			fmt.Fprintf(c.Output.Stdout, "  %-18s %-22s %8s %8s   %q\n",
				r.Profile, r.Model,
				benchFmtDur(r.TTFT), benchFmtDur(r.Total),
				r.Preview,
			)
		}
	}
	fmt.Fprintln(c.Output.Stdout)
	return 0, nil
}

func doBench(ctx context.Context, target profiles.Target, secrets config.Secrets, prompt string) benchResult {
	res := benchResult{Profile: target.Profile, Model: target.Model}

	var apiKey string
	switch target.AuthMode {
	case providers.AuthSecret:
		apiKey = secrets[target.SecretKey]
	case providers.AuthLiteral:
		apiKey = target.LiteralAuthToken
	}

	endpoint := strings.TrimRight(target.BaseURL, "/") + "/v1/messages"

	body, err := json.Marshal(map[string]interface{}{
		"model":      target.Model,
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
	if target.Family == providers.FamilyOpenRouter {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	} else {
		req.Header.Set("x-api-key", apiKey)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		res.Err = err
		return res
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		res.Err = fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
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
	p := strings.TrimSpace(preview.String())
	if len(p) > 40 {
		p = p[:40] + "…"
	}
	res.Preview = p
	return res
}

func benchFmtDur(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}

func benchShortErr(err error) string {
	s := err.Error()
	if len(s) > 40 {
		return s[:40] + "…"
	}
	return s
}
