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
		Prompt:  ui.NewPrompter(strings.NewReader("MiniMax-M2.7-pro\n"), io.Discard),
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
