package launchers

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jolehuit/clother/internal/config"
	"github.com/jolehuit/clother/internal/providers"
)

func TestSyncCreatesBinaryAndLaunchers(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	execPath := filepath.Join(root, "clother-bin")
	if err := os.WriteFile(execPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	catalog, err := providers.Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.File{
		Version:           1,
		ProviderOverrides: map[string]config.ProviderOverride{},
		OpenRouterAliases: map[string]string{"kimi": "moonshotai/kimi-k2.5"},
		CustomProviders: map[string]config.CustomProvider{
			"myprovider": {
				Name:        "myprovider",
				DisplayName: "myprovider",
				BaseURL:     "https://example.com/anthropic",
				APIKeyEnv:   "MYPROVIDER_API_KEY",
			},
		},
	}
	paths := config.Paths{
		ConfigDir:       filepath.Join(root, "config"),
		DataDir:         filepath.Join(root, "data"),
		CacheDir:        filepath.Join(root, "cache"),
		BinDir:          filepath.Join(root, "bin"),
		ManifestFile:    filepath.Join(root, "data", "launchers.json"),
		SessionPatchDir: filepath.Join(root, "data", "session-patches"),
	}

	if err := Sync(execPath, paths, catalog, cfg); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"clother", "claude", "clother-zai", "clother-native", "clother-or-kimi", "clother-myprovider"} {
		if _, err := os.Lstat(filepath.Join(paths.BinDir, name)); err != nil {
			t.Fatalf("missing %s: %v", name, err)
		}
	}
}
