package app

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jolehuit/clother/internal/cli"
	"github.com/jolehuit/clother/internal/commands"
	"github.com/jolehuit/clother/internal/config"
	"github.com/jolehuit/clother/internal/profiles"
	"github.com/jolehuit/clother/internal/providers"
	"github.com/jolehuit/clother/internal/runtime"
	"github.com/jolehuit/clother/internal/ui"
	"github.com/jolehuit/clother/internal/update"
	"github.com/jolehuit/clother/internal/version"
)

type App struct {
	Parsed  cli.Parsed
	Paths   config.Paths
	Config  *config.File
	Secrets config.Secrets
	Catalog providers.Catalog
	Output  *ui.Output
	Prompt  *ui.Prompter
	// ConfigErr and SecretsErr hold the failure of each disk load, if any. An
	// unreadable config degrades Clother, it does not brick it — and the same
	// now goes for secrets.env, which used to make every single command fail,
	// `clother help` and `clother status` included, with a bare errno.
	ConfigErr  error
	SecretsErr error
	// Baselines are the state as it was read from disk, before any in-memory
	// migration. The save path replays the baseline -> memory diff onto a fresh
	// read so concurrent writers do not erase each other.
	ConfigBaseline  *config.File
	SecretsBaseline config.Secrets
}

// commandsWritingConfig must not run on top of a state file we failed to read:
// they would persist a truncated view of it and lose the user's providers.
//
// This table is a courtesy, not the guard: it fails early with a clean message
// for the commands it names. The guard itself lives at the two write sites
// (commands.persistConfig and commands.runInstall), because `update` delegates
// to runInstall and slipped straight through a check keyed by command name.
var commandsWritingConfig = map[string]bool{
	"config":  true,
	"install": true,
	// `remove` rewrites config.json and secrets.env too: on an unreadable
	// config it would persist the empty fallback and drop every provider.
	"remove": true,
}

// state is everything both entry paths (CLI and launcher) build from disk.
type state struct {
	paths           config.Paths
	catalog         providers.Catalog
	secrets         config.Secrets
	config          *config.File
	configErr       error
	secretsErr      error
	configBaseline  *config.File
	secretsBaseline config.Secrets
}

func emptyConfig() *config.File {
	return &config.File{
		Version:           1,
		ProviderOverrides: map[string]config.ProviderOverride{},
		OpenRouterAliases: map[string]string{},
		CustomProviders:   map[string]config.CustomProvider{},
	}
}

// loadState builds the same in-memory state for both the CLI and the launcher
// paths, so a provider resolves identically whichever way Clother was invoked.
func loadState(binOverride string) (state, error) {
	var st state
	paths, err := config.Detect(binOverride)
	if err != nil {
		return st, err
	}
	st.paths = paths
	catalog, err := providers.Load()
	if err != nil {
		return st, err
	}
	st.catalog = catalog

	// secrets.env degrades exactly like config.json. It used to propagate its
	// error out of New(), so a file with the wrong mode made `clother list`,
	// `clother status` and even `clother help` exit 1 with nothing but
	// "permission denied" and no envelope at all in --json.
	secrets, secretsErr := config.LoadSecrets(paths.SecretsFile)
	if secretsErr != nil || secrets == nil {
		secrets = config.Secrets{}
		st.secretsErr = secretsErr
	}
	st.secrets = secrets

	cfg, cfgErr := config.LoadConfig(paths.ConfigFile)
	if cfgErr != nil || cfg == nil {
		// An unreadable config file must not take down every command, --help
		// included. We keep going with an empty config and remember why.
		cfg = emptyConfig()
		st.configErr = cfgErr
	}
	st.config = cfg
	// Snapshot before the in-memory migrations so that what they change counts
	// as a change and still reaches the disk.
	st.configBaseline = cfg.Clone()
	st.secretsBaseline = secrets.Clone()
	cfg.ApplyLegacySecrets(secrets, catalog)
	_ = config.MigrateLegacyLaunchers(paths.BinDir, catalog, cfg)
	cfg.Normalize(catalog)
	return st, nil
}

func New(parsed cli.Parsed) (*App, error) {
	st, err := loadState(parsed.Options.BinDir)
	if err != nil {
		return nil, err
	}

	output := ui.New(ui.Format(parsed.Options.Format), parsed.Options.Quiet)
	output.Verbose = parsed.Options.Verbose
	output.Debug = parsed.Options.Debug

	prompt := ui.NewPrompter(os.Stdin, os.Stdout)
	prompt.NoInput = parsed.Options.NoInput

	return &App{
		Parsed:          parsed,
		Paths:           st.paths,
		Config:          st.config,
		Secrets:         st.secrets,
		Catalog:         st.catalog,
		Output:          output,
		Prompt:          prompt,
		ConfigErr:       st.configErr,
		SecretsErr:      st.secretsErr,
		ConfigBaseline:  st.configBaseline,
		SecretsBaseline: st.secretsBaseline,
	}, nil
}

func Run(ctx context.Context, args []string, argv0 string) (int, error) {
	if filepath.Base(argv0) == "claude" {
		paths, err := config.Detect("")
		if err != nil {
			return 1, err
		}
		return runtime.RunClaudeShim(ctx, paths, args)
	}

	if profile, isLauncher := profiles.Invocation(argv0); isLauncher {
		return runLauncher(ctx, profile, args)
	}

	parsed, err := cli.Parse(args)
	if err != nil {
		return 1, err
	}

	output := ui.New(ui.Format(parsed.Options.Format), parsed.Options.Quiet)
	output.Verbose = parsed.Options.Verbose
	output.Debug = parsed.Options.Debug

	if parsed.Options.Version {
		fmt.Fprintf(output.Stdout, "Clother v%s\n", version.Value)
		return 0, nil
	}

	// Help is answered before any disk state is loaded: `clother --help` has to
	// work even when the config file is broken.
	if parsed.Options.Help {
		catalog, err := providers.Load()
		if err != nil {
			return 1, err
		}
		if parsed.Command != "" {
			if cli.ShowCommand(output.Stdout, parsed.Command, catalog) {
				return 0, nil
			}
			cli.ShowFull(output.Stdout, catalog)
			return 1, fmt.Errorf("unknown command %q", parsed.Command)
		}
		cli.ShowFull(output.Stdout, catalog)
		return 0, nil
	}

	app, err := New(parsed)
	if err != nil {
		// Even a failure to start must leave exactly one envelope on stdout in
		// --json mode; a consumer parsing stdout used to get nothing at all.
		if output.Machine() {
			_ = output.EmitError(parsed.Command, ui.KindUnknown, err.Error(), "")
			return 1, nil
		}
		return 1, err
	}
	app.Output.Verbosef("running %q with config %s", parsed.Command, app.Paths.ConfigFile)
	app.Output.Debugf("command=%q args=%v format=%s", parsed.Command, parsed.Args, parsed.Options.Format)
	app.Output.Debugf("config dir=%s data dir=%s cache dir=%s bin dir=%s", app.Paths.ConfigDir, app.Paths.DataDir, app.Paths.CacheDir, app.Paths.BinDir)
	app.Output.Debugf("config file=%s secrets file=%s", app.Paths.ConfigFile, app.Paths.SecretsFile)

	for _, state := range []struct {
		label string
		path  string
		err   error
	}{
		{"config file", app.Paths.ConfigFile, app.ConfigErr},
		{"secrets file", app.Paths.SecretsFile, app.SecretsErr},
	} {
		if state.err == nil {
			continue
		}
		message := fmt.Sprintf("%s %s cannot be read: %v", state.label, state.path, state.err)
		// The hint follows the nature of the failure. "fix the JSON" on a
		// perfectly valid file whose mode is 000 sent the user hunting for a
		// syntax error that does not exist.
		hint := commands.UnreadableStateHint(state.path, state.err)
		if commandsWritingConfig[parsed.Command] {
			if app.Output.Machine() {
				_ = app.Output.EmitError(parsed.Command, ui.KindConfigInvalid, message, hint)
				return 1, nil
			}
			return 1, fmt.Errorf("%s — %s", message, hint)
		}
		app.Output.Warn("%s — running with defaults (%s)", message, hint)
	}

	if parsed.Options.Format == "human" && !parsed.Options.Quiet && parsed.Command != "install" && parsed.Command != "uninstall" {
		if message, err := update.MaybeMessage(app.Paths, version.Value, time.Now()); err == nil && message != "" {
			fmt.Fprintln(app.Output.Stderr, message)
		}
	}

	code, err := commands.Dispatch(ctx, commands.Context{
		Paths:           app.Paths,
		Config:          app.Config,
		Secrets:         app.Secrets,
		Catalog:         app.Catalog,
		Output:          app.Output,
		Prompt:          app.Prompt,
		Options:         parsed.Options,
		ConfigErr:       app.ConfigErr,
		SecretsErr:      app.SecretsErr,
		ConfigBaseline:  app.ConfigBaseline,
		SecretsBaseline: app.SecretsBaseline,
	}, parsed.Command, parsed.Args)

	// In --json mode a failure used to leave stdout empty and put a human
	// sentence on stderr, so a consumer parsing stdout got nothing at all to
	// parse. Every invocation now ends with exactly one envelope on stdout.
	// Commands that fail through their own data (test reports per-provider
	// verdicts and returns a nil error) have already emitted theirs.
	if err != nil && app.Output.Machine() {
		_ = app.Output.EmitError(parsed.Command, classifyCommandError(err), err.Error(), "")
		return code, nil
	}
	return code, err
}

// classifyCommandError maps a command failure onto the stable error kinds of
// the envelope. The message is for humans and may change; the kind is the
// contract, so an unrecognised failure is reported as unknown rather than
// guessed at.
func classifyCommandError(err error) ui.ErrorKind {
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "not configured"):
		return ui.KindMissingKey
	case strings.Contains(message, "unknown provider"),
		strings.Contains(message, "unknown profile"),
		strings.Contains(message, "unknown command"),
		strings.Contains(message, "unknown openrouter alias"),
		strings.Contains(message, "unknown custom provider"),
		strings.Contains(message, "takes no arguments"),
		strings.Contains(message, "usage:"):
		return ui.KindConfigInvalid
	default:
		return ui.KindUnknown
	}
}

// runLauncher handles a `clother-<profile>` invocation, including the two
// gateways `clother-or <alias>` and `clother-custom <name>`.
func runLauncher(ctx context.Context, profile string, args []string) (int, error) {
	launcherOptions, forwarded := cli.ParseLauncher(args)

	// Launcher arguments belong to claude, so debug tracing is opted into
	// through the environment rather than through a flag we would have to steal.
	debug := ui.NewOutput(io.Discard, os.Stderr, ui.FormatHuman, false)
	debug.Debug = os.Getenv("CLOTHER_DEBUG") == "1"
	debug.Debugf("launcher profile=%q args=%v", profile, forwarded)

	st, err := loadState("")
	if err != nil {
		return 1, err
	}
	if st.configErr != nil {
		fmt.Fprintf(os.Stderr, "clother: config file %s cannot be read (%v); launching with catalog defaults\n", st.paths.ConfigFile, st.configErr)
	}
	if st.secretsErr != nil {
		// Without this the launcher fails with "<KEY> not configured — run
		// `clother config <provider>`", which sends the user to reconfigure a
		// key that is already there and merely unreadable.
		fmt.Fprintf(os.Stderr, "clother: secrets file %s cannot be read (%v); %s\n",
			st.paths.SecretsFile, st.secretsErr, commands.UnreadableStateHint(st.paths.SecretsFile, st.secretsErr))
	}
	debug.Debugf("config dir=%s data dir=%s bin dir=%s", st.paths.ConfigDir, st.paths.DataDir, st.paths.BinDir)

	// Gateway invocations: clother-or <alias> and clother-custom <name>
	// let the user invoke any dynamic provider without a dedicated symlink.
	// Config is loaded first so we can validate the name against known providers.
	if profile == "or" {
		if len(forwarded) == 0 || strings.HasPrefix(forwarded[0], "-") {
			fmt.Fprintln(os.Stderr, "usage: clother-or <alias> [args...]\n\nRun `clother config openrouter` to configure aliases.")
			return 1, nil
		}
		profile = "or-" + forwarded[0]
		forwarded = forwarded[1:]
	} else if profile == "custom" {
		if len(forwarded) == 0 || strings.HasPrefix(forwarded[0], "-") {
			fmt.Fprintln(os.Stderr, "usage: clother-custom <provider-name> [args...]\n\nRun `clother config custom` to configure a custom provider.")
			return 1, nil
		}
		name := forwarded[0]
		if _, ok := st.config.CustomProviders[name]; !ok {
			return 1, fmt.Errorf("unknown custom provider %q — run `clother config custom` to configure one", name)
		}
		profile = name
		forwarded = forwarded[1:]
	}

	target, err := profiles.Resolve(profile, st.catalog, st.config)
	if err != nil {
		return 1, err
	}
	traceTarget(debug, target, st.secrets)

	return commands.RunLauncher(ctx, st.paths, st.secrets, target, forwarded, launcherOptions.NoBanner)
}

// traceTarget prints, under --debug/CLOTHER_DEBUG, exactly what the child claude
// process will see: the resolved target, its base URL, the masked token, and
// every environment variable set or purged. Secrets are always masked.
func traceTarget(output *ui.Output, target profiles.Target, secrets config.Secrets) {
	if !output.Debug {
		return
	}
	output.Debugf("target profile=%q display=%q family=%s auth=%s", target.Profile, target.DisplayName, target.Family, target.AuthMode)
	output.Debugf("base url=%q model=%q test url=%q", target.BaseURL, target.Model, target.TestURL)
	if target.SecretKey != "" {
		output.Debugf("secret %s=%s", target.SecretKey, config.MaskSecret(secrets[target.SecretKey]))
	}

	env, err := runtime.BuildEnv(target, secrets)
	if err != nil {
		output.Debugf("environment not built: %v", err)
		return
	}
	current := map[string]string{}
	for _, pair := range os.Environ() {
		if key, value, ok := splitEnvPair(pair); ok {
			current[key] = value
		}
	}
	next := map[string]string{}
	for _, pair := range env {
		if key, value, ok := splitEnvPair(pair); ok {
			next[key] = value
		}
	}

	var set, purged []string
	for key, value := range next {
		if old, ok := current[key]; !ok || old != value {
			set = append(set, key+"="+maskEnvValue(key, value, secrets))
		}
	}
	for key := range current {
		if _, ok := next[key]; !ok {
			purged = append(purged, key)
		}
	}
	sort.Strings(set)
	sort.Strings(purged)
	for _, entry := range set {
		output.Debugf("env set %s", entry)
	}
	for _, key := range purged {
		output.Debugf("env purged %s", key)
	}
}

// maskEnvValue hides anything that matches a known secret value, plus the token
// variables, so a debug trace can be pasted in a bug report.
func maskEnvValue(key, value string, secrets config.Secrets) string {
	if value == "" {
		return ""
	}
	if strings.Contains(key, "TOKEN") || strings.Contains(key, "KEY") {
		return config.MaskSecret(value)
	}
	for _, secret := range secrets {
		if secret != "" && secret == value {
			return config.MaskSecret(value)
		}
	}
	return value
}

func splitEnvPair(pair string) (string, string, bool) {
	if idx := strings.IndexByte(pair, '='); idx > 0 {
		return pair[:idx], pair[idx+1:], true
	}
	return "", "", false
}
