package session

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

var ErrSessionNotFound = errors.New("session not found")

func ResumeID(args []string) string {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--resume" || arg == "-r" {
			if i+1 < len(args) {
				return args[i+1]
			}
			return ""
		}
		if strings.HasPrefix(arg, "--resume=") {
			return strings.TrimPrefix(arg, "--resume=")
		}
	}
	return ""
}

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
