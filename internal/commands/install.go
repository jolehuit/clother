package commands

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/jolehuit/clother/internal/config"
	"github.com/jolehuit/clother/internal/launchers"
	"github.com/jolehuit/clother/internal/runtime"
)

func runInstall(_ context.Context, c Context) (int, error) {
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
	execPath, err := os.Executable()
	if err != nil {
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
	c.Output.Success("installed Clother to %s", c.Paths.BinDir)
	return 0, nil
}
