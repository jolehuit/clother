package update

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Transport policy for the update channel.
//
// CLOTHER_UPDATE_URL and CLOTHER_RELEASE_BASE_URL are documented extension
// points (internal mirrors, local testing). They used to be taken verbatim, so
// http:// to an arbitrary host was accepted and redirects were followed
// anywhere, including https -> http. Both are now validated, and the update
// channel uses dedicated clients instead of http.DefaultClient so that a
// hostile or merely broken origin cannot hang the process forever.

const (
	maxRedirects = 5

	// maxAssetBytes bounds the release archive download.
	maxAssetBytes = 128 << 20
	// maxMetadataBytes bounds checksums.txt and its signature.
	maxMetadataBytes = 1 << 20
	// maxBinaryBytes bounds the binary extracted from the archive.
	maxBinaryBytes = 256 << 20
)

var errTooManyRedirects = errors.New("too many redirects")

func newUpdateTransport() *http.Transport {
	return &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		IdleConnTimeout:       30 * time.Second,
	}
}

// metadataClient fetches latest.json. Overridden in tests.
var metadataClient = &http.Client{
	Timeout:       30 * time.Second,
	Transport:     newUpdateTransport(),
	CheckRedirect: checkUpdateRedirect,
}

// downloadClient fetches release assets. Overridden in tests.
var downloadClient = &http.Client{
	Timeout:       10 * time.Minute,
	Transport:     newUpdateTransport(),
	CheckRedirect: checkUpdateRedirect,
}

// checkUpdateRedirect refuses a downgrade to cleartext and, when the request
// started on a GitHub host, refuses to be redirected off GitHub. No allowlist is
// enforced for an operator-provided mirror beyond the scheme: the operator chose
// that origin, and GitHub's own asset hosts change over time, so pinning them
// here would break the default channel instead of protecting it.
func checkUpdateRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= maxRedirects {
		return errTooManyRedirects
	}
	if err := checkSecureScheme(req.URL); err != nil {
		return fmt.Errorf("refusing redirect: %w", err)
	}
	if len(via) == 0 {
		return nil
	}
	origin := via[0].URL
	if isGitHubHost(origin.Hostname()) && !isGitHubHost(req.URL.Hostname()) {
		return fmt.Errorf("refusing redirect off the release origin: %s -> %s", origin.Hostname(), req.URL.Hostname())
	}
	return nil
}

// validateUpdateURL parses raw and refuses anything that is not https, with a
// single exception for loopback origins, which keeps the documented local mirror
// workflow (CLOTHER_RELEASE_BASE_URL=http://127.0.0.1:8000) usable without
// opening the door to a network attacker.
func validateUpdateURL(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", errors.New("empty update URL")
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("invalid update URL %q: %w", trimmed, err)
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("invalid update URL %q: missing host", trimmed)
	}
	if parsed.User != nil {
		return "", fmt.Errorf("refusing update URL %q: embedded credentials are not allowed", trimmed)
	}
	if err := checkSecureScheme(parsed); err != nil {
		return "", err
	}
	return parsed.String(), nil
}

func checkSecureScheme(u *url.URL) error {
	switch strings.ToLower(u.Scheme) {
	case "https":
		return nil
	case "http":
		if isLoopbackHost(u.Host) {
			return nil
		}
		return fmt.Errorf("refusing insecure update URL %q: https is required (http is allowed for loopback origins only)", u.Redacted())
	default:
		return fmt.Errorf("refusing update URL %q: scheme %q is not supported, https is required", u.Redacted(), u.Scheme)
	}
}

func isLoopbackHost(host string) bool {
	name := host
	if h, _, err := net.SplitHostPort(host); err == nil {
		name = h
	}
	name = strings.Trim(name, "[]")
	if strings.EqualFold(name, "localhost") {
		return true
	}
	ip := net.ParseIP(name)
	return ip != nil && ip.IsLoopback()
}

func isGitHubHost(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	for _, suffix := range []string{"github.com", "githubusercontent.com"} {
		if host == suffix || strings.HasSuffix(host, "."+suffix) {
			return true
		}
	}
	return false
}
