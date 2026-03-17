package config

import (
	"os"
	"path/filepath"
	"runtime"
)

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
		if runtime.GOOS == "darwin" {
			binDir = filepath.Join(home, "bin")
		} else {
			binDir = filepath.Join(home, ".local", "bin")
		}
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

func (p Paths) EnsureBaseDirs() error {
	for _, dir := range []string{p.ConfigDir, p.DataDir, p.CacheDir, p.SessionPatchDir, p.BinDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return nil
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
