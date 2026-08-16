package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// WriteFileAtomic writes data to path via a temporary file in the same directory
// and renames it into place.
//
// The temporary file is fsynced before the rename and the containing directory
// is fsynced after it: without both, the rename is only atomic with respect to
// the name, and a crash right after `clother config` can leave an empty
// secrets.env behind — which loses every API key at once.
//
// This is the single implementation of that discipline. internal/launchers and
// internal/runtime used to carry their own copies, which had drifted apart:
// one reported a directory it could not open as a fatal error where this one
// tolerates it, and the third fsynced nothing at all while writing the user's
// settings.json.
func WriteFileAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		// Naming the temporary file here is useless to the user: they never
		// created it and cannot act on it. Name the target and its directory.
		return fmt.Errorf("cannot write %s: %w (check the permissions of %s)", path, err, dir)
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("cannot write %s: %w", path, err)
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("cannot write %s: %w", path, err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("cannot write %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("cannot write %s: %w", path, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("cannot write %s: %w (check the permissions of %s)", path, err, dir)
	}
	return syncDir(dir)
}

// writeAtomic is the in-package spelling of WriteFileAtomic.
func writeAtomic(path string, data []byte, mode os.FileMode) error {
	return WriteFileAtomic(path, data, mode)
}

// syncDir fsyncs a directory so that a rename into it survives a crash. A
// failure to open the directory is not fatal (some filesystems refuse it), but
// a failed sync on an opened descriptor is reported.
func syncDir(dir string) error {
	handle, err := os.Open(dir)
	if err != nil {
		return nil
	}
	defer handle.Close()
	if err := handle.Sync(); err != nil {
		return err
	}
	return nil
}

// ensurePrivateDir creates dir (and parents) with 0700 and tightens the
// permissions of an already existing directory that is group- or
// world-accessible. Config and data directories hold credentials; their mode
// must not depend on the user's umask.
func ensurePrivateDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	info, err := os.Stat(dir)
	if err != nil {
		return err
	}
	if info.Mode().Perm()&0o077 != 0 {
		return os.Chmod(dir, 0o700)
	}
	return nil
}
