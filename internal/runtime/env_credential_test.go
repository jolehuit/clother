package runtime

import (
	"fmt"
	"testing"

	"github.com/jolehuit/clother/internal/config"
	"github.com/jolehuit/clother/internal/profiles"
	"github.com/jolehuit/clother/internal/providers"
)

// The catalog declares auth_env_var per provider because Kimi Code and
// SambaNova document ANTHROPIC_API_KEY and reject a bearer token. Before the
// wiring existed, BuildEnv exported every credential as ANTHROPIC_AUTH_TOKEN
// and blanked ANTHROPIC_API_KEY, so those two providers answered 401 on every
// request while the catalog claimed to support them.
func TestBuildEnvHonoursProviderCredentialEnvVar(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		credential string
		wantSet    string
		wantBlank  string
	}{
		{
			name:       "provider documenting ANTHROPIC_API_KEY",
			credential: "ANTHROPIC_API_KEY",
			wantSet:    "ANTHROPIC_API_KEY",
			wantBlank:  "ANTHROPIC_AUTH_TOKEN",
		},
		{
			name:       "provider on the default bearer token",
			credential: "ANTHROPIC_AUTH_TOKEN",
			wantSet:    "ANTHROPIC_AUTH_TOKEN",
			wantBlank:  "ANTHROPIC_API_KEY",
		},
		{
			name:       "provider leaving auth_env_var empty falls back to the default",
			credential: "",
			wantSet:    "ANTHROPIC_AUTH_TOKEN",
			wantBlank:  "ANTHROPIC_API_KEY",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			target := profiles.Target{
				Profile:          "provider",
				Family:           providers.FamilyAnthropicCompatibleNonClaude,
				BaseURL:          "https://api.example.com/anthropic",
				Model:            "some-model",
				AuthMode:         providers.AuthSecret,
				SecretKey:        "PROVIDER_API_KEY",
				CredentialEnvVar: tc.credential,
			}

			env, err := BuildEnv(target, config.Secrets{"PROVIDER_API_KEY": "sk-provider-value"})
			if err != nil {
				t.Fatal(err)
			}
			got := envToMap(env)

			// Never print the whole map: BuildEnv inherits os.Environ(), so a
			// full dump publishes the developer's own API keys into the CI log.
			if got[tc.wantSet] != "sk-provider-value" {
				t.Fatalf("%s should carry the credential, got %q", tc.wantSet, got[tc.wantSet])
			}
			value, present := got[tc.wantBlank]
			if !present {
				t.Fatalf("%s should be present and empty, it is absent", tc.wantBlank)
			}
			if value != "" {
				t.Fatalf("%s should be blanked, got %q", tc.wantBlank, value)
			}
		})
	}
}

// Every catalog entry must reach BuildEnv with the variable it documents; the
// two are wired through profiles.Resolve, which is where the field used to be
// dropped on the floor.
func TestResolveCarriesCatalogAuthEnvVarThroughToTheChildEnv(t *testing.T) {
	t.Parallel()

	catalog, err := providers.Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.File{
		Version:           1,
		ProviderOverrides: map[string]config.ProviderOverride{},
		OpenRouterAliases: map[string]string{},
		CustomProviders:   map[string]config.CustomProvider{},
	}

	for _, provider := range catalog.All() {
		if provider.AuthMode != providers.AuthSecret {
			continue
		}
		target, err := profiles.Resolve(provider.ID, catalog, cfg)
		if err != nil {
			t.Fatalf("resolve %s: %v", provider.ID, err)
		}
		if target.CredentialEnvVar != provider.CredentialEnvVar() {
			t.Fatalf("%s: target carries %q, catalog documents %q",
				provider.ID, target.CredentialEnvVar, provider.CredentialEnvVar())
		}

		env, err := BuildEnv(target, config.Secrets{provider.KeyVar: "sk-value"})
		if err != nil {
			t.Fatalf("build env for %s: %v", provider.ID, err)
		}
		got := envToMap(env)
		if got[provider.CredentialEnvVar()] != "sk-value" {
			t.Fatalf("%s: credential should be exported as %s, got %q",
				provider.ID, provider.CredentialEnvVar(), got[provider.CredentialEnvVar()])
		}
		for _, key := range providers.CredentialEnvVars {
			if key == provider.CredentialEnvVar() {
				continue
			}
			if got[key] != "" {
				t.Fatalf("%s: %s should be blank, got %q", provider.ID, key, got[key])
			}
		}
	}
}

// BuildEnv posts ANTHROPIC_DEFAULT_FABLE_MODEL for every non-strict family, so
// the overlay must own that key too: otherwise `--model <concrete id>` rewrites
// the five other tiers and leaves fable pointing at the provider default.
func TestOverlayModelKeysCoverEveryTierBuildEnvPosts(t *testing.T) {
	t.Parallel()

	owned := map[string]bool{}
	for _, key := range overlayModelKeys {
		owned[key] = true
	}
	for _, entry := range modelTierEnv {
		if !owned[entry.key] {
			t.Fatalf("BuildEnv posts %s for tier %q but the config overlay does not own it, so --model would leave it stale",
				entry.key, entry.tier)
		}
	}
}

// `clother-zai --help` must reach claude's help before any key exists: the
// error that used to come back was the one message hiding the instructions to
// fix it.
func TestIsInfoOnlyInvocation(t *testing.T) {
	t.Parallel()

	cases := []struct {
		args []string
		want bool
	}{
		{args: []string{"--help"}, want: true},
		{args: []string{"-h"}, want: true},
		{args: []string{"--version"}, want: true},
		{args: []string{"--help", "--version"}, want: true},
		{args: nil, want: false},
		{args: []string{"--help", "write me a poem"}, want: false},
		{args: []string{"--print", "--help"}, want: false},
		{args: []string{"--model", "--help"}, want: false},
		{args: []string{"--", "--help"}, want: false},
		{args: []string{"hello"}, want: false},
	}
	for _, tc := range cases {
		if got := IsInfoOnlyInvocation(tc.args); got != tc.want {
			t.Fatalf("IsInfoOnlyInvocation(%v) = %v, want %v", tc.args, got, tc.want)
		}
	}
}

func TestBuildEnvWithoutCredentialToleratesAMissingKey(t *testing.T) {
	t.Parallel()

	target := profiles.Target{
		Profile:          "zai",
		Family:           providers.FamilyAnthropicCompatibleNonClaude,
		BaseURL:          "https://api.z.ai/api/anthropic",
		Model:            "glm-5.3[1m]",
		AuthMode:         providers.AuthSecret,
		SecretKey:        "ZAI_API_KEY",
		CredentialEnvVar: providers.DefaultCredentialEnvVar,
	}

	if _, err := BuildEnv(target, config.Secrets{}); err == nil {
		t.Fatal("BuildEnv must still refuse a real session without a credential")
	}

	env, err := BuildEnvWithoutCredential(target)
	if err != nil {
		t.Fatalf("help must stay reachable without a key: %v", err)
	}
	got := envToMap(env)
	if got["ANTHROPIC_AUTH_TOKEN"] != "" {
		t.Fatalf("no credential should be invented, got %q", got["ANTHROPIC_AUTH_TOKEN"])
	}
	if got["ANTHROPIC_BASE_URL"] != target.BaseURL {
		t.Fatalf("base URL should still be set, got %q", got["ANTHROPIC_BASE_URL"])
	}
}

// The subprocess scrub is what keeps the third-party credential Clother injects
// out of reach of every Bash command, hook and MCP server of the session. It
// used to be a mere default, only posted when the key was absent from the
// inherited environment — and it is not in inheritedRoutingKeys, so it was not
// purged first either. Any inherited value (a shell profile, a repository
// .envrc, a leftover from an older version) switched the protection off without
// a word.
func TestInheritedEnvCannotDisableTheSubprocessScrub(t *testing.T) {
	for _, inherited := range []string{"0", "", "false"} {
		t.Run("inherited="+inherited, func(t *testing.T) {
			t.Setenv(subprocessScrubKey, inherited)

			var warnings []string
			previous := hardeningWarn
			hardeningWarn = func(format string, args ...any) {
				warnings = append(warnings, fmt.Sprintf(format, args...))
			}
			t.Cleanup(func() { hardeningWarn = previous })

			target := profiles.Target{
				Profile:   "provider",
				Family:    providers.FamilyAnthropicCompatibleNonClaude,
				BaseURL:   "https://api.example.com/anthropic",
				Model:     "some-model",
				AuthMode:  providers.AuthSecret,
				SecretKey: "PROVIDER_API_KEY",
			}
			env, err := BuildEnv(target, config.Secrets{"PROVIDER_API_KEY": "sk-provider-value"})
			if err != nil {
				t.Fatal(err)
			}
			if got := envToMap(env)[subprocessScrubKey]; got != "1" {
				t.Fatalf("%s = %q in the child environment, want \"1\"", subprocessScrubKey, got)
			}
			if len(warnings) == 0 {
				t.Fatalf("the inherited value was overridden silently")
			}
		})
	}
}

// claude_strict talks to Anthropic with the user's own credential, so the
// session hardening does not apply there.
func TestStrictFamilyKeepsTheInheritedEnvironment(t *testing.T) {
	t.Setenv(subprocessScrubKey, "0")
	target := profiles.Target{
		Profile:  "native",
		Family:   providers.FamilyClaudeStrict,
		AuthMode: providers.AuthNone,
	}
	env, err := BuildEnv(target, config.Secrets{})
	if err != nil {
		t.Fatal(err)
	}
	if got := envToMap(env)[subprocessScrubKey]; got != "0" {
		t.Fatalf("%s = %q for claude_strict, want the inherited \"0\"", subprocessScrubKey, got)
	}
}
