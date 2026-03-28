package commands

import (
	"context"
	"os"
	"path/filepath"

	"github.com/jolehuit/clother/internal/config"
	"github.com/jolehuit/clother/internal/launchers"
	"github.com/jolehuit/clother/internal/runtime"
	"github.com/jolehuit/clother/internal/update"
	"github.com/jolehuit/clother/internal/version"
)

var downloadLatestBinary = update.DownloadLatestIfNewer

func runInstall(ctx context.Context, c Context) (int, error) {
	isHomebrew := runtime.IsHomebrew()

	var execPath, installedVersion string
	var cleanup func()
	var err error

	if isHomebrew {
		// Homebrew manages the binary lifecycle; skip downloading and copying.
		// os.Executable() returns the stable /opt/homebrew/bin/clother symlink
		// path, so symlinks remain valid after `brew upgrade` without re-running
		// `clother install`.
		execPath, err = os.Executable()
		if err != nil {
			return 1, err
		}
		installedVersion = update.DisplayVersion(version.Value)
	} else {
		execPath, installedVersion, cleanup, err = resolveInstallBinary(ctx)
		if cleanup != nil {
			defer cleanup()
		}
		if err != nil {
			c.Output.Warn("could not fetch latest release; installing current binary instead: %v", err)
		}
	}

	realClaude, claudeErr := runtime.FindRealClaude(c.Paths)
	if claudeErr != nil {
		c.Output.Warn("claude not found; provider symlinks will be created but the `claude` shim will be skipped — run `clother install` again after installing Claude Code")
	}
	if err := runtime.PreserveRealClaude(c.Paths, realClaude); err != nil {
		return 1, err
	}
	if err := c.Paths.EnsureBaseDirs(); err != nil {
		return 1, err
	}
	config.NormalizeLegacySecrets(c.Secrets, c.Catalog)
	if err := config.SaveConfig(c.Paths.ConfigFile, c.Config); err != nil {
		return 1, err
	}
	if err := config.SaveSecrets(c.Paths.SecretsFile, c.Secrets); err != nil {
		return 1, err
	}
	if err := launchers.Sync(execPath, c.Paths, c.Catalog, c.Config, isHomebrew); err != nil {
		return 1, err
	}
	for _, legacy := range []string{
		filepath.Join(c.Paths.DataDir, "clother-full.sh"),
		filepath.Join(c.Paths.DataDir, "banner"),
	} {
		_ = os.Remove(legacy)
	}
	c.Output.Success("installed Clother %s to %s", installedVersion, c.Paths.BinDir)
	if !pathContainsDir(os.Getenv("PATH"), c.Paths.BinDir) {
		c.Output.Warn("%s is not on PATH; add `export PATH=\"%s:$PATH\"` to your shell profile and restart your shell", c.Paths.BinDir, c.Paths.BinDir)
	}
	return 0, nil
}

func resolveInstallBinary(ctx context.Context) (string, string, func(), error) {
	if path, latest, cleanup, err := downloadLatestBinary(ctx, version.Value); err == nil && path != "" {
		return path, latest, cleanup, nil
	} else if err != nil {
		current, currentErr := os.Executable()
		if currentErr != nil {
			return "", "", nil, currentErr
		}
		return current, update.DisplayVersion(version.Value), nil, err
	}

	current, err := os.Executable()
	if err != nil {
		return "", "", nil, err
	}
	return current, update.DisplayVersion(version.Value), nil, nil
}

func pathContainsDir(pathEnv, dir string) bool {
	target := normalizePathDir(dir)
	if target == "" {
		return false
	}
	for _, entry := range filepath.SplitList(pathEnv) {
		if normalizePathDir(entry) == target {
			return true
		}
	}
	return false
}

func normalizePathDir(dir string) string {
	if dir == "" {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	return filepath.Clean(dir)
}
