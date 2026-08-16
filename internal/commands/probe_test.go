package commands

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/jolehuit/clother/internal/config"
	"github.com/jolehuit/clother/internal/profiles"
	"github.com/jolehuit/clother/internal/providers"
	"github.com/jolehuit/clother/internal/ui"
)

const fakeKey = "not-a-real-credential-0123456789"

func secretTarget(profile, baseURL string) profiles.Target {
	return profiles.Target{
		Profile:     profile,
		DisplayName: profile,
		Family:      providers.FamilyAnthropicCompatibleNonClaude,
		BaseURL:     baseURL,
		Model:       "test-model",
		AuthMode:    providers.AuthSecret,
		SecretKey:   "TEST_API_KEY",
		TestURL:     baseURL,
	}
}

// TestCredentialClientRefusesCrossHostRedirect is the non-regression test of the
// credential leak: net/http only strips the headers it knows (Authorization,
// Cookie) on a cross-host redirect, so the previous client — which had no
// CheckRedirect — handed the API key to whatever host a 302 named.
func TestCredentialClientRefusesCrossHostRedirect(t *testing.T) {
	var (
		mu       sync.Mutex
		received []string
	)
	attacker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		received = append(received, r.Header.Get("Authorization")+"|"+r.Header.Get("x-api-key"))
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer attacker.Close()

	victim := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, attacker.URL+"/v1/messages", http.StatusFound)
	}))
	defer victim.Close()

	target := secretTarget("victim", victim.URL)
	secrets := config.Secrets{"TEST_API_KEY": fakeKey}

	result := probeTarget(context.Background(), Context{Secrets: secrets}, target, true)

	mu.Lock()
	defer mu.Unlock()
	if len(received) != 0 {
		t.Fatalf("the redirect was followed and the credential reached another host: %q", received)
	}
	if result.Outcome == testOK {
		t.Fatalf("a 302 must not be reported as ok: %+v", result)
	}
}

// TestProbeTargetDoesNotPanicOnUnparseableBaseURL covers the nil-pointer panic:
// the error of http.NewRequest was dropped and the nil request dereferenced, so
// one malformed custom provider killed the whole command.
func TestProbeTargetDoesNotPanicOnUnparseableBaseURL(t *testing.T) {
	for _, badURL := range []string{"http://exa mple.com", "http://example.com:port", "http://exa\x7fmple.com"} {
		target := secretTarget("evil", badURL)
		result := probeTarget(context.Background(), Context{Secrets: config.Secrets{"TEST_API_KEY": fakeKey}}, target, true)
		if result.Outcome != testFailed {
			t.Fatalf("probeTarget(%q) = %+v, want a failure", badURL, result)
		}
		if !strings.Contains(result.Detail, "invalid base URL") && !strings.Contains(result.Detail, "unreachable") {
			t.Fatalf("probeTarget(%q) detail = %q, want an explicit message", badURL, result.Detail)
		}
	}
}

// TestProbeTargetClassifiesStatusCodes covers the "reachable (HTTP 401)" lie:
// every HTTP answer used to be counted as a success.
func TestProbeTargetClassifiesStatusCodes(t *testing.T) {
	cases := []struct {
		status  int
		outcome testOutcome
	}{
		{http.StatusOK, testOK},
		{http.StatusTooManyRequests, testOK},
		{http.StatusUnauthorized, testFailed},
		{http.StatusForbidden, testFailed},
		{http.StatusNotFound, testFailed},
		{http.StatusBadRequest, testFailed},
		{http.StatusInternalServerError, testFailed},
	}
	for _, tc := range cases {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(tc.status)
		}))
		result := probeTarget(context.Background(), Context{Secrets: config.Secrets{"TEST_API_KEY": fakeKey}}, secretTarget("p", server.URL), true)
		server.Close()
		if result.Outcome != tc.outcome {
			t.Fatalf("HTTP %d -> %+v, want outcome %q", tc.status, result, tc.outcome)
		}
	}
}

// TestProbeTargetSendsTheRuntimeAuthentication checks that the diagnostic
// commands exercise the same channel as the launcher: runtime.BuildEnv exports
// ANTHROPIC_AUTH_TOKEN, which Claude Code sends as `Authorization: Bearer`,
// while the probes used to send x-api-key.
func TestProbeTargetSendsTheRuntimeAuthentication(t *testing.T) {
	var (
		mu     sync.Mutex
		auth   string
		apiKey string
		method string
		path   string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		auth, apiKey, method, path = r.Header.Get("Authorization"), r.Header.Get("x-api-key"), r.Method, r.URL.Path
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	probeTarget(context.Background(), Context{Secrets: config.Secrets{"TEST_API_KEY": fakeKey}}, secretTarget("p", server.URL), true)

	mu.Lock()
	defer mu.Unlock()
	if auth != "Bearer "+fakeKey {
		t.Fatalf("Authorization = %q, want the bearer token the runtime uses", auth)
	}
	if apiKey != "" {
		t.Fatalf("x-api-key = %q, want the runtime header only", apiKey)
	}
	if method != http.MethodPost || path != "/v1/messages" {
		t.Fatalf("request = %s %s, want POST /v1/messages", method, path)
	}
}

// TestProbeTargetSkipsUnconfiguredProviders keeps a provider without a key out
// of the failure count instead of reporting an authentication error.
func TestProbeTargetSkipsUnconfiguredProviders(t *testing.T) {
	result := probeTarget(context.Background(), Context{Secrets: config.Secrets{}}, secretTarget("p", "https://api.example.invalid"), true)
	if result.Outcome != testSkipped {
		t.Fatalf("probeTarget without a key = %+v, want it skipped", result)
	}
}

// TestRunTestExitsNonZeroWhenAProviderFails covers the healthcheck contract:
// the command always returned 0, even with failures on the board.
func TestRunTestExitsNonZeroWhenAProviderFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	c, stdout := newSandboxContext(t, "")
	c.Secrets["ZAI_API_KEY"] = fakeKey
	c.Config.ProviderOverrides["zai"] = config.ProviderOverride{BaseURL: server.URL}

	code, err := runTest(context.Background(), c, []string{"zai"})
	if err != nil {
		t.Fatalf("runTest() error = %v", err)
	}
	if code == 0 {
		t.Fatalf("runTest() = %d, want a non-zero exit code; output: %s", code, stdout.String())
	}
	if strings.Contains(stdout.String(), "reachable") {
		t.Fatalf("a 401 was reported as reachable: %s", stdout.String())
	}
}

// TestTruncateRunesKeepsValidUTF8 covers the byte-offset truncation: the
// catalog is full of providers answering in CJK, where cutting at byte 40
// leaves an orphan continuation byte.
func TestTruncateRunesKeepsValidUTF8(t *testing.T) {
	long := strings.Repeat("你好世界", 20)
	got := truncateRunes(long, 40)
	if !utf8.ValidString(got) {
		t.Fatalf("truncateRunes produced invalid UTF-8: %q", got)
	}
	if runes := []rune(got); len(runes) != 41 { // 40 + the ellipsis
		t.Fatalf("truncateRunes returned %d runes, want 41", len(runes))
	}
	if short := truncateRunes("abc", 40); short != "abc" {
		t.Fatalf("truncateRunes(short) = %q", short)
	}
}

// TestJSONCommandsUseTheSharedEnvelope keeps the machine-readable output of the
// read-only commands on the single envelope shape errors already use, instead
// of one ad-hoc object per command.
func TestJSONCommandsUseTheSharedEnvelope(t *testing.T) {
	for _, command := range []string{"list", "status", "info", "test", "bench"} {
		c, stdout := newSandboxContext(t, "")
		c.Options.Format = "json"
		c.Output.Format = ui.FormatJSON

		var err error
		switch command {
		case "list":
			_, err = runList(context.Background(), c)
		case "status":
			_, err = runStatus(context.Background(), c)
		case "info":
			_, err = runInfo(context.Background(), c, []string{"zai"})
		case "test":
			// No provider is configured in the sandbox, so nothing is probed.
			_, err = runTest(context.Background(), c, nil)
		case "bench":
			// Same here: bench must still print an envelope and nothing else.
			_, err = runBench(context.Background(), c, nil)
		}
		if err != nil {
			t.Fatalf("%s: %v", command, err)
		}

		var envelope struct {
			SchemaVersion int             `json:"schema_version"`
			Command       string          `json:"command"`
			OK            bool            `json:"ok"`
			Data          json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
			t.Fatalf("%s printed something that is not an envelope: %s", command, stdout.String())
		}
		if envelope.SchemaVersion != ui.SchemaVersion || envelope.Command != command || !envelope.OK {
			t.Fatalf("%s envelope = %+v", command, envelope)
		}
		if len(envelope.Data) == 0 {
			t.Fatalf("%s envelope carries no data: %s", command, stdout.String())
		}
	}
}

func TestCredentialClientKeepsTheTimeout(t *testing.T) {
	if client := credentialClient(3 * time.Second); client.Timeout != 3*time.Second {
		t.Fatalf("timeout = %v", client.Timeout)
	}
}
