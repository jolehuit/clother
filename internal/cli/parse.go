package cli

import (
	"errors"
	"fmt"
	"strings"
)

type Options struct {
	Help     bool
	Version  bool
	Verbose  bool
	Debug    bool
	Quiet    bool
	Yes      bool
	NoInput  bool
	NoBanner bool
	BinDir   string
	Format   string
}

type Parsed struct {
	Options Options
	Command string
	Args    []string
}

// errUnknownOption marks an option the global parser does not know about. It is
// an error before the command name (typo protection) and a pass-through after it
// (the subcommand owns its own flags).
var errUnknownOption = errors.New("unknown option")

// Parse reads the global options and the command line.
//
// Global options are accepted before the command (`clother --json list`) and
// after it (`clother list --json`, kept for backwards compatibility). Any option
// the global parser does not know is forwarded verbatim to the subcommand once a
// command has been seen, which is what makes `clother bench --prompt "..."`
// work. Everything after a `--` terminator is forwarded untouched.
func Parse(args []string) (Parsed, error) {
	parsed := Parsed{Options: Options{Format: "human"}}
	var positional []string
	sawCommand := false
	// forwardNext is set right after an unknown option so that its value is not
	// swallowed by the global parser: `bench --prompt --json` keeps `--json`.
	forwardNext := false

	for i := 0; i < len(args); i++ {
		arg := args[i]

		if arg == "--" {
			positional = append(positional, args[i+1:]...)
			break
		}

		if !isOption(arg) {
			positional = append(positional, arg)
			sawCommand = true
			forwardNext = false
			continue
		}

		if forwardNext {
			positional = append(positional, arg)
			forwardNext = false
			continue
		}

		name, value, hasValue := splitOption(arg)
		consumed, err := applyOption(&parsed.Options, name, value, hasValue, args, i)
		if err != nil {
			if errors.Is(err, errUnknownOption) && sawCommand {
				positional = append(positional, arg)
				forwardNext = true
				continue
			}
			return Parsed{}, err
		}
		i += consumed
	}

	if len(positional) > 0 {
		parsed.Command = positional[0]
		parsed.Args = positional[1:]
	}
	return parsed, nil
}

// isOption reports whether arg looks like a flag. A lone "-" is a positional
// argument (it commonly means stdin), never an option.
func isOption(arg string) bool {
	return len(arg) > 1 && arg[0] == '-'
}

// splitOption splits "--bin-dir=/tmp/bin" into ("--bin-dir", "/tmp/bin", true).
func splitOption(arg string) (name, value string, hasValue bool) {
	if strings.HasPrefix(arg, "--") {
		if idx := strings.IndexByte(arg, '='); idx > 0 {
			return arg[:idx], arg[idx+1:], true
		}
	}
	return arg, "", false
}

// applyOption applies one global option and returns how many extra arguments it
// consumed from args.
func applyOption(options *Options, name, value string, hasValue bool, args []string, i int) (int, error) {
	switch name {
	case "-h", "--help":
		if hasValue {
			return 0, fmt.Errorf("option %s takes no value", name)
		}
		options.Help = true
	case "-V", "--version":
		if hasValue {
			return 0, fmt.Errorf("option %s takes no value", name)
		}
		options.Version = true
	case "-v", "--verbose":
		if hasValue {
			return 0, fmt.Errorf("option %s takes no value", name)
		}
		options.Verbose = true
	case "-d", "--debug":
		if hasValue {
			return 0, fmt.Errorf("option %s takes no value", name)
		}
		options.Debug = true
		options.Verbose = true
	case "-q", "--quiet":
		if hasValue {
			return 0, fmt.Errorf("option %s takes no value", name)
		}
		options.Quiet = true
	case "-y", "--yes":
		if hasValue {
			return 0, fmt.Errorf("option %s takes no value", name)
		}
		options.Yes = true
	case "--no-input":
		if hasValue {
			return 0, fmt.Errorf("option %s takes no value", name)
		}
		options.NoInput = true
	case "--no-banner":
		if hasValue {
			return 0, fmt.Errorf("option %s takes no value", name)
		}
		options.NoBanner = true
	case "--json":
		if hasValue {
			return 0, fmt.Errorf("option %s takes no value", name)
		}
		options.Format = "json"
	case "--plain":
		if hasValue {
			return 0, fmt.Errorf("option %s takes no value", name)
		}
		options.Format = "plain"
	case "--bin-dir":
		if hasValue {
			if value == "" {
				return 0, fmt.Errorf("--bin-dir requires a path")
			}
			options.BinDir = value
			return 0, nil
		}
		if i+1 >= len(args) {
			return 0, fmt.Errorf("--bin-dir requires a path")
		}
		next := args[i+1]
		if isOption(next) {
			return 0, fmt.Errorf("--bin-dir requires a path, got option %q (use --bin-dir=%s if that really is the path)", next, next)
		}
		options.BinDir = next
		return 1, nil
	default:
		return 0, fmt.Errorf("%w %s", errUnknownOption, name)
	}
	return 0, nil
}

// ParseLauncher extracts the launcher-only options from the arguments of a
// `clother-<profile>` invocation. Everything else, including everything after a
// `--` terminator, is forwarded to claude untouched and in order.
func ParseLauncher(args []string) (Options, []string) {
	options := Options{}
	var forwarded []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			forwarded = append(forwarded, args[i:]...)
			break
		}
		if arg == "--no-banner" {
			options.NoBanner = true
			continue
		}
		forwarded = append(forwarded, arg)
	}
	return options, forwarded
}
