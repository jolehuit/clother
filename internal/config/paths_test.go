package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectPrefersClaudeDirWhenNotOverridden(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	claudeDir := filepath.Join(root, "claude-bin")

	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(claudeDir, "claude"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("PATH", claudeDir)
	t.Setenv("CLOTHER_BIN", "")

	paths, err := Detect("")
	if err != nil {
		t.Fatal(err)
	}
	if paths.BinDir != claudeDir {
		t.Fatalf("Detect().BinDir = %q, want %q", paths.BinDir, claudeDir)
	}
}

func TestDetectPrefersCLOTHERBINOverClaudeDir(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	claudeDir := filepath.Join(root, "claude-bin")
	overrideDir := filepath.Join(root, "custom-bin")

	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(claudeDir, "claude"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("PATH", claudeDir)
	t.Setenv("CLOTHER_BIN", overrideDir)

	paths, err := Detect("")
	if err != nil {
		t.Fatal(err)
	}
	if paths.BinDir != overrideDir {
		t.Fatalf("Detect().BinDir = %q, want %q", paths.BinDir, overrideDir)
	}
}

func TestDetectPrefersExplicitFlagOverEnvAndClaudeDir(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	claudeDir := filepath.Join(root, "claude-bin")
	envDir := filepath.Join(root, "env-bin")
	flagDir := filepath.Join(root, "flag-bin")

	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(claudeDir, "claude"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("PATH", claudeDir)
	t.Setenv("CLOTHER_BIN", envDir)

	paths, err := Detect(flagDir)
	if err != nil {
		t.Fatal(err)
	}
	if paths.BinDir != flagDir {
		t.Fatalf("Detect().BinDir = %q, want %q", paths.BinDir, flagDir)
	}
}
