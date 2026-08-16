package runtime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/jolehuit/clother/internal/config"
	"github.com/jolehuit/clother/internal/profiles"
	"github.com/jolehuit/clother/internal/session"
	"github.com/jolehuit/clother/internal/ui"
	"github.com/jolehuit/clother/internal/update"
	"github.com/jolehuit/clother/internal/version"
)

type RunOptions struct {
	NoBanner bool
}

func Launch(ctx context.Context, paths config.Paths, target profiles.Target, args []string, env []string, options RunOptions) (int, error) {
	args = NormalizeClaudeArgs(args)
	var cleanup func()
	var err error
	env, cleanup, err = PrepareClaudeConfigOverlay(target, args, env)
	if err != nil {
		return 1, err
	}
	defer cleanup()
	// Homebrew installs are upgraded by brew, so the "Run: clother install"
	// hint is wrong for them — same guard as the `claude` shim.
	if isTTY(os.Stderr) && !IsHomebrew() {
		if message, err := update.MaybeMessage(paths, version.Value, time.Now()); err == nil && message != "" {
			fmt.Fprintln(os.Stderr, message)
		}
	}
	if !options.NoBanner && isTTY(os.Stdout) {
		fmt.Fprint(os.Stdout, ui.Banner(target.DisplayName))
	}

	claudePath, err := FindRealClaude(paths)
	if err != nil {
		return 1, fmt.Errorf("%w\ninstall Claude Code: curl -fsSL https://claude.ai/install.sh | bash", err)
	}

	if err := session.RestoreStale(paths); err != nil {
		return 1, err
	}

	if session.RequiresClaudeSanitization(target.Family) {
		if code, handled, err := runWithTemporaryPatch(ctx, claudePath, paths, args, env, "clother-"+target.Profile); handled {
			return code, err
		}
	}

	return runClaudeCommand(ctx, claudePath, args, env, "clother-"+target.Profile)
}

func runWithTemporaryPatch(ctx context.Context, claudePath string, paths config.Paths, args []string, env []string, resumeCommand string) (int, bool, error) {
	// ResumeOverride, unlike session.ResumeID, stops at `--` and ignores option
	// value positions: the id it returns drives an in-place rewrite of the
	// transcript on disk, so it must never come from a data token.
	resumeID := ResumeOverride(args)
	if resumeID == "" {
		return 0, false, nil
	}
	sessionRoot := filepath.Join(userHomeDir(), ".claude", "projects")
	sessionPath, err := session.FindSession(sessionRoot, resumeID)
	if errors.Is(err, session.ErrSessionNotFound) {
		return 0, false, nil
	}
	if err != nil {
		return 1, true, err
	}
	patch, analysis, err := session.PrepareTemporaryPatch(paths, sessionPath)
	if err != nil {
		return 1, true, err
	}
	if patch == nil || !analysis.NeedsSanitization {
		return 0, false, nil
	}
	if err := patch.Apply(); err != nil {
		return 1, true, err
	}
	// RestoreOrWarn, not Restore: a discarded error here means the user walks
	// away with a transcript still missing its thinking blocks and no idea the
	// .orig backup exists. The warning names both paths on stderr.
	defer patch.RestoreOrWarn()

	code, err := runClaudeCommand(ctx, claudePath, args, env, resumeCommand)
	return code, true, err
}

// claude runs in the same process group and on the same controlling terminal
// as clother, so the kernel already delivers SIGINT, SIGQUIT, SIGWINCH and
// SIGTSTP to it. Relaying those would deliver them twice — one Ctrl-C reaching
// claude as two would quit the session instead of cancelling the current turn.
//
// observedSignals are caught only to keep clother alive while claude handles
// them; they are dropped, not forwarded. SIGTSTP is deliberately absent so
// Ctrl-Z keeps its default disposition and the shell gets a stopped job back.
var observedSignals = []os.Signal{syscall.SIGINT, syscall.SIGQUIT}

// forwardedSignals are the ones a `kill` can send to clother alone; those must
// reach claude.
var forwardedSignals = []os.Signal{syscall.SIGTERM, syscall.SIGHUP}

func forwardSignals(process *os.Process, signals <-chan os.Signal) {
	for sig := range signals {
		signalValue, ok := sig.(syscall.Signal)
		if !ok || !isForwarded(signalValue) {
			continue
		}
		_ = process.Signal(signalValue)
	}
}

func isForwarded(sig syscall.Signal) bool {
	for _, candidate := range forwardedSignals {
		if candidate == sig {
			return true
		}
	}
	return false
}

func runClaudeCommand(ctx context.Context, claudePath string, args []string, env []string, resumeCommand string) (int, error) {
	before, cwd := currentProjectSession()

	cmd := exec.CommandContext(ctx, claudePath, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = env

	signals := make(chan os.Signal, 32)
	signal.Notify(signals, append(append([]os.Signal{}, observedSignals...), forwardedSignals...)...)
	defer func() {
		// Stop first: once it returns the signal package no longer writes to
		// the channel, so closing it is safe and lets forwardSignals exit
		// instead of leaking for the lifetime of the process.
		signal.Stop(signals)
		close(signals)
	}()

	if err := cmd.Start(); err != nil {
		return 1, err
	}
	go forwardSignals(cmd.Process, signals)

	if err := cmd.Wait(); err != nil {
		printResumeHintFromProject(cwd, before, resumeCommand)
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return exitCodeOf(exitErr), nil
		}
		return 1, err
	}
	printResumeHintFromProject(cwd, before, resumeCommand)
	return 0, nil
}

// exitCodeOf converts a child status into a shell exit code. ExitCode() is -1
// when the child died from a signal, and os.Exit(-1) surfaces as 255, which is
// indistinguishable from a generic failure; the shell convention is 128+N.
func exitCodeOf(exitErr *exec.ExitError) int {
	if status, ok := exitErr.Sys().(syscall.WaitStatus); ok && status.Signaled() {
		return 128 + int(status.Signal())
	}
	code := exitErr.ExitCode()
	if code < 0 || code > 255 {
		return 1
	}
	return code
}

func currentProjectSession() (session.ProjectSession, string) {
	cwd, err := os.Getwd()
	if err != nil {
		return session.ProjectSession{}, ""
	}
	root := filepath.Join(userHomeDir(), ".claude", "projects")
	latest, err := session.LatestInProject(root, cwd)
	if err != nil {
		return session.ProjectSession{}, cwd
	}
	return latest, cwd
}

func printResumeHintFromProject(cwd string, before session.ProjectSession, resumeCommand string) {
	if resumeCommand == "" || !isTTY(os.Stdout) || cwd == "" {
		return
	}
	root := filepath.Join(userHomeDir(), ".claude", "projects")
	after, err := session.LatestInProject(root, cwd)
	if err != nil || !session.ChangedProjectSession(before, after) {
		return
	}
	fmt.Fprintf(os.Stdout, "\nOr reopen with the same provider:\n%s --resume %s\n", resumeCommand, after.ID)
}

func isTTY(file *os.File) bool {
	info, err := file.Stat()
	return err == nil && (info.Mode()&os.ModeCharDevice) != 0
}

func userHomeDir() string {
	home, _ := os.UserHomeDir()
	return home
}
