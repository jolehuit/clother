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
