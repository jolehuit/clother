package runtime

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jolehuit/clother/internal/config"
)

// Regression for the finding "FindRealClaude walks the PATH by hand and runs a
// binary relative to the current directory". A relative PATH entry
// (PATH=$PATH:node_modules/.bin) must never produce a candidate: that is the
// exact case Go's ErrDot guard forbids, and the hand-rolled walk bypassed it.
func TestFindRealClaudeIgnoresRelativePathEntries(t *testing.T) {
	work := t.TempDir()
	relDir := filepath.Join("node_modules", ".bin")
	if err := os.MkdirAll(filepath.Join(work, relDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, relDir, "claude"), []byte("#!/bin/sh\necho HIJACKED\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })
	if err := os.Chdir(work); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", relDir)

	got, err := FindRealClaude(config.Paths{BinDir: filepath.Join(work, "bin")})
	if err == nil {
		t.Fatalf("FindRealClaude() = %q, want an error: a relative PATH entry must never be used", got)
	}
	if got != "" && !filepath.IsAbs(got) {
		t.Fatalf("FindRealClaude() returned the relative path %q", got)
	}
}

// A candidate without the executable bit is not a usable claude; picking it made
// the launch fail after the whole setup had been done.
func TestFindRealClaudeSkipsNonExecutableCandidate(t *testing.T) {
	work := t.TempDir()
	dir := filepath.Join(work, "pathdir")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "claude"), []byte("not a binary"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	if got, err := FindRealClaude(config.Paths{BinDir: filepath.Join(work, "bin")}); err == nil {
		t.Fatalf("FindRealClaude() = %q, want an error: the candidate is not executable", got)
	}
}

// The shim must be recognised even when the running binary is not the installed
// one — which is what happens during `clother install` after a self-update, when
// os.Executable() points at the freshly downloaded binary in a temp dir.
func TestFindRealClaudeSkipsShimWhenRunningFromAnotherBinary(t *testing.T) {
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "clother"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("clother", filepath.Join(binDir, "claude")); err != nil {
		t.Fatal(err)
	}
	realClaude := filepath.Join(binDir, "claude-real")
	if err := os.WriteFile(realClaude, []byte("#!/bin/sh\necho real\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)

	got, err := FindRealClaude(config.Paths{BinDir: binDir})
	if err != nil {
		t.Fatal(err)
	}
	if got != realClaude {
		t.Fatalf("FindRealClaude() = %q, want the preserved binary %q (the shim must not be returned)", got, realClaude)
	}
}

// Regression for the destructive re-install: when <BinDir>/claude is our own
// shim, PreserveRealClaude must not archive it over the backup of the genuine
// binary — that used to delete the real claude for good.
func TestPreserveRealClaudeNeverOverwritesBackupWithTheShim(t *testing.T) {
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "clother"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	claude := filepath.Join(binDir, "claude")
	if err := os.Symlink("clother", claude); err != nil {
		t.Fatal(err)
	}
	preserved := filepath.Join(binDir, "claude-real")
	original := []byte("#!/bin/sh\necho THE REAL CLAUDE\n")
	if err := os.WriteFile(preserved, original, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := PreserveRealClaude(config.Paths{BinDir: binDir}, claude); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(preserved)
	if err != nil {
		t.Fatalf("the preserved claude binary was destroyed: %v", err)
	}
	if string(got) != string(original) {
		t.Fatalf("claude-real was overwritten: got %q", got)
	}
	if _, err := os.Lstat(claude); err != nil {
		t.Fatalf("the shim should still be in place: %v", err)
	}
}

// An existing backup of a real binary is never replaced, even by another real
// binary: nothing the user owns may be dropped silently.
func TestPreserveRealClaudeKeepsAnExistingRealBackup(t *testing.T) {
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	claude := filepath.Join(binDir, "claude")
	if err := os.WriteFile(claude, []byte("new claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	preserved := filepath.Join(binDir, "claude-real")
	if err := os.WriteFile(preserved, []byte("older claude"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := PreserveRealClaude(config.Paths{BinDir: binDir}, claude); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(preserved)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "older claude" {
		t.Fatalf("existing backup was overwritten: %q", got)
	}
	if _, err := os.Stat(claude); err != nil {
		t.Fatalf("the current binary must stay in place for Sync to back it up: %v", err)
	}
}

// Regression for the case-insensitive samePath: two different names are two
// different files unless the filesystem says otherwise.
func TestSamePathIsCaseSensitive(t *testing.T) {
	if samePath("/no/such/dir/claude", "/no/such/dir/CLAUDE") {
		t.Fatal("samePath must not fold case on paths that do not exist")
	}
	root := t.TempDir()
	lower := filepath.Join(root, "claude")
	upper := filepath.Join(root, "CLAUDE")
	if err := os.WriteFile(lower, []byte("a"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(upper, []byte("b"), 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(lower)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) == "b" {
		t.Skip("case-insensitive filesystem: the two names are the same file here")
	}
	if samePath(lower, upper) {
		t.Fatalf("samePath(%q, %q) = true on a case-sensitive filesystem", lower, upper)
	}
}
