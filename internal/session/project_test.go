package session

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestProjectDirMatchesClaudeLayout(t *testing.T) {
	t.Parallel()

	root := "/tmp/projects"
	got := ProjectDir(root, "/Users/max/Downloads/clother")
	want := filepath.Join(root, "-Users-max-Downloads-clother")
	if got != want {
		t.Fatalf("ProjectDir() = %q, want %q", got, want)
	}
}

func TestLatestInProjectReturnsMostRecentSession(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	projectDir := ProjectDir(root, "/Users/max/Downloads/clother")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	older := filepath.Join(projectDir, "older.jsonl")
	newer := filepath.Join(projectDir, "newer.jsonl")
	if err := os.WriteFile(older, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond)
	if err := os.WriteFile(newer, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := LatestInProject(root, "/Users/max/Downloads/clother")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "newer" {
		t.Fatalf("LatestInProject() ID = %q, want newer", got.ID)
	}
}

func TestChangedProjectSession(t *testing.T) {
	t.Parallel()

	now := time.Now()
	before := ProjectSession{ID: "abc", ModTime: now}
	after := ProjectSession{ID: "abc", ModTime: now.Add(time.Second)}
	if !ChangedProjectSession(before, after) {
		t.Fatal("expected updated modtime to count as changed")
	}
}
