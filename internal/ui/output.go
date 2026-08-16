package ui

import (
	"fmt"
	"io"
	"os"
	"strings"
)

type Format string

const (
	FormatHuman Format = "human"
	FormatJSON  Format = "json"
	FormatPlain Format = "plain"
)

type Output struct {
	Stdout io.Writer
	Stderr io.Writer
	Format Format
	Quiet  bool
	Color  bool
	// ErrColor tracks stderr separately: stdout may be a pipe while stderr is
	// still a terminal, and the reverse.
	ErrColor bool
	// Verbose enables Verbosef diagnostics on stderr (-v).
	Verbose bool
	// Debug enables Debugf tracing on stderr (-d), and implies Verbose.
	Debug bool
}

// New builds an Output on the process stdout/stderr.
func New(format Format, quiet bool) *Output {
	return NewOutput(os.Stdout, os.Stderr, format, quiet)
}

// NewOutput builds an Output on arbitrary writers. Color is enabled only for a
// human-format terminal that has not opted out through NO_COLOR, and is decided
// per stream so nothing colored is ever written into a pipe or a file.
func NewOutput(stdout, stderr io.Writer, format Format, quiet bool) *Output {
	noColor := os.Getenv("NO_COLOR") != ""
	colorable := format == FormatHuman && !noColor
	return &Output{
		Stdout:   stdout,
		Stderr:   stderr,
		Format:   format,
		Quiet:    quiet,
		Color:    colorable && isTerminal(stdout),
		ErrColor: colorable && isTerminal(stderr),
	}
}

func isTerminal(w io.Writer) bool {
	file, ok := w.(*os.File)
	if !ok {
		return false
	}
	return isTTY(file)
}

func isTTY(file *os.File) bool {
	info, err := file.Stat()
	return err == nil && (info.Mode()&os.ModeCharDevice) != 0
}

func (o *Output) Header(title string) {
	if o.Format != FormatHuman || o.Quiet {
		return
	}
	fmt.Fprintln(o.Stdout, o.style("bold", title))
}

func (o *Output) Line(format string, args ...any) {
	if o.Quiet {
		return
	}
	fmt.Fprintf(o.Stdout, format+"\n", args...)
}

func (o *Output) ErrLine(format string, args ...any) {
	fmt.Fprintf(o.Stderr, format+"\n", args...)
}

// Verbosef writes a diagnostic line on stderr when --verbose (or --debug, which
// implies it) is on. Diagnostics never go to stdout: a script parsing stdout
// must see exactly the same bytes with and without -v.
func (o *Output) Verbosef(format string, args ...any) {
	if !o.Verbose && !o.Debug {
		return
	}
	fmt.Fprintf(o.Stderr, "clother: "+format+"\n", args...)
}

// Debugf writes a diagnostic line on stderr when --debug (or -d) is on. It is a
// no-op otherwise, so callers do not have to guard it.
func (o *Output) Debugf(format string, args ...any) {
	if !o.Debug {
		return
	}
	fmt.Fprintf(o.Stderr, "clother: debug: "+format+"\n", args...)
}

func (o *Output) Success(format string, args ...any) {
	if o.Quiet {
		return
	}
	label := "OK"
	if o.Color {
		label = "\033[0;32m✓\033[0m"
	}
	fmt.Fprintf(o.Stdout, "%s %s\n", label, fmt.Sprintf(format, args...))
}

func (o *Output) Warn(format string, args ...any) {
	label := "WARN"
	if o.ErrColor {
		label = "\033[1;33m⚠\033[0m"
	}
	fmt.Fprintf(o.Stderr, "%s %s\n", label, fmt.Sprintf(format, args...))
}

func (o *Output) Error(format string, args ...any) {
	label := "ERR"
	if o.ErrColor {
		label = "\033[0;31m✗\033[0m"
	}
	fmt.Fprintf(o.Stderr, "%s %s\n", label, fmt.Sprintf(format, args...))
}

func (o *Output) style(kind, input string) string {
	if !o.Color {
		return input
	}
	switch kind {
	case "bold":
		return "\033[1m" + input + "\033[0m"
	case "dim":
		return "\033[2m" + input + "\033[0m"
	default:
		return input
	}
}

func Banner(name string) string {
	lines := []string{
		"  ____ _       _   _",
		" / ___| | ___ | |_| |__   ___ _ __",
		"| |   | |/ _ \\| __| '_ \\ / _ \\ '__|",
		"| |___| | (_) | |_| | | |  __/ |",
		" \\____|_|\\___/ \\__|_| |_|\\___|_|",
	}
	return strings.Join(append(lines,
		"    + "+name,
		"    Tip: add --yolo to skip permission prompts (--dangerously-skip-permissions)",
		"",
	), "\n")
}
