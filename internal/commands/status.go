package commands

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jolehuit/clother/internal/profiles"
	"github.com/jolehuit/clother/internal/version"
)

func runStatus(_ context.Context, c Context) (int, error) {
	targets := profiles.All(c.Catalog, c.Config)
	if c.Options.Format == "json" {
		payload := map[string]any{
			"version":  version.Value,
			"config":   c.Paths.ConfigDir,
			"data":     c.Paths.DataDir,
			"bin":      c.Paths.BinDir,
			"profiles": len(targets),
		}
		data, _ := json.MarshalIndent(payload, "", "  ")
		fmt.Fprintln(c.Output.Stdout, string(data))
		return 0, nil
	}
	c.Output.Header("Clother Status")
	fmt.Fprintf(c.Output.Stdout, "Version:   %s\n", version.Value)
	fmt.Fprintf(c.Output.Stdout, "Config:    %s\n", c.Paths.ConfigDir)
	fmt.Fprintf(c.Output.Stdout, "Data:      %s\n", c.Paths.DataDir)
	fmt.Fprintf(c.Output.Stdout, "Bin:       %s\n", c.Paths.BinDir)
	fmt.Fprintf(c.Output.Stdout, "Profiles:  %d\n", len(targets))
	return 0, nil
}
