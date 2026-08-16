package ui

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"sync"
	"syscall"
)

// ErrNoInput is returned when an answer is required but no input is available:
// --no-input was requested, or stdin reached EOF before the question was
// answered. Callers can test it with errors.Is.
var ErrNoInput = errors.New("no input available")

// ErrNoTerminal is returned when a secret is requested and the terminal cannot
// be put in no-echo mode. Clother never falls back to a visible prompt for a
// secret.
var ErrNoTerminal = errors.New("no usable terminal for secret input")

type Prompter struct {
	In  io.Reader
	Out io.Writer
	// NoInput makes every question fail immediately instead of blocking, for CI
	// and scripted runs (--no-input).
	NoInput bool

	// reader is created once and reused: a bufio.Reader reads ahead up to its
	// buffer size, so allocating one per question throws away everything already
	// buffered and loses every piped answer after the first.
	reader *bufio.Reader
	once   sync.Once
}

func NewPrompter(in io.Reader, out io.Writer) *Prompter {
	return &Prompter{In: in, Out: out}
}

func (p *Prompter) lineReader() *bufio.Reader {
	p.once.Do(func() {
		if p.reader == nil {
			p.reader = bufio.NewReader(p.In)
		}
	})
	return p.reader
}

func (p *Prompter) Prompt(label, defaultValue string) (string, error) {
	if p.NoInput {
		return "", fmt.Errorf("%w: %s is required (--no-input)", ErrNoInput, label)
	}
	if defaultValue != "" {
		fmt.Fprintf(p.Out, "%s [%s]: ", label, defaultValue)
	} else {
		fmt.Fprintf(p.Out, "%s: ", label)
	}
	value, err := p.lineReader().ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	value = strings.TrimSpace(value)
	if value == "" && errors.Is(err, io.EOF) && defaultValue == "" {
		// Silently returning "" here is what made scripted configuration fail
		// with misleading messages ("base URL is required") instead of saying
		// that nothing was piped in.
		fmt.Fprintln(p.Out)
		return "", fmt.Errorf("%w: %s was not answered (stdin reached EOF)", ErrNoInput, label)
	}
	if value == "" {
		return defaultValue, nil
	}
	return value, nil
}

// PromptSecret reads a secret from the controlling terminal with echo disabled.
//
// It never degrades to an echoing prompt: if /dev/tty cannot be opened, or the
// terminal state cannot be saved and changed, it fails and tells the caller to
// provide the value another way. The terminal state is saved verbatim and
// restored both by defer and by a signal handler, so Ctrl-C during the entry
// does not leave the shell without echo.
func (p *Prompter) PromptSecret(label string) (string, error) {
	if p.NoInput {
		return "", fmt.Errorf("%w: %s is required (--no-input)", ErrNoInput, label)
	}

	tty, err := os.OpenFile(ttyDevice, os.O_RDWR, 0)
	if err != nil {
		return "", fmt.Errorf("%w: cannot open %s (%v); set the key in the secrets file or run this in a terminal", ErrNoTerminal, ttyDevice, err)
	}
	defer func() { _ = tty.Close() }()

	saved, err := ttyState(tty)
	if err != nil {
		return "", fmt.Errorf("%w: cannot read the terminal state (%v); refusing to read %s with echo on", ErrNoTerminal, err, label)
	}

	var restoreOnce sync.Once
	restore := func() {
		restoreOnce.Do(func() { _ = restoreTTYState(tty, saved) })
	}
	stopSignals := restoreOnSignal(restore)
	// Defers run last-in-first-out: restore() first, then the handler is removed.
	// The reverse order would leave a window where a signal arriving after
	// signal.Stop kills the process with the echo still off.
	defer stopSignals()
	defer restore()

	if err := setTTYEcho(tty, false); err != nil {
		restore()
		return "", fmt.Errorf("%w: cannot disable terminal echo (%v); refusing to read %s in the clear", ErrNoTerminal, err, label)
	}

	// The prompt is written only once echo is off: written before, a value typed
	// or pasted ahead of it would be echoed.
	fmt.Fprintf(tty, "%s: ", label)
	value, readErr := readLine(tty)
	fmt.Fprintln(tty)
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return "", readErr
	}
	value = strings.TrimSpace(value)
	if value == "" && errors.Is(readErr, io.EOF) {
		return "", fmt.Errorf("%w: %s was not answered", ErrNoInput, label)
	}
	return value, nil
}

// ConfirmDestructive asks a question whose "yes" deletes something.
//
// It has no default-yes form on purpose. Confirm returns its default on EOF,
// which is safe today only because both destructive callers happen to pass
// false; the day one is written with defaultYes=true, `clother ... < /dev/null`
// would confirm the deletion by itself. Deletion paths go through this
// function, which cannot be given a permissive default.
func (p *Prompter) ConfirmDestructive(label string) (bool, error) {
	return p.Confirm(label, false)
}

func (p *Prompter) Confirm(label string, defaultYes bool) (bool, error) {
	hint := "[y/N]"
	if defaultYes {
		hint = "[Y/n]"
	}
	answer, err := p.Prompt(label+" "+hint, "")
	if err != nil {
		if errors.Is(err, ErrNoInput) && !p.NoInput {
			// No answer piped in: keep the safe default rather than failing.
			return defaultYes, nil
		}
		return false, err
	}
	answer = strings.TrimSpace(strings.ToLower(answer))
	if answer == "" {
		return defaultYes, nil
	}
	return strings.HasPrefix(answer, "y"), nil
}

// readLine reads one line byte by byte. bufio would read ahead and swallow input
// meant for whatever runs after us on the same terminal.
func readLine(file *os.File) (string, error) {
	var out []byte
	buf := make([]byte, 1)
	for {
		n, err := file.Read(buf)
		if n > 0 {
			if buf[0] == '\n' {
				return string(out), nil
			}
			out = append(out, buf[0])
		}
		if err != nil {
			return string(out), err
		}
	}
}

// restoreOnSignal restores the terminal if the process is interrupted while the
// echo is off, then exits: a Ctrl-C during a key entry must not leave the shell
// without echo. The returned function removes the handler and stops the
// goroutine; the restore is idempotent, so it does not matter if both fire.
func restoreOnSignal(restore func()) func() {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT)
	done := make(chan struct{})
	go func() {
		select {
		case <-signals:
			restore()
			os.Exit(130)
		case <-done:
		}
	}()
	return func() {
		signal.Stop(signals)
		close(done)
	}
}

// ttyDevice and sttyCandidates are variables so tests can point them elsewhere.
var (
	ttyDevice       = "/dev/tty"
	sttyCandidates  = []string{"/bin/stty", "/usr/bin/stty"}
	errSttyNotFound = errors.New("stty not found")
)

// ttyState returns the full terminal state as `stty -g` prints it.
func ttyState(tty *os.File) (string, error) {
	return runStty(tty, "-g")
}

// restoreTTYState puts back the exact state returned by ttyState, rather than
// forcing `echo` on, which would clobber a deliberate `-echo` of the user.
func restoreTTYState(tty *os.File, saved string) error {
	if strings.TrimSpace(saved) == "" {
		_, err := runStty(tty, "echo")
		return err
	}
	_, err := runStty(tty, saved)
	return err
}

func setTTYEcho(tty *os.File, enabled bool) error {
	flag := "-echo"
	if enabled {
		flag = "echo"
	}
	_, err := runStty(tty, flag)
	return err
}

// sttyPath resolves stty from fixed system locations first: resolving it through
// PATH would let anything named `stty` earlier in PATH run at the exact moment a
// key is about to be typed.
func sttyPath() (string, error) {
	for _, candidate := range sttyCandidates {
		info, err := os.Stat(candidate)
		if err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return candidate, nil
		}
	}
	return "", errSttyNotFound
}

func runStty(tty *os.File, args ...string) (string, error) {
	path, err := sttyPath()
	if err != nil {
		return "", err
	}
	full := append(ttyDeviceArgs(), args...)
	cmd := exec.Command(path, full...)
	cmd.Stdin = tty
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = io.Discard
	// A hijacked environment cannot make stty read anything else than the tty we
	// opened ourselves.
	cmd.Env = []string{"PATH=/bin:/usr/bin", "LC_ALL=C"}
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return strings.TrimSpace(stdout.String()), nil
}

func ttyDeviceArgs() []string {
	switch runtime.GOOS {
	case "darwin", "freebsd", "netbsd", "openbsd", "dragonfly":
		return []string{"-f", ttyDevice}
	default:
		return []string{"-F", ttyDevice}
	}
}
