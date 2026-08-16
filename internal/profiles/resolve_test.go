package profiles

import (
	"os"
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

// Without a credential variable, Claude Code falls back to the user's claude.ai
// login and sends that token to whatever base URL is configured. That is the
// one way Clother can actively harm someone, so it is refused at load time.
func TestValidateRejectsAuthNoneOnThirdPartyEndpoint(t *testing.T) {
	t.Parallel()

	err := Validate(Target{
		Profile:  "rogue",
		BaseURL:  "https://example.invalid/anthropic",
		AuthMode: providers.AuthNone,
	})
	if err == nil {
		t.Fatal("auth_mode none with a non-Anthropic base URL must be refused")
	}
	if !strings.Contains(err.Error(), "claude.ai login") {
		t.Fatalf("error %q does not explain what is at stake", err)
	}
	if err := Validate(Target{Profile: "native", AuthMode: providers.AuthNone}); err != nil {
		t.Fatalf("the native profile has no base URL and must stay valid: %v", err)
	}
	if err := Validate(Target{
		Profile:  "anthropic",
		BaseURL:  "https://api.anthropic.com",
		AuthMode: providers.AuthNone,
	}); err != nil {
		t.Fatalf("Anthropic's own endpoint must stay valid: %v", err)
	}
}

func TestCatalogHasNoUncredentialedThirdPartyEndpoint(t *testing.T) {
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
	for _, id := range catalog.IDs() {
		if _, err := Resolve(id, catalog, cfg); err != nil {
			t.Fatalf("catalog profile %q does not resolve: %v", id, err)
		}
	}
}

// A session pinned to one provider must not hand another provider's credential
// to the agent's Bash tool, hooks or MCP servers.
func TestResolveListsForeignSecretKeys(t *testing.T) {
	t.Parallel()

	catalog, err := providers.Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.File{
		Version:           1,
		ProviderOverrides: map[string]config.ProviderOverride{},
		OpenRouterAliases: map[string]string{},
		CustomProviders: map[string]config.CustomProvider{
			"mycorp": {Name: "mycorp", BaseURL: "https://llm.mycorp.example", APIKeyEnv: "MYCORP_API_KEY"},
		},
	}
	target, err := Resolve("zai", catalog, cfg)
	if err != nil {
		t.Fatal(err)
	}
	keys := map[string]bool{}
	for _, key := range target.ForeignSecretKeys {
		keys[key] = true
	}
	for _, expected := range []string{"MOONSHOT_API_KEY", "OPENROUTER_API_KEY", "MYCORP_API_KEY"} {
		if !keys[expected] {
			t.Fatalf("%s missing from ForeignSecretKeys: %v", expected, target.ForeignSecretKeys)
		}
	}
	if keys[target.SecretKey] {
		t.Fatalf("the target's own key %q must not be listed as foreign", target.SecretKey)
	}
}

// D2/D10: any file named clother-<x> is treated as a provider launcher, so
// `go build -o clother-dev` or `cp clother clother-backup` turns the CLI into a
// launcher for a provider that does not exist. The message has to say so.
func TestUnknownProfileExplainsTheLauncherNaming(t *testing.T) {
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

	original := os.Args
	t.Cleanup(func() { os.Args = original })

	os.Args = []string{"/tmp/build/clother-dev"}
	_, err = Resolve("dev", catalog, cfg)
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "clother-dev") || !strings.Contains(err.Error(), "rename") {
		t.Fatalf("error %q does not point at the binary name", err)
	}

	// A profile typed on the command line keeps the plain message.
	os.Args = []string{"/usr/local/bin/clother"}
	_, err = Resolve("dev", catalog, cfg)
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "rename") {
		t.Fatalf("the naming hint must not appear for a typed profile: %q", err)
	}
}
