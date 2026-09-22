package config

import (
	"os"
	"path/filepath"
)

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
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
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

// WriteFileAtomic replaces path with data through a temporary file in the same
// directory, so a reader never sees a half-written file.
func WriteFileAtomic(path string, data []byte, mode os.FileMode) error {
	return writeAtomic(path, data, mode)
}
