package providers

import (
	"fmt"
	"net"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

var (
	providerIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
	envVarPattern     = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)
	isoDatePattern    = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
)

func knownFamilies() map[Family]struct{} {
	return map[Family]struct{}{
		FamilyClaudeStrict:                 {},
		FamilyAnthropicCompatibleNonClaude: {},
		FamilyLocal:                        {},
		FamilyOpenRouter:                   {},
		FamilyCustomUnknown:                {},
	}
}

func knownAuthModes() map[AuthMode]struct{} {
	return map[AuthMode]struct{}{
		AuthNone:    {},
		AuthSecret:  {},
		AuthLiteral: {},
	}
}

// Validate checks the semantic invariants of a catalogue. It returns every
// violation it finds rather than the first one, so a bad edit is fixed in one
// pass instead of one CI round-trip per mistake.
//
// It is deliberately not called from Load: Load runs at the start of every
// command, and a hard failure there turns a typo in a data file into a binary
// that no longer starts. Validate is enforced by the catalogue test on the
// embedded data, and is meant to be called on any catalogue layer that does not
// ship inside the binary before it is merged in.
func Validate(cat Catalog) []error {
	var errs []error
	seen := map[string]struct{}{}
	families := knownFamilies()
	authModes := knownAuthModes()
	tierKeys := map[string]struct{}{}
	for _, key := range ModelTierKeys {
		tierKeys[key] = struct{}{}
	}
	credentialVars := map[string]struct{}{}
	for _, name := range CredentialEnvVars {
		credentialVars[name] = struct{}{}
	}

	for _, provider := range cat.All() {
		id := provider.ID
		fail := func(format string, args ...any) {
			errs = append(errs, fmt.Errorf("provider %q: "+format, append([]any{id}, args...)...))
		}

		if id == "" {
			errs = append(errs, fmt.Errorf("provider with empty id"))
			continue
		}
		if !providerIDPattern.MatchString(id) {
			fail("id must match %s", providerIDPattern)
		}
		if _, dup := seen[id]; dup {
			fail("duplicate id")
		}
		seen[id] = struct{}{}

		if provider.DisplayName == "" {
			fail("display_name is empty")
		}
		if provider.Category == "" {
			fail("category is empty")
		}
		if _, ok := families[provider.Family]; !ok {
			fail("unknown family %q", provider.Family)
		}
		if _, ok := authModes[provider.AuthMode]; !ok {
			fail("unknown auth_mode %q", provider.AuthMode)
		}

		switch provider.AuthMode {
		case AuthSecret:
			if provider.KeyVar == "" {
				fail("auth_mode secret requires key_var")
			} else if !envVarPattern.MatchString(provider.KeyVar) {
				fail("key_var %q must match %s", provider.KeyVar, envVarPattern)
			}
			if provider.LiteralAuthToken != "" {
				fail("auth_mode secret must not carry literal_auth_token")
			}
		case AuthLiteral:
			if provider.LiteralAuthToken == "" {
				fail("auth_mode literal requires literal_auth_token")
			}
			if provider.KeyVar != "" {
				fail("auth_mode literal must not carry key_var")
			}
		case AuthNone:
			// Documented behaviour: ANTHROPIC_BASE_URL without a credential
			// variable leaves the saved claude.ai login as the active
			// credential, so the user's subscription token would be presented
			// to the third-party endpoint.
			if provider.BaseURL != "" {
				fail("auth_mode none is only allowed with an empty base_url")
			}
		}

		if provider.AuthEnvVar != "" {
			if _, ok := credentialVars[provider.AuthEnvVar]; !ok {
				fail("auth_env_var %q is not one of %v", provider.AuthEnvVar, CredentialEnvVars)
			}
			if provider.AuthMode == AuthNone {
				fail("auth_env_var is meaningless with auth_mode none")
			}
		}

		errs = append(errs, validateURL(id, "base_url", provider.BaseURL, provider.Family, provider.Family == FamilyClaudeStrict)...)
		errs = append(errs, validateURL(id, "test_url", provider.TestURL, provider.Family, false)...)

		choices := map[string]struct{}{}
		for _, choice := range provider.ModelChoices {
			if choice.ID == "" {
				fail("model_choices contains an entry with an empty id")
				continue
			}
			if _, dup := choices[choice.ID]; dup {
				fail("model_choices lists %q twice", choice.ID)
			}
			choices[choice.ID] = struct{}{}
		}
		if provider.DefaultModel != "" && len(choices) > 0 {
			if _, ok := choices[provider.DefaultModel]; !ok {
				fail("default_model %q is absent from model_choices", provider.DefaultModel)
			}
		}
		for _, tier := range sortedKeys(provider.ModelTiers) {
			value := provider.ModelTiers[tier]
			if _, ok := tierKeys[tier]; !ok {
				fail("model_tiers has unknown tier %q (known: %v)", tier, ModelTierKeys)
			}
			if value == "" {
				fail("model_tiers[%s] is empty", tier)
				continue
			}
			if len(choices) == 0 {
				continue
			}
			if _, ok := choices[value]; !ok {
				fail("model_tiers[%s] = %q is absent from model_choices", tier, value)
			}
		}

		for _, key := range sortedKeys(provider.ExtraEnv) {
			if !envVarPattern.MatchString(key) {
				fail("extra_env key %q must match %s", key, envVarPattern)
			}
			if strings.HasPrefix(key, "ANTHROPIC_DEFAULT_") || key == "ANTHROPIC_MODEL" ||
				key == "ANTHROPIC_BASE_URL" || key == "ANTHROPIC_AUTH_TOKEN" || key == "ANTHROPIC_API_KEY" {
				fail("extra_env must not redefine %s, which is derived from the provider fields", key)
			}
			if provider.ExtraEnv[key] == "" {
				fail("extra_env[%s] is empty", key)
			}
		}

		if provider.DocURL == "" {
			fail("doc_url is required: an entry nobody can source is an entry that 404s")
		} else {
			parsed, err := url.Parse(provider.DocURL)
			switch {
			case err != nil:
				fail("doc_url %q does not parse: %v", provider.DocURL, err)
			case parsed.Scheme != "https":
				fail("doc_url %q must be https", provider.DocURL)
			case parsed.Host == "":
				fail("doc_url %q has no host", provider.DocURL)
			}
		}
		if provider.VerifiedAt == "" {
			fail("verified_at is required")
		} else if !isoDatePattern.MatchString(provider.VerifiedAt) {
			fail("verified_at %q must be a YYYY-MM-DD date", provider.VerifiedAt)
		}
	}
	return errs
}

// validateURL enforces https everywhere, with one exception: a local backend
// may use http on a loopback host, because that is what Ollama, LM Studio,
// llama.cpp and vLLM actually serve.
func validateURL(id, field, raw string, family Family, mayBeEmpty bool) []error {
	if raw == "" {
		if mayBeEmpty {
			return nil
		}
		return []error{fmt.Errorf("provider %q: %s is empty", id, field)}
	}
	var errs []error
	fail := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf("provider %q: "+format, append([]any{id}, args...)...))
	}
	if strings.HasSuffix(raw, "/") {
		fail("%s %q must not end with a slash", field, raw)
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		fail("%s %q does not parse: %v", field, raw, err)
		return errs
	}
	if parsed.Host == "" {
		fail("%s %q has no host", field, raw)
		return errs
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		fail("%s %q must not carry a query or fragment", field, raw)
	}
	switch parsed.Scheme {
	case "https":
	case "http":
		if family != FamilyLocal {
			fail("%s %q must use https", field, raw)
		} else if !isLoopback(parsed.Hostname()) {
			fail("%s %q may only use http on a loopback host", field, raw)
		}
	default:
		fail("%s %q has scheme %q, want https", field, raw, parsed.Scheme)
	}
	return errs
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func sortedKeys(input map[string]string) []string {
	keys := make([]string, 0, len(input))
	for key := range input {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
