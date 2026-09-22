package runtime

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
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
	if info, err := os.Stat(fallback); err == nil && !info.IsDir() {
		if selfResolved == "" || !samePath(fallback, selfResolved) {
			return fallback, nil
		}
	}
	// claude-real is a symlink into the native installer's versions dir, and
	// Claude Code's auto-updater prunes old versions: the link then dangles.
	if _, err := os.Lstat(fallback); err == nil {
		if latest := newestInstalledClaude(); latest != "" && (selfResolved == "" || !samePath(latest, selfResolved)) {
			return latest, nil
		}
	}
	return "", fmt.Errorf("could not locate real claude; ensure `claude` is in PATH or `%s` exists", fallback)
}

// newestInstalledClaude returns the highest version installed by Claude
// Code's native installer, which keeps one executable per version in
// ~/.local/share/claude/versions (it does not follow XDG_DATA_HOME).
func newestInstalledClaude() string {
	dir := filepath.Join(userHomeDir(), ".local", "share", "claude", "versions")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	best := ""
	var bestVersion []int
	for _, entry := range entries {
		version, ok := parseVersion(entry.Name())
		if !ok {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
			continue
		}
		if best == "" || compareVersions(version, bestVersion) > 0 {
			best, bestVersion = path, version
		}
	}
	return best
}

func parseVersion(name string) ([]int, bool) {
	parts := strings.Split(name, ".")
	out := make([]int, 0, len(parts))
	for _, part := range parts {
		value, err := strconv.Atoi(part)
		if err != nil || value < 0 {
			return nil, false
		}
		out = append(out, value)
	}
	return out, len(out) > 0
}

func compareVersions(left, right []int) int {
	for i := 0; i < len(left) || i < len(right); i++ {
		var l, r int
		if i < len(left) {
			l = left[i]
		}
		if i < len(right) {
			r = right[i]
		}
		if l != r {
			if l > r {
				return 1
			}
			return -1
		}
	}
	return 0
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
