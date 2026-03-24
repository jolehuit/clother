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

	if err := Sync(execPath, paths, catalog, cfg, false); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"clother", "claude", "clother-zai", "clother-native", "clother-or-kimi", "clother-myprovider"} {
		if _, err := os.Lstat(filepath.Join(paths.BinDir, name)); err != nil {
			t.Fatalf("missing %s: %v", name, err)
		}
	}
	// binary must be a regular file (copied), not a symlink
	info, err := os.Lstat(filepath.Join(paths.BinDir, "clother"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatal("clother should be a regular file in normal mode, not a symlink")
	}
}

func TestSyncHomebrewSkipsCopyAndUsesAbsoluteSymlinks(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	// Simulate the Homebrew-managed binary (not in BinDir)
	homebrewBin := filepath.Join(root, "homebrew", "bin", "clother")
	if err := os.MkdirAll(filepath.Dir(homebrewBin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(homebrewBin, []byte("#!/bin/sh\n"), 0o755); err != nil {
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
	paths := config.Paths{
		ConfigDir:       filepath.Join(root, "config"),
		DataDir:         filepath.Join(root, "data"),
		CacheDir:        filepath.Join(root, "cache"),
		BinDir:          filepath.Join(root, "bin"),
		ManifestFile:    filepath.Join(root, "data", "launchers.json"),
		SessionPatchDir: filepath.Join(root, "data", "session-patches"),
	}

	if err := Sync(homebrewBin, paths, catalog, cfg, true); err != nil {
		t.Fatal(err)
	}

	// clother binary must NOT be copied into BinDir
	if _, err := os.Lstat(filepath.Join(paths.BinDir, "clother")); err == nil {
		t.Fatal("clother binary must not be copied into BinDir in Homebrew mode")
	}

	// provider symlinks must exist and point to the Homebrew binary
	for _, name := range []string{"claude", "clother-zai", "clother-native"} {
		link := filepath.Join(paths.BinDir, name)
		target, err := os.Readlink(link)
		if err != nil {
			t.Fatalf("missing symlink %s: %v", name, err)
		}
		if target != homebrewBin {
			t.Fatalf("%s symlink target = %q, want %q", name, target, homebrewBin)
		}
	}
}
