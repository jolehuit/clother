package config

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// InstallMarkerName is the file Clother drops in every directory it owns so
// that `clother uninstall` can tell its own directories from a directory the
// user pointed at by mistake through CLOTHER_CONFIG_DIR & co.
const InstallMarkerName = ".clother-install"

type Paths struct {
	ConfigDir       string
	DataDir         string
	CacheDir        string
	BinDir          string
	ConfigFile      string
	SecretsFile     string
	ManifestFile    string
	SessionPatchDir string
	UpdateCacheFile string
}

func Detect(binOverride string) (Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Paths{}, err
	}

	xdgConfigHome := getenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	xdgDataHome := getenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	xdgCacheHome := getenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))

	configDir := getenv("CLOTHER_CONFIG_DIR", filepath.Join(xdgConfigHome, "clother"))
	dataDir := getenv("CLOTHER_DATA_DIR", filepath.Join(xdgDataHome, "clother"))
	cacheDir := getenv("CLOTHER_CACHE_DIR", filepath.Join(xdgCacheHome, "clother"))

	binDir := getenv("CLOTHER_BIN", "")
	if binOverride != "" {
		binDir = binOverride
	}
	if binDir == "" {
		binDir = defaultBinDir(home)
	}

	return Paths{
		ConfigDir:       configDir,
		DataDir:         dataDir,
		CacheDir:        cacheDir,
		BinDir:          binDir,
		ConfigFile:      filepath.Join(configDir, "config.json"),
		SecretsFile:     filepath.Join(dataDir, "secrets.env"),
		ManifestFile:    filepath.Join(dataDir, "launchers.json"),
		SessionPatchDir: filepath.Join(dataDir, "session-patches"),
		UpdateCacheFile: filepath.Join(cacheDir, "update.json"),
	}, nil
}

// EnsureBaseDirs creates the directories Clother owns.
//
// Everything Clother owns is created 0700 and tightened if it already exists
// with looser bits: DataDir holds secrets.env, SessionPatchDir holds verbatim
// copies of conversations, and ConfigDir/CacheDir describe which providers a
// user talks to. BinDir is the exception — it is shared with other tools (it is
// usually ~/bin or ~/.local/bin), so it keeps 0755 and is never tightened.
func (p Paths) EnsureBaseDirs() error {
	for _, dir := range []string{p.ConfigDir, p.DataDir, p.CacheDir, p.SessionPatchDir} {
		if dir == "" {
			continue
		}
		if err := ensurePrivateDir(dir); err != nil {
			return err
		}
	}
	if p.BinDir != "" {
		if err := os.MkdirAll(p.BinDir, 0o755); err != nil {
			return err
		}
	}
	p.writeInstallMarkers()
	return nil
}

// markerPathPrefix introduces the line that binds a marker to the directory it
// was written in.
const markerPathPrefix = "path="

// writeInstallMarkers stamps the directories Clother owns. BinDir is left alone
// on purpose: it is shared with other tools and must never be removed.
// Failures are not fatal — the marker is a safety hint, not a requirement.
//
// The marker records the canonical path it was written at. Without it the file
// self-certifies: a marker copied along with a backup, or restored into another
// tree, would vouch for a directory Clother never installed into.
func (p Paths) writeInstallMarkers() {
	for _, dir := range []string{p.ConfigDir, p.DataDir, p.CacheDir} {
		if dir == "" {
			continue
		}
		marker := filepath.Join(dir, InstallMarkerName)
		if info, err := os.Lstat(marker); err == nil && !info.Mode().IsRegular() {
			// A symlink or a device named like our marker: never write through it.
			continue
		}
		body := "This directory is managed by clother.\n" + markerPathPrefix + filepath.Clean(dir) + "\n"
		_ = os.WriteFile(marker, []byte(body), 0o600)
	}
}

// IsClotherManagedDir reports whether dir carries an install marker that names
// dir itself.
//
// It used to fall back on two heuristics — a directory called "clother", or a
// directory holding any of config.json, secrets.env, launchers.json,
// update.json, session-patches. `config.json` is one of the most common file
// names there is, and `clother uninstall -y` feeds this answer to a recursive
// delete, so an unrelated project directory containing a config.json was listed
// as removable. Only the marker counts now.
func IsClotherManagedDir(dir string) bool {
	if dir == "" {
		return false
	}
	marker := filepath.Join(dir, InstallMarkerName)
	info, err := os.Lstat(marker)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	data, err := os.ReadFile(marker)
	if err != nil {
		return false
	}
	want := filepath.Clean(dir)
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, markerPathPrefix) {
			continue
		}
		return filepath.Clean(strings.TrimPrefix(line, markerPathPrefix)) == want
	}
	return false
}

// SafeToRemoveDir returns an error when dir must not be handed to os.RemoveAll.
// The Clother directories come straight from the environment
// (CLOTHER_CONFIG_DIR, CLOTHER_DATA_DIR, CLOTHER_CACHE_DIR): a typo in a shell
// profile must not turn `clother uninstall -y` into a data shredder.
func SafeToRemoveDir(dir string) error {
	if strings.TrimSpace(dir) == "" {
		return fmt.Errorf("empty path")
	}
	clean := filepath.Clean(dir)
	if !filepath.IsAbs(clean) {
		return fmt.Errorf("%s is not an absolute path", dir)
	}
	separator := string(filepath.Separator)
	if clean == separator || clean == filepath.VolumeName(clean)+separator {
		return fmt.Errorf("%s is the filesystem root", clean)
	}
	if len(pathSegments(clean)) < 2 {
		return fmt.Errorf("%s is too close to the filesystem root", clean)
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		cleanHome := filepath.Clean(home)
		if clean == cleanHome {
			return fmt.Errorf("%s is your home directory", clean)
		}
		if strings.HasPrefix(cleanHome+separator, clean+separator) {
			return fmt.Errorf("%s contains your home directory", clean)
		}
	}
	if !IsClotherManagedDir(clean) {
		return fmt.Errorf("%s does not look like a Clother directory (no %s marker naming it)", clean, InstallMarkerName)
	}
	return nil
}

// ManagedEntries lists the paths inside dir that Clother itself created.
//
// It is the whole delete list of `uninstall` for that directory: the directory
// itself is only removed afterwards, and only when nothing else is left. The
// install marker is no protection on its own, because EnsureBaseDirs writes it
// into whatever CLOTHER_CONFIG_DIR names — a typo in a shell profile included —
// so the marker cannot tell a real Clother directory from a mistyped one. What
// can be told apart is which files Clother wrote, and those are these.
func (p Paths) ManagedEntries(dir string) []string {
	clean := filepath.Clean(dir)
	var out []string
	seen := map[string]struct{}{}
	add := func(path string) {
		if path == "" || filepath.Dir(filepath.Clean(path)) != clean {
			return
		}
		path = filepath.Clean(path)
		if _, ok := seen[path]; ok {
			return
		}
		if _, err := os.Lstat(path); err != nil {
			return
		}
		seen[path] = struct{}{}
		out = append(out, path)
	}

	for _, path := range []string{
		p.ConfigFile,
		p.SecretsFile,
		p.ManifestFile,
		p.UpdateCacheFile,
		p.SessionPatchDir,
		// Files a previous version of Clother wrote and no longer creates.
		filepath.Join(p.DataDir, "clother-full.sh"),
		filepath.Join(p.DataDir, "banner"),
	} {
		add(path)
		add(LockPathFor(path))
	}
	add(filepath.Join(clean, InstallMarkerName))

	// Leftovers of an interrupted atomic write, in any of our directories.
	for _, pattern := range []string{".tmp-*", ".download-*", ".patch-*", ".launcher-*", ".settings-*", ".binary-*", ".lock-*"} {
		matches, err := filepath.Glob(filepath.Join(clean, pattern))
		if err != nil {
			continue
		}
		for _, match := range matches {
			add(match)
		}
	}
	return out
}

func pathSegments(path string) []string {
	trimmed := strings.TrimPrefix(path, filepath.VolumeName(path))
	return strings.FieldsFunc(trimmed, func(r rune) bool {
		return r == filepath.Separator || r == '/'
	})
}

func defaultBinDir(home string) string {
	if dir := claudeBinDir(); dir != "" {
		return dir
	}
	if runtime.GOOS == "darwin" {
		return filepath.Join(home, "bin")
	}
	return filepath.Join(home, ".local", "bin")
}

func claudeBinDir() string {
	claudePath, err := exec.LookPath("claude")
	if err != nil || claudePath == "" {
		return ""
	}
	if abs, err := filepath.Abs(claudePath); err == nil {
		claudePath = abs
	}
	return filepath.Dir(claudePath)
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
