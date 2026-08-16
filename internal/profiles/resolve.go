package profiles

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jolehuit/clother/internal/config"
	"github.com/jolehuit/clother/internal/providers"
)

type Target struct {
	Profile          string
	DisplayName      string
	Description      string
	Category         string
	Family           providers.Family
	BaseURL          string
	Model            string
	ModelTiers       map[string]string
	AuthMode         providers.AuthMode
	SecretKey        string
	LiteralAuthToken string
	TestURL          string
	// CredentialEnvVar is the Claude Code variable the credential must be
	// exported as, straight from the catalog's auth_env_var. Kimi Code and
	// SambaNova document ANTHROPIC_API_KEY and reject a bearer token; everybody
	// else documents ANTHROPIC_AUTH_TOKEN. Empty means the default.
	CredentialEnvVar string
	// ForeignSecretKeys are the credential variables of every OTHER provider
	// known to this installation. They are scrubbed from the child environment
	// so a session pinned to one provider cannot hand another provider's key to
	// the agent's Bash tool, hooks or MCP servers.
	ForeignSecretKeys []string
	// ExtraEnv carries provider-specific tuning declared by the catalog
	// (context window, timeouts, betas to disable). It is applied last by
	// runtime.BuildEnv, after every default Clother posts itself.
	ExtraEnv map[string]string
}

func Invocation(argv0 string) (string, bool) {
	base := filepath.Base(argv0)
	if base == "clother" || base == "clother.sh" {
		return "", false
	}
	if strings.HasPrefix(base, "clother-") {
		return strings.TrimPrefix(base, "clother-"), true
	}
	return "", false
}

// Validate rejects a target that cannot be launched safely.
//
// The only way Clother can actively harm a user is by pointing claude at a
// third-party endpoint without giving it a credential: claude then falls back
// to the user's own claude.ai login and sends that token to the third party.
// A non-empty, non-Anthropic base URL therefore requires a credential.
func Validate(target Target) error {
	if target.AuthMode == providers.AuthNone && target.BaseURL != "" && !isAnthropicEndpoint(target.BaseURL) {
		return fmt.Errorf(
			"profile %q is unsafe: it points at %s with no credential, so Claude Code would fall back to your claude.ai login and send it to that endpoint; give the provider a credential (auth_mode \"secret\" or \"literal\")",
			target.Profile, target.BaseURL)
	}
	return nil
}

// isAnthropicEndpoint reports whether a base URL points at Anthropic itself.
func isAnthropicEndpoint(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	return host == "anthropic.com" || strings.HasSuffix(host, ".anthropic.com")
}

// unknownProfileError explains the multi-call naming trap when the profile was
// deduced from argv[0]: any file named `clother-<x>` is treated as a provider
// launcher, so `go build -o clother-dev` or `cp clother clother-backup` turns
// the CLI into a launcher for a provider that does not exist.
func unknownProfileError(profile string) error {
	if base := filepath.Base(os.Args[0]); profile != "" && base == "clother-"+profile {
		return fmt.Errorf("unknown profile %q (this binary was invoked as %s, and Clother treats any `clother-<name>` file as a provider launcher; rename it to `clother` to use the CLI)", profile, base)
	}
	return fmt.Errorf("unknown profile %q — run `clother list` to see the available profiles", profile)
}

func Resolve(profile string, catalog providers.Catalog, cfg *config.File) (Target, error) {
	if provider, ok := catalog.Get(profile); ok {
		model := provider.DefaultModel
		modelTiers := copyMap(provider.ModelTiers)
		baseURL := provider.BaseURL
		testURL := provider.TestURL
		override := cfg.ProviderOverrides[profile]
		if override.Model != "" {
			model = override.Model
			modelTiers = map[string]string{
				"haiku":  override.Model,
				"sonnet": override.Model,
				"opus":   override.Model,
				"fable":  override.Model,
				"small":  override.Model,
			}
		}
		if override.BaseURL != "" {
			baseURL = override.BaseURL
			testURL = override.BaseURL
		}
		return validated(Target{
			Profile:           profile,
			DisplayName:       provider.DisplayName,
			Description:       provider.Description,
			Category:          provider.Category,
			Family:            provider.Family,
			BaseURL:           baseURL,
			Model:             model,
			ModelTiers:        compactModelTiers(modelTiers),
			AuthMode:          provider.AuthMode,
			SecretKey:         provider.KeyVar,
			LiteralAuthToken:  provider.LiteralAuthToken,
			CredentialEnvVar:  provider.CredentialEnvVar(),
			TestURL:           testURL,
			ForeignSecretKeys: foreignSecretKeys(provider.KeyVar, catalog, cfg),
			// Per-provider compatibility knobs declared by the catalog
			// (context window, timeouts, betas to disable).
			ExtraEnv: copyMap(provider.ExtraEnv),
		})
	}
	if strings.HasPrefix(profile, "or-") {
		name := strings.TrimPrefix(profile, "or-")
		model := cfg.OpenRouterAliases[name]
		if model == "" {
			// The custom-provider gateway names its next step; this one used to
			// stop at the diagnosis.
			return Target{}, fmt.Errorf("unknown OpenRouter alias %q — run `clother config openrouter` to add one", name)
		}
		return validated(Target{
			Profile:     profile,
			DisplayName: "OpenRouter: " + name,
			Description: "OpenRouter alias",
			Category:    "advanced",
			Family:      providers.FamilyOpenRouter,
			BaseURL:     "https://openrouter.ai/api",
			Model:       model,
			ModelTiers: map[string]string{
				"haiku":  model,
				"sonnet": model,
				"opus":   model,
				"fable":  model,
				"small":  model,
			},
			AuthMode:          providers.AuthSecret,
			SecretKey:         "OPENROUTER_API_KEY",
			CredentialEnvVar:  providers.DefaultCredentialEnvVar,
			TestURL:           "https://openrouter.ai/api",
			ForeignSecretKeys: foreignSecretKeys("OPENROUTER_API_KEY", catalog, cfg),
		})
	}
	if custom, ok := cfg.CustomProviders[profile]; ok {
		return validated(Target{
			Profile:           profile,
			DisplayName:       custom.DisplayName,
			Description:       "Custom provider",
			Category:          "advanced",
			Family:            providers.FamilyCustomUnknown,
			BaseURL:           custom.BaseURL,
			Model:             custom.DefaultModel,
			ModelTiers:        map[string]string{},
			AuthMode:          providers.AuthSecret,
			SecretKey:         custom.APIKeyEnv,
			CredentialEnvVar:  providers.DefaultCredentialEnvVar,
			TestURL:           custom.BaseURL,
			ForeignSecretKeys: foreignSecretKeys(custom.APIKeyEnv, catalog, cfg),
		})
	}
	return Target{}, unknownProfileError(profile)
}

func validated(target Target) (Target, error) {
	if err := Validate(target); err != nil {
		return Target{}, err
	}
	return target, nil
}

// foreignSecretKeys lists every credential variable of this installation except
// the one the target itself needs.
func foreignSecretKeys(own string, catalog providers.Catalog, cfg *config.File) []string {
	seen := map[string]struct{}{}
	add := func(key string) {
		if key == "" || key == own {
			return
		}
		seen[key] = struct{}{}
	}
	for key := range catalog.BuiltinSecretKeys() {
		add(key)
	}
	add("OPENROUTER_API_KEY")
	if cfg != nil {
		for _, custom := range cfg.CustomProviders {
			add(custom.APIKeyEnv)
		}
	}
	out := make([]string, 0, len(seen))
	for key := range seen {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func All(catalog providers.Catalog, cfg *config.File) []Target {
	var out []Target
	// A profile that fails to resolve cannot be launched (and, for the
	// AuthNone rule, must not be), so it is left out instead of being listed
	// as a blank row.
	appendResolved := func(profile string) {
		target, err := Resolve(profile, catalog, cfg)
		if err != nil {
			return
		}
		out = append(out, target)
	}
	for _, provider := range catalog.All() {
		appendResolved(provider.ID)
	}
	for _, name := range cfg.OpenRouterNames() {
		appendResolved("or-" + name)
	}
	for _, name := range cfg.CustomProviderNames() {
		appendResolved(name)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Profile < out[j].Profile
	})
	return out
}

func copyMap(input map[string]string) map[string]string {
	out := make(map[string]string, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func compactModelTiers(input map[string]string) map[string]string {
	out := map[string]string{}
	for key, value := range input {
		if value != "" {
			out[key] = value
		}
	}
	return out
}
