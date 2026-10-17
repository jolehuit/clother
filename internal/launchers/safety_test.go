package launchers

import (
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jolehuit/clother/internal/config"
	"github.com/jolehuit/clother/internal/providers"
)

func safetyPaths(t *testing.T) config.Paths {
	t.Helper()
	root := t.TempDir()
	paths := config.Paths{
		ConfigDir:       filepath.Join(root, "config"),
		DataDir:         filepath.Join(root, "data"),
		CacheDir:        filepath.Join(root, "cache"),
		SessionPatchDir: filepath.Join(root, "data", "session-patches"),
		BinDir:          filepath.Join(root, "bin"),
		ManifestFile:    filepath.Join(root, "data", "launchers.json"),
	}
	if err := paths.EnsureBaseDirs(); err != nil {
		t.Fatal(err)
	}
	return paths
}

func safetyConfig() *config.File {
	return &config.File{
		Version:           1,
		ProviderOverrides: map[string]config.ProviderOverride{},
		OpenRouterAliases: map[string]string{},
		CustomProviders:   map[string]config.CustomProvider{},
	}
}

func fakeClotherBinary(t *testing.T) string {
	t.Helper()
	src := filepath.Join(t.TempDir(), "clother")
	if err := os.WriteFile(src, []byte("#!/bin/sh\necho clother\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return src
}

// Regression for the finding "Sync destroys any pre-existing file bearing a
// launcher name, including a real claude binary". Sync must move the binary
// aside (to claude-real, the name uninstall restores from), never delete it.
func TestSyncPreservesForeignClaudeBinary(t *testing.T) {
	paths := safetyPaths(t)
	catalog, err := providers.Load()
	if err != nil {
		t.Fatal(err)
	}

	claude := filepath.Join(paths.BinDir, "claude")
	original := []byte("#!/bin/sh\necho THE REAL CLAUDE CODE CLI\n")
	if err := os.WriteFile(claude, original, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := Sync(fakeClotherBinary(t), paths, catalog, safetyConfig(), false); err != nil {
		t.Fatal(err)
	}

	info, err := os.Lstat(claude)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("%s should now be the clother shim", claude)
	}
	preserved, err := os.ReadFile(filepath.Join(paths.BinDir, "claude-real"))
	if err != nil {
		t.Fatalf("the real claude binary was destroyed instead of preserved: %v", err)
	}
	if string(preserved) != string(original) {
		t.Fatalf("preserved claude differs: got %q want %q", preserved, original)
	}
}

// An unrelated binary that happens to carry a launcher name is backed up too.
func TestSyncBacksUpForeignLauncherFile(t *testing.T) {
	paths := safetyPaths(t)
	catalog, err := providers.Load()
	if err != nil {
		t.Fatal(err)
	}

	victim := filepath.Join(paths.BinDir, "clother-zai")
	original := []byte("#!/bin/sh\necho SOMEONE ELSES TOOL\n")
	if err := os.WriteFile(victim, original, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := Sync(fakeClotherBinary(t), paths, catalog, safetyConfig(), false); err != nil {
		t.Fatal(err)
	}

	backup, err := os.ReadFile(victim + ".bak")
	if err != nil {
		t.Fatalf("pre-existing binary was destroyed: %v", err)
	}
	if string(backup) != string(original) {
		t.Fatalf("backup differs: got %q", backup)
	}
	if _, err := os.Readlink(victim); err != nil {
		t.Fatalf("%s should be a clother symlink: %v", victim, err)
	}
}

// Regression for the finding "the manifest dictates which files are deleted".
// Entries that are not clother symlinks are left alone, and an entry containing
// a path separator can no longer reach outside BinDir.
func TestSyncPrunesOnlyItsOwnSymlinks(t *testing.T) {
	paths := safetyPaths(t)
	catalog, err := providers.Load()
	if err != nil {
		t.Fatal(err)
	}

	foreign := filepath.Join(paths.BinDir, "clother-important")
	if err := os.WriteFile(foreign, []byte("data"), 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(filepath.Dir(paths.BinDir), "outside-file")
	if err := os.WriteFile(outside, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(paths.BinDir, "clother-stale")
	if err := os.Symlink("clother", stale); err != nil {
		t.Fatal(err)
	}
	if err := SaveManifest(paths.ManifestFile, Manifest{
		Launchers: []string{"clother-important", "../outside-file", "clother-stale"},
	}); err != nil {
		t.Fatal(err)
	}

	if err := Sync(fakeClotherBinary(t), paths, catalog, safetyConfig(), false); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(foreign); err != nil {
		t.Fatalf("a regular file listed in the manifest was deleted: %v", err)
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatalf("path traversal from the manifest deleted a file outside BinDir: %v", err)
	}
	if _, err := os.Lstat(stale); !os.IsNotExist(err) {
		t.Fatalf("a stale clother symlink should still be pruned, err=%v", err)
	}
}

// The catalog dropped alibaba-us and alibaba-cn. On a machine installed before
// that, the two symlinks survive and point at a profile the binary no longer
// knows, so running them prints "unknown profile". Reinstalling must clean them
// up — the manifest is what makes that possible.
func TestSyncRemovesLaunchersForProfilesDroppedFromTheCatalog(t *testing.T) {
	paths := safetyPaths(t)
	catalog, err := providers.Load()
	if err != nil {
		t.Fatal(err)
	}

	dropped := []string{"clother-alibaba-us", "clother-alibaba-cn"}
	for _, name := range dropped {
		if err := os.Symlink("clother", filepath.Join(paths.BinDir, name)); err != nil {
			t.Fatal(err)
		}
	}
	if err := SaveManifest(paths.ManifestFile, Manifest{Launchers: dropped}); err != nil {
		t.Fatal(err)
	}

	if err := Sync(fakeClotherBinary(t), paths, catalog, safetyConfig(), false); err != nil {
		t.Fatal(err)
	}

	for _, name := range dropped {
		if _, ok := catalog.Get(strings.TrimPrefix(name, "clother-")); ok {
			t.Skipf("%s is back in the catalog, this test no longer applies", name)
		}
		if _, err := os.Lstat(filepath.Join(paths.BinDir, name)); !os.IsNotExist(err) {
			t.Fatalf("%s should have been pruned, err=%v", name, err)
		}
	}
	// The profiles that remain must still be there.
	if _, err := os.Lstat(filepath.Join(paths.BinDir, "clother-alibaba")); err != nil {
		t.Fatalf("clother-alibaba is still in the catalog and must survive: %v", err)
	}
}

func TestSaveManifestIsNotWorldWritable(t *testing.T) {
	paths := safetyPaths(t)
	if err := SaveManifest(paths.ManifestFile, Manifest{Launchers: []string{"clother-zai"}}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(paths.ManifestFile)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("manifest mode = %v, want 0600", info.Mode().Perm())
	}
}

func TestValidLauncherNameRejectsSeparators(t *testing.T) {
	for _, name := range []string{"../outside", "clother-../evil", "clother-a/b", "/etc/passwd", "", "important-tool", "clother-"} {
		if ValidLauncherName(name) {
			t.Fatalf("ValidLauncherName(%q) = true, want false", name)
		}
	}
	for _, name := range []string{"clother-zai", "clother-or", "clother-or-kimi", "clother-custom", "clother-my.provider"} {
		if !ValidLauncherName(name) {
			t.Fatalf("ValidLauncherName(%q) = false, want true", name)
		}
	}
}

// Regression for the non-atomic replacement: the launcher name must never
// disappear from BinDir, not even for an instant, while it is being replaced.
// With the previous os.Remove + os.Symlink sequence a watcher observes the gap.
func TestReplaceSymlinkNeverLeavesTheNameMissing(t *testing.T) {
	paths := safetyPaths(t)
	link := filepath.Join(paths.BinDir, "clother-zai")
	if err := os.Symlink("clother", link); err != nil {
		t.Fatal(err)
	}

	var missing, done atomic.Bool
	watcher := make(chan struct{})
	go func() {
		defer close(watcher)
		for !done.Load() {
			if _, err := os.Lstat(link); os.IsNotExist(err) {
				missing.Store(true)
				return
			}
		}
	}()

	for i := 0; i < 300; i++ {
		if err := replaceSymlink("clother", link); err != nil {
			done.Store(true)
			<-watcher
			t.Fatalf("replaceSymlink failed: %v", err)
		}
	}
	done.Store(true)
	<-watcher

	if missing.Load() {
		t.Fatal("the launcher name disappeared from BinDir during replacement")
	}
	if target, err := os.Readlink(link); err != nil || target != "clother" {
		t.Fatalf("link = %q, err = %v", target, err)
	}
}
