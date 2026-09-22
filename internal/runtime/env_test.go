package runtime

import (
	"strings"
	"testing"

	"github.com/jolehuit/clother/internal/config"
	"github.com/jolehuit/clother/internal/profiles"
	"github.com/jolehuit/clother/internal/providers"
)

func TestBuildEnvForOpenRouter(t *testing.T) {
	t.Parallel()

	target := profiles.Target{
		Profile:   "or-kimi",
		Family:    providers.FamilyOpenRouter,
		BaseURL:   "https://openrouter.ai/api",
		AuthMode:  providers.AuthSecret,
		SecretKey: "OPENROUTER_API_KEY",
		ModelTiers: map[string]string{
			"haiku":  "moonshotai/kimi-k2.5",
			"sonnet": "moonshotai/kimi-k2.5",
			"opus":   "moonshotai/kimi-k2.5",
		},
	}

	env, err := BuildEnv(target, config.Secrets{"OPENROUTER_API_KEY": "sk-openrouter"})
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Join(env, "\n")
	for _, expected := range []string{
		"ANTHROPIC_BASE_URL=https://openrouter.ai/api",
		"ANTHROPIC_AUTH_TOKEN=sk-openrouter",
		"ANTHROPIC_API_KEY=",
		"ANTHROPIC_DEFAULT_OPUS_MODEL=moonshotai/kimi-k2.5",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("env missing %q:\n%s", expected, text)
		}
	}
}

func TestBuildEnvCustomProviderClearsAPIKey(t *testing.T) {
	t.Parallel()

	target := profiles.Target{
		Profile:   "myprovider",
		Family:    providers.FamilyCustomUnknown,
		BaseURL:   "https://api.example.com/anthropic",
		AuthMode:  providers.AuthSecret,
		SecretKey: "MYPROVIDER_API_KEY",
	}

	env, err := BuildEnv(target, config.Secrets{"MYPROVIDER_API_KEY": "sk-custom"})
	if err != nil {
		t.Fatal(err)
	}
	got := envToMap(env)
	if got["ANTHROPIC_AUTH_TOKEN"] != "sk-custom" {
		t.Fatalf("ANTHROPIC_AUTH_TOKEN = %q, want sk-custom", got["ANTHROPIC_AUTH_TOKEN"])
	}
	if v, ok := got["ANTHROPIC_API_KEY"]; !ok || v != "" {
		t.Fatalf("ANTHROPIC_API_KEY should be cleared for custom providers, got %q (present=%v)", v, ok)
	}
}

func TestBuildEnvFailsWhenSecretMissing(t *testing.T) {
	t.Parallel()

	target := profiles.Target{
		Profile:   "zai",
		Family:    providers.FamilyAnthropicCompatibleNonClaude,
		AuthMode:  providers.AuthSecret,
		SecretKey: "ZAI_API_KEY",
		BaseURL:   "https://api.z.ai/api/anthropic",
	}
	if _, err := BuildEnv(target, config.Secrets{}); err == nil {
		t.Fatal("expected missing secret error")
	}
}

func TestBuildEnvNativeClearsInheritedAnthropic(t *testing.T) {
	t.Setenv("ANTHROPIC_BASE_URL", "https://evil.example")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "bad-token")
	t.Setenv("ANTHROPIC_DEFAULT_OPUS_MODEL", "bad-model")

	target := profiles.Target{
		Profile:  "native",
		Family:   providers.FamilyClaudeStrict,
		AuthMode: providers.AuthNone,
	}

	env, err := BuildEnv(target, config.Secrets{})
	if err != nil {
		t.Fatal(err)
	}
	for key := range envToMap(env) {
		if strings.HasPrefix(key, "ANTHROPIC_") {
			t.Fatalf("native launcher leaked %s from parent environment", key)
		}
	}
}

func TestBuildEnvClearsUnusedTierVariables(t *testing.T) {
	t.Setenv("ANTHROPIC_DEFAULT_HAIKU_MODEL", "stale-haiku")
	t.Setenv("ANTHROPIC_DEFAULT_SONNET_MODEL", "stale-sonnet")
	t.Setenv("ANTHROPIC_DEFAULT_OPUS_MODEL", "stale-opus")

	target := profiles.Target{
		Profile:   "zai",
		Family:    providers.FamilyAnthropicCompatibleNonClaude,
		BaseURL:   "https://api.z.ai/api/anthropic",
		AuthMode:  providers.AuthSecret,
		SecretKey: "ZAI_API_KEY",
		ModelTiers: map[string]string{
			"opus": "glm-5",
		},
	}

	env, err := BuildEnv(target, config.Secrets{"ZAI_API_KEY": "sk-zai"})
	if err != nil {
		t.Fatal(err)
	}
	got := envToMap(env)
	if got["ANTHROPIC_DEFAULT_OPUS_MODEL"] != "glm-5" {
		t.Fatalf("unexpected opus model: %q", got["ANTHROPIC_DEFAULT_OPUS_MODEL"])
	}
	// Unmapped tiers are filled from the provider's own model, never kept
	// from the parent environment.
	if got["ANTHROPIC_DEFAULT_HAIKU_MODEL"] != "glm-5" {
		t.Fatalf("haiku tier = %q, want glm-5", got["ANTHROPIC_DEFAULT_HAIKU_MODEL"])
	}
	if got["ANTHROPIC_DEFAULT_SONNET_MODEL"] != "glm-5" {
		t.Fatalf("sonnet tier = %q, want glm-5", got["ANTHROPIC_DEFAULT_SONNET_MODEL"])
	}
}

func envToMap(env []string) map[string]string {
	out := map[string]string{}
	for _, pair := range env {
		key, value, ok := splitEnv(pair)
		if ok {
			out[key] = value
		}
	}
	return out
}

func TestBuildEnvExportsTheDocumentedCredentialVariable(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-from-shell")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "sk-ant-from-shell")

	kimi := profiles.Target{
		Profile:          "kimi",
		Family:           providers.FamilyAnthropicCompatibleNonClaude,
		BaseURL:          "https://api.kimi.ai/coding/",
		Model:            "k3-256k",
		AuthMode:         providers.AuthSecret,
		SecretKey:        "KIMI_API_KEY",
		CredentialEnvVar: providers.APIKeyEnvVar,
	}
	got := mustBuildEnv(t, kimi, config.Secrets{"KIMI_API_KEY": "sk-kimi"})
	if got["ANTHROPIC_API_KEY"] != "sk-kimi" {
		t.Fatalf("ANTHROPIC_API_KEY = %q, want the Kimi key", got["ANTHROPIC_API_KEY"])
	}
	if v, ok := got["ANTHROPIC_AUTH_TOKEN"]; !ok || v != "" {
		t.Fatalf("ANTHROPIC_AUTH_TOKEN must be blanked, got %q (present=%v)", v, ok)
	}

	zai := profiles.Target{
		Profile:          "zai",
		Family:           providers.FamilyAnthropicCompatibleNonClaude,
		BaseURL:          "https://api.z.ai/api/anthropic",
		Model:            "glm-5.3[1m]",
		AuthMode:         providers.AuthSecret,
		SecretKey:        "ZAI_API_KEY",
		CredentialEnvVar: providers.AuthTokenEnvVar,
	}
	got = mustBuildEnv(t, zai, config.Secrets{"ZAI_API_KEY": "sk-zai"})
	if got["ANTHROPIC_AUTH_TOKEN"] != "sk-zai" {
		t.Fatalf("ANTHROPIC_AUTH_TOKEN = %q, want the Z.AI key", got["ANTHROPIC_AUTH_TOKEN"])
	}
	if v, ok := got["ANTHROPIC_API_KEY"]; !ok || v != "" {
		t.Fatalf("ANTHROPIC_API_KEY must be blanked, got %q (present=%v)", v, ok)
	}
}

func TestBuildEnvLiteralTokenCanBeReplacedBySecret(t *testing.T) {
	t.Parallel()

	target := profiles.Target{
		Profile:          "lmstudio",
		Family:           providers.FamilyLocal,
		BaseURL:          "http://localhost:1234",
		AuthMode:         providers.AuthLiteral,
		LiteralAuthToken: "lmstudio",
		SecretKey:        "LMSTUDIO_API_KEY",
	}
	if got := mustBuildEnv(t, target, config.Secrets{}); got["ANTHROPIC_AUTH_TOKEN"] != "lmstudio" {
		t.Fatalf("without a token the literal must be used, got %q", got["ANTHROPIC_AUTH_TOKEN"])
	}
	got := mustBuildEnv(t, target, config.Secrets{"LMSTUDIO_API_KEY": "sk-lm-token"})
	if got["ANTHROPIC_AUTH_TOKEN"] != "sk-lm-token" {
		t.Fatalf("ANTHROPIC_AUTH_TOKEN = %q, want the configured LM Studio token", got["ANTHROPIC_AUTH_TOKEN"])
	}
}

func TestBuildEnvFillsEveryTierForThirdParties(t *testing.T) {
	t.Parallel()

	target := profiles.Target{
		Profile:  "zai",
		Family:   providers.FamilyAnthropicCompatibleNonClaude,
		BaseURL:  "https://api.z.ai/api/anthropic",
		Model:    "glm-5.3[1m]",
		AuthMode: providers.AuthSecret, SecretKey: "ZAI_API_KEY",
		ModelTiers: map[string]string{"haiku": "glm-5.3-flash[1m]"},
	}
	got := mustBuildEnv(t, target, config.Secrets{"ZAI_API_KEY": "sk"})
	want := map[string]string{
		"ANTHROPIC_DEFAULT_OPUS_MODEL":   "glm-5.3[1m]",
		"ANTHROPIC_DEFAULT_SONNET_MODEL": "glm-5.3[1m]",
		"ANTHROPIC_DEFAULT_FABLE_MODEL":  "glm-5.3[1m]",
		"ANTHROPIC_DEFAULT_HAIKU_MODEL":  "glm-5.3-flash[1m]",
		// Claude Code prefers the small-fast variable over haiku for
		// background work, so it must follow the haiku mapping.
		"ANTHROPIC_SMALL_FAST_MODEL": "glm-5.3-flash[1m]",
	}
	for key, value := range want {
		if got[key] != value {
			t.Fatalf("%s = %q, want %q", key, got[key], value)
		}
	}
	if _, ok := got["CLAUDE_CODE_SUBAGENT_MODEL"]; ok {
		t.Fatal("the subagent model is only set when the catalog asks for it")
	}
}

func TestBuildEnvAppliesCatalogEnvAndDropsInheritedRouting(t *testing.T) {
	t.Setenv("CLAUDE_CODE_USE_BEDROCK", "1")
	t.Setenv("CLAUDE_CODE_SUBAGENT_MODEL", "claude-sonnet-5")
	t.Setenv("CLAUDE_CODE_MAX_CONTEXT_TOKENS", "123")
	t.Setenv("CLAUDE_SECURESTORAGE_CONFIG_DIR", "/home/me/.claude")
	t.Setenv("CLAUDE_CODE_EFFORT_LEVEL", "low")

	target := profiles.Target{
		Profile:  "deepseek",
		Family:   providers.FamilyAnthropicCompatibleNonClaude,
		BaseURL:  "https://api.deepseek.com/anthropic",
		Model:    "deepseek-flash[1m]",
		AuthMode: providers.AuthSecret, SecretKey: "DEEPSEEK_API_KEY",
		ModelTiers: map[string]string{"subagent": "deepseek-flash"},
		ExtraEnv:   map[string]string{"CLAUDE_CODE_EFFORT_LEVEL": "max"},
		ModelEnv: map[string]map[string]string{
			"deepseek-flash[1m]": {"CLAUDE_CODE_AUTO_COMPACT_WINDOW": "786432"},
		},
	}
	got := mustBuildEnv(t, target, config.Secrets{"DEEPSEEK_API_KEY": "sk"})
	for key, value := range map[string]string{
		"CLAUDE_CODE_EFFORT_LEVEL":        "max",
		"CLAUDE_CODE_AUTO_COMPACT_WINDOW": "786432",
		"CLAUDE_CODE_SUBAGENT_MODEL":      "deepseek-flash",
	} {
		if got[key] != value {
			t.Fatalf("%s = %q, want %q", key, got[key], value)
		}
	}
	for _, key := range []string{"CLAUDE_CODE_USE_BEDROCK", "CLAUDE_CODE_MAX_CONTEXT_TOKENS", "CLAUDE_SECURESTORAGE_CONFIG_DIR"} {
		if _, ok := got[key]; ok {
			t.Fatalf("%s leaked from the parent environment", key)
		}
	}
}

func TestBuildEnvNativeKeepsUserRouting(t *testing.T) {
	t.Setenv("CLAUDE_CODE_USE_BEDROCK", "1")
	t.Setenv("CLAUDE_CODE_SUBAGENT_MODEL", "claude-sonnet-5")

	target := profiles.Target{Profile: "native", Family: providers.FamilyClaudeStrict, AuthMode: providers.AuthNone}
	got := mustBuildEnv(t, target, config.Secrets{})
	if got["CLAUDE_CODE_USE_BEDROCK"] != "1" || got["CLAUDE_CODE_SUBAGENT_MODEL"] != "claude-sonnet-5" {
		t.Fatalf("native must keep the user's own Anthropic routing: %+v", got)
	}
}

func mustBuildEnv(t *testing.T, target profiles.Target, secrets config.Secrets) map[string]string {
	t.Helper()
	env, err := BuildEnv(target, secrets)
	if err != nil {
		t.Fatal(err)
	}
	return envToMap(env)
}
