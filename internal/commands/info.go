package commands

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jolehuit/clother/internal/profiles"
)

func runInfo(_ context.Context, c Context, args []string) (int, error) {
	if len(args) == 0 {
		return 1, fmt.Errorf("usage: clother info <provider>")
	}
	target, err := profiles.Resolve(args[0], c.Catalog, c.Config)
	if err != nil {
		return 1, err
	}
	if c.Options.Format == "json" {
		data, _ := json.MarshalIndent(target, "", "  ")
		fmt.Fprintln(c.Output.Stdout, string(data))
		return 0, nil
	}
	c.Output.Header("Provider Info")
	fmt.Fprintf(c.Output.Stdout, "Profile:     %s\n", target.Profile)
	fmt.Fprintf(c.Output.Stdout, "Name:        %s\n", target.DisplayName)
	fmt.Fprintf(c.Output.Stdout, "Family:      %s\n", target.Family)
	fmt.Fprintf(c.Output.Stdout, "Base URL:    %s\n", target.BaseURL)
	if target.Model != "" {
		fmt.Fprintf(c.Output.Stdout, "Model:       %s\n", target.Model)
	}
	if target.SecretKey != "" {
		status := "configured"
		if c.Secrets[target.SecretKey] == "" {
			status = "not configured"
		}
		fmt.Fprintf(c.Output.Stdout, "Credential:  %s (%s)\n", target.SecretKey, status)
	}
	return 0, nil
}
