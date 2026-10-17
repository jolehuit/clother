package commands

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jolehuit/clother/internal/cli"
	"github.com/jolehuit/clother/internal/config"
	"github.com/jolehuit/clother/internal/providers"
	"github.com/jolehuit/clother/internal/ui"
)

type uninstallFixture struct {
	paths   config.Paths
	binDir  string
	catalog providers.Catalog
	stdout  *bytes.Buffer
	stderr  *bytes.Buffer
}

// installFixture runs a real `clother install` in a sandboxed HOME/BinDir with a
// fake but genuine-looking claude binary already present.
func installFixture(t *testing.T, claudeBody []byte) uninstallFixture {
	t.Helper()

	root := t.TempDir()
	home := filepath.Join(root, "home")
	binDir := filepath.Join(root, "bin")

	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("CLOTHER_BIN", binDir)
	t.Setenv("CLOTHER_SKIP_SELF_UPDATE", "1")
	t.Setenv("HOMEBREW_PREFIX", "")

	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if claudeBody != nil {
		if err := os.WriteFile(filepath.Join(binDir, "claude"), claudeBody, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	paths, err := config.Detect("")
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := providers.Load()
	if err != nil {
		t.Fatal(err)
	}
	fixture := uninstallFixture{
		paths:   paths,
		binDir:  binDir,
		catalog: catalog,
		stdout:  &bytes.Buffer{},
		stderr:  &bytes.Buffer{},
	}

	code, err := runInstall(context.Background(), Context{
		Paths:   paths,
		Config:  &config.File{Version: 1, ProviderOverrides: map[string]config.ProviderOverride{}, OpenRouterAliases: map[string]string{}, CustomProviders: map[string]config.CustomProvider{}},
		Secrets: config.Secrets{},
		Catalog: catalog,
		Output:  &ui.Output{Stdout: io.Discard, Stderr: io.Discard, Format: ui.FormatHuman},
	})
	if err != nil || code != 0 {
		t.Fatalf("runInstall() = %d, %v", code, err)
	}
	return fixture
}

func (f uninstallFixture) uninstall(t *testing.T) (int, error) {
	t.Helper()
	return runUninstall(context.Background(), Context{
		Paths:   f.paths,
		Config:  &config.File{Version: 1, ProviderOverrides: map[string]config.ProviderOverride{}, OpenRouterAliases: map[string]string{}, CustomProviders: map[string]config.CustomProvider{}},
		Secrets: config.Secrets{},
		Catalog: f.catalog,
		Output:  &ui.Output{Stdout: f.stdout, Stderr: f.stderr, Format: ui.FormatHuman},
		Prompt:  ui.NewPrompter(strings.NewReader("\n"), io.Discard),
		Options: cli.Options{Yes: true},
	})
}

// End-to-end regression: a real claude binary present in BinDir must survive
// `clother install` byte for byte, and `clother uninstall` must put it back
// where the user expects to find it.
func TestInstallThenUninstallRestoresTheRealClaude(t *testing.T) {
	original := []byte("#!/bin/sh\necho THE REAL CLAUDE CODE CLI\n")
	fixture := installFixture(t, original)

	claude := filepath.Join(fixture.binDir, "claude")
	preserved := filepath.Join(fixture.binDir, "claude-real")

	// After install the shim is in place and the real binary is preserved.
	if info, err := os.Lstat(claude); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("expected a shim at %s (info=%v err=%v)", claude, info, err)
	}
	if got, err := os.ReadFile(preserved); err != nil || string(got) != string(original) {
		t.Fatalf("real claude not preserved byte for byte: %q err=%v", got, err)
	}

	code, err := fixture.uninstall(t)
	if err != nil || code != 0 {
		t.Fatalf("runUninstall() = %d, %v (stderr: %s)", code, err, fixture.stderr.String())
	}

	got, err := os.ReadFile(claude)
	if err != nil {
		t.Fatalf("`claude` disappeared from the user's PATH after uninstall: %v", err)
	}
	if string(got) != string(original) {
		t.Fatalf("restored claude differs from the original: %q", got)
	}
	info, err := os.Lstat(claude)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatal("restored claude must be the real binary, not a symlink")
	}
	if _, err := os.Lstat(preserved); !os.IsNotExist(err) {
		t.Fatalf("claude-real should be gone after restoration, err=%v", err)
	}
	if _, err := os.Lstat(filepath.Join(fixture.binDir, "clother-zai")); !os.IsNotExist(err) {
		t.Fatalf("launcher symlinks should have been removed, err=%v", err)
	}
	if _, err := os.Lstat(fixture.paths.DataDir); !os.IsNotExist(err) {
		t.Fatalf("DataDir should have been removed, err=%v", err)
	}
}

// A second `clother install` (the case where the running binary is not the
// installed one, e.g. after a self-update) must not turn the preserved binary
// into a symlink to clother.
func TestSecondInstallKeepsThePreservedClaude(t *testing.T) {
	original := []byte("#!/bin/sh\necho THE REAL CLAUDE CODE CLI\n")
	fixture := installFixture(t, original)

	code, err := runInstall(context.Background(), Context{
		Paths:   fixture.paths,
		Config:  &config.File{Version: 1, ProviderOverrides: map[string]config.ProviderOverride{}, OpenRouterAliases: map[string]string{}, CustomProviders: map[string]config.CustomProvider{}},
		Secrets: config.Secrets{},
		Catalog: fixture.catalog,
		Output:  &ui.Output{Stdout: io.Discard, Stderr: io.Discard, Format: ui.FormatHuman},
	})
	if err != nil || code != 0 {
		t.Fatalf("second runInstall() = %d, %v", code, err)
	}

	preserved := filepath.Join(fixture.binDir, "claude-real")
	info, err := os.Lstat(preserved)
	if err != nil {
		t.Fatalf("the preserved binary vanished on re-install: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatal("claude-real became a symlink to clother: the real binary was destroyed")
	}
	if got, _ := os.ReadFile(preserved); string(got) != string(original) {
		t.Fatalf("claude-real content changed: %q", got)
	}
}

// If the user reinstalled Claude Code natively over the shim, uninstall must
// leave that binary alone instead of deleting it.
func TestUninstallLeavesAForeignClaudeAlone(t *testing.T) {
	fixture := installFixture(t, []byte("#!/bin/sh\necho original\n"))

	claude := filepath.Join(fixture.binDir, "claude")
	if err := os.Remove(claude); err != nil {
		t.Fatal(err)
	}
	native := []byte("#!/bin/sh\necho NATIVE REINSTALL\n")
	if err := os.WriteFile(claude, native, 0o755); err != nil {
		t.Fatal(err)
	}

	if code, err := fixture.uninstall(t); err != nil || code != 0 {
		t.Fatalf("runUninstall() = %d, %v", code, err)
	}
	got, err := os.ReadFile(claude)
	if err != nil {
		t.Fatalf("the natively reinstalled claude was deleted: %v", err)
	}
	if string(got) != string(native) {
		t.Fatalf("claude was overwritten: %q", got)
	}
}

// Regression for `uninstall -y` shredding whatever CLOTHER_CONFIG_DIR happens to
// point at: a directory that is not a Clother directory is listed as skipped and
// left untouched.
func TestUninstallRefusesDirectoriesItDoesNotOwn(t *testing.T) {
	fixture := installFixture(t, []byte("#!/bin/sh\n"))

	precious := filepath.Join(t.TempDir(), "precious", "deeply", "nested")
	if err := os.MkdirAll(precious, 0o755); err != nil {
		t.Fatal(err)
	}
	important := filepath.Join(precious, "important.txt")
	if err := os.WriteFile(important, []byte("IRREPLACEABLE USER DATA"), 0o644); err != nil {
		t.Fatal(err)
	}
	fixture.paths.ConfigDir = filepath.Dir(filepath.Dir(precious))

	code, err := fixture.uninstall(t)
	if err != nil || code != 0 {
		t.Fatalf("runUninstall() = %d, %v", code, err)
	}
	if _, err := os.Stat(important); err != nil {
		t.Fatalf("user data outside Clother was deleted: %v", err)
	}
	if !strings.Contains(fixture.stderr.String(), "skipping") {
		t.Fatalf("expected a warning about the skipped directory, stderr = %q", fixture.stderr.String())
	}
}

// Regression, scenario A of the report: CLOTHER_CONFIG_DIR mistyped onto a
// directory full of the user's own files. `clother install` stamps its marker
// there — the marker cannot tell a real Clother directory from a typo, since
// Clother is the one writing it — and `clother uninstall -y` then called
// os.RemoveAll on the lot. Uninstall now deletes only the files it wrote and
// keeps the directory when anything else remains.
func TestUninstallOnlyRemovesTheFilesClotherWrote(t *testing.T) {
	fixture := installFixture(t, []byte("#!/bin/sh\n"))

	// The config directory really is a Clother one (install ran on it), but the
	// user also keeps files there.
	thesis := filepath.Join(fixture.paths.ConfigDir, "thesis.txt")
	if err := os.WriteFile(thesis, []byte("IRREPLACEABLE USER DATA"), 0o600); err != nil {
		t.Fatal(err)
	}
	album := filepath.Join(fixture.paths.ConfigDir, "album")
	if err := os.MkdirAll(album, 0o755); err != nil {
		t.Fatal(err)
	}
	photo := filepath.Join(album, "photo.jpg")
	if err := os.WriteFile(photo, []byte("JPEG"), 0o600); err != nil {
		t.Fatal(err)
	}

	code, err := fixture.uninstall(t)
	if err != nil || code != 0 {
		t.Fatalf("runUninstall() = %d, %v (stderr: %s)", code, err, fixture.stderr.String())
	}

	for _, path := range []string{thesis, photo} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("uninstall deleted a file Clother never created (%s): %v", path, err)
		}
	}
	// What Clother did write is gone.
	for _, path := range []string{fixture.paths.ConfigFile, filepath.Join(fixture.paths.ConfigDir, config.InstallMarkerName)} {
		if _, err := os.Lstat(path); err == nil {
			t.Fatalf("%s survived the uninstall", path)
		}
	}
	if !strings.Contains(fixture.stderr.String(), "files Clother did not create") {
		t.Fatalf("the user was not told the directory was kept, stderr = %q", fixture.stderr.String())
	}
	// A directory holding nothing but Clother files is still removed entirely.
	if _, err := os.Stat(fixture.paths.DataDir); !os.IsNotExist(err) {
		t.Fatalf("the data directory, which held only Clother files, was kept: %v", err)
	}
}

// Regression, scenario B: an unrelated directory holding a config.json was
// accepted as "a Clother directory" by the fallback heuristic and listed for
// recursive deletion, even though Clother had never run there.
func TestForeignDirectoryWithAConfigJSONIsNotAClotherDirectory(t *testing.T) {
	project := t.TempDir()
	for _, name := range []string{"config.json", "secrets.env", "launchers.json", "update.json"} {
		if err := os.WriteFile(filepath.Join(project, name), []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if config.IsClotherManagedDir(project) {
		t.Fatal("a foreign directory holding a config.json was taken for a Clother directory")
	}
	if err := config.SafeToRemoveDir(project); err == nil {
		t.Fatal("SafeToRemoveDir accepted a directory Clother never installed into")
	}

	// A directory merely *named* clother is not one either.
	named := filepath.Join(t.TempDir(), "clother")
	if err := os.MkdirAll(named, 0o755); err != nil {
		t.Fatal(err)
	}
	if config.IsClotherManagedDir(named) {
		t.Fatal("a directory was accepted on its name alone")
	}
}

// The marker must not certify a directory it was merely copied into: a backup
// restored elsewhere, or a marker moved by hand, used to vouch for any path.
func TestInstallMarkerDoesNotTravel(t *testing.T) {
	original := t.TempDir()
	paths := config.Paths{ConfigDir: original, DataDir: original, CacheDir: original}
	if err := paths.EnsureBaseDirs(); err != nil {
		t.Fatal(err)
	}
	if !config.IsClotherManagedDir(original) {
		t.Fatal("the directory the marker was written in is not recognised")
	}

	elsewhere := t.TempDir()
	marker, err := os.ReadFile(filepath.Join(original, config.InstallMarkerName))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(elsewhere, config.InstallMarkerName), marker, 0o600); err != nil {
		t.Fatal(err)
	}
	if config.IsClotherManagedDir(elsewhere) {
		t.Fatal("a marker copied into another directory certified it")
	}
}

func TestUninstallRefusesHomeAndShallowPaths(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	for _, dir := range []string{"/", home, filepath.Dir(home), "relative/path", ""} {
		if err := config.SafeToRemoveDir(dir); err == nil {
			t.Fatalf("SafeToRemoveDir(%q) = nil, want a refusal", dir)
		}
	}
}

// Even with -y the user is told exactly what is about to be deleted.
func TestUninstallListsPathsEvenWithYes(t *testing.T) {
	fixture := installFixture(t, []byte("#!/bin/sh\n"))
	if code, err := fixture.uninstall(t); err != nil || code != 0 {
		t.Fatalf("runUninstall() = %d, %v", code, err)
	}
	out := fixture.stdout.String()
	if !strings.Contains(out, "will be removed") {
		t.Fatalf("uninstall did not announce what it removes: %q", out)
	}
	if !strings.Contains(out, fixture.paths.DataDir) {
		t.Fatalf("the data directory was not listed: %q", out)
	}
}

func TestHomebrewStableExecAvoidsCellarPaths(t *testing.T) {
	prefix := t.TempDir()
	if err := os.MkdirAll(filepath.Join(prefix, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	stable := filepath.Join(prefix, "bin", "clother")
	if err := os.WriteFile(stable, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cellar := filepath.Join(prefix, "Cellar", "clother", "3.0.10", "bin", "clother")
	if err := os.MkdirAll(filepath.Dir(cellar), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cellar, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("HOMEBREW_PREFIX", "")
	got := homebrewStableExec(cellar)
	if strings.Contains(got, "/Cellar/") {
		t.Fatalf("symlink target still points inside the Cellar: %q", got)
	}
	if got != stable {
		t.Fatalf("homebrewStableExec() = %q, want %q", got, stable)
	}

	t.Setenv("HOMEBREW_PREFIX", prefix)
	if got := homebrewStableExec(cellar); got != stable {
		t.Fatalf("with HOMEBREW_PREFIX set: got %q, want %q", got, stable)
	}
}

// `clother uninstall` in a HOME where Clother never installed anything used to
// end on "the `claude` shim was removed and no backup was found; reinstall
// Claude Code", which is false on both counts: no shim existed and none was
// removed. Following that advice means reinstalling a tool that was never
// touched.
func TestUninstallWithoutAShimSaysNothingAboutIt(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	binDir := filepath.Join(root, "bin")

	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("CLOTHER_BIN", binDir)
	t.Setenv("PATH", binDir)
	t.Setenv("HOMEBREW_PREFIX", "")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}

	paths, err := config.Detect("")
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := providers.Load()
	if err != nil {
		t.Fatal(err)
	}
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	code, err := runUninstall(context.Background(), Context{
		Paths:   paths,
		Config:  &config.File{Version: 1, ProviderOverrides: map[string]config.ProviderOverride{}, OpenRouterAliases: map[string]string{}, CustomProviders: map[string]config.CustomProvider{}},
		Secrets: config.Secrets{},
		Catalog: catalog,
		Output:  &ui.Output{Stdout: stdout, Stderr: stderr, Format: ui.FormatHuman},
		Prompt:  ui.NewPrompter(strings.NewReader("\n"), io.Discard),
		Options: cli.Options{Yes: true},
	})
	if err != nil || code != 0 {
		t.Fatalf("runUninstall() = %d, %v", code, err)
	}
	if strings.Contains(stderr.String(), "shim was removed") {
		t.Errorf("uninstall claimed it removed a shim that never existed: %q", stderr.String())
	}
}

// The counterpart: when a shim really was deleted and no backup exists, the
// warning must still fire — the fix above must not silence a real problem.
func TestUninstallStillWarnsWhenARealShimHadNoBackup(t *testing.T) {
	fixture := installFixture(t, []byte("#!/bin/sh\necho REAL\n"))
	// Drop the preserved binary: the shim now stands alone, exactly the state
	// the warning exists for.
	if err := os.Remove(filepath.Join(fixture.binDir, "claude-real")); err != nil {
		t.Fatal(err)
	}
	if code, err := fixture.uninstall(t); err != nil || code != 0 {
		t.Fatalf("uninstall() = %d, %v", code, err)
	}
	if !strings.Contains(fixture.stderr.String(), "shim was removed") {
		t.Errorf("a genuinely unbacked shim removal went unreported: %q", fixture.stderr.String())
	}
}
