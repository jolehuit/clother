package commands

import (
	"context"
	"os"
	"os/exec"

	"github.com/jolehuit/clother/internal/runtime"
)

func runUpdate(ctx context.Context, c Context) (int, error) {
	if runtime.IsHomebrew() {
		brew, err := exec.LookPath("brew")
		if err != nil {
			return 1, err
		}
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
