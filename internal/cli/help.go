package cli

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/jolehuit/clother/internal/providers"
	"github.com/jolehuit/clother/internal/version"
)

// Command describes one clother subcommand. The table below is the single
// source of truth for both help screens, so a command can no longer exist in one
// listing and be missing from the other.
type Command struct {
	Name     string
	Usage    string
	Summary  string
	Details  []string
	Examples []string
}

var commands = []Command{
	{
		Name:    "config",
		Usage:   "config [provider]",
		Summary: "Configure a provider (API key, model, base URL)",
		Details: []string{
			"Without an argument, config shows a numbered menu of every provider.",
			"Special names: `openrouter` manages OpenRouter aliases, `custom` registers",
			"any Anthropic-compatible endpoint.",
			"The API key is read from the terminal with echo disabled; it is never",
			"printed back in full.",
			"For CI and scripts, set CLOTHER_API_KEY (with CLOTHER_MODEL,",
			"CLOTHER_BASE_URL, CLOTHER_ALIAS or CLOTHER_PROVIDER_NAME as needed):",
			"config then runs without a terminal. CLOTHER_API_KEY=- reads the key",
			"from stdin so it never appears in the process list or the shell history.",
		},
		Examples: []string{
			"clother config zai",
			"clother config custom",
			"CLOTHER_API_KEY=- clother config zai < key.txt",
		},
	},
	{
		Name:    "remove",
		Usage:   "remove <provider>",
		Summary: "Forget a provider: its stored key and its settings",
		Details: []string{
			"Asks for confirmation; -y skips it. This is the only command that deletes",
			"a stored credential.",
			"`remove openrouter` drops the key and every alias, `remove or-<alias>`",
			"drops a single alias and keeps the shared key.",
		},
		Examples: []string{
			"clother remove zai",
			"clother remove or-fast -y",
		},
	},
	{
		Name:    "list",
		Usage:   "list",
		Summary: "List profiles and whether they are configured",
		Details: []string{
			"Supports --json and --plain for scripting.",
		},
		Examples: []string{
			"clother list",
			"clother list --json",
		},
	},
	{
		Name:    "info",
		Usage:   "info <provider>",
		Summary: "Show the resolved settings of one provider",
		Details: []string{
			"Prints the base URL, the model tiers and the key variable actually used",
			"when launching claude against that provider.",
		},
		Examples: []string{
			"clother info zai",
			"clother info --json kimi",
		},
	},
	{
		Name:    "test",
		Usage:   "test [provider]",
		Summary: "Check that provider endpoints are reachable",
		Details: []string{
			"Without an argument, every profile is tested. Each endpoint is queried",
			"with a 5 second timeout.",
		},
		Examples: []string{
			"clother test",
			"clother test zai",
		},
	},
	{
		Name:    "bench",
		Usage:   "bench [provider...] [--prompt \"...\"]",
		Summary: "Benchmark latency of configured providers",
		Details: []string{
			"Only providers with a configured key are benchmarked.",
			"--prompt, -p <text>   send a custom prompt instead of the default one.",
			// The divergence with `test` is deliberate but was written nowhere,
			// so a CI script watching provider health with bench let an outage
			// through as long as one provider still answered.
			"Exits non-zero only when every provider failed; use `clother test` to fail as soon as one provider is unreachable.",
		},
		Examples: []string{
			"clother bench",
			"clother bench --prompt \"Write a haiku\"",
		},
	},
	{
		Name:    "status",
		Usage:   "status",
		Summary: "Show version, directories and profile count",
		Examples: []string{
			"clother status",
			"clother status --json",
		},
	},
	{
		Name:    "install",
		Usage:   "install",
		Summary: "Install/update Clother (refresh launcher symlinks)",
		Details: []string{
			"Recreates one `clother-<profile>` launcher per configured provider in the",
			"bin directory, plus the `claude` shim. Use --bin-dir to choose where.",
		},
		Examples: []string{
			"clother install",
			"clother install --bin-dir ~/.local/bin",
		},
	},
	{
		Name:    "update",
		Usage:   "update",
		Summary: "Update Clother to the latest release",
		Examples: []string{
			"clother update",
		},
	},
	{
		Name:    "uninstall",
		Usage:   "uninstall",
		Summary: "Remove Clother launchers and shim",
		Details: []string{
			"Asks for confirmation; -y skips it.",
		},
		Examples: []string{
			"clother uninstall",
			"clother uninstall -y",
		},
	},
	{
		Name:    "help",
		Usage:   "help [command]",
		Summary: "Show this help, or the help of one command",
		Examples: []string{
			"clother help",
			"clother help bench",
		},
	},
}

type option struct {
	Flags       string
	Description string
}

var globalOptions = []option{
	{"-h, --help", "Show help. After a command, shows that command's help."},
	{"-V, --version", "Print the Clother version."},
	{"-v, --verbose", "Print diagnostic details on stderr."},
	{"-d, --debug", "Print the resolved target, base URL, masked token and every environment variable set or purged, on stderr. Implies --verbose."},
	{"-q, --quiet", "Suppress non-essential output."},
	{"-y, --yes", "Answer yes to confirmations."},
	{"--no-input", "Never prompt; fail instead. For CI and scripts."},
	{"--no-banner", "Do not print the launcher banner."},
	{"--bin-dir <path>", "Directory where launchers are installed or removed."},
	{"--json", "Machine-readable output."},
	{"--plain", "Bare output, one item per line."},
}

var environment = []option{
	{"CLOTHER_CONFIG_DIR", "Config directory (default $XDG_CONFIG_HOME/clother)."},
	{"CLOTHER_DATA_DIR", "Data directory, holds secrets.env (default $XDG_DATA_HOME/clother)."},
	{"CLOTHER_CACHE_DIR", "Cache directory (default $XDG_CACHE_HOME/clother)."},
	{"CLOTHER_BIN", "Directory for the launcher symlinks."},
	{"CLOTHER_DEBUG", "Set to 1 to trace launcher invocations on stderr."},
	{"CLOTHER_NO_UPDATE_CHECK", "Set to 1 to disable the update check."},
	{"CLOTHER_UPDATE_URL", "Override the update metadata URL (https only, loopback excepted)."},
	{"CLOTHER_API_KEY", "Non-interactive `clother config`: the API key to store. Use `-` to read it from stdin."},
	{"CLOTHER_MODEL", "Non-interactive `clother config`: the model to pin for the provider."},
	{"CLOTHER_BASE_URL", "Non-interactive `clother config`: the base URL, for local and custom providers."},
	{"CLOTHER_ALIAS", "Non-interactive `clother config openrouter`: the alias name to create."},
	{"CLOTHER_PROVIDER_NAME", "Non-interactive `clother config custom`: the custom provider name."},
	{"NO_COLOR", "Set to any value to disable colored output."},
}

// Commands returns the documented command table.
func Commands() []Command {
	out := make([]Command, len(commands))
	copy(out, commands)
	return out
}

// CommandByName looks a command up in the table.
func CommandByName(name string) (Command, bool) {
	for _, command := range commands {
		if command.Name == name {
			return command, true
		}
	}
	return Command{}, false
}

func ShowBrief(w io.Writer) {
	fmt.Fprintf(w, "Clother v%s - Multi-provider launcher for Claude CLI\n\n", version.Value)
	fmt.Fprintln(w, "Usage: clother [options] <command>")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Commands:")
	for _, command := range commands {
		fmt.Fprintf(w, "  %-12s %s\n", command.Name, command.Summary)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Tip: add --yolo to a launcher command to skip permission prompts.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Run clother --help for full help, clother <command> --help for one command.")
}

func ShowFull(w io.Writer, catalog providers.Catalog) {
	fmt.Fprintf(w, "Clother v%s\n", version.Value)
	fmt.Fprintln(w, "Multi-provider launcher for Claude CLI")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  clother [options] <command> [args]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Commands:")
	for _, command := range commands {
		fmt.Fprintf(w, "  %-32s %s\n", command.Usage, command.Summary)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Options:")
	writeOptions(w, globalOptions)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Launchers:")
	fmt.Fprintln(w, "  clother-<provider>       run claude against that provider")
	fmt.Fprintln(w, "  clother-or <alias>       run claude against an OpenRouter alias")
	fmt.Fprintln(w, "  clother-custom <name>    run claude against a custom provider")
	fmt.Fprintln(w, "  claude                   same behavior via the Clother shim")
	fmt.Fprintln(w, "  --yolo                   shorthand for --dangerously-skip-permissions")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Environment:")
	writeOptions(w, environment)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Providers:")
	for _, category := range catalog.Categories() {
		fmt.Fprintf(w, "  %s\n", category)
		providersInCategory := catalog.ProvidersByCategory(category)
		sort.SliceStable(providersInCategory, func(i, j int) bool {
			return providersInCategory[i].ID < providersInCategory[j].ID
		})
		for _, provider := range providersInCategory {
			fmt.Fprintf(w, "    %-12s %s\n", provider.ID, provider.Description)
		}
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Advanced:")
	fmt.Fprintln(w, "    openrouter   100+ models via native API")
	fmt.Fprintln(w, "    custom       Anthropic-compatible endpoint")
}

// ShowCommand prints the help page of one command. It reports false when the
// command is unknown, so the caller can fall back to the full help.
func ShowCommand(w io.Writer, name string, catalog providers.Catalog) bool {
	command, ok := CommandByName(name)
	if !ok {
		return false
	}
	fmt.Fprintf(w, "clother %s - %s\n", command.Name, command.Summary)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintf(w, "  clother [options] %s\n", command.Usage)
	if len(command.Details) > 0 {
		fmt.Fprintln(w)
		for _, line := range command.Details {
			fmt.Fprintf(w, "  %s\n", line)
		}
	}
	if len(command.Examples) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Examples:")
		for _, example := range command.Examples {
			fmt.Fprintf(w, "  %s\n", example)
		}
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Global options:")
	writeOptions(w, globalOptions)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Run `clother --help` for the provider catalog and the environment variables.")
	return true
}

func writeOptions(w io.Writer, entries []option) {
	width := 0
	for _, entry := range entries {
		if len(entry.Flags) > width {
			width = len(entry.Flags)
		}
	}
	for _, entry := range entries {
		lines := wrap(entry.Description, 62)
		fmt.Fprintf(w, "  %-*s  %s\n", width, entry.Flags, lines[0])
		for _, line := range lines[1:] {
			fmt.Fprintf(w, "  %-*s  %s\n", width, "", line)
		}
	}
}

func wrap(text string, width int) []string {
	words := strings.Fields(text)
	if len(words) == 0 {
		return []string{""}
	}
	var lines []string
	current := words[0]
	for _, word := range words[1:] {
		if len(current)+1+len(word) > width {
			lines = append(lines, current)
			current = word
			continue
		}
		current += " " + word
	}
	return append(lines, current)
}
