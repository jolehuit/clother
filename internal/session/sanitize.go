package session

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jolehuit/clother/internal/config"
)

const (
	// The patch directory holds full copies of session transcripts.
	patchDirMode = 0o700
	// Past this age a patch whose owner is gone is considered abandoned: a
	// restore that keeps failing stops being retried silently at every launch.
	patchAbandonAfter = 24 * time.Hour
	// Past this age an unrestorable backup is deleted: it is a full copy of the
	// conversation and must not linger forever.
	patchPurgeAfter = 7 * 24 * time.Hour
)

// patchWarn reports a restore problem to the user. Restore failures used to be
// discarded with `_ =`, which left sessions silently truncated of their
// thinking blocks.
var patchWarn = func(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "clother: "+format+"\n", args...)
}

type Patch struct {
	OriginalPath  string `json:"original_path"`
	BackupPath    string `json:"backup_path"`
	SanitizedPath string `json:"sanitized_path"`
	MetadataPath  string `json:"metadata_path"`
	LockPath      string `json:"lock_path"`
	OwnerPID      int    `json:"owner_pid"`
	Messages      int    `json:"messages"`
	Blocks        int    `json:"blocks"`
	AppliedUnix   int64  `json:"applied_unix"`
}

// PrepareTemporaryPatch reserves an exclusive patch slot for the current
// process. It returns a nil patch — without an error — when another live
// clother instance already owns that session file, so the caller simply runs
// claude without touching the transcript.
func PrepareTemporaryPatch(paths config.Paths, sessionPath string) (*Patch, Analysis, error) {
	analysis, err := Analyze(sessionPath)
	if err != nil || !analysis.NeedsSanitization {
		return nil, analysis, err
	}
	if err := ensurePatchDir(paths.SessionPatchDir); err != nil {
		return nil, analysis, err
	}
	base := strings.TrimSuffix(filepath.Base(sessionPath), filepath.Ext(sessionPath))
	lockPath := filepath.Join(paths.SessionPatchDir, base+".lock")
	pid := os.Getpid()
	if !acquireSessionLock(lockPath, pid) {
		return nil, analysis, nil
	}
	// The slot carries the pid: two instances resuming the same session never
	// share a backup, so one can no longer delete the other's.
	slot := base + "-" + strconv.Itoa(pid)
	return &Patch{
		OriginalPath:  sessionPath,
		BackupPath:    filepath.Join(paths.SessionPatchDir, slot+".orig"),
		SanitizedPath: filepath.Join(paths.SessionPatchDir, slot+".san"),
		MetadataPath:  filepath.Join(paths.SessionPatchDir, slot+".json"),
		LockPath:      lockPath,
		OwnerPID:      pid,
		Messages:      analysis.MessagesTouched,
		Blocks:        analysis.BlocksRemoved,
		AppliedUnix:   time.Now().Unix(),
	}, analysis, nil
}

// ensurePatchDir creates the patch directory private, and narrows it when it
// already exists with wider bits: config.Paths pre-creates every data directory
// in 0755, and MkdirAll leaves the mode of an existing directory alone — so
// without this the full transcript copies sat in a world-readable directory.
func ensurePatchDir(dir string) error {
	if err := os.MkdirAll(dir, patchDirMode); err != nil {
		return err
	}
	info, err := os.Stat(dir)
	if err != nil {
		return err
	}
	if info.Mode().Perm()&^os.FileMode(patchDirMode) == 0 {
		return nil
	}
	return os.Chmod(dir, patchDirMode)
}

func (p *Patch) Apply() error {
	original, err := os.ReadFile(p.OriginalPath)
	if err != nil {
		return err
	}
	// Sanitize first: a failure here must not leave an orphan backup behind.
	sanitized, err := sanitizeJSONL(original)
	if err != nil {
		return err
	}
	if err := configWriteFile(p.BackupPath, original, 0o600); err != nil {
		return err
	}
	if err := configWriteFile(p.SanitizedPath, sanitized, 0o600); err != nil {
		return err
	}
	meta, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	meta = append(meta, '\n')
	if err := configWriteFile(p.MetadataPath, meta, 0o600); err != nil {
		return err
	}
	return writeSessionInPlace(p.OriginalPath, sanitized)
}

func (p *Patch) Restore() error {
	backup, err := os.ReadFile(p.BackupPath)
	if err != nil {
		return err
	}
	current, err := os.ReadFile(p.OriginalPath)
	if err != nil {
		return err
	}
	sanitized, err := os.ReadFile(p.SanitizedPath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	restored, err := mergedRestoreContent(backup, sanitized, current)
	if err != nil {
		return err
	}
	if err := writeSessionInPlace(p.OriginalPath, restored); err != nil {
		return err
	}
	p.discard()
	return nil
}

// RestoreOrWarn is the form meant for a `defer`: a failed restore leaves the
// user's transcript stripped of its thinking blocks, so it must never be
// swallowed the way `defer patch.Restore()` did. The backup path is printed so
// the transcript can be recovered by hand.
func (p *Patch) RestoreOrWarn() {
	if err := p.Restore(); err != nil {
		patchWarn("could not restore %s: %v; the original transcript is kept at %s",
			p.OriginalPath, err, p.BackupPath)
	}
}

func (p *Patch) discard() {
	_ = os.Remove(p.BackupPath)
	_ = os.Remove(p.SanitizedPath)
	_ = os.Remove(p.MetadataPath)
	p.releaseLock()
}

func (p *Patch) releaseLock() {
	if p.LockPath == "" {
		return
	}
	if owner, _, ok := readLockOwner(p.LockPath); ok && p.OwnerPID != 0 && owner != p.OwnerPID {
		// The slot was taken over by another instance; leave its lock alone.
		return
	}
	_ = os.Remove(p.LockPath)
}

func RestoreStale(paths config.Paths) error {
	entries, err := os.ReadDir(paths.SessionPatchDir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	self := os.Getpid()
	now := time.Now()
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		metaPath := filepath.Join(paths.SessionPatchDir, entry.Name())
		data, err := os.ReadFile(metaPath)
		if err != nil {
			continue
		}
		var patch Patch
		if err := json.Unmarshal(data, &patch); err != nil {
			patchWarn("ignoring unreadable session patch %s: %v", metaPath, err)
			continue
		}
		if patch.SanitizedPath == "" {
			base := strings.TrimSuffix(entry.Name(), ".json")
			patch.SanitizedPath = filepath.Join(paths.SessionPatchDir, base+".san")
		}
		if patch.MetadataPath == "" {
			patch.MetadataPath = metaPath
		}
		if err := patch.validate(paths.SessionPatchDir); err != nil {
			patchWarn("ignoring out-of-bounds session patch %s: %v", metaPath, err)
			_ = os.Remove(metaPath)
			continue
		}
		age := patch.age(now)
		if patch.OwnerPID != 0 && patch.OwnerPID != self && processAlive(patch.OwnerPID) && age < patchAbandonAfter {
			// A live clother instance owns this session: restoring now would
			// undo its sanitization and delete the backup under its feet.
			continue
		}
		if _, err := os.Stat(patch.BackupPath); err != nil {
			_ = os.Remove(metaPath)
			patch.releaseLock()
			continue
		}
		if err := patch.Restore(); err != nil {
			patch.reportRestoreFailure(err, age)
		}
	}
	return nil
}

func (p *Patch) reportRestoreFailure(err error, age time.Duration) {
	switch {
	case age >= patchPurgeAfter:
		patchWarn("giving up on the session backup for %s after %s: %v; deleting %s",
			p.OriginalPath, age.Truncate(time.Hour), err, p.BackupPath)
		_ = os.Remove(p.BackupPath)
		_ = os.Remove(p.SanitizedPath)
		_ = os.Remove(p.MetadataPath)
		p.releaseLock()
	case age >= patchAbandonAfter:
		patchWarn("cannot restore %s: %v; the original transcript is kept at %s, copy it back by hand if you need it",
			p.OriginalPath, err, p.BackupPath)
		_ = os.Remove(p.MetadataPath)
		p.releaseLock()
	default:
		patchWarn("could not restore %s yet: %v (will retry on the next launch)", p.OriginalPath, err)
	}
}

func (p *Patch) age(now time.Time) time.Duration {
	if p.AppliedUnix <= 0 {
		return 0
	}
	age := now.Sub(time.Unix(p.AppliedUnix, 0))
	if age < 0 {
		return 0
	}
	return age
}

// validate confines a patch read from disk: the three work files must live in
// the patch directory and the restore target must look like a session
// transcript. Without it, RestoreStale is an arbitrary-write primitive driven
// by whatever JSON sits in the patch directory.
func (p *Patch) validate(patchDir string) error {
	for _, path := range []string{p.BackupPath, p.SanitizedPath, p.MetadataPath} {
		if path == "" {
			return errors.New("patch has an empty work path")
		}
		if !isWithin(patchDir, path) {
			return fmt.Errorf("%s escapes %s", path, patchDir)
		}
	}
	if p.LockPath != "" && !isWithin(patchDir, p.LockPath) {
		return fmt.Errorf("%s escapes %s", p.LockPath, patchDir)
	}
	original := filepath.Clean(p.OriginalPath)
	if !filepath.IsAbs(original) {
		return fmt.Errorf("session path %q is not absolute", p.OriginalPath)
	}
	if filepath.Ext(original) != ".jsonl" {
		return fmt.Errorf("session path %q is not a .jsonl transcript", p.OriginalPath)
	}
	if isWithin(patchDir, original) {
		return fmt.Errorf("session path %q points back into the patch directory", p.OriginalPath)
	}
	return nil
}

func isWithin(dir, path string) bool {
	rel, err := filepath.Rel(filepath.Clean(dir), filepath.Clean(path))
	if err != nil {
		return false
	}
	if rel == "." || rel == ".." {
		return false
	}
	return !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// acquireSessionLock reserves the patch slot for this session file.
//
// The lock carries the time it was taken as well as the owner pid. A pid alone
// is not evidence: the operating system recycles them, so a lock left behind by
// a crash whose number has since been handed to an unrelated process looked
// alive forever, and every later launch skipped the transcript sanitization
// without a word. An owner older than patchAbandonAfter is treated as gone, the
// same rule RestoreStale already applies.
func acquireSessionLock(lockPath string, pid int) bool {
	for attempt := 0; attempt < 2; attempt++ {
		file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_, writeErr := fmt.Fprintf(file, "pid=%d\nunix=%d\n", pid, time.Now().Unix())
			closeErr := file.Close()
			if writeErr != nil || closeErr != nil {
				_ = os.Remove(lockPath)
				return false
			}
			return true
		}
		if !os.IsExist(err) {
			return false
		}
		if owner, taken, ok := readLockOwner(lockPath); ok && owner != pid && processAlive(owner) {
			if taken.IsZero() || time.Since(taken) < patchAbandonAfter {
				// Not silent any more: the session runs unsanitized, and the
				// user is the only one who can tell a real concurrent instance
				// from a lock nobody will ever release.
				patchWarn("another clother instance (pid %d) owns %s; running without sanitizing this transcript",
					owner, lockPath)
				return false
			}
		}
		if err := os.Remove(lockPath); err != nil {
			return false
		}
	}
	return false
}

// readLockOwner reads a lock file. The current format is "pid=N\nunix=T\n"; a
// bare pid, written by a version that recorded no timestamp, is still accepted
// and reported with a zero time.
func readLockOwner(lockPath string) (int, time.Time, bool) {
	data, err := os.ReadFile(lockPath)
	if err != nil {
		return 0, time.Time{}, false
	}
	body := strings.TrimSpace(string(data))
	if pid, err := strconv.Atoi(body); err == nil {
		if pid <= 0 {
			return 0, time.Time{}, false
		}
		return pid, time.Time{}, true
	}

	pid := 0
	var taken time.Time
	for _, line := range strings.Split(body, "\n") {
		key, value, found := strings.Cut(strings.TrimSpace(line), "=")
		if !found {
			continue
		}
		switch key {
		case "pid":
			if parsed, err := strconv.Atoi(value); err == nil {
				pid = parsed
			}
		case "unix":
			if parsed, err := strconv.ParseInt(value, 10, 64); err == nil {
				taken = time.Unix(parsed, 0)
			}
		}
	}
	if pid <= 0 {
		return 0, time.Time{}, false
	}
	return pid, taken, true
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = process.Signal(syscall.Signal(0))
	if err == nil {
		return true
	}
	if errors.Is(err, os.ErrProcessDone) {
		return false
	}
	var errno syscall.Errno
	if errors.As(err, &errno) {
		return errno == syscall.EPERM
	}
	return false
}

func mergedRestoreContent(backup, sanitized, current []byte) ([]byte, error) {
	if bytes.Equal(current, backup) {
		return backup, nil
	}
	if len(sanitized) == 0 {
		return nil, fmt.Errorf("missing sanitized snapshot; refusing restore to avoid data loss")
	}
	if !bytes.HasPrefix(current, sanitized) {
		return nil, fmt.Errorf("session changed unexpectedly; refusing restore to avoid data loss")
	}
	restored := append([]byte{}, backup...)
	restored = append(restored, current[len(sanitized):]...)
	return restored, nil
}

func sanitizeJSONL(input []byte) ([]byte, error) {
	var out strings.Builder
	scanner := bufio.NewScanner(bytes.NewReader(input))
	buf := make([]byte, 0, 1024*1024)
	scanner.Buffer(buf, maxSessionLine)

	for scanner.Scan() {
		line := scanner.Bytes()
		var payload map[string]any
		if err := json.Unmarshal(line, &payload); err != nil {
			out.Write(line)
			out.WriteByte('\n')
			continue
		}
		model, role, content := extractMessage(payload)
		if role == "assistant" && isNonClaudeModel(model) && len(content) > 0 {
			cleaned, changed := sanitizeContent(content)
			if changed {
				if len(cleaned) == 0 {
					continue
				}
				message := payload["message"].(map[string]any)
				message["content"] = cleaned
				encoded, err := json.Marshal(payload)
				if err != nil {
					return nil, err
				}
				out.Write(encoded)
				out.WriteByte('\n')
				continue
			}
		}
		out.Write(line)
		out.WriteByte('\n')
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return []byte(out.String()), nil
}

func sanitizeContent(content []any) ([]any, bool) {
	var out []any
	changed := false
	for _, part := range content {
		block, ok := part.(map[string]any)
		if !ok {
			out = append(out, part)
			continue
		}
		blockType, _ := block["type"].(string)
		if isReasoningType(blockType) {
			changed = true
			continue
		}
		out = append(out, part)
	}
	return out, changed
}

// writeSessionInPlace rewrites the transcript through its existing inode. The
// previous CreateTemp+rename swapped the inode under a claude process that may
// still hold the file open, which silently dropped every turn written after.
func writeSessionInPlace(path string, data []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.WriteAt(data, 0); err != nil {
		file.Close()
		return err
	}
	if err := file.Truncate(int64(len(data))); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

func configWriteFile(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), patchDirMode); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".patch-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	// This file is the metadata that says where the .orig backup of a live
	// transcript lives. Losing it to a crash means the transcript stays
	// sanitized with no record of how to put it back, so both the file and the
	// directory entry are flushed before we call it written.
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("rename %s: %w", path, err)
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	if err := dir.Sync(); err != nil {
		dir.Close()
		return err
	}
	return dir.Close()
}
