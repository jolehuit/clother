package commands

import (
	"context"
	"fmt"

	"github.com/jolehuit/clother/internal/profiles"
	"github.com/jolehuit/clother/internal/version"
)

func runStatus(_ context.Context, c Context) (int, error) {
	targets := profiles.All(c.Catalog, c.Config)
	if c.Output.Machine() {
		// One envelope per invocation, the same shape errors already use.
		return 0, c.Output.Emit("status", map[string]any{
			"version":  version.Value,
			"config":   c.Paths.ConfigDir,
			"data":     c.Paths.DataDir,
			"bin":      c.Paths.BinDir,
			"profiles": len(targets),
		})
	}
	c.Output.Header("Clother Status")
	fmt.Fprintf(c.Output.Stdout, "Version:   %s\n", version.Value)
	fmt.Fprintf(c.Output.Stdout, "Config:    %s\n", c.Paths.ConfigDir)
	fmt.Fprintf(c.Output.Stdout, "Data:      %s\n", c.Paths.DataDir)
	fmt.Fprintf(c.Output.Stdout, "Bin:       %s\n", c.Paths.BinDir)
	fmt.Fprintf(c.Output.Stdout, "Profiles:  %d\n", len(targets))
	return 0, nil
}
