package commands

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/jolehuit/clother/internal/config"
	"github.com/jolehuit/clother/internal/launchers"
	"github.com/jolehuit/clother/internal/providers"
	"github.com/jolehuit/clother/internal/runtime"
)

var (
	validName = regexp.MustCompile(`^[a-z0-9_-]+$`)
)

func runConfig(_ context.Context, c Context, args []string) (int, error) {
	providerID := ""
	if len(args) > 0 {
		providerID = args[0]
	} else {
		var err error
		providerID, err = chooseProvider(c)
		if err != nil || providerID == "" {
			return 0, err
		}
	}

	switch providerID {
	case "openrouter":
		return configOpenRouter(c)
	case "custom":
		return configCustom(c)
	default:
		if provider, ok := c.Catalog.Get(providerID); ok {
			return configBuiltin(c, provider)
		}
		return 1, fmt.Errorf("unknown provider %q", providerID)
	}
}

func chooseProvider(c Context) (string, error) {
	index := 1
	choices := map[int]string{}
	c.Output.Header("Clother Configuration")
	for _, category := range c.Catalog.Categories() {
		fmt.Fprintln(c.Output.Stdout, category)
		for _, provider := range c.Catalog.ProvidersByCategory(category) {
			fmt.Fprintf(c.Output.Stdout, "  %2d. %-14s %s\n", index, provider.ID, provider.Description)
			choices[index] = provider.ID
			index++
		}
	}
	fmt.Fprintf(c.Output.Stdout, "  %2d. %-14s %s\n", index, "openrouter", "100+ models")
	choices[index] = "openrouter"
	index++
	fmt.Fprintf(c.Output.Stdout, "  %2d. %-14s %s\n", index, "custom", "Anthropic-compatible endpoint")
	choices[index] = "custom"

	answer, err := c.Prompt.Prompt("Choose provider number", "")
	if err != nil {
		return "", err
	}
	for number, providerID := range choices {
		if fmt.Sprint(number) == answer {
			return providerID, nil
		}
	}
	return "", fmt.Errorf("invalid choice %q", answer)
}

func configBuiltin(c Context, provider providers.Provider) (int, error) {
	if provider.AuthMode == providers.AuthSecret {
		current := c.Secrets[provider.KeyVar]
		if current != "" {
			fmt.Fprintf(c.Output.Stdout, "Current key: %s\n", config.MaskSecret(current))
		}
		label := "API key"
		if current != "" {
			label = "API key (empty to keep current)"
		}
		value, err := c.Prompt.PromptSecret(label)
		if err != nil {
			return 1, err
		}
		if strings.TrimSpace(value) != "" {
			c.Secrets[provider.KeyVar] = value
		}
	}

	if provider.DefaultModel != "" {
		fmt.Fprintln(c.Output.Stdout, "Choose model:")
		for idx, choice := range provider.ModelChoices {
			fmt.Fprintf(c.Output.Stdout, "  %d. %-24s %s\n", idx+1, choice.ID, choice.Description)
		}
		defaultValue := provider.DefaultModel
		if override := c.Config.ProviderOverrides[provider.ID]; override.Model != "" {
			defaultValue = override.Model
		}
		answer, err := c.Prompt.Prompt("Model", defaultValue)
		if err != nil {
			return 1, err
		}
		answer = resolveModelChoice(answer, provider.ModelChoices)
		if answer != "" && answer != provider.DefaultModel {
			c.Config.ProviderOverrides[provider.ID] = config.ProviderOverride{Model: answer}
		} else {
			delete(c.Config.ProviderOverrides, provider.ID)
		}
	}
	return persistConfig(c)
}

func configOpenRouter(c Context) (int, error) {
	current := c.Secrets["OPENROUTER_API_KEY"]
	if current != "" {
		fmt.Fprintf(c.Output.Stdout, "Current key: %s\n", config.MaskSecret(current))
	}
	value, err := c.Prompt.PromptSecret("OpenRouter API key (empty to keep current)")
	if err != nil {
		return 1, err
	}
	if strings.TrimSpace(value) != "" {
		c.Secrets["OPENROUTER_API_KEY"] = value
	}
	for {
		model, err := c.Prompt.Prompt("Model ID (empty to stop)", "")
		if err != nil {
			return 1, err
		}
		if strings.TrimSpace(model) == "" {
			break
		}
		name, err := c.Prompt.Prompt("Alias", defaultAliasName(model))
		if err != nil {
			return 1, err
		}
		if !validName.MatchString(name) {
			return 1, fmt.Errorf("invalid alias %q", name)
		}
		c.Config.OpenRouterAliases[name] = model
	}
	return persistConfig(c)
}

func configCustom(c Context) (int, error) {
	name, err := c.Prompt.Prompt("Provider name", "")
	if err != nil {
		return 1, err
	}
	if !validName.MatchString(name) {
		return 1, fmt.Errorf("invalid provider name %q", name)
	}
	baseURL, err := c.Prompt.Prompt("Base URL", "")
	if err != nil {
		return 1, err
	}
	defaultModel, err := c.Prompt.Prompt("Default model (optional)", "")
	if err != nil {
		return 1, err
	}
	apiKey, err := c.Prompt.PromptSecret("API key")
	if err != nil {
		return 1, err
	}
	keyVar := strings.ToUpper(strings.ReplaceAll(name, "-", "_")) + "_API_KEY"
	c.Secrets[keyVar] = apiKey
	c.Config.CustomProviders[name] = config.CustomProvider{
		Name:         name,
		DisplayName:  name,
		BaseURL:      baseURL,
		APIKeyEnv:    keyVar,
		DefaultModel: strings.TrimSpace(defaultModel),
	}
	return persistConfig(c)
}

func persistConfig(c Context) (int, error) {
	config.NormalizeLegacySecrets(c.Secrets, c.Catalog)
	if err := config.SaveConfig(c.Paths.ConfigFile, c.Config); err != nil {
		return 1, err
	}
	if err := config.SaveSecrets(c.Paths.SecretsFile, c.Secrets); err != nil {
		return 1, err
	}
	execPath, execErr := os.Executable()
	if execErr != nil {
		return 1, execErr
	}
	if err := launchers.Sync(execPath, c.Paths, c.Catalog, c.Config, runtime.IsHomebrew()); err != nil {
		return 1, err
	}
	c.Output.Success("configuration saved")
	return 0, nil
}

func defaultAliasName(model string) string {
	model = strings.ToLower(model)
	if slash := strings.LastIndex(model, "/"); slash >= 0 {
		model = model[slash+1:]
	}
	model = strings.ReplaceAll(model, ".", "-")
	model = strings.ReplaceAll(model, "_", "-")
	return model
}

func resolveModelChoice(answer string, choices []providers.ModelChoice) string {
	answer = strings.TrimSpace(answer)
	if idx, err := strconv.Atoi(answer); err == nil {
		if idx >= 1 && idx <= len(choices) {
			return choices[idx-1].ID
		}
	}
	return answer
}
