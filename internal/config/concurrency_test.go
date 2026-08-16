package config

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Saving state is a read-modify-write cycle. WriteFileAtomic makes the rename
// atomic, but not the cycle: four `clother config` started at once all read the
// version from before, and the last rename won. Measured on the shipped binary:
// four commands, four "configuration saved", one key of four on disk.
//
// These tests drive real, separate processes — goroutines would share the same
// address space and could pass on a lock that does not actually work across
// processes.

const (
	saveHelperEnv     = "CLOTHER_TEST_SAVE_HELPER"
	saveHelperKeyEnv  = "CLOTHER_TEST_SAVE_KEY"
	saveHelperFileEnv = "CLOTHER_TEST_SAVE_FILE"
	saveHelperGateEnv = "CLOTHER_TEST_SAVE_GATE"
	saveHelperKindEnv = "CLOTHER_TEST_SAVE_KIND"
)

// TestSaveHelperProcess is one competing `clother config`: it reads the file,
// waits on the shared barrier so every process collides on purpose, adds its own
// entry and saves.
func TestSaveHelperProcess(t *testing.T) {
	name := os.Getenv(saveHelperKeyEnv)
	if os.Getenv(saveHelperEnv) == "" {
		t.Skip("helper process")
	}
	path := os.Getenv(saveHelperFileEnv)

	switch os.Getenv(saveHelperKindEnv) {
	case "secrets":
		loaded, err := LoadSecrets(path)
		if err != nil {
			t.Fatalf("helper %s: load: %v", name, err)
		}
		baseline := loaded.Clone()
		loaded[name] = "value-for-" + name
		waitForGate(t, os.Getenv(saveHelperGateEnv))
		if err := SaveSecretsMerged(path, baseline, loaded); err != nil {
			t.Fatalf("helper %s: save: %v", name, err)
		}
	default:
		loaded, err := LoadConfig(path)
		if err != nil {
			t.Fatalf("helper %s: load: %v", name, err)
		}
		baseline := loaded.Clone()
		loaded.OpenRouterAliases[name] = "model/" + name
		waitForGate(t, os.Getenv(saveHelperGateEnv))
		if err := SaveConfigMerged(path, baseline, loaded); err != nil {
			t.Fatalf("helper %s: save: %v", name, err)
		}
	}
}

func waitForGate(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("barrier %s never opened", path)
}

func runCompetingSavers(t *testing.T, kind, path string, names []string) {
	t.Helper()
	gate := filepath.Join(t.TempDir(), "gate")

	var procs []*exec.Cmd
	var outputs []*bytes.Buffer
	for _, name := range names {
		out := &bytes.Buffer{}
		cmd := exec.Command(os.Args[0], "-test.run=TestSaveHelperProcess", "-test.v")
		cmd.Env = append(os.Environ(),
			saveHelperEnv+"=1",
			saveHelperKindEnv+"="+kind,
			saveHelperKeyEnv+"="+name,
			saveHelperFileEnv+"="+path,
			saveHelperGateEnv+"="+gate,
		)
		cmd.Stdout = out
		cmd.Stderr = out
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		procs = append(procs, cmd)
		outputs = append(outputs, out)
	}

	// Let every process reach the barrier before any of them writes.
	time.Sleep(300 * time.Millisecond)
	if err := os.WriteFile(gate, []byte("go"), 0o600); err != nil {
		t.Fatal(err)
	}
	for i, cmd := range procs {
		if err := cmd.Wait(); err != nil {
			t.Fatalf("helper %s failed: %v\n%s", names[i], err, outputs[i].String())
		}
	}
}

func TestConcurrentSaveSecretsKeepsEveryKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secrets.env")
	names := []string{"ZAI_API_KEY", "DEEPSEEK_API_KEY", "MOONSHOT_API_KEY", "KIMI_API_KEY", "MINIMAX_API_KEY"}

	runCompetingSavers(t, "secrets", path, names)

	got, err := LoadSecrets(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		if got[name] != "value-for-"+name {
			t.Errorf("%s was lost: the file holds %d of %d keys (%v)", name, len(got), len(names), got)
		}
	}
}

func TestConcurrentSaveConfigKeepsEveryEntry(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	names := []string{"alpha", "bravo", "charlie", "delta", "echo"}

	runCompetingSavers(t, "config", path, names)

	got, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		if got.OpenRouterAliases[name] != "model/"+name {
			t.Errorf("alias %s was lost: the file holds %d of %d aliases (%v)", name, len(got.OpenRouterAliases), len(names), got.OpenRouterAliases)
		}
	}
}

// A deletion made in memory must still be a deletion after the merge, otherwise
// `clother remove` would resurrect the key it just dropped.
func TestSaveSecretsMergedHonoursDeletions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.env")
	if err := SaveSecrets(path, Secrets{"ZAI_API_KEY": "one", "KIMI_API_KEY": "two"}); err != nil {
		t.Fatal(err)
	}

	baseline := Secrets{"ZAI_API_KEY": "one", "KIMI_API_KEY": "two"}
	current := Secrets{"KIMI_API_KEY": "two"}
	if err := SaveSecretsMerged(path, baseline, current); err != nil {
		t.Fatal(err)
	}

	got, err := LoadSecrets(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got["ZAI_API_KEY"]; ok {
		t.Fatalf("the removed key came back: %v", got)
	}
	if got["KIMI_API_KEY"] != "two" {
		t.Fatalf("the untouched key was lost: %v", got)
	}
}

// A lock whose owner is gone must not wedge the CLI.
func TestFileLockStealsAnAbandonedLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	// pid 1 exists but the timestamp is far in the past, which is the shape of a
	// lock left behind by a crash on a recycled pid.
	if err := os.WriteFile(LockPathFor(path), []byte("pid=1\nunix=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ran := false
	if err := WithFileLock(path, func() error { ran = true; return nil }); err != nil {
		t.Fatalf("WithFileLock refused an abandoned lock: %v", err)
	}
	if !ran {
		t.Fatal("the critical section never ran")
	}
	if _, err := os.Stat(LockPathFor(path)); !os.IsNotExist(err) {
		t.Fatalf("the lock file survived the critical section: %v", err)
	}
}

// Taking the lock is now the first thing that touches the disk, so it is also
// the first thing that fails on a directory the user cannot write. It must fail
// the way WriteFileAtomic does — naming the file the user asked for and the
// directory to fix — and never name the throwaway `.lock-1723467706` it tried
// to create along the way, which the user never made and cannot act on.
func TestLockFailureNamesTheTargetNotItsTemporaryFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	dir := filepath.Join(t.TempDir(), "clother")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "secrets.env")
	if err := SaveSecrets(path, Secrets{"ZAI_API_KEY": "one"}); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	err := SaveSecretsMerged(path, Secrets{"ZAI_API_KEY": "one"}, Secrets{"ZAI_API_KEY": "one", "KIMI_API_KEY": "two"})
	if err == nil {
		t.Fatal("saving into a read-only directory must fail")
	}
	message := err.Error()

	if strings.Contains(message, ".lock-") {
		t.Errorf("the error names a temporary file the user never created: %q", message)
	}
	if !strings.Contains(message, path) {
		t.Errorf("the error does not name the file being written (%s): %q", path, message)
	}
	if !strings.Contains(message, dir) {
		t.Errorf("the error does not name the directory to fix (%s): %q", dir, message)
	}
	// The errno has to survive the rewording: UnreadableStateHint and every
	// caller that branches on the kind of failure test it with errors.Is.
	if !errors.Is(err, fs.ErrPermission) {
		t.Errorf("the permission error was flattened into a plain string: %v", err)
	}
}

// The wait timeout names the file being written too, plus the lock to delete
// when nothing is actually running.
func TestLockTimeoutNamesTheTargetAndTheLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	// pid 1 always exists and is never us, so the owner reads as alive and the
	// lock is never stolen for being orphaned.
	base := time.Now()
	if err := os.WriteFile(LockPathFor(path), []byte(fmt.Sprintf("pid=1\nunix=%d\n", base.Unix())), 0o600); err != nil {
		t.Fatal(err)
	}

	// A clock that advances half a second per reading: it crosses lockMaxWait
	// long before lockStaleAfter, so the wait ends on the deadline rather than
	// on the lock being judged abandoned.
	step := 0
	restore := lockNow
	lockNow = func() time.Time {
		step++
		return base.Add(time.Duration(step) * 500 * time.Millisecond)
	}
	t.Cleanup(func() { lockNow = restore })

	err := WithFileLock(path, func() error { return nil })
	if err == nil {
		t.Fatal("a lock held by a live foreign pid must eventually time out")
	}
	message := err.Error()
	if !strings.Contains(message, LockPathFor(path)) {
		t.Errorf("the timeout does not name the lock to delete: %q", message)
	}
	// The lock path contains the target path as a prefix, so "does it mention
	// the target" has to mean "on its own, not only inside <target>.lock" —
	// which is exactly what the old "timed out waiting for the lock %s held by
	// pid %d" failed to do.
	if strings.Count(message, path) < 2 {
		t.Errorf("the timeout names only the lock file, never the file the user asked to write: %q", message)
	}
	if !strings.Contains(message, "delete") {
		t.Errorf("the timeout gives no next action: %q", message)
	}
}
