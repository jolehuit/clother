package commands

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jolehuit/clother/internal/config"
	"github.com/jolehuit/clother/internal/launchers"
	"github.com/jolehuit/clother/internal/runtime"
)

func runUninstall(_ context.Context, c Context) (int, error) {
	binEntries := uninstallBinEntries(c)
	dirs, skipped := uninstallDirs(c)

	// Always show what is about to disappear, -y included: the directories come
	// from CLOTHER_CONFIG_DIR & co, so the user is the only one who can tell a
	// legitimate path from a typo.
	fmt.Fprintln(c.Output.Stdout, "The following paths will be removed:")
	if len(binEntries) == 0 && len(dirs) == 0 {
		fmt.Fprintln(c.Output.Stdout, "  (nothing to remove)")
	}
	for _, path := range append(append([]string{}, binEntries...), dirs...) {
		fmt.Fprintf(c.Output.Stdout, "  %s\n", path)
	}
	for _, reason := range skipped {
		c.Output.Warn("skipping %s", reason)
	}

	if !c.Options.Yes {
		ok, err := c.Prompt.ConfirmDestructive("Remove all Clother files?")
		if err != nil {
			return 1, err
		}
		if !ok {
			return 0, nil
		}
	}

	failures := 0
	shimRemoved := false
	shim := filepath.Join(c.Paths.BinDir, "claude")
	for _, path := range binEntries {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			c.Output.Warn("could not remove %s: %v", path, err)
			failures++
			continue
		}
		if path == shim {
			shimRemoved = true
		}
	}
	for _, dir := range dirs {
		if err := removeManagedDir(c, dir); err != nil {
			c.Output.Warn("could not remove %s: %v", dir, err)
			failures++
		}
	}

	restoreRealClaude(c, shimRemoved)

	fmt.Fprintln(c.Output.Stdout, "Clother uninstalled")
	if failures > 0 {
		return 1, fmt.Errorf("uninstall incomplete: %d path(s) could not be removed", failures)
	}
	return 0, nil
}

// uninstallBinEntries lists the files Clother may delete from BinDir. BinDir is
// shared with other tools, so only our own symlinks (and the clother binary
// itself) qualify; anything else is left untouched.
func uninstallBinEntries(c Context) []string {
	var entries []string
	seen := map[string]struct{}{}
	add := func(path string) {
		if _, ok := seen[path]; ok {
			return
		}
		seen[path] = struct{}{}
		entries = append(entries, path)
	}

	manifest, _ := launchers.LoadManifest(c.Paths.ManifestFile)
	for _, name := range manifest.Launchers {
		if !launchers.ValidLauncherName(name) {
			continue
		}
		path := filepath.Join(c.Paths.BinDir, name)
		if runtime.IsClotherShim(c.Paths, path) {
			add(path)
		}
	}
	claude := filepath.Join(c.Paths.BinDir, "claude")
	if runtime.IsClotherShim(c.Paths, claude) {
		add(claude)
	}
	// Under Homebrew, Sync never copies the binary into BinDir: anything named
	// clother there belongs to brew and must be left to `brew uninstall`.
	if !runtime.IsHomebrew() {
		clother := filepath.Join(c.Paths.BinDir, "clother")
		if _, err := os.Lstat(clother); err == nil {
			add(clother)
		}
	}
	return entries
}

// uninstallDirs returns the directories that may be removed recursively, plus a
// human-readable reason for each directory that is refused.
func uninstallDirs(c Context) ([]string, []string) {
	var dirs, skipped []string
	for _, dir := range []string{c.Paths.ConfigDir, c.Paths.DataDir, c.Paths.CacheDir} {
		if dir == "" {
			continue
		}
		if _, err := os.Lstat(dir); os.IsNotExist(err) {
			continue
		}
		if err := config.SafeToRemoveDir(dir); err != nil {
			skipped = append(skipped, fmt.Sprintf("%v — remove it by hand if you really meant it", err))
			continue
		}
		dirs = append(dirs, filepath.Clean(dir))
	}
	return dirs, skipped
}

// removeManagedDir deletes the files Clother created in dir, then the directory
// itself — but only when nothing else is left in it.
//
// The previous code called os.RemoveAll on dir. The directories come straight
// from CLOTHER_CONFIG_DIR & co and EnsureBaseDirs stamps its own marker into
// whatever they name, so a typo in a shell profile made `clother uninstall -y`
// delete the user's own files: pointing CLOTHER_CONFIG_DIR at ~/Documents and
// running install then uninstall wiped ~/Documents. Removing only what Clother
// wrote makes the mistake recoverable instead of terminal.
func removeManagedDir(c Context, dir string) error {
	for _, entry := range c.Paths.ManagedEntries(dir) {
		if err := os.RemoveAll(entry); err != nil {
			return err
		}
	}
	err := os.Remove(dir)
	switch {
	case err == nil, os.IsNotExist(err):
		return nil
	default:
		// Not empty: something in there is not ours. Say so rather than
		// deleting it, and keep the exit code clean — nothing failed.
		c.Output.Warn("kept %s: it still holds files Clother did not create", dir)
		return nil
	}
}

// restoreRealClaude puts the genuine claude binary back where it was found at
// install time. Uninstalling a wrapper must never leave the wrapped tool
// missing from the PATH.
//
// shimRemoved says whether this run actually deleted a `claude` shim. Without
// it the "no backup was found" warning also fired when Clother had never
// installed anything, telling the user to reinstall Claude Code for no reason.
func restoreRealClaude(c Context, shimRemoved bool) {
	claude := filepath.Join(c.Paths.BinDir, "claude")
	preserved := filepath.Join(c.Paths.BinDir, "claude-real")

	if _, err := os.Lstat(preserved); err != nil {
		if _, err := os.Lstat(claude); os.IsNotExist(err) && shimRemoved {
			c.Output.Warn("the `claude` shim was removed and no backup was found; reinstall Claude Code if `claude` is no longer found")
		}
		return
	}
	if _, err := os.Lstat(claude); err == nil {
		c.Output.Line("kept the backup %s (a claude binary already sits at %s)", preserved, claude)
		return
	}
	if err := os.Rename(preserved, claude); err != nil {
		c.Output.Warn("could not restore the original claude binary from %s: %v", preserved, err)
		return
	}
	c.Output.Success("restored the original claude binary at %s", claude)
}
