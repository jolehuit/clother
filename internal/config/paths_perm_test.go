package config

import (
	"os"
	"path/filepath"
	"testing"
)

// EnsureBaseDirs used to create every directory 0755, umask permitting. DataDir
// holds secrets.env and SessionPatchDir holds verbatim copies of conversations,
// so on a shared machine both were world-readable until the first write
// tightened them — and CacheDir and SessionPatchDir never were.
func TestEnsureBaseDirsKeepsCredentialDirectoriesPrivate(t *testing.T) {
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")

	paths := Paths{
		ConfigDir:       filepath.Join(root, "config"),
		DataDir:         filepath.Join(root, "data"),
		CacheDir:        filepath.Join(root, "cache"),
		SessionPatchDir: filepath.Join(root, "data", "session-patches"),
		BinDir:          binDir,
	}
	if err := paths.EnsureBaseDirs(); err != nil {
		t.Fatal(err)
	}

	for _, dir := range []string{paths.ConfigDir, paths.DataDir, paths.CacheDir, paths.SessionPatchDir} {
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatal(err)
		}
		if perm := info.Mode().Perm(); perm&0o077 != 0 {
			t.Fatalf("%s is %#o: group and other must have no access", dir, perm)
		}
	}

	// BinDir is shared with other tools; tightening it would hide the launchers
	// from anything that legitimately lists the directory.
	info, err := os.Stat(binDir)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm&0o055 == 0 {
		t.Fatalf("BinDir is %#o: it is shared and must stay readable and traversable", perm)
	}
}

// A directory that already exists with loose bits is tightened, not left alone:
// upgrading Clother must fix an installation made before this rule existed.
func TestEnsureBaseDirsTightensAPreexistingLooseDirectory(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}

	paths := Paths{
		ConfigDir:       filepath.Join(root, "config"),
		DataDir:         dataDir,
		CacheDir:        filepath.Join(root, "cache"),
		SessionPatchDir: filepath.Join(dataDir, "session-patches"),
		BinDir:          filepath.Join(root, "bin"),
	}
	if err := paths.EnsureBaseDirs(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		t.Fatalf("a preexisting %#o data dir must be tightened, it is still %#o", 0o755, perm)
	}
}
