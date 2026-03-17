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
	TestURL          string
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
		if override := cfg.ProviderOverrides[profile]; override.Model != "" {
			model = override.Model
			modelTiers = map[string]string{
				"haiku":  override.Model,
				"sonnet": override.Model,
				"opus":   override.Model,
				"small":  override.Model,
			}
		}
		return Target{
			Profile:          profile,
			DisplayName:      provider.DisplayName,
			Description:      provider.Description,
			Category:         provider.Category,
			Family:           provider.Family,
			BaseURL:          provider.BaseURL,
			Model:            model,
			ModelTiers:       compactModelTiers(modelTiers),
			AuthMode:         provider.AuthMode,
			SecretKey:        provider.KeyVar,
			LiteralAuthToken: provider.LiteralAuthToken,
			TestURL:          provider.TestURL,
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
				"haiku":  model,
				"sonnet": model,
				"opus":   model,
				"small":  model,
			},
			AuthMode:  providers.AuthSecret,
			SecretKey: "OPENROUTER_API_KEY",
			TestURL:   "https://openrouter.ai/api",
		}, nil
	}
	if custom, ok := cfg.CustomProviders[profile]; ok {
		return Target{
			Profile:     profile,
			DisplayName: custom.DisplayName,
			Description: "Custom provider",
			Category:    "advanced",
			Family:      providers.FamilyCustomUnknown,
			BaseURL:     custom.BaseURL,
			Model:       custom.DefaultModel,
			ModelTiers:  map[string]string{},
			AuthMode:    providers.AuthSecret,
			SecretKey:   custom.APIKeyEnv,
			TestURL:     custom.BaseURL,
		}, nil
	}
	return Target{}, fmt.Errorf("unknown profile %q", profile)
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
		if value != "" {
			out[key] = value
		}
	}
	return out
}
