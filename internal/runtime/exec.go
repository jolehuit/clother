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
	if isTTY(os.Stderr) {
		if message, err := update.MaybeMessage(paths, version.Value, time.Now()); err == nil && message != "" {
			fmt.Fprintln(os.Stderr, message)
		}
	}
	if !options.NoBanner && isTTY(os.Stdout) {
		fmt.Fprint(os.Stdout, ui.Banner(target.DisplayName))
	}

	claudePath, err := FindRealClaude(paths)
	if err != nil {
		return 1, fmt.Errorf("real claude not found in PATH")
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
	resumeID := session.ResumeID(args)
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
	defer patch.Restore()

	code, err := runClaudeCommand(ctx, claudePath, args, env, resumeCommand)
	return code, true, err
}

func forwardSignals(process *os.Process, signals <-chan os.Signal) {
	for sig := range signals {
		if signalValue, ok := sig.(syscall.Signal); ok {
			_ = process.Signal(signalValue)
		}
	}
}

func execReplace(path string, args []string, env []string) (int, error) {
	argv := append([]string{"claude"}, args...)
	if err := syscall.Exec(path, argv, env); err != nil {
		return 1, err
	}
	return 0, nil
}

func runClaudeCommand(ctx context.Context, claudePath string, args []string, env []string, resumeCommand string) (int, error) {
	before, cwd := currentProjectSession()

	cmd := exec.CommandContext(ctx, claudePath, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = env

	signals := make(chan os.Signal, 4)
	signal.Notify(signals)
	defer signal.Stop(signals)

	if err := cmd.Start(); err != nil {
		return 1, err
	}
	go forwardSignals(cmd.Process, signals)

	if err := cmd.Wait(); err != nil {
		printResumeHintFromProject(cwd, before, resumeCommand)
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return exitErr.ExitCode(), nil
		}
		return 1, err
	}
	printResumeHintFromProject(cwd, before, resumeCommand)
	return 0, nil
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
