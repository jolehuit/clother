package session

import (
	"path/filepath"
	"testing"
)

// TestProjectDirSlugifiesEveryNonAlphanumeric matches the layout Claude Code
// really writes: `~/.claude/projects/-Users-dev--claude-projects-my-app`
// for `/Users/dev/.claude/projects/my-app`. Replacing only the separator
// made the resume hint silently dead for every dotted path.
func TestProjectDirSlugifiesEveryNonAlphanumeric(t *testing.T) {
	t.Parallel()

	root := "/root"
	cases := map[string]string{
		"/Users/dev/Downloads/clother":       "-Users-dev-Downloads-clother",
		"/Users/dev/.claude/projects/my-app": "-Users-dev--claude-projects-my-app",
		"/Users/dev/.config/nvim":            "-Users-dev--config-nvim",
		"/Users/dev/my_project":              "-Users-dev-my-project",
		"/Users/dev/worktrees/v1.2":          "-Users-dev-worktrees-v1-2",
		"/Users/dev/mon app":                 "-Users-dev-mon-app",
	}
	for cwd, want := range cases {
		got := ProjectDir(root, cwd)
		if got != filepath.Join(root, want) {
			t.Errorf("ProjectDir(%q) = %q, want %q", cwd, got, filepath.Join(root, want))
		}
	}
}
