package commands

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jolehuit/clother/internal/config"
	"github.com/jolehuit/clother/internal/launchers"
	"github.com/jolehuit/clother/internal/runtime"
	"github.com/jolehuit/clother/internal/update"
	"github.com/jolehuit/clother/internal/version"
)

var downloadLatestBinary = update.DownloadLatestIfNewer

// installDownloadTimeout bounds the release download. main.go hands down a
// context.Background(), so without this a stalled connection hangs forever.
const installDownloadTimeout = 15 * time.Minute

func runInstall(ctx context.Context, c Context) (int, error) {
	// Checked here, before anything is downloaded or linked, and not only in the
	// command table of internal/app: `clother update` delegates to runInstall,
	// was absent from that table, and happily persisted the empty fallback over
	// a config.json it had failed to parse.
	if err := c.refuseUnreadableState(); err != nil {
		return 1, err
	}

	isHomebrew := runtime.IsHomebrew()

	var execPath, installedVersion string
	var cleanup func()
	var err error
	// downloadErr survives the whole function: a failed download must not end on
	// "OK installed Clother v3.0.10" with exit 0, which is exactly how a frozen
	// or hijacked update channel stays invisible (finding A14).
	var downloadErr error

	if isHomebrew {
		// Homebrew manages the binary lifecycle; skip downloading and copying.
		// os.Executable() alone is not enough: it is only the invoked symlink
		// path on macOS, while on Linux it reads /proc/self/exe and yields the
		// versioned Cellar path. homebrewStableExec keeps the symlinks valid
		// across `brew upgrade`.
		execPath, err = os.Executable()
		if err != nil {
			return 1, err
		}
		execPath = homebrewStableExec(execPath)
		installedVersion = update.DisplayVersion(version.Value)
	} else {
		// The download is bounded here rather than in main.go: without a
		// deadline a server that accepts the connection and then stalls holds
		// `clother install` open forever.
		downloadCtx, cancel := context.WithTimeout(ctx, installDownloadTimeout)
		defer cancel()
		execPath, installedVersion, cleanup, err = resolveInstallBinary(downloadCtx)
		if cleanup != nil {
			defer cleanup()
		}
		if err != nil {
			downloadErr = err
			c.Output.Warn("could not fetch latest release; relinking the current binary instead: %v", err)
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
	if err := config.SaveConfigMerged(c.Paths.ConfigFile, c.ConfigBaseline, c.Config); err != nil {
		return 1, err
	}
	if err := config.SaveSecretsMerged(c.Paths.SecretsFile, c.SecretsBaseline, c.Secrets); err != nil {
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
	if !pathContainsDir(os.Getenv("PATH"), c.Paths.BinDir) {
		c.Output.Warn("%s is not on PATH; add `export PATH=\"%s:$PATH\"` to your shell profile and restart your shell", c.Paths.BinDir, c.Paths.BinDir)
	}
	if downloadErr != nil {
		// The launchers were relinked, which is worth saying, but the update
		// itself did NOT happen: exiting 0 here is what lets a frozen update
		// channel look like a successful upgrade.
		c.Output.Line("relinked the launchers in %s using the binary already installed (%s)", c.Paths.BinDir, installedVersion)
		return 1, fmt.Errorf("Clother was not updated: %w", downloadErr)
	}
	c.Output.Success("installed Clother %s to %s", installedVersion, c.Paths.BinDir)
	return 0, nil
}

// homebrewStableExec returns a path to the clother binary that survives
// `brew upgrade`. A versioned Cellar path is rewritten to <prefix>/bin/clother
// whenever that stable path really exists; otherwise execPath is returned
// unchanged.
func homebrewStableExec(execPath string) string {
	if prefix := os.Getenv("HOMEBREW_PREFIX"); prefix != "" {
		if candidate := filepath.Join(prefix, "bin", "clother"); fileExists(candidate) {
			return candidate
		}
	}
	if index := strings.Index(execPath, "/Cellar/"); index > 0 {
		if candidate := filepath.Join(execPath[:index], "bin", "clother"); fileExists(candidate) {
			return candidate
		}
	}
	return execPath
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
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
