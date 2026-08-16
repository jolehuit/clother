package session

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

var ErrSessionNotFound = errors.New("session not found")

// The session id of `--resume` is extracted by runtime.ResumeOverride, which
// classifies each token before reading it. The naive scan that used to live
// here matched option values and everything after `--` too, and the id it
// returned drove an in-place rewrite of a transcript on disk. It had no caller
// left once runtime took over, so it is gone rather than left as a trap for the
// next one.

func FindSession(root, id string) (string, error) {
	if id == "" {
		return "", ErrSessionNotFound
	}
	target := id + ".jsonl"
	var match string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if d.IsDir() {
			rel, _ := filepath.Rel(root, path)
			if rel != "." && strings.Count(rel, string(os.PathSeparator)) > 1 {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() == target {
			match = path
			return errors.New("found")
		}
		return nil
	})
	if match != "" {
		return match, nil
	}
	if err != nil && err.Error() == "found" {
		return match, nil
	}
	return "", ErrSessionNotFound
}
