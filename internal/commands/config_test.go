package commands

import (
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jolehuit/clother/internal/config"
	"github.com/jolehuit/clother/internal/providers"
	"github.com/jolehuit/clother/internal/ui"
)

func TestResolveModelChoiceMapsNumericSelections(t *testing.T) {
	t.Parallel()

	choices := []providers.ModelChoice{
		{ID: "glm-5"},
		{ID: "glm-4.7"},
	}

	if got := resolveModelChoice("1", choices); got != "glm-5" {
		t.Fatalf("resolveModelChoice(1) = %q, want glm-5", got)
	}
	if got := resolveModelChoice("glm-4.7", choices); got != "glm-4.7" {
		t.Fatalf("resolveModelChoice(glm-4.7) = %q", got)
	}
}

func TestResolveModelChoiceKeepsCustomModelID(t *testing.T) {
	t.Parallel()

	choices := []providers.ModelChoice{
		{ID: "MiniMax-M2.7"},
		{ID: "MiniMax-M2.5"},
	}

	if got := resolveModelChoice("MiniMax-M2.7-pro", choices); got != "MiniMax-M2.7-pro" {
		t.Fatalf("resolveModelChoice(MiniMax-M2.7-pro) = %q, want MiniMax-M2.7-pro", got)
	}
}

func TestDefaultAliasNameProducesValidAliases(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"minimax/minimax-m2.5:free":      "minimax-m2-5-free",
		"qwen/qwen3.6-plus":              "qwen3-6-plus",
		"moonshotai/kimi-k2-0905:exacto": "kimi-k2-0905-exacto",
		"openai/gpt-4o":                  "gpt-4o",
		"Vendor/Model__Name":             "model__name",
		"weird/:::":                      "",
	}
	for model, want := range cases {
		got := defaultAliasName(model)
		if got != want {
			t.Fatalf("defaultAliasName(%q) = %q, want %q", model, got, want)
		}
		if got != "" && !validName.MatchString(got) {
			t.Fatalf("defaultAliasName(%q) = %q does not match validName", model, got)
		}
	}
}

func TestConfigBuiltinAllowsModelOverrideWithoutCatalogChoices(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, ".local", "share"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, ".cache"))
	t.Setenv("CLOTHER_BIN", filepath.Join(root, "bin"))

	paths, err := config.Detect("")
	if err != nil {
		t.Fatal(err)
	}
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

	provider := providers.Provider{
		ID:           "minimax",
		DisplayName:  "MiniMax",
		DefaultModel: "MiniMax-M2.7",
	}

	ctx := Context{
		Paths:   paths,
		Config:  cfg,
		Secrets: config.Secrets{},
		Catalog: catalog,
		Output:  &ui.Output{Stdout: io.Discard, Stderr: io.Discard, Format: ui.FormatHuman},
		Prompt:  testPrompter("MiniMax-M2.7-pro\n"),
	}

	code, err := configBuiltin(ctx, provider)
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 {
		t.Fatalf("configBuiltin() code = %d, want 0", code)
	}
	if got := cfg.ProviderOverrides["minimax"].Model; got != "MiniMax-M2.7-pro" {
		t.Fatalf("override model = %q, want MiniMax-M2.7-pro", got)
	}
}

func TestConfigBuiltinLocalProviderStoresRemoteBaseURL(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, ".local", "share"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, ".cache"))
	t.Setenv("CLOTHER_BIN", filepath.Join(root, "bin"))

	paths, err := config.Detect("")
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := providers.Load()
	if err != nil {
		t.Fatal(err)
	}
	provider, ok := catalog.Get("lmstudio")
	if !ok {
		t.Fatal("lmstudio provider missing from catalog")
	}

	cfg := &config.File{
		Version:           1,
		ProviderOverrides: map[string]config.ProviderOverride{},
		OpenRouterAliases: map[string]string{},
		CustomProviders:   map[string]config.CustomProvider{},
	}

	ctx := Context{
		Paths:   paths,
		Config:  cfg,
		Secrets: config.Secrets{},
		Catalog: catalog,
		Output:  &ui.Output{Stdout: io.Discard, Stderr: io.Discard, Format: ui.FormatHuman},
		Prompt:  testPrompter("http://192.168.123.123:1234\n"),
	}

	code, err := configBuiltin(ctx, provider)
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 {
		t.Fatalf("configBuiltin() code = %d, want 0", code)
	}
	if got := cfg.ProviderOverrides["lmstudio"].BaseURL; got != "http://192.168.123.123:1234" {
		t.Fatalf("override base URL = %q, want http://192.168.123.123:1234", got)
	}

	// Re-running config and accepting the default keeps the override.
	ctx.Prompt = testPrompter("\n")
	if _, err := configBuiltin(ctx, provider); err != nil {
		t.Fatal(err)
	}
	if got := cfg.ProviderOverrides["lmstudio"].BaseURL; got != "http://192.168.123.123:1234" {
		t.Fatalf("override base URL after reconfig = %q, want kept", got)
	}

	// Entering the catalog default clears the override.
	ctx.Prompt = testPrompter("http://localhost:1234\n")
	if _, err := configBuiltin(ctx, provider); err != nil {
		t.Fatal(err)
	}
	if _, ok := cfg.ProviderOverrides["lmstudio"]; ok {
		t.Fatalf("expected override cleared, got %+v", cfg.ProviderOverrides)
	}

	// A non-HTTP value is rejected.
	ctx.Prompt = testPrompter("192.168.1.4:1234\n")
	if code, err := configBuiltin(ctx, provider); err == nil || code == 0 {
		t.Fatalf("expected invalid base URL error, got code=%d err=%v", code, err)
	}
}

// testPrompter reads every answer, secrets included, from input: it never
// opens the controlling terminal, which would block a test run.
func testPrompter(input string) *ui.Prompter {
	return &ui.Prompter{In: strings.NewReader(input), Out: io.Discard}
}

func newConfigTestContext(t *testing.T, input string) (Context, *config.File) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, ".local", "share"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, ".cache"))
	t.Setenv("CLOTHER_BIN", filepath.Join(root, "bin"))

	paths, err := config.Detect("")
	if err != nil {
		t.Fatal(err)
	}
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
	return Context{
		Paths:   paths,
		Config:  cfg,
		Secrets: config.Secrets{},
		Catalog: catalog,
		Output:  &ui.Output{Stdout: io.Discard, Stderr: io.Discard, Format: ui.FormatHuman},
		Prompt:  testPrompter(input),
	}, cfg
}

// Issue #37: an LM Studio server with "Require Authentication" needs a token.
func TestConfigLMStudioStoresOptionalToken(t *testing.T) {
	ctx, cfg := newConfigTestContext(t, "https://lmstudio.example.com\nsk-lm-token\n\n\n\n\n")
	provider, _ := ctx.Catalog.Get("lmstudio")
	if _, err := configBuiltin(ctx, provider); err != nil {
		t.Fatal(err)
	}
	if got := ctx.Secrets["LMSTUDIO_API_KEY"]; got != "sk-lm-token" {
		t.Fatalf("LMSTUDIO_API_KEY = %q, want the token", got)
	}
	if got := cfg.ProviderOverrides["lmstudio"].BaseURL; got != "https://lmstudio.example.com" {
		t.Fatalf("base URL = %q", got)
	}

	// Empty keeps the token...
	ctx.Prompt = testPrompter("\n\n\n\n\n\n")
	if _, err := configBuiltin(ctx, provider); err != nil {
		t.Fatal(err)
	}
	if got := ctx.Secrets["LMSTUDIO_API_KEY"]; got != "sk-lm-token" {
		t.Fatalf("token after keeping = %q", got)
	}
	// ...and "-" removes it.
	ctx.Prompt = testPrompter("\n-\n\n\n\n\n")
	if _, err := configBuiltin(ctx, provider); err != nil {
		t.Fatal(err)
	}
	if _, ok := ctx.Secrets["LMSTUDIO_API_KEY"]; ok {
		t.Fatal("\"-\" did not remove the token")
	}
}

// Issue #38: local backends map each Claude tier to a backend model.
func TestConfigLocalProviderStoresTierModels(t *testing.T) {
	ctx, cfg := newConfigTestContext(t, "\nqwen3.8-27b-mtp\nqwen3.8-27b-mtp\nqwopus3.6-27b-v2-mtp\nqwen3.6-35b-a3b-mtp\n")
	provider, _ := ctx.Catalog.Get("ollama")
	if _, err := configBuiltin(ctx, provider); err != nil {
		t.Fatal(err)
	}
	override := cfg.ProviderOverrides["ollama"]
	want := config.ProviderOverride{
		Model: "qwen3.8-27b-mtp",
		TierModels: config.TierModels{
			OpusModel:   "qwen3.8-27b-mtp",
			SonnetModel: "qwopus3.6-27b-v2-mtp",
			HaikuModel:  "qwen3.6-35b-a3b-mtp",
		},
	}
	if override != want {
		t.Fatalf("override = %+v, want %+v", override, want)
	}

	// "-" clears a single tier and keeps the others.
	ctx.Prompt = testPrompter("\n\n\n-\n\n")
	if _, err := configBuiltin(ctx, provider); err != nil {
		t.Fatal(err)
	}
	if got := cfg.ProviderOverrides["ollama"]; got.SonnetModel != "" || got.OpusModel != want.OpusModel || got.HaikuModel != want.HaikuModel {
		t.Fatalf("override after clearing sonnet = %+v", got)
	}
}

func TestConfigCustomProviderStoresTierModels(t *testing.T) {
	ctx, cfg := newConfigTestContext(t, "gateway\nhttps://gateway.example.com\nmodel-a\nmodel-a\nmodel-b\nmodel-c\nsk-gateway\n")
	if _, err := configCustom(ctx); err != nil {
		t.Fatal(err)
	}
	custom := cfg.CustomProviders["gateway"]
	if custom.DefaultModel != "model-a" || custom.OpusModel != "model-a" || custom.SonnetModel != "model-b" || custom.HaikuModel != "model-c" {
		t.Fatalf("custom provider = %+v", custom)
	}
	if ctx.Secrets["GATEWAY_API_KEY"] != "sk-gateway" {
		t.Fatalf("custom key not stored: %+v", ctx.Secrets)
	}
}
