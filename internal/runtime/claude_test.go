package runtime

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jolehuit/clother/internal/config"
)

func TestFindRealClaudeCanUseSameBinDir(t *testing.T) {
	root := t.TempDir()
	binDir := filepath.Join(root, "clother-bin")
	realDir := filepath.Join(root, "real-bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(realDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "claude"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	realClaude := filepath.Join(realDir, "claude")
	if err := os.WriteFile(realClaude, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	oldPath := os.Getenv("PATH")
	t.Cleanup(func() { _ = os.Setenv("PATH", oldPath) })
	if err := os.Setenv("PATH", binDir+string(os.PathListSeparator)+realDir); err != nil {
		t.Fatal(err)
	}

	got, err := FindRealClaude(config.Paths{BinDir: binDir})
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join(binDir, "claude") {
		t.Fatalf("FindRealClaude() = %q, want %q", got, filepath.Join(binDir, "claude"))
	}
}

func TestFindRealClaudeSkipsSelfAndFallsBack(t *testing.T) {
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(self, filepath.Join(binDir, "claude")); err != nil {
		t.Fatal(err)
	}
	realFallback := filepath.Join(binDir, "claude-real")
	if err := os.WriteFile(realFallback, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	oldPath := os.Getenv("PATH")
	t.Cleanup(func() { _ = os.Setenv("PATH", oldPath) })
	if err := os.Setenv("PATH", binDir); err != nil {
		t.Fatal(err)
	}

	got, err := FindRealClaude(config.Paths{BinDir: binDir})
	if err != nil {
		t.Fatal(err)
	}
	if got != realFallback {
		t.Fatalf("FindRealClaude() = %q, want %q", got, realFallback)
	}
}

func TestFindRealClaudeRecoversFromBrokenSymlink(t *testing.T) {
	// Simulate: clother install creates claude-real symlink to old Claude version,
	// Claude Code auto-updates and uninstalls the old version (broken symlink),
	// but a newer version exists in the versions directory.
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Self shim in PATH.
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(self, filepath.Join(binDir, "claude")); err != nil {
		t.Fatal(err)
	}

	// claude-real is a broken symlink (old version uninstalled).
	brokenTarget := filepath.Join(root, "nonexistent-old-version")
	realFallback := filepath.Join(binDir, "claude-real")
	if err := os.Symlink(brokenTarget, realFallback); err != nil {
		t.Fatal(err)
	}

	// Claude Code version directory with a real binary (file, not dir).
	// Must match the path the production code looks for:
	//   $HOME/.local/share/claude/versions/<version>   (binary file, named by version)
	versionsDir := filepath.Join(root, ".local", "share", "claude", "versions")
	// Use the test name as the version filename (e.g. "TestFindRealClaudeRecoversFromBrokenSymlink").
	// t.TempDir() returns a unique path per test invocation, so no collisions.
	if err := os.MkdirAll(versionsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	realClaude := filepath.Join(versionsDir, t.Name())
	if err := os.WriteFile(realClaude, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	oldHome := os.Getenv("HOME")
	oldPath := os.Getenv("PATH")
	t.Cleanup(func() {
		_ = os.Setenv("HOME", oldHome)
		_ = os.Setenv("PATH", oldPath)
	})
	if err := os.Setenv("HOME", root); err != nil {
		t.Fatal(err)
	}
	if err := os.Setenv("PATH", binDir); err != nil {
		t.Fatal(err)
	}

	got, err := FindRealClaude(config.Paths{BinDir: binDir})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if got != realClaude {
		t.Fatalf("FindRealClaude() = %q, want %q (binary from versions dir)", got, realClaude)
	}
}

func TestPreserveRealClaudeMovesClaudeToClaudeReal(t *testing.T) {
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	claudePath := filepath.Join(binDir, "claude")
	content := []byte("real-claude-binary")
	if err := os.WriteFile(claudePath, content, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := PreserveRealClaude(config.Paths{BinDir: binDir}, claudePath); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(claudePath); !os.IsNotExist(err) {
		t.Fatalf("expected %s to be moved, stat err=%v", claudePath, err)
	}
	preserved := filepath.Join(binDir, "claude-real")
	got, err := os.ReadFile(preserved)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(content) {
		t.Fatalf("preserved content mismatch: got %q", string(got))
	}
}
