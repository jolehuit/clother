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
	// BuildEnv inherits os.Environ(), so the assertion is made key by key and
	// the failure message names only the key that is wrong. Dumping the whole
	// slice would publish the developer's own API keys into the CI log.
	got := envToMap(env)
	for key, want := range map[string]string{
		"ANTHROPIC_BASE_URL":           "https://openrouter.ai/api",
		"ANTHROPIC_AUTH_TOKEN":         "sk-openrouter",
		"ANTHROPIC_API_KEY":            "",
		"ANTHROPIC_DEFAULT_OPUS_MODEL": "moonshotai/kimi-k2.5",
	} {
		value, ok := got[key]
		if !ok {
			t.Fatalf("env is missing %s", key)
		}
		if value != want {
			t.Fatalf("%s = %q, want %q", key, value, want)
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
	// Unmapped tiers must fall back to the target's own model, never to the
	// inherited value and never to claude's builtin Anthropic ids.
	if got["ANTHROPIC_DEFAULT_HAIKU_MODEL"] != "glm-5" {
		t.Fatalf("haiku tier = %q, want the target fallback glm-5", got["ANTHROPIC_DEFAULT_HAIKU_MODEL"])
	}
	if got["ANTHROPIC_DEFAULT_SONNET_MODEL"] != "glm-5" {
		t.Fatalf("sonnet tier = %q, want the target fallback glm-5", got["ANTHROPIC_DEFAULT_SONNET_MODEL"])
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

// zaiTarget is a stand-in for any third-party provider: an anthropic-compatible
// endpoint authenticated with its own secret.
func zaiTarget() profiles.Target {
	return profiles.Target{
		Profile:           "zai",
		Family:            providers.FamilyAnthropicCompatibleNonClaude,
		BaseURL:           "https://api.z.ai/api/anthropic",
		Model:             "glm-5.2",
		ModelTiers:        map[string]string{"opus": "glm-5.2"},
		AuthMode:          providers.AuthSecret,
		SecretKey:         "ZAI_API_KEY",
		ForeignSecretKeys: []string{"MOONSHOT_API_KEY", "OPENROUTER_API_KEY", "MYCORP_API_KEY"},
	}
}

// D4/D6: the scrub used to be a plain ANTHROPIC_ prefix match, so every
// inherited routing switch survived into the claude process. One case per
// variable name.
func TestBuildEnvScrubsInheritedRoutingVariables(t *testing.T) {
	for _, key := range []string{
		"API_TIMEOUT_MS",
		"AWS_BEARER_TOKEN_BEDROCK",
		"CLAUDE_CODE_MAX_OUTPUT_TOKENS",
		"CLAUDE_CODE_SKIP_BEDROCK_AUTH",
		"CLAUDE_CODE_SKIP_VERTEX_AUTH",
		"CLAUDE_CODE_SUBAGENT_MODEL",
		"CLAUDE_CODE_USE_BEDROCK",
		"CLAUDE_CODE_USE_FOUNDRY",
		"CLAUDE_CODE_USE_VERTEX",
		"CLOUD_ML_REGION",
	} {
		t.Run(key, func(t *testing.T) {
			t.Setenv(key, "inherited-value")
			env, err := BuildEnv(zaiTarget(), config.Secrets{"ZAI_API_KEY": "zai-secret"})
			if err != nil {
				t.Fatal(err)
			}
			if value, ok := envToMap(env)[key]; ok {
				t.Fatalf("%s survived into the claude environment with value %q", key, value)
			}
		})
	}
}

// D4: another provider's credential must not stay readable by every Bash
// command, hook and MCP server of the session.
func TestBuildEnvScrubsForeignProviderKeys(t *testing.T) {
	for _, key := range []string{
		"MOONSHOT_API_KEY",
		"OPENROUTER_API_KEY",
		"DEEPSEEK_API_KEY",
		"MINIMAX_API_KEY",
		"MYCORP_API_KEY",
	} {
		t.Run(key, func(t *testing.T) {
			t.Setenv(key, "foreign-credential")
			env, err := BuildEnv(zaiTarget(), config.Secrets{"ZAI_API_KEY": "zai-secret"})
			if err != nil {
				t.Fatal(err)
			}
			if value, ok := envToMap(env)[key]; ok {
				t.Fatalf("%s survived into the claude environment with value %q", key, value)
			}
		})
	}
}

func TestBuildEnvKeepsTheTargetOwnSecretKey(t *testing.T) {
	t.Setenv("ZAI_API_KEY", "zai-secret")
	env, err := BuildEnv(zaiTarget(), config.Secrets{"ZAI_API_KEY": "zai-secret"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := envToMap(env)["ZAI_API_KEY"]; !ok {
		t.Fatal("the target's own key must not be scrubbed")
	}
}

// Without CLAUDE_CODE_SUBPROCESS_ENV_SCRUB the provider credential Clother just
// injected is readable by every subprocess of the session.
func TestBuildEnvPostsSessionHardeningDefaults(t *testing.T) {
	for key, want := range map[string]string{
		"CLAUDE_CODE_SUBPROCESS_ENV_SCRUB":         "1",
		"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1",
		"DISABLE_TELEMETRY":                        "1",
		"DISABLE_ERROR_REPORTING":                  "1",
	} {
		t.Run(key, func(t *testing.T) {
			env, err := BuildEnv(zaiTarget(), config.Secrets{"ZAI_API_KEY": "zai-secret"})
			if err != nil {
				t.Fatal(err)
			}
			if got := envToMap(env)[key]; got != want {
				t.Fatalf("%s = %q, want %q", key, got, want)
			}
		})
	}
}

func TestBuildEnvHardeningDefaultsAreOptOut(t *testing.T) {
	t.Setenv("DISABLE_TELEMETRY", "")
	env, err := BuildEnv(zaiTarget(), config.Secrets{"ZAI_API_KEY": "zai-secret"})
	if err != nil {
		t.Fatal(err)
	}
	if got := envToMap(env)["DISABLE_TELEMETRY"]; got != "" {
		t.Fatalf("DISABLE_TELEMETRY = %q, want the inherited empty value to win", got)
	}
}

func TestBuildEnvLeavesClaudeStrictAlone(t *testing.T) {
	t.Setenv("CLAUDE_CODE_USE_BEDROCK", "1")
	target := profiles.Target{Profile: "native", Family: providers.FamilyClaudeStrict, AuthMode: providers.AuthNone}
	env, err := BuildEnv(target, config.Secrets{})
	if err != nil {
		t.Fatal(err)
	}
	got := envToMap(env)
	if got["CLAUDE_CODE_USE_BEDROCK"] != "1" {
		t.Fatal("the native profile must keep the user's own Anthropic routing")
	}
	if _, ok := got["CLAUDE_CODE_SUBPROCESS_ENV_SCRUB"]; ok {
		t.Fatal("claude_strict must not be reconfigured behind the user's back")
	}
}

// D5: no third-party provider knows the Claude aliases, so every tier has to
// resolve — including the fable tier, which nothing used to set.
func TestBuildEnvResolvesEveryModelTier(t *testing.T) {
	target := profiles.Target{
		Profile:    "kimi",
		Family:     providers.FamilyAnthropicCompatibleNonClaude,
		BaseURL:    "https://api.kimi.com/coding",
		Model:      "k3-256k",
		ModelTiers: map[string]string{"small": "k3-256k"},
		AuthMode:   providers.AuthSecret,
		SecretKey:  "KIMI_API_KEY",
	}
	env, err := BuildEnv(target, config.Secrets{"KIMI_API_KEY": "kimi-secret"})
	if err != nil {
		t.Fatal(err)
	}
	got := envToMap(env)
	for _, key := range []string{
		"ANTHROPIC_MODEL",
		"ANTHROPIC_DEFAULT_HAIKU_MODEL",
		"ANTHROPIC_DEFAULT_SONNET_MODEL",
		"ANTHROPIC_DEFAULT_OPUS_MODEL",
		"ANTHROPIC_DEFAULT_FABLE_MODEL",
		"ANTHROPIC_SMALL_FAST_MODEL",
	} {
		if got[key] != "k3-256k" {
			t.Fatalf("%s = %q, want k3-256k", key, got[key])
		}
	}
}

func TestBuildEnvDoesNotPinTiersForClaudeStrict(t *testing.T) {
	target := profiles.Target{Profile: "native", Family: providers.FamilyClaudeStrict, AuthMode: providers.AuthNone}
	env, err := BuildEnv(target, config.Secrets{})
	if err != nil {
		t.Fatal(err)
	}
	for key := range envToMap(env) {
		if strings.HasPrefix(key, "ANTHROPIC_") {
			t.Fatalf("native must not pin %s", key)
		}
	}
}

// D9: the message has to carry the next action.
func TestBuildEnvMissingSecretNamesTheConfigCommand(t *testing.T) {
	_, err := BuildEnv(zaiTarget(), config.Secrets{})
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "clother config zai") {
		t.Fatalf("error %q does not tell the user what to run", err)
	}
}

// Without a credential variable, claude falls back to the user's claude.ai
// login and ships it to the third-party endpoint.
func TestBuildEnvRejectsAuthNoneOnThirdPartyEndpoint(t *testing.T) {
	target := profiles.Target{
		Profile:  "rogue",
		Family:   providers.FamilyCustomUnknown,
		BaseURL:  "https://example.invalid/anthropic",
		AuthMode: providers.AuthNone,
	}
	if _, err := BuildEnv(target, config.Secrets{}); err == nil {
		t.Fatal("auth_mode none with a non-Anthropic base URL must be refused")
	}
}

// D17: every non-claude_strict family gets the same treatment.
func TestBuildEnvBlanksAnthropicAPIKeyForEveryNonStrictFamily(t *testing.T) {
	for _, family := range []providers.Family{
		providers.FamilyAnthropicCompatibleNonClaude,
		providers.FamilyOpenRouter,
		providers.FamilyCustomUnknown,
	} {
		t.Run(string(family), func(t *testing.T) {
			target := zaiTarget()
			target.Family = family
			env, err := BuildEnv(target, config.Secrets{"ZAI_API_KEY": "zai-secret"})
			if err != nil {
				t.Fatal(err)
			}
			value, ok := envToMap(env)["ANTHROPIC_API_KEY"]
			if !ok || value != "" {
				t.Fatalf("ANTHROPIC_API_KEY = %q (present=%v), want an empty value", value, ok)
			}
		})
	}
}

func TestBuildEnvAppliesExtraEnvLast(t *testing.T) {
	target := zaiTarget()
	target.ExtraEnv = map[string]string{
		"API_TIMEOUT_MS":                   "3000000",
		"CLAUDE_CODE_SUBPROCESS_ENV_SCRUB": "1",
		"CLAUDE_CODE_AUTO_COMPACT_WINDOW":  "1000000",
	}
	t.Setenv("API_TIMEOUT_MS", "1000")
	env, err := BuildEnv(target, config.Secrets{"ZAI_API_KEY": "zai-secret"})
	if err != nil {
		t.Fatal(err)
	}
	got := envToMap(env)
	if got["API_TIMEOUT_MS"] != "3000000" {
		t.Fatalf("API_TIMEOUT_MS = %q, want the catalog value to replace the inherited one", got["API_TIMEOUT_MS"])
	}
	if got["CLAUDE_CODE_AUTO_COMPACT_WINDOW"] != "1000000" {
		t.Fatalf("extra_env was not applied: %q", got["CLAUDE_CODE_AUTO_COMPACT_WINDOW"])
	}
}

// D1: `brew shellenv` exports HOMEBREW_PREFIX for every process, so it cannot
// stand on its own as the "installed by Homebrew" signal.
func TestIsHomebrewIgnoresHomebrewPrefixAlone(t *testing.T) {
	t.Setenv("HOMEBREW_PREFIX", "/opt/homebrew")
	if IsHomebrew() {
		t.Fatal("a binary outside any Cellar must not be reported as Homebrew-managed")
	}
}
