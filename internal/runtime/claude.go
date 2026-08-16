package runtime

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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

// FindRealClaude locates the genuine claude binary, skipping Clother's own shim.
//
// Only absolute PATH entries are honoured: a relative entry (the common
// `PATH=$PATH:node_modules/.bin`) would make exec resolve the binary from the
// current working directory, which is precisely what Go's ErrDot guard forbids.
// Candidates are validated through exec.LookPath so that the executable bit and
// the access rights are checked by the standard library rather than by a
// hand-rolled os.Stat, and the returned path is always absolute.
func FindRealClaude(paths config.Paths) (string, error) {
	self, _ := os.Executable()
	selfResolved := resolvedPath(self)
	ours := clotherBinaries(paths, selfResolved)

	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if dir == "" || !filepath.IsAbs(dir) {
			continue
		}
		candidate := filepath.Join(dir, "claude")
		resolved, err := exec.LookPath(candidate)
		if err != nil || !filepath.IsAbs(resolved) {
			continue
		}
		if isOneOf(resolved, ours) {
			continue
		}
		return resolved, nil
	}

	fallback := filepath.Join(paths.BinDir, "claude-real")
	if abs, err := filepath.Abs(fallback); err == nil {
		fallback = abs
	}
	if resolved, err := exec.LookPath(fallback); err == nil && filepath.IsAbs(resolved) && !isOneOf(resolved, ours) {
		return resolved, nil
	}
	return "", fmt.Errorf("could not locate real claude; ensure `claude` is in PATH or `%s` exists", fallback)
}

// PreserveRealClaude archives <BinDir>/claude as <BinDir>/claude-real before the
// shim takes its place. It never destroys anything: the shim itself is not
// archived, and an existing backup is only replaced when it is a stale shim.
func PreserveRealClaude(paths config.Paths, realClaudePath string) error {
	if realClaudePath == "" {
		return nil
	}
	defaultClaude := filepath.Join(paths.BinDir, "claude")
	if !samePath(realClaudePath, defaultClaude) {
		return nil
	}
	// Archiving our own shim would overwrite the backup of the genuine binary
	// with a symlink pointing back at clother — an unrecoverable loop.
	if IsClotherShim(paths, defaultClaude) {
		return nil
	}

	preserved := filepath.Join(paths.BinDir, "claude-real")
	if samePath(defaultClaude, preserved) {
		return nil
	}

	if _, err := os.Lstat(preserved); err == nil {
		// A backup already exists. Only a stale shim may be replaced; a real
		// binary is left alone (launchers.Sync then moves the current file
		// aside under a free name instead of deleting it).
		if !IsClotherShim(paths, preserved) {
			return nil
		}
		if err := os.Remove(preserved); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	return os.Rename(defaultClaude, preserved)
}

// IsClotherShim reports whether path is a symlink installed by Clother, i.e. a
// link resolving to the clother binary rather than to the real claude.
func IsClotherShim(paths config.Paths, path string) bool {
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		return false
	}
	self, _ := os.Executable()
	if isOneOf(path, clotherBinaries(paths, resolvedPath(self))) {
		return true
	}
	// Dangling link (upgraded or moved binary) that still names clother.
	dest, err := os.Readlink(path)
	if err != nil {
		return false
	}
	return filepath.Base(dest) == "clother"
}

// clotherBinaries lists the paths that are known to be the clother binary
// itself: the running executable and the copy installed in BinDir.
func clotherBinaries(paths config.Paths, selfResolved string) []string {
	var known []string
	if selfResolved != "" {
		known = append(known, selfResolved)
	}
	if paths.BinDir != "" {
		known = append(known, filepath.Join(paths.BinDir, "clother"))
	}
	return known
}

func isOneOf(path string, candidates []string) bool {
	for _, candidate := range candidates {
		if samePath(path, candidate) {
			return true
		}
	}
	return false
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

// samePath reports whether two paths designate the same file.
//
// The comparison is case sensitive: on Linux and on case-sensitive macOS
// volumes `claude` and `CLAUDE` are two different files, and treating them as
// one made Clother skip a legitimate binary or skip its backup. When both paths
// exist, os.SameFile answers the question exactly — including on
// case-insensitive volumes, where the two spellings are the same inode.
func samePath(left, right string) bool {
	if left == "" || right == "" {
		return false
	}
	if resolvedPath(left) == resolvedPath(right) {
		return true
	}
	leftInfo, err := os.Stat(left)
	if err != nil {
		return false
	}
	rightInfo, err := os.Stat(right)
	if err != nil {
		return false
	}
	return os.SameFile(leftInfo, rightInfo)
}
