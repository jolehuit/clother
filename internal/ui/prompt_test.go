package ui

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Regression: a bufio.Reader used to be allocated per question, so it read ahead
// up to 4096 bytes and threw away everything after the first line. Piped and
// heredoc configuration silently took the defaults.
func TestPromptKeepsPipedAnswersAfterTheFirst(t *testing.T) {
	prompter := NewPrompter(strings.NewReader("myprov\nhttps://api.example.com\nmy-model-1\n"), io.Discard)

	want := []string{"myprov", "https://api.example.com", "my-model-1"}
	for i, expected := range want {
		got, err := prompter.Prompt("question", "")
		if err != nil {
			t.Fatalf("answer %d: unexpected error: %v", i+1, err)
		}
		if got != expected {
			t.Fatalf("answer %d = %q, want %q (piped answers after the first are lost)", i+1, got, expected)
		}
	}
}

func TestPromptUsesDefaultOnEmptyLine(t *testing.T) {
	prompter := NewPrompter(strings.NewReader("\nexplicit\n"), io.Discard)

	got, err := prompter.Prompt("question", "fallback")
	if err != nil {
		t.Fatal(err)
	}
	if got != "fallback" {
		t.Fatalf("empty answer = %q, want the default", got)
	}
	got, err = prompter.Prompt("question", "fallback")
	if err != nil {
		t.Fatal(err)
	}
	if got != "explicit" {
		t.Fatalf("second answer = %q, want explicit", got)
	}
}

func TestPromptReportsEOFWhenNoDefault(t *testing.T) {
	prompter := NewPrompter(strings.NewReader(""), io.Discard)

	got, err := prompter.Prompt("Base URL", "")
	if !errors.Is(err, ErrNoInput) {
		t.Fatalf("Prompt on empty stdin = %q, %v; want ErrNoInput", got, err)
	}
	if !strings.Contains(err.Error(), "Base URL") {
		t.Fatalf("error %q does not name the question", err)
	}
}

func TestPromptEOFStillHonoursADefault(t *testing.T) {
	prompter := NewPrompter(strings.NewReader(""), io.Discard)

	got, err := prompter.Prompt("Model", "glm-4.7")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "glm-4.7" {
		t.Fatalf("got %q, want the default", got)
	}
}

func TestNoInputFailsEveryQuestion(t *testing.T) {
	prompter := NewPrompter(strings.NewReader("typed\n"), io.Discard)
	prompter.NoInput = true

	if _, err := prompter.Prompt("Model", "default"); !errors.Is(err, ErrNoInput) {
		t.Fatalf("Prompt with --no-input = %v, want ErrNoInput", err)
	}
	if _, err := prompter.PromptSecret("API key"); !errors.Is(err, ErrNoInput) {
		t.Fatalf("PromptSecret with --no-input = %v, want ErrNoInput", err)
	}
	if _, err := prompter.Confirm("Continue?", false); !errors.Is(err, ErrNoInput) {
		t.Fatalf("Confirm with --no-input = %v, want ErrNoInput", err)
	}
}

func TestConfirm(t *testing.T) {
	tests := []struct {
		input      string
		defaultYes bool
		want       bool
	}{
		{"y\n", false, true},
		{"yes\n", false, true},
		{"Y\n", false, true},
		{"n\n", true, false},
		{"no\n", true, false},
		{"\n", true, true},
		{"\n", false, false},
		{"maybe\n", true, false},
	}
	for _, test := range tests {
		prompter := NewPrompter(strings.NewReader(test.input), io.Discard)
		got, err := prompter.Confirm("Continue?", test.defaultYes)
		if err != nil {
			t.Fatalf("Confirm(%q): %v", test.input, err)
		}
		if got != test.want {
			t.Fatalf("Confirm(%q, default=%v) = %v, want %v", test.input, test.defaultYes, got, test.want)
		}
	}
}

// Regression: PromptSecret used to fall back to the plain, echoing Prompt when
// /dev/tty could not be opened, so the key was typed in the clear with no
// warning. It must fail instead, and must not read the secret from stdin.
func TestPromptSecretNeverFallsBackToAnEchoingPrompt(t *testing.T) {
	ttyDevice = filepath.Join(t.TempDir(), "no-such-tty")
	t.Cleanup(func() { ttyDevice = "/dev/tty" })

	var out strings.Builder
	prompter := NewPrompter(strings.NewReader("sk-not-a-real-key\n"), &out)

	got, err := prompter.PromptSecret("API key")
	if err == nil {
		t.Fatalf("PromptSecret succeeded without a terminal, returning %q", got)
	}
	if !errors.Is(err, ErrNoTerminal) {
		t.Fatalf("error = %v, want ErrNoTerminal", err)
	}
	if got != "" {
		t.Fatalf("PromptSecret returned %q; it read the secret from stdin with echo on", got)
	}
	if strings.Contains(out.String(), "API key") {
		t.Fatalf("PromptSecret prompted on the visible stream: %q", out.String())
	}
}

// Regression: PromptSecret must also refuse when the terminal state cannot be
// saved and restored, rather than reading the key with echo on.
func TestPromptSecretFailsWhenSttyIsUnavailable(t *testing.T) {
	tty := filepath.Join(t.TempDir(), "fake-tty")
	if err := os.WriteFile(tty, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	ttyDevice = tty
	sttyCandidates = []string{filepath.Join(t.TempDir(), "no-stty-here")}
	t.Cleanup(func() {
		ttyDevice = "/dev/tty"
		sttyCandidates = []string{"/bin/stty", "/usr/bin/stty"}
	})

	prompter := NewPrompter(strings.NewReader("sk-not-a-real-key\n"), io.Discard)
	got, err := prompter.PromptSecret("API key")
	if err == nil || got != "" {
		t.Fatalf("PromptSecret = %q, %v; want a failure when stty is missing", got, err)
	}
	if !errors.Is(err, ErrNoTerminal) {
		t.Fatalf("error = %v, want ErrNoTerminal", err)
	}
}

// PromptSecret must save the terminal state verbatim (stty -g), disable echo,
// and put the saved state back — never force `echo` on, which would clobber a
// deliberate -echo. And the prompt must be written only once echo is off, so a
// value typed or pasted ahead of it is not echoed.
func TestPromptSecretSavesAndRestoresTheTerminalState(t *testing.T) {
	dir := t.TempDir()
	record := filepath.Join(dir, "record")
	if err := os.MkdirAll(record, 0o755); err != nil {
		t.Fatal(err)
	}

	// The fake stty records every invocation. The record directory is baked into
	// the script because runStty deliberately hands the child a minimal
	// environment.
	script := "#!/bin/sh\n" +
		"tty=\"$2\"\n" +
		"shift 2\n" +
		"echo \"$*\" >> " + record + "/calls\n" +
		"case \"$1\" in\n" +
		"  -g) echo SAVED-STATE-42 ;;\n" +
		"  -echo) cp \"$tty\" " + record + "/tty-at-echo-off ;;\n" +
		"esac\n" +
		"exit 0\n"
	fakeStty := filepath.Join(dir, "stty")
	if err := os.WriteFile(fakeStty, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	// The fake tty is a regular file. PromptSecret writes the prompt at offset 0,
	// so the answer is placed right after a padding of the prompt's length.
	const label = "API key"
	const secret = "sk-not-a-real-key"
	padding := strings.Repeat("_", len(label)+len(": "))
	ttyFile := filepath.Join(dir, "fake-tty")
	if err := os.WriteFile(ttyFile, []byte(padding+secret+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	ttyDevice = ttyFile
	sttyCandidates = []string{fakeStty}
	t.Cleanup(func() {
		ttyDevice = "/dev/tty"
		sttyCandidates = []string{"/bin/stty", "/usr/bin/stty"}
	})

	got, err := NewPrompter(strings.NewReader(""), io.Discard).PromptSecret(label)
	if err != nil {
		t.Fatalf("PromptSecret: %v", err)
	}
	if got != secret {
		t.Fatalf("PromptSecret = %q, want %q", got, secret)
	}

	calls, err := os.ReadFile(filepath.Join(record, "calls"))
	if err != nil {
		t.Fatalf("stty was never called: %v", err)
	}
	want := "-g\n-echo\nSAVED-STATE-42\n"
	if string(calls) != want {
		t.Fatalf("stty calls =\n%q\nwant\n%q (state must be saved, echo disabled, then the saved state restored verbatim)", calls, want)
	}

	// Nothing had been written to the terminal when echo was disabled.
	atEchoOff, err := os.ReadFile(filepath.Join(record, "tty-at-echo-off"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(atEchoOff), label) {
		t.Fatalf("the prompt was written before echo was disabled: %q", atEchoOff)
	}
}

// stty must not be resolved through PATH: anything named stty earlier in PATH
// would run at the exact moment a key is about to be typed.
func TestSttyIsNotResolvedThroughPATH(t *testing.T) {
	dir := t.TempDir()
	fake := filepath.Join(dir, "stty")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	path, err := sttyPath()
	if err != nil {
		t.Skipf("no system stty on this machine: %v", err)
	}
	if path == fake {
		t.Fatalf("sttyPath() = %q, a PATH-resolved binary", path)
	}
	if !strings.HasPrefix(path, "/bin/") && !strings.HasPrefix(path, "/usr/bin/") {
		t.Fatalf("sttyPath() = %q, want a fixed system location", path)
	}
}

// Confirm returns its default on EOF, which is only safe because both
// destructive callers happen to pass false. ConfirmDestructive removes the
// possibility: there is no permissive default to pass, so `clother remove ... <
// /dev/null` can never confirm its own deletion.
func TestConfirmDestructiveRefusesOnEOF(t *testing.T) {
	prompter := NewPrompter(strings.NewReader(""), io.Discard)
	ok, err := prompter.ConfirmDestructive("Remove everything?")
	if err != nil {
		t.Fatalf("ConfirmDestructive returned an error on EOF: %v", err)
	}
	if ok {
		t.Fatal("an empty stdin confirmed a destructive action")
	}
}

func TestConfirmDestructiveAcceptsAnExplicitYes(t *testing.T) {
	prompter := NewPrompter(strings.NewReader("y\n"), io.Discard)
	ok, err := prompter.ConfirmDestructive("Remove everything?")
	if err != nil || !ok {
		t.Fatalf("ConfirmDestructive(y) = %v, %v", ok, err)
	}
}
