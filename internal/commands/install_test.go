package commands

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jolehuit/clother/internal/config"
	"github.com/jolehuit/clother/internal/providers"
	"github.com/jolehuit/clother/internal/ui"
)

func TestRunInstallPreservesSameBinClaude(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	binDir := filepath.Join(root, "bin")

	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("CLOTHER_BIN", binDir)
	t.Setenv("CLOTHER_SKIP_SELF_UPDATE", "1")

	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	realClaude := filepath.Join(binDir, "claude")
	if err := os.WriteFile(realClaude, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	oldPath := os.Getenv("PATH")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+oldPath)

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
	output := &ui.Output{Stdout: io.Discard, Stderr: io.Discard, Format: ui.FormatHuman}

	code, err := runInstall(context.Background(), Context{
		Paths:   paths,
		Config:  cfg,
		Secrets: config.Secrets{},
		Catalog: catalog,
		Output:  output,
	})
	if err != nil {
		t.Fatalf("runInstall() error = %v", err)
	}
	if code != 0 {
		t.Fatalf("runInstall() code = %d, want 0", code)
	}

	if _, err := os.Stat(filepath.Join(binDir, "claude-real")); err != nil {
		t.Fatalf("expected preserved real claude, stat error: %v", err)
	}
	claudeInfo, err := os.Lstat(filepath.Join(binDir, "claude"))
	if err != nil {
		t.Fatal(err)
	}
	if claudeInfo.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("expected %s to be a symlink", filepath.Join(binDir, "claude"))
	}
}

func TestRunInstallUpgradesToLatestRelease(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	binDir := filepath.Join(root, "bin")

	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("CLOTHER_BIN", binDir)

	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	realClaude := filepath.Join(binDir, "claude")
	if err := os.WriteFile(realClaude, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	oldPath := os.Getenv("PATH")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+oldPath)

	releaseBinary := filepath.Join(root, "release-clother")
	if err := os.WriteFile(releaseBinary, []byte("#!/bin/sh\necho release-3.0.3\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	originalDownloader := downloadLatestBinary
	downloadLatestBinary = func(_ context.Context, _ string) (string, string, func(), error) {
		return releaseBinary, "v3.0.3", nil, nil
	}
	defer func() {
		downloadLatestBinary = originalDownloader
	}()

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

	code, err := runInstall(context.Background(), Context{
		Paths:   paths,
		Config:  cfg,
		Secrets: config.Secrets{},
		Catalog: catalog,
		Output:  &ui.Output{Stdout: io.Discard, Stderr: io.Discard, Format: ui.FormatHuman},
	})
	if err != nil {
		t.Fatalf("runInstall() error = %v", err)
	}
	if code != 0 {
		t.Fatalf("runInstall() code = %d, want 0", code)
	}

	installed, err := os.ReadFile(filepath.Join(binDir, "clother"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(installed), "release-3.0.3") {
		t.Fatalf("expected installed clother to come from latest release, got %q", string(installed))
	}
}
