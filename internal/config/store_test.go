package config

import (
	"testing"

	"github.com/jolehuit/clother/internal/providers"
)

func TestNormalizeRepairsLegacyProviderOverridesAndOpenRouterAliases(t *testing.T) {
	t.Parallel()

	catalog, err := providers.Load()
	if err != nil {
		t.Fatal(err)
	}

	cfg := &File{
		Version: 1,
		ProviderOverrides: map[string]ProviderOverride{
			"zai": {Model: "1"},
		},
		OpenRouterAliases: map[string]string{
			"clother-or-kimi-k25": "clother-or-kimi-k25",
			"kimi-k25":            "moonshotai/kimi-k2.5",
		},
		CustomProviders: map[string]CustomProvider{},
	}

	cfg.Normalize(catalog)

	if _, ok := cfg.ProviderOverrides["zai"]; ok {
		t.Fatalf("expected default-model override to be removed, got %+v", cfg.ProviderOverrides)
	}
	if len(cfg.OpenRouterAliases) != 1 {
		t.Fatalf("expected one valid OpenRouter alias, got %+v", cfg.OpenRouterAliases)
	}
	if cfg.OpenRouterAliases["kimi-k25"] != "moonshotai/kimi-k2.5" {
		t.Fatalf("expected normalized alias to keep valid model, got %+v", cfg.OpenRouterAliases)
	}
}

func TestNormalizeKeepsBaseURLOnlyOverridesAndDropsCatalogDefaults(t *testing.T) {
	t.Parallel()

	catalog, err := providers.Load()
	if err != nil {
		t.Fatal(err)
	}

	cfg := &File{
		Version: 1,
		ProviderOverrides: map[string]ProviderOverride{
			"lmstudio": {BaseURL: "http://192.168.123.123:1234"},
			"ollama":   {BaseURL: "http://localhost:11434"}, // catalog default
			"zai":      {Model: "glm-4.7", BaseURL: "  "},
		},
		OpenRouterAliases: map[string]string{},
		CustomProviders:   map[string]CustomProvider{},
	}

	cfg.Normalize(catalog)

	if got := cfg.ProviderOverrides["lmstudio"].BaseURL; got != "http://192.168.123.123:1234" {
		t.Fatalf("lmstudio base URL override = %q, want kept", got)
	}
	if _, ok := cfg.ProviderOverrides["ollama"]; ok {
		t.Fatalf("expected catalog-default base URL override to be removed, got %+v", cfg.ProviderOverrides)
	}
	zai := cfg.ProviderOverrides["zai"]
	if zai.Model != "glm-4.7" || zai.BaseURL != "" {
		t.Fatalf("zai override = %+v, want model kept and blank base URL trimmed away", zai)
	}
}

func TestApplyLegacySecretsIgnoresLauncherShapedOpenRouterValues(t *testing.T) {
	t.Parallel()

	catalog, err := providers.Load()
	if err != nil {
		t.Fatal(err)
	}

	cfg := &File{
		Version:           1,
		ProviderOverrides: map[string]ProviderOverride{},
		OpenRouterAliases: map[string]string{},
		CustomProviders:   map[string]CustomProvider{},
	}

	cfg.ApplyLegacySecrets(Secrets{
		"OPENROUTER_MODEL_CLOTHER_OR_KIMI_K25": "clother-or-kimi-k25",
		"OPENROUTER_MODEL_KIMI_K25":            "moonshotai/kimi-k2.5",
	}, catalog)

	if len(cfg.OpenRouterAliases) != 1 {
		t.Fatalf("expected one migrated alias, got %+v", cfg.OpenRouterAliases)
	}
	if cfg.OpenRouterAliases["kimi-k25"] != "moonshotai/kimi-k2.5" {
		t.Fatalf("unexpected aliases: %+v", cfg.OpenRouterAliases)
	}
}
