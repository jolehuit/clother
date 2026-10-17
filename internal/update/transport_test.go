package update

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jolehuit/clother/internal/config"
)

func TestValidateUpdateURLRefusesInsecureSchemes(t *testing.T) {
	t.Parallel()

	refused := []string{
		"http://attacker.invalid/clother",
		"http://192.168.1.10:8000",
		"ftp://mirror.invalid/clother",
		"file:///tmp/evil",
		"javascript:alert(1)",
		"https://user:pass@github.com/x",
		"https://",
		"   ",
	}
	for _, raw := range refused {
		if got, err := validateUpdateURL(raw); err == nil {
			t.Fatalf("validateUpdateURL(%q) = %q, want an error", raw, got)
		}
	}

	accepted := []string{
		"https://github.com/jolehuit/clother/releases/download",
		"https://mirror.example.com/clother/",
		"http://127.0.0.1:8000",
		"http://localhost:8000/clother",
		"http://[::1]:8000",
	}
	for _, raw := range accepted {
		if _, err := validateUpdateURL(raw); err != nil {
			t.Fatalf("validateUpdateURL(%q) returned %v", raw, err)
		}
	}
}

func TestReleaseAssetURLRefusesInsecureOverride(t *testing.T) {
	t.Setenv("CLOTHER_RELEASE_BASE_URL", "http://attacker.invalid/clother")
	if got, err := releaseAssetURL("v1.0.0", "clother_linux_amd64.tar.gz"); err == nil {
		t.Fatalf("releaseAssetURL = %q, want an error on a cleartext override", got)
	}
}

func TestReleaseAssetURLDefaultsToTaggedHTTPSURL(t *testing.T) {
	t.Setenv("CLOTHER_RELEASE_BASE_URL", "")
	got, err := releaseAssetURL("v1.2.3", "checksums.txt")
	if err != nil {
		t.Fatal(err)
	}
	want := defaultReleaseBaseURL + "/v1.2.3/checksums.txt"
	if got != want {
		t.Fatalf("releaseAssetURL = %q, want %q", got, want)
	}
}

func TestMetadataURLRefusesInsecureOverride(t *testing.T) {
	t.Setenv("CLOTHER_UPDATE_URL", "http://attacker.invalid/latest.json")
	if got, err := metadataURL(); err == nil {
		t.Fatalf("metadataURL = %q, want an error on a cleartext override", got)
	}
}

func TestMetadataURLDefault(t *testing.T) {
	t.Setenv("CLOTHER_UPDATE_URL", "")
	got, err := metadataURL()
	if err != nil {
		t.Fatal(err)
	}
	if got != defaultMetadataURL {
		t.Fatalf("metadataURL = %q, want %q", got, defaultMetadataURL)
	}
}

// A cleartext CLOTHER_UPDATE_URL must stop the background check before any
// network call, not be followed silently.
func TestMaybeMessageRefusesInsecureMetadataURL(t *testing.T) {
	t.Setenv("CLOTHER_UPDATE_URL", "http://attacker.invalid/latest.json")

	root := t.TempDir()
	paths := config.Paths{CacheDir: root, UpdateCacheFile: filepath.Join(root, "update.json")}
	_, err := MaybeMessage(paths, "3.0.0", time.Unix(1_700_000_000, 0))
	if err == nil {
		t.Fatal("expected an error for a cleartext CLOTHER_UPDATE_URL")
	}
	if !strings.Contains(err.Error(), "CLOTHER_UPDATE_URL") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func mustParse(t *testing.T, raw string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func TestCheckUpdateRedirectPolicy(t *testing.T) {
	t.Parallel()

	newReq := func(target string) *http.Request {
		return &http.Request{URL: mustParse(t, target)}
	}

	cases := []struct {
		name    string
		target  string
		via     []*http.Request
		wantErr string
	}{
		{
			name:    "https to http is refused",
			target:  "http://cdn.example.com/asset",
			via:     []*http.Request{newReq("https://github.com/a")},
			wantErr: "https is required",
		},
		{
			name:    "off github is refused",
			target:  "https://cdn.attacker.invalid/asset",
			via:     []*http.Request{newReq("https://github.com/a")},
			wantErr: "refusing redirect off the release origin",
		},
		{
			name:   "github to githubusercontent is allowed",
			target: "https://release-assets.githubusercontent.com/asset",
			via:    []*http.Request{newReq("https://github.com/a")},
		},
		{
			name:   "operator mirror may redirect within https",
			target: "https://cdn.mirror.example.com/asset",
			via:    []*http.Request{newReq("https://mirror.example.com/a")},
		},
		{
			name:    "operator mirror may not downgrade",
			target:  "http://cdn.mirror.example.com/asset",
			via:     []*http.Request{newReq("https://mirror.example.com/a")},
			wantErr: "https is required",
		},
		{
			name:    "redirect chains are bounded",
			target:  "https://github.com/z",
			via:     make([]*http.Request, maxRedirects),
			wantErr: "too many redirects",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := checkUpdateRedirect(newReq(tc.target), tc.via)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("got %v, want an error containing %q", err, tc.wantErr)
			}
		})
	}
}

// End to end: an https origin that tries to bounce the download to cleartext
// must not be followed.
func TestDownloadFileRefusesRedirectToCleartext(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://downgrade.invalid/asset", http.StatusFound)
	}))
	defer server.Close()

	client := server.Client()
	client.CheckRedirect = checkUpdateRedirect
	previous := downloadClient
	downloadClient = client
	t.Cleanup(func() { downloadClient = previous })

	err := downloadFile(context.Background(), server.URL+"/asset", filepath.Join(t.TempDir(), "asset"), maxAssetBytes)
	if err == nil {
		t.Fatal("expected the redirect to cleartext to be refused")
	}
	if !strings.Contains(err.Error(), "https is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestIsGitHubHost(t *testing.T) {
	t.Parallel()

	for _, host := range []string{"github.com", "objects.githubusercontent.com", "release-assets.githubusercontent.com", "GitHub.com"} {
		if !isGitHubHost(host) {
			t.Fatalf("%q should be recognised as a GitHub host", host)
		}
	}
	for _, host := range []string{"github.com.attacker.invalid", "notgithub.com", "githubusercontent.com.evil.tld", ""} {
		if isGitHubHost(host) {
			t.Fatalf("%q should not be recognised as a GitHub host", host)
		}
	}
}
