package config

import (
	"testing"

	"github.com/jolehuit/clother/internal/providers"
)

func TestNormalizeLegacySecretsDropsInvalidLegacyEntries(t *testing.T) {
	t.Parallel()

	catalog, err := providers.Load()
	if err != nil {
		t.Fatal(err)
	}

	secrets := Secrets{
		"OPENROUTER_MODEL_CLOTHER_OR_KIMI_K25": "clother-or-kimi-k25",
		"OPENROUTER_MODEL_KIMI_K25":            "moonshotai/kimi-k2.5",
		"CLOTHER_ALIBABA_API_KEY_BASE_URL":     "https://example.com/unused",
		"ALIBABA_API_KEY":                      "secret",
	}

	NormalizeLegacySecrets(secrets, catalog)

	if _, ok := secrets["OPENROUTER_MODEL_CLOTHER_OR_KIMI_K25"]; ok {
		t.Fatalf("expected invalid OpenRouter launcher-shaped entry to be removed: %+v", secrets)
	}
	if _, ok := secrets["CLOTHER_ALIBABA_API_KEY_BASE_URL"]; ok {
		t.Fatalf("expected builtin provider legacy base URL to be removed: %+v", secrets)
	}
	if got := secrets["OPENROUTER_MODEL_KIMI_K25"]; got != "moonshotai/kimi-k2.5" {
		t.Fatalf("expected valid OpenRouter model to remain, got %+v", secrets)
	}
}
