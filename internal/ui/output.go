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
}

func New(format Format, quiet bool) *Output {
	stdout := os.Stdout
	stderr := os.Stderr
	color := format == FormatHuman && os.Getenv("NO_COLOR") == "" && isTTY(stdout)
	return &Output{
		Stdout: stdout,
		Stderr: stderr,
		Format: format,
		Quiet:  quiet,
		Color:  color,
	}
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
	if o.Color {
		label = "\033[1;33m⚠\033[0m"
	}
	fmt.Fprintf(o.Stderr, "%s %s\n", label, fmt.Sprintf(format, args...))
}

func (o *Output) Error(format string, args ...any) {
	label := "ERR"
	if o.Color {
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
