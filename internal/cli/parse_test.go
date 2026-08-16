package cli

import (
	"reflect"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		want    Parsed
		wantErr string
	}{
		{
			name: "no arguments",
			want: Parsed{Options: Options{Format: "human"}},
		},
		{
			name: "command only",
			args: []string{"list"},
			want: Parsed{Options: Options{Format: "human"}, Command: "list"},
		},
		{
			name: "global options before the command",
			args: []string{"--json", "-q", "list"},
			want: Parsed{Options: Options{Format: "json", Quiet: true}, Command: "list"},
		},
		{
			name: "global options after the command stay global",
			args: []string{"list", "--json"},
			want: Parsed{Options: Options{Format: "json"}, Command: "list"},
		},
		{
			name: "debug implies verbose",
			args: []string{"-d", "status"},
			want: Parsed{Options: Options{Format: "human", Debug: true, Verbose: true}, Command: "status"},
		},
		{
			name: "plain then json keeps the last format",
			args: []string{"--plain", "--json", "list"},
			want: Parsed{Options: Options{Format: "json"}, Command: "list"},
		},
		{
			name: "long and short flags",
			args: []string{"--verbose", "-y", "--no-input", "--no-banner", "-V", "-h", "uninstall"},
			want: Parsed{
				Options: Options{Format: "human", Verbose: true, Yes: true, NoInput: true, NoBanner: true, Version: true, Help: true},
				Command: "uninstall",
			},
		},
		{
			name: "bin dir with a value",
			args: []string{"install", "--bin-dir", "/tmp/bin"},
			want: Parsed{Options: Options{Format: "human", BinDir: "/tmp/bin"}, Command: "install"},
		},
		{
			name: "bin dir with an equals sign",
			args: []string{"--bin-dir=/tmp/bin", "install"},
			want: Parsed{Options: Options{Format: "human", BinDir: "/tmp/bin"}, Command: "install"},
		},
		{
			name:    "bin dir without a value",
			args:    []string{"install", "--bin-dir"},
			wantErr: "--bin-dir requires a path",
		},
		{
			name:    "bin dir swallowing the next option",
			args:    []string{"install", "--bin-dir", "--json"},
			wantErr: "--bin-dir requires a path, got option",
		},
		{
			name: "bin dir accepting a dashed path through equals",
			args: []string{"--bin-dir=-weird-dir", "install"},
			want: Parsed{Options: Options{Format: "human", BinDir: "-weird-dir"}, Command: "install"},
		},
		{
			name:    "unknown option before the command",
			args:    []string{"--nope", "list"},
			wantErr: "unknown option --nope",
		},
		{
			name: "subcommand flags are forwarded",
			args: []string{"bench", "--prompt", "Write a haiku"},
			want: Parsed{Options: Options{Format: "human"}, Command: "bench", Args: []string{"--prompt", "Write a haiku"}},
		},
		{
			name: "short subcommand flag is forwarded",
			args: []string{"bench", "-p", "hi"},
			want: Parsed{Options: Options{Format: "human"}, Command: "bench", Args: []string{"-p", "hi"}},
		},
		{
			name: "value of an unknown flag is not eaten by the global parser",
			args: []string{"bench", "--prompt", "--json"},
			want: Parsed{Options: Options{Format: "human"}, Command: "bench", Args: []string{"--prompt", "--json"}},
		},
		{
			name: "terminator forwards everything verbatim",
			args: []string{"bench", "--", "--prompt", "-q", "zai"},
			want: Parsed{Options: Options{Format: "human"}, Command: "bench", Args: []string{"--prompt", "-q", "zai"}},
		},
		{
			name: "terminator in last position",
			args: []string{"list", "--"},
			want: Parsed{Options: Options{Format: "human"}, Command: "list"},
		},
		{
			name: "terminator first",
			args: []string{"--", "list", "--json"},
			want: Parsed{Options: Options{Format: "human"}, Command: "list", Args: []string{"--json"}},
		},
		{
			name: "several positional arguments",
			args: []string{"bench", "zai", "kimi"},
			want: Parsed{Options: Options{Format: "human"}, Command: "bench", Args: []string{"zai", "kimi"}},
		},
		{
			name: "a lone dash is positional",
			args: []string{"config", "-"},
			want: Parsed{Options: Options{Format: "human"}, Command: "config", Args: []string{"-"}},
		},
		{
			name:    "boolean option refuses a value",
			args:    []string{"--json=yes", "list"},
			wantErr: "option --json takes no value",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := Parse(test.args)
			if test.wantErr != "" {
				if err == nil {
					t.Fatalf("Parse(%q) = %+v, want error %q", test.args, got, test.wantErr)
				}
				if !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("Parse(%q) error = %q, want it to contain %q", test.args, err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Parse(%q) unexpected error: %v", test.args, err)
			}
			if !sameParsed(got, test.want) {
				t.Fatalf("Parse(%q) =\n  %+v\nwant\n  %+v", test.args, got, test.want)
			}
		})
	}
}

// sameParsed compares two Parsed values, treating a nil Args and an empty Args
// as equal: only the content of the argument list is meaningful.
func sameParsed(got, want Parsed) bool {
	if got.Options != want.Options || got.Command != want.Command {
		return false
	}
	if len(got.Args) != len(want.Args) {
		return false
	}
	return len(got.Args) == 0 || reflect.DeepEqual(got.Args, want.Args)
}

// Regression for the documented `clother bench --prompt "..."`, rejected by the
// old flat parser with "unknown option --prompt".
func TestParseKeepsDocumentedBenchPrompt(t *testing.T) {
	parsed, err := Parse([]string{"bench", "--prompt", "Write a haiku"})
	if err != nil {
		t.Fatalf("documented invocation rejected: %v", err)
	}
	if parsed.Command != "bench" {
		t.Fatalf("command = %q, want bench", parsed.Command)
	}
	if len(parsed.Args) != 2 || parsed.Args[0] != "--prompt" || parsed.Args[1] != "Write a haiku" {
		t.Fatalf("args = %q, want [--prompt \"Write a haiku\"]", parsed.Args)
	}
}

// Regression: --bin-dir drives where launchers are written and removed, so it
// must never silently absorb the next option as a path.
func TestParseBinDirRejectsOptionAsValue(t *testing.T) {
	if _, err := Parse([]string{"install", "--bin-dir", "--json"}); err == nil {
		t.Fatal("--bin-dir swallowed --json as a path")
	}
	parsed, err := Parse([]string{"install", "--bin-dir", "/tmp/x"})
	if err != nil || parsed.Options.BinDir != "/tmp/x" {
		t.Fatalf("--bin-dir /tmp/x = %q, %v", parsed.Options.BinDir, err)
	}
}

func TestParseLauncher(t *testing.T) {
	options, forwarded := ParseLauncher([]string{"--no-banner", "-p", "hello", "--yolo"})
	if !options.NoBanner {
		t.Fatal("--no-banner not detected")
	}
	want := []string{"-p", "hello", "--yolo"}
	if !reflect.DeepEqual(forwarded, want) {
		t.Fatalf("forwarded = %q, want %q", forwarded, want)
	}
}

func TestParseLauncherKeepsOrderAndTerminator(t *testing.T) {
	options, forwarded := ParseLauncher([]string{"a", "--", "--no-banner", "b"})
	if options.NoBanner {
		t.Fatal("--no-banner after -- must be forwarded, not consumed")
	}
	want := []string{"a", "--", "--no-banner", "b"}
	if !reflect.DeepEqual(forwarded, want) {
		t.Fatalf("forwarded = %q, want %q", forwarded, want)
	}
}
