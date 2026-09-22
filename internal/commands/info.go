package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jolehuit/clother/internal/profiles"
	"github.com/jolehuit/clother/internal/providers"
)

func runInfo(_ context.Context, c Context, args []string) (int, error) {
	if len(args) == 0 {
		return 1, fmt.Errorf("usage: clother info <provider>")
	}
	target, err := profiles.Resolve(args[0], c.Catalog, c.Config)
	if err != nil {
		return 1, err
	}
	tiers := profiles.EffectiveTiers(target)
	if c.Options.Format == "json" {
		data, _ := json.MarshalIndent(struct {
			profiles.Target
			EffectiveTiers map[string]string
		}{target, tiers}, "", "  ")
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
	if line := formatTiers(tiers); line != "" {
		fmt.Fprintf(c.Output.Stdout, "Tiers:       %s\n", line)
	}
	if subagent := tiers[providers.TierSubagent]; subagent != "" {
		fmt.Fprintf(c.Output.Stdout, "Subagents:   %s\n", subagent)
	}
	if target.SecretKey != "" {
		status := "configured"
		if c.Secrets[target.SecretKey] == "" {
			status = "not configured"
			if target.AuthMode == providers.AuthLiteral {
				status = "not set, optional"
			}
		}
		via := target.CredentialEnvVar
		if via == "" {
			via = providers.AuthTokenEnvVar
		}
		fmt.Fprintf(c.Output.Stdout, "Credential:  %s (%s), sent as %s\n", target.SecretKey, status, via)
	}
	return 0, nil
}

func formatTiers(tiers map[string]string) string {
	var parts []string
	for _, tier := range []string{providers.TierOpus, providers.TierSonnet, providers.TierHaiku, providers.TierFable} {
		if model := tiers[tier]; model != "" {
			parts = append(parts, tier+"="+model)
		}
	}
	return strings.Join(parts, "  ")
}
