package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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
}

func New(parsed cli.Parsed) (*App, error) {
	paths, err := config.Detect(parsed.Options.BinDir)
	if err != nil {
		return nil, err
	}
	catalog, err := providers.Load()
	if err != nil {
		return nil, err
	}
	secrets, err := config.LoadSecrets(paths.SecretsFile)
	if err != nil {
		return nil, err
	}
	cfg, err := config.LoadConfig(paths.ConfigFile)
	if err != nil {
		return nil, err
	}
	cfg.ApplyLegacySecrets(secrets, catalog)
	_ = config.MigrateLegacyLaunchers(paths.BinDir, catalog, cfg)
	cfg.Normalize(catalog)

	output := ui.New(ui.Format(parsed.Options.Format), parsed.Options.Quiet)
	return &App{
		Parsed:  parsed,
		Paths:   paths,
		Config:  cfg,
		Secrets: secrets,
		Catalog: catalog,
		Output:  output,
		Prompt:  ui.NewPrompter(os.Stdin, os.Stdout),
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
		// Gateway invocations: clother-or <alias> and clother-custom <name>
		// let the user invoke any dynamic provider without a dedicated symlink.
		if profile == "or" {
			if len(args) == 0 || strings.HasPrefix(args[0], "-") {
				fmt.Fprintln(os.Stderr, "usage: clother-or <alias> [args...]\n\nRun `clother config openrouter` to configure aliases.")
				return 1, nil
			}
			profile = "or-" + args[0]
			args = args[1:]
		} else if profile == "custom" {
			if len(args) == 0 || strings.HasPrefix(args[0], "-") {
				fmt.Fprintln(os.Stderr, "usage: clother-custom <provider-name> [args...]\n\nRun `clother config custom` to configure a custom provider.")
				return 1, nil
			}
			profile = args[0]
			args = args[1:]
		}
		launcherOptions, forwarded := cli.ParseLauncher(args)
		paths, err := config.Detect("")
		if err != nil {
			return 1, err
		}
		catalog, err := providers.Load()
		if err != nil {
			return 1, err
		}
		secrets, err := config.LoadSecrets(paths.SecretsFile)
		if err != nil {
			return 1, err
		}
		cfg, err := config.LoadConfig(paths.ConfigFile)
		if err != nil {
			return 1, err
		}
		cfg.ApplyLegacySecrets(secrets, catalog)
		cfg.Normalize(catalog)
		target, err := profiles.Resolve(profile, catalog, cfg)
		if err != nil {
			return 1, err
		}
		return commands.RunLauncher(ctx, paths, secrets, target, forwarded, launcherOptions.NoBanner)
	}

	parsed, err := cli.Parse(args)
	if err != nil {
		return 1, err
	}
	app, err := New(parsed)
	if err != nil {
		return 1, err
	}

	if parsed.Options.Version {
		fmt.Fprintf(app.Output.Stdout, "Clother v%s\n", version.Value)
		return 0, nil
	}

	if parsed.Options.Help {
		if parsed.Command == "" {
			cli.ShowFull(app.Output.Stdout, app.Catalog)
			return 0, nil
		}
		cli.ShowFull(app.Output.Stdout, app.Catalog)
		return 0, nil
	}

	if parsed.Options.Format == "human" && !parsed.Options.Quiet && parsed.Command != "install" && parsed.Command != "uninstall" {
		if message, err := update.MaybeMessage(app.Paths, version.Value, time.Now()); err == nil && message != "" {
			fmt.Fprintln(app.Output.Stderr, message)
		}
	}

	return commands.Dispatch(ctx, commands.Context{
		Paths:   app.Paths,
		Config:  app.Config,
		Secrets: app.Secrets,
		Catalog: app.Catalog,
		Output:  app.Output,
		Prompt:  app.Prompt,
		Options: parsed.Options,
	}, parsed.Command, parsed.Args)
}
