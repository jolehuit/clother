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

// PR #29: Claude Code's auto-updater prunes the version claude-real points to.
func TestFindRealClaudeRecoversFromDanglingClaudeReal(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
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
	versions := filepath.Join(root, ".local", "share", "claude", "versions")
	if err := os.MkdirAll(versions, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(versions, "2.1.9"), filepath.Join(binDir, "claude-real")); err != nil {
		t.Fatal(err)
	}
	for name, mode := range map[string]os.FileMode{
		"2.1.10":   0o755, // numerically newest: 10 > 9
		"2.1.9.1":  0o755,
		"2.2.0":    0o644, // not executable
		"nightly":  0o755, // not a version
		"2.1.2":    0o755,
		"2.1.10.x": 0o755,
	} {
		if err := os.WriteFile(filepath.Join(versions, name), []byte("#!/bin/sh\n"), mode); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", binDir)

	got, err := FindRealClaude(config.Paths{BinDir: binDir})
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(versions, "2.1.10"); got != want {
		t.Fatalf("FindRealClaude() = %q, want %q", got, want)
	}
}

func TestFindRealClaudeWithoutClaudeRealStillFails(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	versions := filepath.Join(root, ".local", "share", "claude", "versions")
	if err := os.MkdirAll(versions, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(versions, "2.1.10"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", filepath.Join(root, "empty"))

	// Without a claude-real link there is no sign Clother installed over the
	// native installer, so the versions dir is not guessed at.
	if _, err := FindRealClaude(config.Paths{BinDir: filepath.Join(root, "bin")}); err == nil {
		t.Fatal("expected an error when neither PATH nor claude-real has claude")
	}
}
