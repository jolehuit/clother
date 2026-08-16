package commands

import (
	"context"
	"fmt"

	"github.com/jolehuit/clother/internal/cli"
)

func Dispatch(ctx context.Context, c Context, command string, args []string) (int, error) {
	switch command {
	case "":
		cli.ShowBrief(c.Output.Stdout)
		return 0, nil
	case "config":
		return runConfig(ctx, c, args)
	case "remove":
		return runRemove(ctx, c, args)
	case "list":
		if err := rejectArgs(command, args); err != nil {
			return 1, err
		}
		return runList(ctx, c)
	case "info":
		return runInfo(ctx, c, args)
	case "test":
		return runTest(ctx, c, args)
	case "bench":
		return runBench(ctx, c, args)
	case "status":
		if err := rejectArgs(command, args); err != nil {
			return 1, err
		}
		return runStatus(ctx, c)
	case "install":
		if err := rejectArgs(command, args); err != nil {
			return 1, err
		}
		return runInstall(ctx, c)
	case "update":
		if err := rejectArgs(command, args); err != nil {
			return 1, err
		}
		return runUpdate(ctx, c)
	case "uninstall":
		if err := rejectArgs(command, args); err != nil {
			return 1, err
		}
		return runUninstall(ctx, c)
	case "help":
		// `clother help bench` is advertised by the command table alongside
		// `clother bench --help`; it used to dump the 60-line global help.
		if len(args) > 0 {
			if cli.ShowCommand(c.Output.Stdout, args[0], c.Catalog) {
				return 0, nil
			}
			cli.ShowFull(c.Output.Stdout, c.Catalog)
			return 1, fmt.Errorf("unknown command %q", args[0])
		}
		cli.ShowFull(c.Output.Stdout, c.Catalog)
		return 0, nil
	default:
		// Every other refusal in Clother names the command to run next; this one
		// left the user with a dead end.
		return 1, fmt.Errorf("unknown command %q — run `clother help` to see the available commands", command)
	}
}

// rejectArgs refuses the extra positional arguments that used to be swallowed
// in silence (`clother status extraarg` exited 0 without a word).
func rejectArgs(command string, args []string) error {
	if len(args) == 0 {
		return nil
	}
	return fmt.Errorf("%s takes no arguments (got %q)", command, args[0])
}
