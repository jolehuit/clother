package ui

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

type Prompter struct {
	In  io.Reader
	Out io.Writer
}

func NewPrompter(in io.Reader, out io.Writer) *Prompter {
	return &Prompter{In: in, Out: out}
}

func (p *Prompter) Prompt(label, defaultValue string) (string, error) {
	if defaultValue != "" {
		fmt.Fprintf(p.Out, "%s [%s]: ", label, defaultValue)
	} else {
		fmt.Fprintf(p.Out, "%s: ", label)
	}
	reader := bufio.NewReader(p.In)
	value, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return defaultValue, nil
	}
	return value, nil
}

func (p *Prompter) PromptSecret(label string) (string, error) {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return p.Prompt(label, "")
	}
	defer tty.Close()

	fmt.Fprintf(tty, "%s: ", label)
	if err := setTTYEcho(false); err != nil {
		return p.Prompt(label, "")
	}
	defer setTTYEcho(true)

	reader := bufio.NewReader(tty)
	value, readErr := reader.ReadString('\n')
	fmt.Fprintln(tty)
	if readErr != nil && readErr != io.EOF {
		return "", readErr
	}
	return strings.TrimSpace(value), nil
}

func (p *Prompter) Confirm(label string, defaultYes bool) (bool, error) {
	hint := "[y/N]"
	if defaultYes {
		hint = "[Y/n]"
	}
	answer, err := p.Prompt(label+" "+hint, "")
	if err != nil {
		return false, err
	}
	answer = strings.TrimSpace(strings.ToLower(answer))
	if answer == "" {
		return defaultYes, nil
	}
	return strings.HasPrefix(answer, "y"), nil
}

func setTTYEcho(enabled bool) error {
	args := []string{}
	switch runtime.GOOS {
	case "darwin", "freebsd":
		args = append(args, "-f", "/dev/tty")
	default:
		args = append(args, "-F", "/dev/tty")
	}
	if enabled {
		args = append(args, "echo")
	} else {
		args = append(args, "-echo")
	}
	cmd := exec.Command("stty", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	return cmd.Run()
}
