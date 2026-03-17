package session

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type ProjectSession struct {
	ID      string
	Path    string
	ModTime time.Time
}

func ProjectDir(root, cwd string) string {
	clean := filepath.Clean(cwd)
	if clean == "." || clean == "" {
		return filepath.Join(root, "-")
	}
	slug := strings.ReplaceAll(clean, string(filepath.Separator), "-")
	if !strings.HasPrefix(slug, "-") {
		slug = "-" + slug
	}
	return filepath.Join(root, slug)
}

func LatestInProject(root, cwd string) (ProjectSession, error) {
	dir := ProjectDir(root, cwd)
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return ProjectSession{}, nil
	}
	if err != nil {
		return ProjectSession{}, err
	}

	var sessions []ProjectSession
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".jsonl")
		sessions = append(sessions, ProjectSession{
			ID:      id,
			Path:    filepath.Join(dir, entry.Name()),
			ModTime: info.ModTime(),
		})
	}
	sort.SliceStable(sessions, func(i, j int) bool {
		if sessions[i].ModTime.Equal(sessions[j].ModTime) {
			return sessions[i].ID < sessions[j].ID
		}
		return sessions[i].ModTime.After(sessions[j].ModTime)
	})
	if len(sessions) == 0 {
		return ProjectSession{}, nil
	}
	return sessions[0], nil
}

func ChangedProjectSession(before, after ProjectSession) bool {
	if after.ID == "" {
		return false
	}
	if before.ID == "" {
		return true
	}
	if before.ID != after.ID {
		return true
	}
	return after.ModTime.After(before.ModTime)
}
