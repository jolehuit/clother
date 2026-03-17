package launchers

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"

	"github.com/jolehuit/clother/internal/config"
	"github.com/jolehuit/clother/internal/profiles"
	"github.com/jolehuit/clother/internal/providers"
)

type Manifest struct {
	Launchers []string `json:"launchers"`
}

func Sync(execPath string, paths config.Paths, catalog providers.Catalog, cfg *config.File) error {
	if err := paths.EnsureBaseDirs(); err != nil {
		return err
	}

	destBinary := filepath.Join(paths.BinDir, "clother")
	if err := copyExecutable(execPath, destBinary); err != nil {
		return err
	}

	previous, _ := LoadManifest(paths.ManifestFile)
	desired := map[string]struct{}{}
	for _, target := range profiles.All(catalog, cfg) {
		desired[launcherName(target.Profile)] = struct{}{}
	}

	for _, old := range previous.Launchers {
		if _, ok := desired[old]; ok {
			continue
		}
		_ = os.Remove(filepath.Join(paths.BinDir, old))
	}

	var launchers []string
	for name := range desired {
		launchers = append(launchers, name)
	}
	sort.Strings(launchers)
	for _, name := range launchers {
		link := filepath.Join(paths.BinDir, name)
		_ = os.Remove(link)
		if err := os.Symlink("clother", link); err != nil {
			return err
		}
	}
	claudeShim := filepath.Join(paths.BinDir, "claude")
	_ = os.Remove(claudeShim)
	if err := os.Symlink("clother", claudeShim); err != nil {
		return err
	}
	return SaveManifest(paths.ManifestFile, Manifest{Launchers: launchers})
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
	return writeAtomic(path, data, 0o644)
}

func launcherName(profile string) string {
	return "clother-" + profile
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

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".launcher-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}
