package profiles

import (
	"strings"
	"testing"

	"github.com/jolehuit/clother/internal/config"
	"github.com/jolehuit/clother/internal/providers"
)

func TestResolveAppliesBaseURLOverride(t *testing.T) {
	t.Parallel()

	catalog, err := providers.Load()
	if err != nil {
		t.Fatal(err)
	}

	cfg := &config.File{
		Version: 1,
		ProviderOverrides: map[string]config.ProviderOverride{
			"lmstudio": {BaseURL: "http://192.168.123.123:1234"},
		},
		OpenRouterAliases: map[string]string{},
		CustomProviders:   map[string]config.CustomProvider{},
	}

	target, err := Resolve("lmstudio", catalog, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if target.BaseURL != "http://192.168.123.123:1234" {
		t.Fatalf("BaseURL = %q, want the override", target.BaseURL)
	}
	if target.TestURL != "http://192.168.123.123:1234" {
		t.Fatalf("TestURL = %q, want the override", target.TestURL)
	}

	// Without an override the catalog URL is untouched.
	plain, err := Resolve("ollama", catalog, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if plain.BaseURL != "http://localhost:11434" {
		t.Fatalf("ollama BaseURL = %q, want catalog default", plain.BaseURL)
	}
}

func TestResolveCombinesModelAndBaseURLOverrides(t *testing.T) {
	t.Parallel()

	catalog, err := providers.Load()
	if err != nil {
		t.Fatal(err)
	}

	cfg := &config.File{
		Version: 1,
		ProviderOverrides: map[string]config.ProviderOverride{
			"zai": {Model: "glm-4.7", BaseURL: "https://proxy.example.com/anthropic"},
		},
		OpenRouterAliases: map[string]string{},
		CustomProviders:   map[string]config.CustomProvider{},
	}

	target, err := Resolve("zai", catalog, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if target.Model != "glm-4.7" {
		t.Fatalf("Model = %q, want glm-4.7", target.Model)
	}
	if target.BaseURL != "https://proxy.example.com/anthropic" {
		t.Fatalf("BaseURL = %q, want the override", target.BaseURL)
	}
}

func newTestConfig() *config.File {
	return &config.File{
		Version:           1,
		ProviderOverrides: map[string]config.ProviderOverride{},
		OpenRouterAliases: map[string]string{},
		CustomProviders:   map[string]config.CustomProvider{},
	}
}

// Issue #38: tier mappings for local and custom providers.
func TestResolveTierMappingsForLocalAndCustomProviders(t *testing.T) {
	t.Parallel()

	catalog, err := providers.Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg := newTestConfig()
	cfg.ProviderOverrides["lmstudio"] = config.ProviderOverride{
		Model:      "qwen3.8-27b-mtp",
		TierModels: config.TierModels{SonnetModel: "qwopus3.6-27b-v2-mtp", HaikuModel: "qwen3.6-35b-a3b-mtp"},
	}
	cfg.CustomProviders["gateway"] = config.CustomProvider{
		Name: "gateway", DisplayName: "gateway", BaseURL: "https://gateway.example.com", APIKeyEnv: "GATEWAY_API_KEY",
		DefaultModel: "model-a",
		TierModels:   config.TierModels{HaikuModel: "model-c"},
	}

	lm, err := Resolve("lmstudio", catalog, cfg)
	if err != nil {
		t.Fatal(err)
	}
	tiers := EffectiveTiers(lm)
	for tier, want := range map[string]string{
		"opus": "qwen3.8-27b-mtp", "sonnet": "qwopus3.6-27b-v2-mtp", "haiku": "qwen3.6-35b-a3b-mtp",
		"fable": "qwen3.8-27b-mtp", "small": "qwen3.6-35b-a3b-mtp",
	} {
		if tiers[tier] != want {
			t.Fatalf("lmstudio %s = %q, want %q (tiers %+v)", tier, tiers[tier], want, tiers)
		}
	}

	custom, err := Resolve("gateway", catalog, cfg)
	if err != nil {
		t.Fatal(err)
	}
	tiers = EffectiveTiers(custom)
	if tiers["opus"] != "model-a" || tiers["sonnet"] != "model-a" || tiers["haiku"] != "model-c" || tiers["fable"] != "model-a" {
		t.Fatalf("custom tiers = %+v", tiers)
	}
}

func TestResolveDefaultOverrideAppliesToEveryTier(t *testing.T) {
	t.Parallel()

	catalog, err := providers.Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg := newTestConfig()
	cfg.ProviderOverrides["deepseek"] = config.ProviderOverride{Model: "deepseek-v4-pro[1m]"}

	target, err := Resolve("deepseek", catalog, cfg)
	if err != nil {
		t.Fatal(err)
	}
	for tier, model := range EffectiveTiers(target) {
		if model != "deepseek-v4-pro[1m]" {
			t.Fatalf("tier %s = %q, want the chosen default everywhere", tier, model)
		}
	}
	if target.ModelEnv["deepseek-v4-pro[1m]"]["CLAUDE_CODE_AUTO_COMPACT_WINDOW"] != "786432" {
		t.Fatalf("the chosen model lost its context settings: %+v", target.ModelEnv)
	}
}

func TestResolveExplainsRemovedProfiles(t *testing.T) {
	t.Parallel()

	catalog, err := providers.Load()
	if err != nil {
		t.Fatal(err)
	}
	_, err = Resolve("alibaba-us", catalog, newTestConfig())
	if err == nil || !strings.Contains(err.Error(), "clother-alibaba") {
		t.Fatalf("err = %v, want a pointer to clother-alibaba", err)
	}
}

func TestEffectiveTiersLeaveNativeAlone(t *testing.T) {
	t.Parallel()

	target := Target{Family: providers.FamilyClaudeStrict}
	if tiers := EffectiveTiers(target); len(tiers) != 0 {
		t.Fatalf("native must keep Claude Code's own model aliases, got %+v", tiers)
	}
}
