package commands

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
)

// recordingServer counts the requests it receives and remembers the
// credentials they carried.
func recordingServer(t *testing.T) (*httptest.Server, func() []string) {
	t.Helper()
	var (
		mu       sync.Mutex
		received []string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		received = append(received, r.Header.Get("Authorization")+"|"+r.Header.Get("x-api-key"))
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)
	return server, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), received...)
	}
}

// redirectingServer answers every request with a 302 to target.
func redirectingServer(t *testing.T, target string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target+r.URL.Path, http.StatusFound)
	}))
	t.Cleanup(server.Close)
	return server
}

// Non-regression test of the credential leak: net/http only strips the headers
// it knows (Authorization, Cookie) on a cross-host redirect, so x-api-key
// followed a 302 to whatever host it named.
func TestTestDoesNotFollowRedirects(t *testing.T) {
	attacker, received := recordingServer(t)
	victim := redirectingServer(t, attacker.URL)

	c, _, _ := leakContext(t, victim.URL)
	if _, err := runTest(context.Background(), c, []string{"leaky"}); err != nil {
		t.Fatalf("runTest: %v", err)
	}
	if got := received(); len(got) != 0 {
		t.Fatalf("clother test followed the redirect and sent %q to another host", got)
	}
}

func TestBenchDoesNotFollowRedirects(t *testing.T) {
	attacker, received := recordingServer(t)
	victim := redirectingServer(t, attacker.URL)

	c, _, _ := leakContext(t, victim.URL)
	if _, err := runBench(context.Background(), c, []string{"leaky"}); err != nil {
		t.Fatalf("runBench: %v", err)
	}
	if got := received(); len(got) != 0 {
		t.Fatalf("clother bench followed the redirect and sent %q to another host", got)
	}
}

// An unparseable base URL used to hand a nil request to setCredentialHeader.
func TestTestReportsAnInvalidURL(t *testing.T) {
	c, stdout, _ := leakContext(t, "http://[::1")
	if _, err := runTest(context.Background(), c, []string{"leaky"}); err != nil {
		t.Fatalf("runTest: %v", err)
	}
	if !bytes.Contains(stdout.Bytes(), []byte("invalid URL")) {
		t.Fatalf("an invalid base URL was not reported:\n%s", stdout.String())
	}
}

func TestCredentialClientKeepsTheTimeout(t *testing.T) {
	if got := credentialClient(7 * time.Second).Timeout; got != 7*time.Second {
		t.Fatalf("credentialClient timeout = %v, want 7s", got)
	}
}

func TestTruncateRunesKeepsValidUTF8(t *testing.T) {
	const chinese = "模型不存在模型不存在模型不存在模型不存在模型不存在"
	got := truncateRunes(chinese, 7)
	if !utf8.ValidString(got) {
		t.Fatalf("truncateRunes produced invalid UTF-8: %q", got)
	}
	if want := string([]rune(chinese)[:7]) + "…"; got != want {
		t.Fatalf("truncateRunes = %q, want %q", got, want)
	}
	if got := truncateRunes("short", 40); got != "short" {
		t.Fatalf("truncateRunes changed a short string: %q", got)
	}
}
