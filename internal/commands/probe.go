package commands

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/jolehuit/clother/internal/config"
	"github.com/jolehuit/clother/internal/profiles"
	"github.com/jolehuit/clother/internal/providers"
)

// authHeader returns the authentication header a request must carry to
// reproduce what Claude Code actually sends.
//
// runtime.BuildEnv only ever exports ANTHROPIC_AUTH_TOKEN (never
// ANTHROPIC_API_KEY), which Claude Code turns into `Authorization: Bearer`.
// The diagnostic commands used to send `x-api-key` instead, i.e. they probed a
// different authentication channel than the product.
func authHeader(target profiles.Target, secrets config.Secrets) (name, value string, configured bool) {
	switch target.AuthMode {
	case providers.AuthNone:
		return "", "", true
	case providers.AuthLiteral:
		if target.LiteralAuthToken == "" {
			return "", "", true
		}
		return "Authorization", "Bearer " + target.LiteralAuthToken, true
	case providers.AuthSecret:
		key := secrets[target.SecretKey]
		if strings.TrimSpace(key) == "" {
			return "", "", false
		}
		return "Authorization", "Bearer " + key, true
	default:
		return "", "", false
	}
}

// credentialClient builds an HTTP client for requests that carry a credential.
//
// Redirects are never followed: Go only strips Authorization/Cookie on a
// cross-host redirect for the headers it knows about, and following a 302 from
// a provider endpoint would hand the API key to whatever host the redirect
// names.
func credentialClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// messagesEndpoint is the endpoint both `test` and `bench` exercise.
func messagesEndpoint(baseURL string) string {
	return strings.TrimRight(baseURL, "/") + "/v1/messages"
}

// resolveTargetArg resolves a profile name given on the command line, with a
// helpful message for `openrouter`, which is accepted by `clother config` but
// is not a profile of its own.
func resolveTargetArg(c Context, name string) (profiles.Target, error) {
	if name == "openrouter" {
		aliases := c.Config.OpenRouterNames()
		if len(aliases) == 0 {
			return profiles.Target{}, fmt.Errorf("openrouter is used through aliases: run `clother config openrouter` to add one, then use `or-<alias>`")
		}
		return profiles.Target{}, fmt.Errorf("openrouter is used through aliases: try %s", strings.Join(prefixed("or-", aliases), ", "))
	}
	return profiles.Resolve(name, c.Catalog, c.Config)
}

func prefixed(prefix string, values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, prefix+value)
	}
	return out
}

// minRedactableSecret is the shortest value redactSecrets will look for. Below
// it a "secret" is more likely to be a placeholder than a credential, and
// blanking three-letter strings out of a response body would mangle it.
const minRedactableSecret = 8

// redactSecrets replaces every configured credential found in text by its
// masked form.
//
// `clother test` and `clother bench` put the provider's response body in their
// output. Gateways answer "invalid token: <token>" and echo/debug endpoints
// reflect the Authorization header, so the API key landed verbatim on stdout —
// with no truncation at all in --json — and from there into CI logs and into
// any `clother bench --json` pasted in a bug report. A hostile custom endpoint
// can reflect the header on purpose to force exactly that.
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
