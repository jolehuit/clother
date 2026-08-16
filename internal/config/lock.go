package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// An exclusive, advisory, inter-process lock.
//
// WriteFileAtomic makes the *replacement* of a file atomic, but it does nothing
// for the read-modify-write cycle around it: every process that saves
// secrets.env or config.json reads the whole file, changes one entry in memory
// and writes the whole file back. Two `clother config` running at the same time
// both read the version from before, and the last rename wins — which silently
// dropped API keys (four parallel `clother config` left one key out of four on
// disk, all four reporting "configuration saved").
//
// The lock file sits next to the file it protects, carries the owner pid and the
// time it was taken, and is stolen when its owner is gone or when it is older
// than lockStaleAfter, so a process killed mid-write cannot wedge the CLI.
const (
	lockStaleAfter   = 30 * time.Second
	lockPollInterval = 15 * time.Millisecond
	lockMaxWait      = 20 * time.Second
)

// lockNow is the clock used for lock timestamps. Overridden in tests.
var lockNow = time.Now

// WithFileLock runs fn while holding an exclusive lock on path. The lock is
// released whatever fn does, including on panic.
func WithFileLock(path string, fn func() error) error {
	release, err := acquireFileLock(path)
	if err != nil {
		return err
	}
	defer release()
	return fn()
}

// LockPathFor is the lock file protecting path. Exported so `uninstall` knows
// the file belongs to Clother and can delete it.
func LockPathFor(path string) string {
	return path + ".lock"
}

// acquireFileLock takes the lock protecting path. Errors are phrased in terms of
// path itself: the caller asked to write config.json or secrets.env, and naming
// the lock file — let alone the temporary file used to publish it — sends them
// looking for something they never created.
func acquireFileLock(path string) (func(), error) {
	lockPath := LockPathFor(path)
	pid := os.Getpid()
	deadline := lockNow().Add(lockMaxWait)
	lastOwner := 0
	for {
		taken, err := tryTakeLock(path, lockPath, pid)
		if err != nil {
			return nil, err
		}
		if taken {
			return func() { releaseFileLock(lockPath, pid) }, nil
		}

		owner, since, ok := readLockFile(lockPath)
		lastOwner = owner
		stale := ok &&
			((owner != pid && !lockOwnerAlive(owner)) || lockNow().Sub(since) > lockStaleAfter)
		if !ok {
			// Unreadable content: not a race (the file appears complete or not
			// at all, see tryTakeLock) but a truncated or hand-made file. Judge
			// it on its own mtime rather than wedging the CLI forever.
			if info, statErr := os.Stat(lockPath); statErr != nil || lockNow().Sub(info.ModTime()) > lockStaleAfter {
				stale = true
			}
		}
		if stale {
			// Removing a lock judged stale can race with its owner taking a
			// fresh one; the link below is what actually arbitrates, so a failed
			// remove is not fatal.
			_ = os.Remove(lockPath)
			continue
		}
		if !lockNow().Before(deadline) {
			return nil, fmt.Errorf("timed out waiting for another clother (pid %d) to finish writing %s; delete %s by hand if no clother is running",
				lastOwner, path, lockPath)
		}
		time.Sleep(lockPollInterval)
	}
}

// tryTakeLock publishes the lock file through a hard link.
//
// An O_CREATE|O_EXCL open followed by a write is not enough: between the two,
// the file exists and is empty, so a competitor reads it, finds nothing usable,
// declares the lock stale and takes it — which is exactly the lost update the
// lock is there to prevent (measured: 4 of 5 keys kept, then 3 of 5). The link
// is atomic and fails when the name already exists, so the lock file is never
// observed without its contents.
func tryTakeLock(path, lockPath string, pid int) (bool, error) {
	dir := filepath.Dir(lockPath)
	tmp, err := os.CreateTemp(dir, ".lock-*")
	if err != nil {
		return false, lockWriteError(path, dir, err)
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()

	_, writeErr := fmt.Fprintf(tmp, "pid=%d\nunix=%d\n", pid, lockNow().Unix())
	closeErr := tmp.Close()
	if writeErr != nil || closeErr != nil {
		return false, lockWriteError(path, dir, errors.Join(writeErr, closeErr))
	}

	if err := os.Link(tmpPath, lockPath); err != nil {
		if os.IsExist(err) {
			return false, nil
		}
		return false, lockWriteError(path, dir, err)
	}
	return true, nil
}

// lockWriteError phrases a failure to take the lock the way WriteFileAtomic
// phrases a failure to write: the file the user asked for, plus the directory
// whose permissions are the actual problem.
//
// The raw error names the throwaway `.lock-1723467706` this function just tried
// to create, which the user never made and cannot act on — the very complaint
// that got the atomic writer fixed, reintroduced one layer down.
func lockWriteError(path, dir string, err error) error {
	var pathErr *fs.PathError
	if errors.As(err, &pathErr) {
		// Keep the errno (so errors.Is(err, fs.ErrPermission) still answers) and
		// drop the temporary path from the message.
		return fmt.Errorf("cannot write %s: %w (check the permissions of %s)", path, pathErr.Err, dir)
	}
	return fmt.Errorf("cannot write %s: %w (check the permissions of %s)", path, err, dir)
}

func releaseFileLock(lockPath string, pid int) {
	if owner, _, ok := readLockFile(lockPath); ok && owner != pid {
		// Someone judged our lock stale and took it over: leave theirs alone.
		return
	}
	_ = os.Remove(lockPath)
}

func readLockFile(lockPath string) (pid int, taken time.Time, ok bool) {
	data, err := os.ReadFile(lockPath)
	if err != nil {
		return 0, time.Time{}, false
	}
	for _, line := range strings.Split(string(data), "\n") {
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
	if pid <= 0 || taken.IsZero() {
		return 0, time.Time{}, false
	}
	return pid, taken, true
}

func lockOwnerAlive(pid int) bool {
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
