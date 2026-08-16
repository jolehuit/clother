package commands

import (
	"context"
	"fmt"
	"strings"

	"github.com/jolehuit/clother/internal/providers"
)

// removalPlan describes what `clother remove <name>` would delete. Nothing is
// ever removed implicitly elsewhere: this is the only path that drops a stored
// credential.
type removalPlan struct {
	description string
	secretKeys  []string
	apply       func(c Context)
}

func runRemove(_ context.Context, c Context, args []string) (int, error) {
	if len(args) == 0 {
		return 1, fmt.Errorf("usage: clother remove <provider>")
	}
	if len(args) > 1 {
		return 1, fmt.Errorf("remove takes a single provider (got %q and %q)", args[0], args[1])
	}
	name := strings.TrimSpace(args[0])

	plan, err := buildRemovalPlan(c, name)
	if err != nil {
		return 1, err
	}

	present := false
	for _, key := range plan.secretKeys {
		if c.Secrets[key] != "" {
			present = true
		}
	}
	if !present && plan.description == "" {
		c.Output.Line("nothing configured for %s", name)
		return 0, nil
	}

	if !c.Options.Yes {
		if !interactiveAvailable(c) {
			return 1, fmt.Errorf("refusing to remove %s without confirmation: pass --yes", name)
		}
		ok, err := c.Prompt.ConfirmDestructive(fmt.Sprintf("Remove %s?", plan.description))
		if err != nil {
			return 1, err
		}
		if !ok {
			return 0, nil
		}
	}

	var changes []string
	for _, key := range plan.secretKeys {
		if _, ok := c.Secrets[key]; ok {
			delete(c.Secrets, key)
			changes = append(changes, "removed "+key)
		}
	}
	if plan.apply != nil {
		plan.apply(c)
	}
	changes = append(changes, "removed "+plan.description)
	return persistConfig(c, changes...)
}

func buildRemovalPlan(c Context, name string) (removalPlan, error) {
	switch {
	case name == "":
		return removalPlan{}, fmt.Errorf("usage: clother remove <provider>")

	case name == "openrouter":
		return removalPlan{
			description: "the OpenRouter key and every alias",
			secretKeys:  []string{"OPENROUTER_API_KEY"},
			apply: func(c Context) {
				for alias := range c.Config.OpenRouterAliases {
					delete(c.Config.OpenRouterAliases, alias)
				}
			},
		}, nil

	case strings.HasPrefix(name, "or-"):
		alias := strings.TrimPrefix(name, "or-")
		if _, ok := c.Config.OpenRouterAliases[alias]; !ok {
			// The launcher path already names the next command for this exact
			// failure; leaving this one a dead end made the two disagree.
			return removalPlan{}, fmt.Errorf("unknown OpenRouter alias %q — run `clother list` to see the configured aliases", alias)
		}
		return removalPlan{
			description: "the OpenRouter alias " + alias,
			apply: func(c Context) {
				delete(c.Config.OpenRouterAliases, alias)
				c.Output.Line("OPENROUTER_API_KEY is shared by all aliases and was kept; run `clother remove openrouter` to drop it")
			},
		}, nil
	}

	if provider, ok := c.Catalog.Get(name); ok {
		plan := removalPlan{
			description: "the configuration of " + provider.ID,
			apply: func(c Context) {
				delete(c.Config.ProviderOverrides, provider.ID)
			},
		}
		if provider.AuthMode == providers.AuthSecret && provider.KeyVar != "" {
			plan.secretKeys = []string{provider.KeyVar}
			plan.description = "the API key and settings of " + provider.ID
		}
		return plan, nil
	}

	if custom, ok := c.Config.CustomProviders[name]; ok {
		return removalPlan{
			description: "the custom provider " + name,
			secretKeys:  []string{custom.APIKeyEnv},
			apply: func(c Context) {
				delete(c.Config.CustomProviders, name)
			},
		}, nil
	}

	return removalPlan{}, fmt.Errorf("unknown provider %q — run `clother list` to see what is configured", name)
}
