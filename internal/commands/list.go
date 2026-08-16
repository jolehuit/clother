package commands

import (
	"context"
	"fmt"
	"strings"

	"github.com/jolehuit/clother/internal/config"
	"github.com/jolehuit/clother/internal/profiles"
	"github.com/jolehuit/clother/internal/providers"
)

func runList(_ context.Context, c Context) (int, error) {
	targets := profiles.All(c.Catalog, c.Config)
	// The machine branch is keyed on the Output, which is the single authority
	// on the rendering mode, so the envelope cannot be skipped by a Context
	// whose Options and Output disagree.
	switch {
	case c.Output.Machine():
		type item struct {
			Name       string `json:"name"`
			Command    string `json:"command"`
			Configured bool   `json:"configured"`
		}
		payload := struct {
			Profiles []item `json:"profiles"`
		}{}
		for _, target := range targets {
			payload.Profiles = append(payload.Profiles, item{
				Name:       target.Profile,
				Command:    "clother-" + target.Profile,
				Configured: configured(target, c.Secrets),
			})
		}
		if err := c.Output.Emit("list", payload); err != nil {
			return 1, err
		}
	case c.Options.Format == "plain":
		for _, target := range targets {
			fmt.Fprintln(c.Output.Stdout, target.Profile)
		}
	default:
		c.Output.Header(fmt.Sprintf("Available Profiles (%d)", len(targets)))
		for _, target := range targets {
			status := "configured"
			if !configured(target, c.Secrets) {
				status = "not configured"
			}
			fmt.Fprintf(c.Output.Stdout, "  %-18s %s\n", target.Profile, status)
		}
		if len(targets) > 0 {
			fmt.Fprintln(c.Output.Stdout)
			fmt.Fprintln(c.Output.Stdout, "Run: clother-<name>")
		}
	}
	return 0, nil
}

func configured(target profiles.Target, secrets config.Secrets) bool {
	switch target.AuthMode {
	case providers.AuthNone, providers.AuthLiteral:
		return true
	case providers.AuthSecret:
		return strings.TrimSpace(secrets[target.SecretKey]) != ""
	default:
		return false
	}
}
