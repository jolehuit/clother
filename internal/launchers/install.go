package launchers

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/jolehuit/clother/internal/config"
	"github.com/jolehuit/clother/internal/profiles"
	"github.com/jolehuit/clother/internal/providers"
)

type Manifest struct {
	Launchers []string `json:"launchers"`
}

// launcherNamePattern accepts the names Sync may create or prune inside BinDir.
// It rejects every path separator, so a hand-edited or corrupted manifest can
// never make Clother delete a file outside BinDir.
var launcherNamePattern = regexp.MustCompile(`^clother-[A-Za-z0-9][A-Za-z0-9._-]*$`)

// ValidLauncherName reports whether name may be used as a symlink name in BinDir.
func ValidLauncherName(name string) bool {
	if name == "" || len(name) > 128 {
		return false
	}
	if strings.ContainsRune(name, '/') || strings.ContainsRune(name, filepath.Separator) {
		return false
	}
	return launcherNamePattern.MatchString(name)
}

// Sync installs the clother binary and provider symlinks into paths.BinDir.
//
// When skipCopy is false (normal install), the binary at execPath is copied to
// paths.BinDir/clother and symlinks are created relative to it.
//
// When skipCopy is true (Homebrew install), no binary is copied; symlinks are
// created as absolute references to execPath so that a Homebrew-managed binary
// upgrade is reflected automatically without running `clother install` again.
func Sync(execPath string, paths config.Paths, catalog providers.Catalog, cfg *config.File, skipCopy bool) error {
	if err := paths.EnsureBaseDirs(); err != nil {
		return err
	}

	symlinkTarget := "clother" // relative — works when binary lives in the same dir
	if skipCopy {
		symlinkTarget = execPath // absolute — points directly to the Homebrew binary
	} else {
		destBinary := filepath.Join(paths.BinDir, "clother")
		if err := copyExecutable(execPath, destBinary); err != nil {
			return err
		}
	}

	previous, _ := LoadManifest(paths.ManifestFile)
	desired := map[string]struct{}{}
	for _, target := range profiles.All(catalog, cfg) {
		// Under Homebrew the formula already installs static provider symlinks in
		// the Homebrew prefix, and clother-or / clother-custom cover dynamic
		// providers via gateway invocation. Skip individual dynamic symlinks to
		// keep ~/bin clean for Homebrew users.
		if skipCopy && isDynamicProfile(target.Profile, cfg) {
			continue
		}
		desired[launcherName(target.Profile)] = struct{}{}
	}
	// Always create gateway symlinks regardless of install method or whether
	// any dynamic providers are configured. The isDynamicProfile skip above
	// only applies to per-alias/per-provider symlinks, never to these gateways.
	desired["clother-or"] = struct{}{}
	desired["clother-custom"] = struct{}{}

	// Prune launchers we no longer want. Only names that pass validation and
	// that are still one of our own symlinks are removed: the manifest is a
	// plain file on disk and must never dictate the deletion of anything else.
	for _, old := range previous.Launchers {
		if _, ok := desired[old]; ok {
			continue
		}
		if !ValidLauncherName(old) {
			continue
		}
		link := filepath.Join(paths.BinDir, old)
		if !isClotherLink(link, symlinkTarget) {
			continue
		}
		_ = os.Remove(link)
	}

	var launchers []string
	for name := range desired {
		if !ValidLauncherName(name) {
			return fmt.Errorf("refusing to install launcher %q: invalid launcher name", name)
		}
		launchers = append(launchers, name)
	}
	sort.Strings(launchers)
	for _, name := range launchers {
		if err := installLink(filepath.Join(paths.BinDir, name), symlinkTarget, ""); err != nil {
			return err
		}
	}
	// The claude shim is the sensitive one: anything already sitting there is
	// moved to claude-real (the name `clother uninstall` restores from) instead
	// of being deleted.
	claudeShim := filepath.Join(paths.BinDir, "claude")
	if err := installLink(claudeShim, symlinkTarget, filepath.Join(paths.BinDir, "claude-real")); err != nil {
		return err
	}
	return SaveManifest(paths.ManifestFile, Manifest{Launchers: launchers})
}

// installLink points link at target, preserving whatever was there before.
func installLink(link, target, preferredBackup string) error {
	if err := backupForeignEntry(link, target, preferredBackup); err != nil {
		return err
	}
	return replaceSymlink(target, link)
}

// backupForeignEntry moves aside anything at link that Clother did not create.
// A real binary — the user's claude, or another tool that happens to share a
// launcher name — is never destroyed.
func backupForeignEntry(link, target, preferredBackup string) error {
	info, err := os.Lstat(link)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if isClotherLinkInfo(info, link, target) {
		return nil
	}
	backup, err := freeBackupPath(link, preferredBackup)
	if err != nil {
		return err
	}
	return os.Rename(link, backup)
}

// freeBackupPath returns a path that does not exist yet, preferring the caller's
// suggestion (claude-real for the shim) then <link>.bak, <link>.bak.1, ...
func freeBackupPath(link, preferred string) (string, error) {
	candidates := make([]string, 0, 12)
	if preferred != "" {
		candidates = append(candidates, preferred)
	}
	candidates = append(candidates, link+".bak")
	for i := 1; i <= 10; i++ {
		candidates = append(candidates, fmt.Sprintf("%s.bak.%d", link, i))
	}
	for _, candidate := range candidates {
		if _, err := os.Lstat(candidate); os.IsNotExist(err) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("refusing to replace %s: it is not a clother symlink and every backup name is taken", link)
}

// isClotherLink reports whether link is a symlink Clother installed.
func isClotherLink(link, target string) bool {
	info, err := os.Lstat(link)
	if err != nil {
		return false
	}
	return isClotherLinkInfo(info, link, target)
}

func isClotherLinkInfo(info os.FileInfo, link, target string) bool {
	if info.Mode()&os.ModeSymlink == 0 {
		return false
	}
	dest, err := os.Readlink(link)
	if err != nil {
		return false
	}
	// Either the exact target we are about to install, or any link naming the
	// clother binary (install method changes flip between a relative name and
	// an absolute Homebrew path).
	return dest == target || filepath.Base(dest) == "clother"
}

// replaceSymlink installs the link atomically: the new link is created under a
// temporary name in the same directory, then renamed over the final name. There
// is never a moment where the name is missing from BinDir.
func replaceSymlink(target, link string) error {
	dir := filepath.Dir(link)
	tmp, err := os.CreateTemp(dir, ".launcher-link-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Remove(tmpPath); err != nil {
		return err
	}
	if err := os.Symlink(target, tmpPath); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, link); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}

func LoadManifest(path string) (Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, err
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func SaveManifest(path string, manifest Manifest) error {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	// 0600: the manifest decides which files Sync prunes from BinDir, so it is
	// not world-writable material.
	return writeAtomic(path, data, 0o600)
}

func launcherName(profile string) string {
	return "clother-" + profile
}

// isDynamicProfile reports whether a profile is user-defined (OpenRouter alias
// or custom provider) rather than a catalog-builtin static provider.
func isDynamicProfile(profile string, cfg *config.File) bool {
	if strings.HasPrefix(profile, "or-") {
		return true
	}
	if cfg == nil {
		return false
	}
	_, isCustom := cfg.CustomProviders[profile]
	return isCustom
}

func copyExecutable(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return writeAtomic(dst, data, 0o755)
}

// writeAtomic is config.WriteFileAtomic.
//
// This package used to carry its own copy, announced as "same discipline as
// internal/config/atomic.go" while doing something else: it treated a directory
// it could not open for fsync as a fatal error, on exactly the filesystems the
// other copy documents wanting to tolerate, so `launchers.Sync` failed where
// saving the config succeeded. One implementation, one behaviour.
func writeAtomic(path string, data []byte, mode os.FileMode) error {
	return config.WriteFileAtomic(path, data, mode)
}
