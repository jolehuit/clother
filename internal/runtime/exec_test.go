package runtime

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

// D7/D8/D16: claude shares clother's process group and controlling terminal, so
// the kernel already delivers SIGINT, SIGQUIT, SIGTSTP and SIGWINCH to it.
// Relaying those doubled every Ctrl-C, and catching SIGTSTP kept clother
// running while the shell waited for a stopped job that never came.
func TestSignalPolicyDoesNotDuplicateTerminalSignals(t *testing.T) {
	t.Parallel()

	for _, sig := range []syscall.Signal{syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTSTP, syscall.SIGWINCH} {
		if isForwarded(sig) {
			t.Fatalf("%v is delivered to the whole process group by the tty; forwarding it delivers it twice", sig)
		}
	}
	for _, sig := range []os.Signal{syscall.SIGTERM, syscall.SIGHUP} {
		if !isForwarded(sig.(syscall.Signal)) {
			t.Fatalf("%v can target clother alone and must reach claude", sig)
		}
	}
	for _, sig := range append(append([]os.Signal{}, observedSignals...), forwardedSignals...) {
		if sig == syscall.Signal(syscall.SIGTSTP) {
			t.Fatal("SIGTSTP must keep its default disposition so Ctrl-Z gives the shell a stopped job")
		}
	}
	// SIGURG is emitted continuously by the Go runtime for async preemption;
	// notifying on it flooded the child with pointless signals.
	for _, sig := range append(append([]os.Signal{}, observedSignals...), forwardedSignals...) {
		if sig == syscall.Signal(syscall.SIGURG) {
			t.Fatal("SIGURG must never be observed nor forwarded")
		}
	}
}

// D18: forwardSignals looped over a channel that was never closed, so the
// goroutine outlived every call.
func TestForwardSignalsStopsWhenChannelCloses(t *testing.T) {
	t.Parallel()

	signals := make(chan os.Signal, 1)
	done := make(chan struct{})
	go func() {
		forwardSignals(nil, signals)
		close(done)
	}()
	close(signals)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("forwardSignals did not return after the channel was closed")
	}
}

// D13: ExitCode() is -1 for a child killed by a signal, and os.Exit(-1) shows
// up as 255 — indistinguishable from a generic failure.
func TestExitCodeOfSignaledChild(t *testing.T) {
	t.Parallel()

	err := exec.Command("/bin/sh", "-c", "kill -TERM $$").Run()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected an ExitError, got %v", err)
	}
	if got := exitErr.ExitCode(); got != -1 {
		t.Skipf("this platform reports %d for a signaled child; the conversion is a no-op here", got)
	}
	if got := exitCodeOf(exitErr); got != 128+int(syscall.SIGTERM) {
		t.Fatalf("exitCodeOf() = %d, want %d", got, 128+int(syscall.SIGTERM))
	}
}

func TestExitCodeOfNormalExit(t *testing.T) {
	t.Parallel()

	err := exec.Command("/bin/sh", "-c", "exit 42").Run()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected an ExitError, got %v", err)
	}
	if got := exitCodeOf(exitErr); got != 42 {
		t.Fatalf("exitCodeOf() = %d, want 42", got)
	}
}
