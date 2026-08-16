package commands

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jolehuit/clother/internal/config"
	"github.com/jolehuit/clother/internal/providers"
	"github.com/jolehuit/clother/internal/ui"
)

// canaryKey stands in for a live credential. It is long enough to pass
// MaskSecret's 16 character threshold and distinctive enough that a substring
// search cannot match by accident. It is not, and must never be, a real key.
const canaryKey = "sk-CLOTHER-TEST-CANARY-0123456789"

// reflectingProvider answers 400 with a body that leads with the Authorization
// header it received. Real gateways do exactly this ("invalid token: <token>",
// debug endpoints, misconfigured proxies), and a hostile custom provider can do
// it deliberately so the key ends up in the user's bug report.
func reflectingProvider(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid token ` + r.Header.Get("Authorization") + `"}`))
	}))
	t.Cleanup(server.Close)
	return server
}

func leakContext(t *testing.T, baseURL, format string) (Context, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	catalog, err := providers.Load()
	if err != nil {
		t.Fatal(err)
	}
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	return Context{
		Config: &config.File{
			Version:           1,
			ProviderOverrides: map[string]config.ProviderOverride{},
			OpenRouterAliases: map[string]string{},
			CustomProviders: map[string]config.CustomProvider{
				"leaky": {
					Name: "leaky", DisplayName: "leaky",
					BaseURL: baseURL, APIKeyEnv: "LEAKY_API_KEY", DefaultModel: "some-model",
				},
			},
		},
		Catalog: catalog,
		Secrets: config.Secrets{"LEAKY_API_KEY": canaryKey},
		Output:  ui.NewOutput(stdout, stderr, ui.Format(format), false),
	}, stdout, stderr
}

// assertNoCredential fails when the output carries the key or any usable
// fragment of it. A prefix is enough to leak: the response body is read through
// a 256 byte limit, so a key can arrive cut in half.
func assertNoCredential(t *testing.T, what, output string) {
	t.Helper()
	if strings.Contains(output, canaryKey) {
		t.Fatalf("%s printed the API key verbatim:\n%s", what, output)
	}
	for _, size := range []int{12, 16, 24} {
		if size <= len(canaryKey) && strings.Contains(output, canaryKey[:size]) {
			t.Fatalf("%s printed a %d character prefix of the API key:\n%s", what, size, output)
		}
	}
}

// Regression: `clother bench --json` copied the provider's error body into the
// machine envelope verbatim and unbounded, so an endpoint reflecting the
// Authorization header put the API key on stdout.
func TestBenchNeverPrintsTheCredential(t *testing.T) {
	server := reflectingProvider(t)

	for _, format := range []string{"json", "human"} {
		t.Run(format, func(t *testing.T) {
			c, stdout, stderr := leakContext(t, server.URL, format)
			if _, err := runBench(context.Background(), c, []string{"leaky"}); err != nil {
				t.Logf("runBench: %v", err)
			}
			combined := stdout.String() + stderr.String()
			if !strings.Contains(combined, "HTTP 400") {
				t.Fatalf("the provider failure was not reported at all:\n%s", combined)
			}
			assertNoCredential(t, "clother bench --"+format, combined)
		})
	}
}

// Same for `clother test`, whose 60 rune truncation let a body leading with the
// credential through.
func TestTestNeverPrintsTheCredential(t *testing.T) {
	server := reflectingProvider(t)

	for _, format := range []string{"json", "human"} {
		t.Run(format, func(t *testing.T) {
			c, stdout, stderr := leakContext(t, server.URL, format)
			if _, err := runTest(context.Background(), c, []string{"leaky"}); err != nil {
				t.Logf("runTest: %v", err)
			}
			combined := stdout.String() + stderr.String()
			if !strings.Contains(combined, "400") {
				t.Fatalf("the provider failure was not reported at all:\n%s", combined)
			}
			assertNoCredential(t, "clother test --"+format, combined)
		})
	}
}

// The machine envelope had no bound at all on the error string.
func TestBenchJSONErrorIsBounded(t *testing.T) {
	flood := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write(bytes.Repeat([]byte("A"), 4096))
	}))
	t.Cleanup(flood.Close)

	c, stdout, _ := leakContext(t, flood.URL, "json")
	if _, err := runBench(context.Background(), c, []string{"leaky"}); err != nil {
		t.Logf("runBench: %v", err)
	}
	if runes := len([]rune(stdout.String())); runes > 1000 {
		t.Fatalf("the JSON envelope grew to %d runes on a hostile body", runes)
	}
}

func TestRedactSecrets(t *testing.T) {
	secrets := config.Secrets{
		"LEAKY_API_KEY": canaryKey,
		"EMPTY_API_KEY": "",
		"SHORT_KEY":     "abc",
	}

	t.Run("whole value", func(t *testing.T) {
		got := redactSecrets("invalid token Bearer "+canaryKey+" rejected", secrets)
		if strings.Contains(got, canaryKey) {
			t.Fatalf("the key survived redaction: %q", got)
		}
		if !strings.Contains(got, config.MaskSecret(canaryKey)) {
			t.Fatalf("the masked form is missing: %q", got)
		}
	})

	t.Run("truncated prefix", func(t *testing.T) {
		// The body limit cuts the key mid-way; a whole-value match would miss it.
		got := redactSecrets("invalid token Bearer "+canaryKey[:20], secrets)
		if strings.Contains(got, canaryKey[:12]) {
			t.Fatalf("a truncated key survived redaction: %q", got)
		}
	})

	t.Run("leaves ordinary text alone", func(t *testing.T) {
		const body = `{"error":"model abc not found"}`
		if got := redactSecrets(body, secrets); got != body {
			t.Fatalf("an unrelated body was mangled: %q", got)
		}
	})
}
