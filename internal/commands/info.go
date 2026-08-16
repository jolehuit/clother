package commands

import (
	"context"
	"fmt"

	"github.com/jolehuit/clother/internal/profiles"
	"github.com/jolehuit/clother/internal/providers"
)

func runInfo(_ context.Context, c Context, args []string) (int, error) {
	if len(args) == 0 {
		return 1, fmt.Errorf("usage: clother info <provider> [provider...]")
	}
	// Every argument is honoured: `clother info zai kimi` used to display zai
	// only and drop the rest without a word.
	targets := make([]profiles.Target, 0, len(args))
	for _, name := range args {
		target, err := resolveTargetArg(c, name)
		if err != nil {
			return 1, err
		}
		targets = append(targets, target)
	}

	if c.Output.Machine() {
		var payload any = targets
		if len(targets) == 1 {
			payload = targets[0]
		}
		return 0, c.Output.Emit("info", payload)
	}

	for i, target := range targets {
		if i > 0 {
			fmt.Fprintln(c.Output.Stdout)
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
			if target.CredentialEnvVar != "" && target.CredentialEnvVar != providers.DefaultCredentialEnvVar {
				fmt.Fprintf(c.Output.Stdout, "Exported as: %s\n", target.CredentialEnvVar)
			}
		}
		// The catalog carries warnings that cost real money when ignored (the
		// sk-sp- key Alibaba requires to stay on the Coding Plan, the /api/v3
		// path that does not consume the VolcEngine quota, the providers whose
		// base URL must not end in /v1). Nothing displayed them until now.
		printProviderNotes(c, target.Profile)
	}
	return 0, nil
}

func printProviderNotes(c Context, profile string) {
	provider, ok := c.Catalog.Get(profile)
	if !ok {
		return
	}
	if len(provider.Setup) > 0 {
		fmt.Fprintln(c.Output.Stdout, "Setup:")
		for _, line := range provider.Setup {
			fmt.Fprintf(c.Output.Stdout, "  - %s\n", line)
		}
	}
	if len(provider.Usage) > 0 {
		fmt.Fprintln(c.Output.Stdout, "Usage:")
		for _, line := range provider.Usage {
			fmt.Fprintf(c.Output.Stdout, "  - %s\n", line)
		}
	}
	if provider.DocURL != "" {
		fmt.Fprintf(c.Output.Stdout, "Docs:        %s", provider.DocURL)
		if provider.VerifiedAt != "" {
			fmt.Fprintf(c.Output.Stdout, " (verified %s)", provider.VerifiedAt)
		}
		fmt.Fprintln(c.Output.Stdout)
	}
}
