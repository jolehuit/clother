package commands

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/jolehuit/clother/internal/config"
	"github.com/jolehuit/clother/internal/launchers"
	"github.com/jolehuit/clother/internal/providers"
	"github.com/jolehuit/clother/internal/runtime"
	"github.com/jolehuit/clother/internal/ui"
)

// Names are validated against the config package so the interactive path, the
// legacy migrations and Normalize cannot diverge (a name is both a launcher
// file name and, for custom providers, the root of an env var).
var (
	validName         = config.ValidName
	validProviderName = config.ValidProviderName
)

// Environment variables forming the scriptable, non-interactive path of
// `clother config`. They exist because there is no way to pass a key on the
// command line, and because a piped `clother config <provider>` used to report
// success without writing anything.
const (
	envConfigAPIKey       = "CLOTHER_API_KEY"
	envConfigModel        = "CLOTHER_MODEL"
	envConfigBaseURL      = "CLOTHER_BASE_URL"
	envConfigAlias        = "CLOTHER_ALIAS"
	envConfigProviderName = "CLOTHER_PROVIDER_NAME"
)

// defaultStdinIsTerminal reports whether stdin is a terminal a human can type
// into.
//
// A character device is not enough on its own: /dev/null is one too, so
// `clother config zai </dev/null` — the classic CI and Dockerfile shape — would
// be taken for an interactive session. The name of the device is checked as
// well, which covers the redirections that actually occur in scripts.
func defaultStdinIsTerminal() bool {
	info, err := os.Stdin.Stat()
	if err != nil || info.Mode()&os.ModeCharDevice == 0 {
		return false
	}
	return !isNullDevice(os.Stdin)
}

// isNullDevice reports whether file is the null device, which is a character
// device that no human is typing into.
func isNullDevice(file *os.File) bool {
	nullInfo, err := os.Stat(os.DevNull)
	if err != nil {
		return false
	}
	info, err := file.Stat()
	if err != nil {
		return false
	}
	return os.SameFile(info, nullInfo)
}

// scriptedInputPresent reports whether the caller supplied the configuration
// through the environment. It short-circuits terminal detection: a script that
// exports CLOTHER_API_KEY means to be non-interactive, whatever stdin happens
// to be attached to.
func scriptedInputPresent() bool {
	for _, name := range []string{envConfigAPIKey, envConfigModel, envConfigBaseURL, envConfigAlias, envConfigProviderName} {
		if _, ok := os.LookupEnv(name); ok && strings.TrimSpace(os.Getenv(name)) != "" {
			return true
		}
	}
	return false
}

// stdinIsTerminal is a variable so tests can drive both paths.
var stdinIsTerminal = defaultStdinIsTerminal

// stdinReader is the stream `CLOTHER_API_KEY=-` reads the key from.
var stdinReader io.Reader = os.Stdin

func runConfig(_ context.Context, c Context, args []string) (int, error) {
	if len(args) > 1 {
		return 1, fmt.Errorf("config takes a single provider (got %q and %q)", args[0], args[1])
	}
	providerID := ""
	if len(args) > 0 {
		providerID = args[0]
	}

	if !interactiveAvailable(c) {
		return configNonInteractive(c, providerID)
	}

	if providerID == "" {
		var err error
		providerID, err = chooseProvider(c)
		if err != nil || providerID == "" {
			return 0, err
		}
	}

	switch providerID {
	case "openrouter":
		return withScriptableHint(configOpenRouter(c))
	case "custom":
		return withScriptableHint(configCustom(c))
	default:
		if provider, ok := c.Catalog.Get(providerID); ok {
			return withScriptableHint(configBuiltin(c, provider))
		}
		return 1, fmt.Errorf("unknown provider %q — run `clother list` to see the available providers", providerID)
	}
}

// withScriptableHint turns "no input available" into a message that names the
// way out. The interactive path is still attempted when stdin looks like a
// terminal, and it may fail late — inside the prompt — in an environment that
// has no controlling terminal at all.
func withScriptableHint(code int, err error) (int, error) {
	if err == nil {
		return code, nil
	}
	if !errors.Is(err, ui.ErrNoInput) && !errors.Is(err, ui.ErrNoTerminal) {
		return code, err
	}
	return code, fmt.Errorf("%w — for a script, set %s (or %s=- to read the key from stdin) and re-run", err, envConfigAPIKey, envConfigAPIKey)
}

func interactiveAvailable(c Context) bool {
	if c.Options.NoInput {
		return false
	}
	if scriptedInputPresent() {
		return false
	}
	return stdinIsTerminal()
}

// configNonInteractive is the scriptable path: no prompt is ever issued and the
// command fails loudly instead of pretending to have saved something.
func configNonInteractive(c Context, providerID string) (int, error) {
	if providerID == "" {
		return 1, fmt.Errorf("clother config needs a provider when stdin is not a terminal: clother config <provider>, with the key in %s (or %s=- to read it from stdin) and, optionally, %s / %s",
			envConfigAPIKey, envConfigAPIKey, envConfigModel, envConfigBaseURL)
	}

	apiKey, err := nonInteractiveAPIKey()
	if err != nil {
		return 1, err
	}
	model := strings.TrimSpace(os.Getenv(envConfigModel))
	baseURL := strings.TrimSpace(os.Getenv(envConfigBaseURL))

	switch providerID {
	case "openrouter":
		return configOpenRouterNonInteractive(c, apiKey, model)
	case "custom":
		return configCustomNonInteractive(c, apiKey, model, baseURL)
	}

	provider, ok := c.Catalog.Get(providerID)
	if !ok {
		return 1, fmt.Errorf("unknown provider %q — run `clother list` to see the available providers", providerID)
	}

	var changes []string
	if provider.AuthMode == providers.AuthSecret {
		switch {
		case apiKey != "":
			c.Secrets[provider.KeyVar] = apiKey
			changes = append(changes, provider.KeyVar)
		case c.Secrets[provider.KeyVar] == "":
			return 1, fmt.Errorf("no API key for %s: set %s (or %s=- to read it from stdin) — `clother config %s` on a terminal prompts for it",
				provider.ID, envConfigAPIKey, envConfigAPIKey, provider.ID)
		}
	}

	override := c.Config.ProviderOverrides[provider.ID]
	if model != "" {
		resolved := resolveModelChoice(model, provider.ModelChoices)
		if resolved == provider.DefaultModel {
			override.Model = ""
		} else {
			override.Model = resolved
		}
		changes = append(changes, "model "+resolved)
	}
	if baseURL != "" {
		if !config.ValidBaseURL(baseURL) {
			return 1, fmt.Errorf("invalid base URL %q (must be an absolute http:// or https:// URL)", baseURL)
		}
		trimmed := strings.TrimRight(baseURL, "/")
		warnIfCleartextBaseURL(c, provider.ID, trimmed)
		if trimmed == provider.BaseURL {
			override.BaseURL = ""
		} else {
			override.BaseURL = trimmed
		}
		changes = append(changes, "base URL "+trimmed)
	}
	if override == (config.ProviderOverride{}) {
		delete(c.Config.ProviderOverrides, provider.ID)
	} else {
		c.Config.ProviderOverrides[provider.ID] = override
	}

	if len(changes) == 0 {
		return 1, fmt.Errorf("nothing to configure for %s: set %s, %s or %s", provider.ID, envConfigAPIKey, envConfigModel, envConfigBaseURL)
	}
	return persistConfig(c, changes...)
}

func configOpenRouterNonInteractive(c Context, apiKey, model string) (int, error) {
	var changes []string
	if apiKey != "" {
		c.Secrets["OPENROUTER_API_KEY"] = apiKey
		changes = append(changes, "OPENROUTER_API_KEY")
	} else if c.Secrets["OPENROUTER_API_KEY"] == "" && model == "" {
		return 1, fmt.Errorf("no API key for openrouter: set %s (or %s=- to read it from stdin)", envConfigAPIKey, envConfigAPIKey)
	}
	if model != "" {
		alias := strings.TrimSpace(os.Getenv(envConfigAlias))
		if alias == "" {
			alias = defaultAliasName(model)
		}
		if !validName.MatchString(alias) {
			return 1, fmt.Errorf("invalid alias %q (use lowercase letters, digits, \"-\" or \"_\"; set %s)", alias, envConfigAlias)
		}
		c.Config.OpenRouterAliases[alias] = model
		changes = append(changes, "alias or-"+alias)
	}
	if len(changes) == 0 {
		return 1, fmt.Errorf("nothing to configure for openrouter: set %s or %s", envConfigAPIKey, envConfigModel)
	}
	return persistConfig(c, changes...)
}

func configCustomNonInteractive(c Context, apiKey, model, baseURL string) (int, error) {
	name := strings.TrimSpace(strings.ToLower(os.Getenv(envConfigProviderName)))
	if name == "" {
		return 1, fmt.Errorf("custom providers need a name: set %s", envConfigProviderName)
	}
	if err := checkCustomProviderName(name); err != nil {
		return 1, err
	}
	keyVar := config.CustomProviderKeyVar(name)

	existing := c.Config.CustomProviders[name]
	if baseURL == "" {
		baseURL = existing.BaseURL
	}
	if baseURL == "" {
		return 1, fmt.Errorf("custom providers need a base URL: set %s", envConfigBaseURL)
	}
	if !config.ValidBaseURL(baseURL) {
		return 1, fmt.Errorf("invalid base URL %q (must be an absolute http:// or https:// URL)", baseURL)
	}
	warnIfCleartextBaseURL(c, name, baseURL)
	if model == "" {
		model = existing.DefaultModel
	}

	changes := []string{"custom provider " + name}
	if apiKey != "" {
		c.Secrets[keyVar] = apiKey
		changes = append(changes, keyVar)
	}
	c.Config.CustomProviders[name] = config.CustomProvider{
		Name:         name,
		DisplayName:  name,
		BaseURL:      strings.TrimRight(baseURL, "/"),
		APIKeyEnv:    keyVar,
		DefaultModel: model,
	}
	return persistConfig(c, changes...)
}

// nonInteractiveAPIKey reads the key from CLOTHER_API_KEY, or from stdin when
// the variable is exactly "-" so the key never reaches the process table or the
// shell history.
func nonInteractiveAPIKey() (string, error) {
	raw, ok := os.LookupEnv(envConfigAPIKey)
	if !ok {
		return "", nil
	}
	if strings.TrimSpace(raw) != "-" {
		return strings.TrimSpace(raw), nil
	}
	data, err := io.ReadAll(io.LimitReader(stdinReader, 1<<16))
	if err != nil {
		return "", fmt.Errorf("read API key from stdin: %w", err)
	}
	key := strings.TrimSpace(string(data))
	if key == "" {
		return "", fmt.Errorf("%s=- but stdin was empty — pipe the key in, or set %s to the key itself",
			envConfigAPIKey, envConfigAPIKey)
	}
	return key, nil
}

func chooseProvider(c Context) (string, error) {
	index := 1
	choices := map[int]string{}
	known := map[string]bool{}
	c.Output.Header("Clother Configuration")
	for _, category := range c.Catalog.Categories() {
		fmt.Fprintln(c.Output.Stdout, category)
		for _, provider := range c.Catalog.ProvidersByCategory(category) {
			fmt.Fprintf(c.Output.Stdout, "  %2d. %-14s %s\n", index, provider.ID, provider.Description)
			choices[index] = provider.ID
			known[provider.ID] = true
			index++
		}
	}
	fmt.Fprintf(c.Output.Stdout, "  %2d. %-14s %s\n", index, "openrouter", "100+ models")
	choices[index] = "openrouter"
	known["openrouter"] = true
	index++
	fmt.Fprintf(c.Output.Stdout, "  %2d. %-14s %s\n", index, "custom", "Anthropic-compatible endpoint")
	choices[index] = "custom"
	known["custom"] = true

	// A typo used to abort the whole command, an empty line was reported as an
	// error, and the provider id shown in the menu was refused.
	const attempts = 3
	for attempt := 0; attempt < attempts; attempt++ {
		answer, err := c.Prompt.Prompt("Choose provider number or name", "")
		if err != nil {
			return "", err
		}
		answer = strings.TrimSpace(answer)
		if answer == "" {
			return "", nil // empty input cancels
		}
		if number, err := strconv.Atoi(answer); err == nil {
			if providerID, ok := choices[number]; ok {
				return providerID, nil
			}
		} else if known[strings.ToLower(answer)] {
			return strings.ToLower(answer), nil
		}
		if attempt == attempts-1 {
			return "", fmt.Errorf("invalid choice %q", answer)
		}
		c.Output.ErrLine("invalid choice %q — enter a number from the list, a provider name, or nothing to cancel", answer)
	}
	return "", nil
}

func configBuiltin(c Context, provider providers.Provider) (int, error) {
	var changes []string
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
			changes = append(changes, provider.KeyVar)
		} else if current == "" {
			return 1, fmt.Errorf("no API key entered for %s; nothing was saved", provider.ID)
		}
	}

	override := c.Config.ProviderOverrides[provider.ID]
	previous := override

	if provider.DefaultModel != "" {
		// The header used to be printed for any provider carrying a default
		// model, listing nothing underneath when model_choices was empty.
		if len(provider.ModelChoices) > 0 {
			fmt.Fprintln(c.Output.Stdout, "Choose model:")
			for idx, choice := range provider.ModelChoices {
				line := fmt.Sprintf("  %d. %-24s %s", idx+1, choice.ID, choice.Description)
				if choice.Note != "" {
					line += " (" + choice.Note + ")"
				}
				fmt.Fprintln(c.Output.Stdout, line)
			}
		} else if provider.DocURL != "" {
			fmt.Fprintf(c.Output.Stdout, "Model ids for this provider: %s\n", provider.DocURL)
		}
		defaultValue := provider.DefaultModel
		if override.Model != "" {
			defaultValue = override.Model
		}
		answer, err := c.Prompt.Prompt("Model", defaultValue)
		if err != nil {
			return 1, err
		}
		answer = resolveModelChoice(answer, provider.ModelChoices)
		if answer != "" && answer != provider.DefaultModel {
			override.Model = answer
		} else {
			override.Model = ""
		}
	}

	// Local backends may run on another machine (e.g. LM Studio on a LAN
	// host), so let the user point the launcher at a remote base URL.
	if provider.Family == providers.FamilyLocal {
		defaultURL := provider.BaseURL
		if override.BaseURL != "" {
			defaultURL = override.BaseURL
		}
		answer, err := c.Prompt.Prompt("Base URL", defaultURL)
		if err != nil {
			return 1, err
		}
		answer = strings.TrimSpace(answer)
		if answer != "" && answer != provider.BaseURL {
			if !config.ValidBaseURL(answer) {
				return 1, fmt.Errorf("invalid base URL %q (must start with http:// or https://)", answer)
			}
			warnIfCleartextBaseURL(c, provider.ID, answer)
			override.BaseURL = strings.TrimRight(answer, "/")
		} else {
			override.BaseURL = ""
		}
	}

	if override != previous {
		if override.Model != "" {
			changes = append(changes, "model "+override.Model)
		}
		if override.BaseURL != "" {
			changes = append(changes, "base URL "+override.BaseURL)
		}
		if override == (config.ProviderOverride{}) {
			changes = append(changes, "catalog defaults restored")
		}
	}

	if override == (config.ProviderOverride{}) {
		delete(c.Config.ProviderOverrides, provider.ID)
	} else {
		c.Config.ProviderOverrides[provider.ID] = override
	}
	return persistConfig(c, changes...)
}

func configOpenRouter(c Context) (int, error) {
	var changes []string
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
		changes = append(changes, "OPENROUTER_API_KEY")
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
			return 1, fmt.Errorf("invalid alias %q (use lowercase letters, digits, \"-\" or \"_\")", name)
		}
		c.Config.OpenRouterAliases[name] = model
		changes = append(changes, "alias or-"+name)
	}
	return persistConfig(c, changes...)
}

func configCustom(c Context) (int, error) {
	name, err := c.Prompt.Prompt("Provider name", "")
	if err != nil {
		return 1, err
	}
	name = strings.TrimSpace(strings.ToLower(name))
	if err := checkCustomProviderName(name); err != nil {
		return 1, err
	}

	existing := c.Config.CustomProviders[name]

	urlLabel := "Base URL"
	if existing.BaseURL != "" {
		fmt.Fprintf(c.Output.Stdout, "Current URL: %s\n", existing.BaseURL)
		urlLabel = "Base URL (empty to keep current)"
	}
	baseURL, err := c.Prompt.Prompt(urlLabel, "")
	if err != nil {
		return 1, err
	}
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		baseURL = existing.BaseURL
	}
	if baseURL == "" {
		return 1, fmt.Errorf("base URL is required")
	}
	// Without this check a mistyped URL reaches http.NewRequest, which then
	// returns a nil request that `clother test` dereferences.
	if !config.ValidBaseURL(baseURL) {
		return 1, fmt.Errorf("invalid base URL %q (must be an absolute http:// or https:// URL)", baseURL)
	}
	baseURL = strings.TrimRight(baseURL, "/")
	warnIfCleartextBaseURL(c, name, baseURL)

	modelLabel := "Default model (optional)"
	if existing.DefaultModel != "" {
		modelLabel = fmt.Sprintf("Default model (empty to keep %q)", existing.DefaultModel)
	}
	defaultModel, err := c.Prompt.Prompt(modelLabel, "")
	if err != nil {
		return 1, err
	}
	defaultModel = strings.TrimSpace(defaultModel)
	if defaultModel == "" {
		defaultModel = existing.DefaultModel
	}

	keyVar := config.CustomProviderKeyVar(name)
	current := c.Secrets[keyVar]
	keyLabel := "API key"
	if current != "" {
		fmt.Fprintf(c.Output.Stdout, "Current key: %s\n", config.MaskSecret(current))
		keyLabel = "API key (empty to keep current)"
	}
	apiKey, err := c.Prompt.PromptSecret(keyLabel)
	if err != nil {
		return 1, err
	}
	changes := []string{"custom provider " + name}
	if strings.TrimSpace(apiKey) != "" {
		c.Secrets[keyVar] = apiKey
		changes = append(changes, keyVar)
	}

	c.Config.CustomProviders[name] = config.CustomProvider{
		Name:         name,
		DisplayName:  name,
		BaseURL:      baseURL,
		APIKeyEnv:    keyVar,
		DefaultModel: defaultModel,
	}
	return persistConfig(c, changes...)
}

// warnIfCleartextBaseURL warns when a base URL would carry the API key in the
// clear.
//
// The warning existed at a single one of the four places `ValidBaseURL` is
// called — the interactive creation of a custom provider — so
// `CLOTHER_BASE_URL=http://attacker.example/v1 clother config zai` redirected a
// built-in provider to a cleartext host and answered "configuration saved"
// without a word. A loopback host is exempt: that is what Ollama, LM Studio and
// llama.cpp actually serve, and warning about it every time trains the user to
// ignore the warning.
func warnIfCleartextBaseURL(c Context, label, baseURL string) {
	parsed, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || !strings.EqualFold(parsed.Scheme, "http") {
		return
	}
	if isLoopbackHostname(parsed.Hostname()) {
		return
	}
	c.Output.Warn("%s uses plain http://: the API key will travel unencrypted to %s", label, parsed.Host)
}

func isLoopbackHostname(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// checkCustomProviderName validates the name against everything it has to
// satisfy: a launcher file name and an environment variable root. Both are
// checked before any write so config.json cannot be saved with a provider
// whose key SaveSecrets will later refuse.
func checkCustomProviderName(name string) error {
	if !validProviderName.MatchString(name) {
		return fmt.Errorf("invalid provider name %q (start with a lowercase letter, then lowercase letters, digits, \"-\" or \"_\")", name)
	}
	if keyVar := config.CustomProviderKeyVar(name); !config.ValidEnvKey(keyVar) {
		return fmt.Errorf("provider name %q would produce the invalid environment variable %q", name, keyVar)
	}
	return nil
}

// persistConfig writes config.json and secrets.env, then re-syncs the
// launchers.
//
// It preserves the real `claude` binary first, exactly like runInstall: without
// it, launchers.Sync unconditionally removes BinDir/claude — which is the real
// Claude Code binary on a machine where `clother install` never ran (the
// Homebrew path in the README) — and replaces it with a symlink to clother, so
// a plain `clother config zai` destroyed Claude Code beyond recovery.
func persistConfig(c Context, changes ...string) (int, error) {
	// Same reason as runInstall: the guard sits at the write, not in a table of
	// command names that any delegation can step around.
	if err := c.refuseUnreadableState(); err != nil {
		return 1, err
	}

	realClaude, claudeErr := runtime.FindRealClaude(c.Paths)
	if claudeErr != nil {
		c.Output.Warn("claude not found; the `claude` shim will point at clother without a preserved binary — install Claude Code and run `clother install`")
	}
	if err := runtime.PreserveRealClaude(c.Paths, realClaude); err != nil {
		return 1, err
	}

	config.NormalizeLegacySecrets(c.Secrets, c.Catalog)
	// Merged, under a lock: four `clother config` in parallel used to all report
	// "configuration saved" and leave a single key of the four on disk.
	if err := config.SaveConfigMerged(c.Paths.ConfigFile, c.ConfigBaseline, c.Config); err != nil {
		return 1, err
	}
	if err := config.SaveSecretsMerged(c.Paths.SecretsFile, c.SecretsBaseline, c.Secrets); err != nil {
		return 1, err
	}
	execPath, execErr := os.Executable()
	if execErr != nil {
		return 1, execErr
	}
	if err := launchers.Sync(execPath, c.Paths, c.Catalog, c.Config, runtime.IsHomebrew()); err != nil {
		return 1, err
	}
	if len(changes) == 0 {
		c.Output.Line("no changes; configuration left as it was")
		return 0, nil
	}
	c.Output.Success("configuration saved: %s", strings.Join(changes, ", "))
	return 0, nil
}

func defaultAliasName(model string) string {
	model = strings.ToLower(model)
	if slash := strings.LastIndex(model, "/"); slash >= 0 {
		model = model[slash+1:]
	}
	// Model IDs may carry characters that are invalid in an alias (used as
	// launcher name), e.g. the ":free"/":exacto" variant suffixes. Map anything
	// outside the alias charset to "-" so the suggested default always passes
	// validName.
	var b strings.Builder
	for _, r := range model {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	name := b.String()
	for strings.Contains(name, "--") {
		name = strings.ReplaceAll(name, "--", "-")
	}
	return strings.Trim(name, "-")
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
