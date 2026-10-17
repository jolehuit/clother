package commands

import (
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/jolehuit/clother/internal/config"
)

// credentialClient builds an HTTP client for requests that carry a credential.
//
// Redirects are never followed: Go only strips Authorization/Cookie on a
// cross-host redirect for the headers it knows about, so x-api-key went along,
// and following a 302 from a provider endpoint handed the API key to whatever
// host the redirect named.
func credentialClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// minRedactableSecret is the shortest value redactSecrets will look for. Below
// it a "secret" is more likely to be a placeholder than a credential, and
// blanking three-letter strings out of a response body would mangle it.
const minRedactableSecret = 8

// redactSecrets replaces every configured credential found in text by its
// masked form.
//
// `clother bench` puts the provider's error body in its output. Gateways answer
// "invalid token: <token>" and echo/debug endpoints reflect the Authorization
// header, so the API key landed on stdout, and from there into CI logs and bug
// reports. A hostile custom endpoint can reflect the header on purpose to force
// exactly that.
//
// Prefixes are covered too: the body is read through a 256-byte limit, so a key
// sitting near the end arrives cut in half and a whole-value match would miss
// it.
func redactSecrets(text string, secrets config.Secrets) string {
	if text == "" || len(secrets) == 0 {
		return text
	}
	values := make([]string, 0, len(secrets))
	for _, value := range secrets {
		if len(strings.TrimSpace(value)) < minRedactableSecret {
			continue
		}
		values = append(values, value)
	}
	// Longest first: a short credential that is a prefix of a longer one must
	// not consume the match before the longer one is tried.
	sort.SliceStable(values, func(i, j int) bool { return len(values[i]) > len(values[j]) })

	for _, value := range values {
		masked := config.MaskSecret(value)
		for size := len(value); size >= minRedactableSecret; size-- {
			fragment := value[:size]
			if !strings.Contains(text, fragment) {
				continue
			}
			text = strings.ReplaceAll(text, fragment, masked)
			break
		}
	}
	return text
}

// truncateRunes shortens a string to at most max runes. Cutting on a byte
// offset splits multi-byte characters, which is the common case for the
// Chinese providers of the catalog.
func truncateRunes(value string, max int) string {
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	return string(runes[:max]) + "…"
}
