package runtime

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jolehuit/clother/internal/config"
	"github.com/jolehuit/clother/internal/session"
	"github.com/jolehuit/clother/internal/update"
	"github.com/jolehuit/clother/internal/version"
)

func RunClaudeShim(ctx context.Context, paths config.Paths, args []string) (int, error) {
	args = NormalizeClaudeArgs(args)
	if isTTY(os.Stderr) && !IsHomebrew() {
		if message, err := update.MaybeMessage(paths, version.Value, time.Now()); err == nil && message != "" {
			fmt.Fprintln(os.Stderr, message)
		}
	}
	claudePath, err := FindRealClaude(paths)
	if err != nil {
		return 1, err
	}
	if err := session.RestoreStale(paths); err != nil {
		return 1, err
	}
	if code, handled, err := runWithTemporaryPatch(ctx, claudePath, paths, args, os.Environ(), ""); handled {
		return code, err
	}
	return runClaudeCommand(ctx, claudePath, args, os.Environ(), "")
}

func FindRealClaude(paths config.Paths) (string, error) {
	self, _ := os.Executable()
	selfResolved := resolvedPath(self)
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if dir == "" {
			continue
		}
		candidate := filepath.Join(dir, "claude")
		info, err := os.Stat(candidate)
		if err != nil || info.IsDir() {
			continue
		}
		if selfResolved != "" && samePath(candidate, selfResolved) {
			continue
		}
		return candidate, nil
	}
	fallback := filepath.Join(paths.BinDir, "claude-real")
	// Validate claude-real before using it:
	//   - Symlink:   verify target exists (detects broken symlinks from stale Claude updates)
	//   - Plain file: use it directly (backward-compatible with existing test)
	if linfo, err := os.Lstat(fallback); err == nil && !linfo.IsDir() {
		if linfo.Mode()&os.ModeSymlink != 0 {
			// Symlink — Stat fails if target is missing (broken).
			if _, err := os.Stat(fallback); err == nil {
				if selfResolved == "" || !samePath(fallback, selfResolved) {
					return fallback, nil
				}
			}
			// Broken symlink; fall through to version directory scan.
		} else {
			// Plain file — use it as-is.
			if selfResolved == "" || !samePath(fallback, selfResolved) {
				return fallback, nil
			}
		}
	}
	// Scan the Claude Code version directory for any installed binary as a
	// last resort. This handles auto-update scenarios where the symlink target
	// was uninstalled but a newer version exists.
	//
	// Note: We intentionally use $HOME/.local/share rather than XDG_DATA_HOME
	// here, because Claude Code's install path (set by its own installer) lives
	// under $HOME/.local/share — it does not follow XDG_DATA_HOME.
	home := os.Getenv("HOME")
	if home == "" {
		// Fall back if HOME is unset (unusual but defensible).
		if home = os.Getenv("USERPROFILE"); home == "" {
			return "", fmt.Errorf("could not locate real claude; HOME is not set and claude-real is broken")
		}
	}
	versionsDir := filepath.Join(home, ".local", "share", "claude", "versions")
	entries, err := os.ReadDir(versionsDir)
	if err == nil && len(entries) > 0 {
		var newest string
		var newestTime int64
		for _, entry := range entries {
			// Each version entry is a file named by version (e.g. "2.1.114"), not a directory.
			if entry.IsDir() {
				continue
			}
			path := filepath.Join(versionsDir, entry.Name())
			if info, err := os.Stat(path); err == nil && !info.IsDir() && info.Mode().IsRegular() && info.ModTime().Unix() > newestTime {
				newest, newestTime = path, info.ModTime().Unix()
			}
		}
		if newest != "" && (selfResolved == "" || !samePath(newest, selfResolved)) {
			return newest, nil
		}
	}
	return "", fmt.Errorf("could not locate real claude; ensure `claude` is in PATH or `%s` points to a valid Claude Code binary", fallback)
}

func PreserveRealClaude(paths config.Paths, realClaudePath string) error {
	if realClaudePath == "" {
		return nil
	}
	defaultClaude := filepath.Join(paths.BinDir, "claude")
	if !samePath(realClaudePath, defaultClaude) {
		return nil
	}

	preserved := filepath.Join(paths.BinDir, "claude-real")
	if samePath(defaultClaude, preserved) {
		return nil
	}

	if _, err := os.Stat(preserved); err == nil {
		if err := os.Remove(preserved); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	return os.Rename(defaultClaude, preserved)
}

func resolvedPath(path string) string {
	if path == "" {
		return ""
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil {
		path = resolved
	}
	abs, err := filepath.Abs(path)
	if err == nil {
		path = abs
	}
	return filepath.Clean(path)
}

func samePath(left, right string) bool {
	if left == "" || right == "" {
		return false
	}
	return strings.EqualFold(resolvedPath(left), resolvedPath(right))
}
