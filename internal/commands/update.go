package commands

import (
	"context"
	"io"
	"os"
	"os/exec"

	"github.com/jolehuit/clother/internal/runtime"
)

func runUpdate(ctx context.Context, c Context) (int, error) {
	if brew, ok := brewOwnsClother(ctx); ok {
		cmd := exec.CommandContext(ctx, brew, "upgrade", "clother")
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			if exit, ok := err.(*exec.ExitError); ok {
				return exit.ExitCode(), nil
			}
			return 1, err
		}
		return 0, nil
	}
	return runInstall(ctx, c)
}

// brewOwnsClother reports whether `brew upgrade clother` can actually work: the
// binary must sit in a Cellar AND brew must know a formula by that name.
// Checking only the path yields `Error: No available formula with the name
// clother` for anyone who copied the binary next to a Homebrew tree, and the
// user is left with no way to update at all. Falling back to the direct
// download is always safe.
func brewOwnsClother(ctx context.Context) (string, bool) {
	if !runtime.IsHomebrew() {
		return "", false
	}
	brew, err := exec.LookPath("brew")
	if err != nil {
		return "", false
	}
	cmd := exec.CommandContext(ctx, brew, "list", "--formula", "clother")
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		return "", false
	}
	return brew, true
}
