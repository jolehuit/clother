package profiles

import (
	"fmt"
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
	// CredentialEnvVar is ANTHROPIC_AUTH_TOKEN or ANTHROPIC_API_KEY.
	CredentialEnvVar string
	// ExtraEnv is provider-wide tuning; ModelEnv holds per-model values such
	// as context window sizes, keyed by model ID.
	ExtraEnv map[string]string
	ModelEnv map[string]map[string]string
	TestURL  string
}

// removedProfiles explains launchers that used to exist, instead of the bare
// "unknown profile" a stale symlink would otherwise print.
var removedProfiles = map[string]string{
	"alibaba-us": "alibaba-us was removed: Alibaba has no US Coding Plan endpoint (coding-us.dashscope.aliyuncs.com does not resolve); use clother-alibaba",
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

func Resolve(profile string, catalog providers.Catalog, cfg *config.File) (Target, error) {
	if provider, ok := catalog.Get(profile); ok {
		model := provider.DefaultModel
		modelTiers := copyMap(provider.ModelTiers)
		baseURL := provider.BaseURL
		testURL := provider.TestURL
		override := cfg.ProviderOverrides[profile]
		if override.Model != "" {
			// A default picked with `clother config` replaces the whole
			// catalog mapping: every tier then asks for that model, and
			// subagents follow the session model as Claude Code does by
			// default.
			model = override.Model
			modelTiers = map[string]string{
				providers.TierOpus:   override.Model,
				providers.TierSonnet: override.Model,
				providers.TierHaiku:  override.Model,
				providers.TierFable:  override.Model,
			}
		}
		for tier, tierModel := range override.TierModels.Map() {
			modelTiers[tier] = tierModel
			if tier == providers.TierHaiku {
				delete(modelTiers, providers.TierSmall)
			}
		}
		if override.BaseURL != "" {
			baseURL = override.BaseURL
			testURL = override.BaseURL
		}
		return Target{
			Profile:          profile,
			DisplayName:      provider.DisplayName,
			Description:      provider.Description,
			Category:         provider.Category,
			Family:           provider.Family,
			BaseURL:          baseURL,
			Model:            model,
			ModelTiers:       compactModelTiers(modelTiers),
			AuthMode:         provider.AuthMode,
			SecretKey:        provider.KeyVar,
			LiteralAuthToken: provider.LiteralAuthToken,
			CredentialEnvVar: provider.CredentialEnvVar(),
			ExtraEnv:         copyMap(provider.ExtraEnv),
			ModelEnv:         provider.ModelEnv(),
			TestURL:          testURL,
		}, nil
	}
	if strings.HasPrefix(profile, "or-") {
		name := strings.TrimPrefix(profile, "or-")
		model := cfg.OpenRouterAliases[name]
		if model == "" {
			return Target{}, fmt.Errorf("unknown OpenRouter alias %q", name)
		}
		return Target{
			Profile:     profile,
			DisplayName: "OpenRouter: " + name,
			Description: "OpenRouter alias",
			Category:    "advanced",
			Family:      providers.FamilyOpenRouter,
			BaseURL:     "https://openrouter.ai/api",
			ModelTiers: map[string]string{
				providers.TierHaiku:  model,
				providers.TierSonnet: model,
				providers.TierOpus:   model,
				providers.TierFable:  model,
				providers.TierSmall:  model,
			},
			AuthMode:         providers.AuthSecret,
			SecretKey:        "OPENROUTER_API_KEY",
			CredentialEnvVar: providers.AuthTokenEnvVar,
			TestURL:          "https://openrouter.ai/api",
		}, nil
	}
	if custom, ok := cfg.CustomProviders[profile]; ok {
		return Target{
			Profile:          profile,
			DisplayName:      custom.DisplayName,
			Description:      "Custom provider",
			Category:         "advanced",
			Family:           providers.FamilyCustomUnknown,
			BaseURL:          custom.BaseURL,
			Model:            custom.DefaultModel,
			ModelTiers:       custom.TierModels.Map(),
			AuthMode:         providers.AuthSecret,
			SecretKey:        custom.APIKeyEnv,
			CredentialEnvVar: providers.AuthTokenEnvVar,
			TestURL:          custom.BaseURL,
		}, nil
	}
	if reason, ok := removedProfiles[profile]; ok {
		return Target{}, fmt.Errorf("%s", reason)
	}
	return Target{}, fmt.Errorf("unknown profile %q", profile)
}

// EffectiveTiers returns the model each tier resolves to once launched.
//
// Third-party endpoints know none of Claude Code's built-in model IDs, so for
// every family but claude_strict an unmapped tier is filled from the default
// model: leaving it empty would send claude-haiku-* or claude-fable-* to the
// provider and fail. Fable falls back to the opus mapping first, since it is
// the tier above it. The deprecated small-fast tier follows haiku because
// Claude Code prefers it over haiku for background work. The subagent tier is
// only set when configured: Claude Code otherwise resolves subagents through
// the tiers above.
func EffectiveTiers(target Target) map[string]string {
	tiers := compactModelTiers(target.ModelTiers)
	if target.Family == providers.FamilyClaudeStrict {
		return tiers
	}
	fallback := strings.TrimSpace(target.Model)
	if fallback == "" {
		for _, tier := range []string{providers.TierOpus, providers.TierSonnet, providers.TierHaiku, providers.TierFable} {
			if tiers[tier] != "" {
				fallback = tiers[tier]
				break
			}
		}
	}
	for _, tier := range []string{providers.TierOpus, providers.TierSonnet, providers.TierHaiku} {
		if tiers[tier] == "" {
			tiers[tier] = fallback
		}
	}
	if tiers[providers.TierFable] == "" {
		tiers[providers.TierFable] = tiers[providers.TierOpus]
	}
	if tiers[providers.TierSmall] == "" {
		tiers[providers.TierSmall] = tiers[providers.TierHaiku]
	}
	return compactModelTiers(tiers)
}

func All(catalog providers.Catalog, cfg *config.File) []Target {
	var out []Target
	for _, provider := range catalog.All() {
		target, _ := Resolve(provider.ID, catalog, cfg)
		out = append(out, target)
	}
	for _, name := range cfg.OpenRouterNames() {
		target, _ := Resolve("or-"+name, catalog, cfg)
		out = append(out, target)
	}
	for _, name := range cfg.CustomProviderNames() {
		target, _ := Resolve(name, catalog, cfg)
		out = append(out, target)
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
		if value = strings.TrimSpace(value); value != "" {
			out[key] = value
		}
	}
	return out
}
