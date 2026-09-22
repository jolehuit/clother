package providers

import (
	"regexp"
	"strings"
	"testing"
)

var envKeyPattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)

func TestCatalogInvariants(t *testing.T) {
	t.Parallel()

	catalog, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	seenIDs := map[string]bool{}
	keyOwners := map[string]string{}
	for _, provider := range catalog.All() {
		id := provider.ID
		if id == "" || seenIDs[id] {
			t.Fatalf("empty or duplicate provider id %q", id)
		}
		seenIDs[id] = true
		if provider.DisplayName == "" || provider.Description == "" || provider.Category == "" {
			t.Fatalf("%s: display name, description and category are required", id)
		}
		switch provider.AuthEnvVar {
		case "", AuthTokenEnvVar, APIKeyEnvVar:
		default:
			t.Fatalf("%s: unknown auth_env_var %q", id, provider.AuthEnvVar)
		}
		if strings.HasSuffix(strings.TrimRight(provider.BaseURL, "/"), "/v1") {
			t.Fatalf("%s: base URL must not end in /v1, Claude Code appends /v1/messages itself", id)
		}
		if provider.Family == FamilyClaudeStrict {
			continue
		}
		// Every third-party profile must send a credential of its own:
		// without one Claude Code falls back to the user's Anthropic login.
		if provider.AuthMode == AuthNone {
			t.Fatalf("%s: a third-party profile needs auth_mode secret or literal", id)
		}
		if provider.AuthMode == AuthSecret {
			if provider.KeyVar == "" {
				t.Fatalf("%s: auth_mode secret without key_var", id)
			}
			if owner, taken := keyOwners[provider.KeyVar]; taken && !sharedKey(owner, id) {
				t.Fatalf("%s and %s share %s", owner, id, provider.KeyVar)
			}
			keyOwners[provider.KeyVar] = id
		}
		choices := map[string]bool{}
		for _, choice := range provider.ModelChoices {
			if choice.ID == "" || choices[choice.ID] {
				t.Fatalf("%s: empty or duplicate model choice %q", id, choice.ID)
			}
			choices[choice.ID] = true
			for key := range choice.Env {
				if !envKeyPattern.MatchString(key) {
					t.Fatalf("%s: invalid env key %q on %s", id, key, choice.ID)
				}
			}
		}
		if provider.DefaultModel != "" && !choices[provider.DefaultModel] {
			t.Fatalf("%s: default model %q is not a model choice", id, provider.DefaultModel)
		}
		for tier, model := range provider.ModelTiers {
			switch tier {
			case TierOpus, TierSonnet, TierHaiku, TierFable, TierSmall, TierSubagent:
			default:
				t.Fatalf("%s: unknown tier %q", id, tier)
			}
			if !choices[model] {
				t.Fatalf("%s: tier %s maps to %q, which is not a model choice", id, tier, model)
			}
		}
		for key := range provider.ExtraEnv {
			if !envKeyPattern.MatchString(key) || strings.HasPrefix(key, "ANTHROPIC_") {
				t.Fatalf("%s: invalid extra_env key %q", id, key)
			}
		}
	}
	if seenIDs["alibaba-us"] {
		t.Fatal("alibaba-us has no endpoint: coding-us.dashscope.aliyuncs.com does not resolve")
	}
}

// The Alibaba Coding Plan predates per-region keys in Clother: both regions
// read ALIBABA_API_KEY so that existing configurations keep working.
func sharedKey(left, right string) bool {
	pair := map[string]bool{left: true, right: true}
	return pair["alibaba"] && pair["alibaba-cn"]
}
