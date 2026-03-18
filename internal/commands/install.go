package commands

import (
	"context"
	"errors"
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
	execPath, installedVersion, cleanup, err := resolveInstallBinary(ctx)
	if cleanup != nil {
		defer cleanup()
	}
	if err != nil {
		c.Output.Warn("could not fetch latest release; installing current binary instead: %v", err)
	}

	realClaude, err := runtime.FindRealClaude(c.Paths)
	if err != nil {
		return 1, errors.New("claude command not found; install Claude Code first")
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
	if err := launchers.Sync(execPath, c.Paths, c.Catalog, c.Config); err != nil {
		return 1, err
	}
	for _, legacy := range []string{
		filepath.Join(c.Paths.DataDir, "clother-full.sh"),
		filepath.Join(c.Paths.DataDir, "banner"),
	} {
		_ = os.Remove(legacy)
	}
	c.Output.Success("installed Clother %s to %s", installedVersion, c.Paths.BinDir)
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
