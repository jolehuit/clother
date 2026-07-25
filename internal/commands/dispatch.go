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
	case "list":
		return runList(ctx, c)
	case "info":
		return runInfo(ctx, c, args)
	case "test":
		return runTest(ctx, c, args)
	case "bench":
		return runBench(ctx, c, args)
	case "status":
		return runStatus(ctx, c)
	case "install":
		return runInstall(ctx, c)
	case "update":
		return runUpdate(ctx, c)
	case "uninstall":
		return runUninstall(ctx, c)
	case "help":
		cli.ShowFull(c.Output.Stdout, c.Catalog)
		return 0, nil
	default:
		return 1, fmt.Errorf("unknown command %q", command)
	}
}
