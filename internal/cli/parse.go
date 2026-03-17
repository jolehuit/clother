package cli

import "fmt"

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

func Parse(args []string) (Parsed, error) {
	parsed := Parsed{Options: Options{Format: "human"}}
	var positional []string

	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "-h", "--help":
			parsed.Options.Help = true
		case "-V", "--version":
			parsed.Options.Version = true
		case "-v", "--verbose":
			parsed.Options.Verbose = true
		case "-d", "--debug":
			parsed.Options.Debug = true
			parsed.Options.Verbose = true
		case "-q", "--quiet":
			parsed.Options.Quiet = true
		case "-y", "--yes":
			parsed.Options.Yes = true
		case "--no-input":
			parsed.Options.NoInput = true
		case "--no-banner":
			parsed.Options.NoBanner = true
		case "--json":
			parsed.Options.Format = "json"
		case "--plain":
			parsed.Options.Format = "plain"
		case "--bin-dir":
			if i+1 >= len(args) {
				return Parsed{}, fmt.Errorf("--bin-dir requires a path")
			}
			i++
			parsed.Options.BinDir = args[i]
		case "--":
			positional = append(positional, args[i+1:]...)
			i = len(args)
		default:
			if len(arg) > 0 && arg[0] == '-' {
				return Parsed{}, fmt.Errorf("unknown option %s", arg)
			}
			positional = append(positional, arg)
		}
	}

	if len(positional) > 0 {
		parsed.Command = positional[0]
		parsed.Args = positional[1:]
	}
	return parsed, nil
}

func ParseLauncher(args []string) (Options, []string) {
	options := Options{}
	var forwarded []string
	for _, arg := range args {
		if arg == "--no-banner" {
			options.NoBanner = true
			continue
		}
		forwarded = append(forwarded, arg)
	}
	return options, forwarded
}
