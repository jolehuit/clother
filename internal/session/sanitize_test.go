package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jolehuit/clother/internal/config"
)

func TestAnalyzeAndPatchNonClaudeThinking(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	sessionPath := filepath.Join(root, "abc.jsonl")
	content := strings.Join([]string{
		`{"message":{"role":"assistant","model":"qwen3.5-plus","content":[{"type":"thinking","thinking":"secret"},{"type":"text","text":"hello"}]}}`,
		`{"message":{"role":"assistant","model":"claude-opus-4-1","content":[{"type":"thinking","thinking":"keep"},{"type":"text","text":"world"}]}}`,
		"",
	}, "\n")
	if err := os.WriteFile(sessionPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	analysis, err := Analyze(sessionPath)
	if err != nil {
		t.Fatal(err)
	}
	if !analysis.NeedsSanitization || analysis.MessagesTouched != 1 || analysis.BlocksRemoved != 1 {
		t.Fatalf("unexpected analysis: %+v", analysis)
	}

	paths := config.Paths{SessionPatchDir: filepath.Join(root, "patches")}
	patch, _, err := PrepareTemporaryPatch(paths, sessionPath)
	if err != nil {
		t.Fatal(err)
	}
	if patch == nil {
		t.Fatal("expected patch")
	}
	if err := patch.Apply(); err != nil {
		t.Fatal(err)
	}

	patched, err := os.ReadFile(sessionPath)
	if err != nil {
		t.Fatal(err)
	}
	patchedText := string(patched)
	if strings.Contains(patchedText, `"model":"qwen3.5-plus","content":[{"type":"thinking"`) {
		t.Fatalf("non-Claude thinking block still present: %s", patchedText)
	}
	if !strings.Contains(patchedText, `"model":"claude-opus-4-1","content":[{"type":"thinking"`) {
		t.Fatalf("Claude thinking block should be preserved: %s", patchedText)
	}

	if err := patch.Restore(); err != nil {
		t.Fatal(err)
	}
	restored, err := os.ReadFile(sessionPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(restored) != content {
		t.Fatalf("restore mismatch\nwant:\n%s\ngot:\n%s", content, string(restored))
	}
}

func TestPatchDropsThinkingOnlyAssistantMessage(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	sessionPath := filepath.Join(root, "thinking-only.jsonl")
	content := strings.Join([]string{
		`{"message":{"role":"assistant","model":"kimi-k2.5","content":[{"type":"thinking","thinking":"secret only"}]}}`,
		`{"message":{"role":"user","content":[{"type":"text","text":"hello"}]}}`,
		"",
	}, "\n")
	if err := os.WriteFile(sessionPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	paths := config.Paths{SessionPatchDir: filepath.Join(root, "patches")}
	patch, analysis, err := PrepareTemporaryPatch(paths, sessionPath)
	if err != nil {
		t.Fatal(err)
	}
	if patch == nil || !analysis.NeedsSanitization {
		t.Fatalf("expected patch and sanitization, got patch=%v analysis=%+v", patch, analysis)
	}
	if err := patch.Apply(); err != nil {
		t.Fatal(err)
	}

	patched, err := os.ReadFile(sessionPath)
	if err != nil {
		t.Fatal(err)
	}
	patchedText := string(patched)
	if strings.Contains(patchedText, "non-Claude thinking removed for Claude compatibility") {
		t.Fatalf("placeholder should not be present: %s", patchedText)
	}
	if strings.Contains(patchedText, `"model":"kimi-k2.5"`) {
		t.Fatalf("thinking-only assistant message should be dropped: %s", patchedText)
	}
	if !strings.Contains(patchedText, `"role":"user"`) {
		t.Fatalf("non-target lines should be preserved: %s", patchedText)
	}
}

func TestPatchRestorePreservesAppendedMessages(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	sessionPath := filepath.Join(root, "append.jsonl")
	original := strings.Join([]string{
		`{"message":{"role":"assistant","model":"qwen3.5-plus","content":[{"type":"thinking","thinking":"secret"},{"type":"text","text":"hello"}]}}`,
		`{"message":{"role":"user","content":[{"type":"text","text":"next"}]}}`,
		"",
	}, "\n")
	if err := os.WriteFile(sessionPath, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}

	paths := config.Paths{SessionPatchDir: filepath.Join(root, "patches")}
	patch, _, err := PrepareTemporaryPatch(paths, sessionPath)
	if err != nil {
		t.Fatal(err)
	}
	if patch == nil {
		t.Fatal("expected patch")
	}
	if err := patch.Apply(); err != nil {
		t.Fatal(err)
	}

	appended := `{"message":{"role":"assistant","model":"claude-opus-4-1","content":[{"type":"text","text":"new reply"}]}}` + "\n"
	f, err := os.OpenFile(sessionPath, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(appended); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	if err := patch.Restore(); err != nil {
		t.Fatal(err)
	}
	restored, err := os.ReadFile(sessionPath)
	if err != nil {
		t.Fatal(err)
	}
	want := original + appended
	if string(restored) != want {
		t.Fatalf("restore should keep appended data\nwant:\n%s\ngot:\n%s", want, string(restored))
	}
	if _, err := os.Stat(patch.BackupPath); !os.IsNotExist(err) {
		t.Fatalf("backup file should be removed, stat err=%v", err)
	}
	if _, err := os.Stat(patch.SanitizedPath); !os.IsNotExist(err) {
		t.Fatalf("sanitized snapshot should be removed, stat err=%v", err)
	}
	if _, err := os.Stat(patch.MetadataPath); !os.IsNotExist(err) {
		t.Fatalf("metadata file should be removed, stat err=%v", err)
	}
}

func TestRestoreStalePreservesAppendedMessages(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	sessionPath := filepath.Join(root, "stale.jsonl")
	original := strings.Join([]string{
		`{"message":{"role":"assistant","model":"qwen3.5-plus","content":[{"type":"thinking","thinking":"secret"},{"type":"text","text":"hello"}]}}`,
		`{"message":{"role":"user","content":[{"type":"text","text":"next"}]}}`,
		"",
	}, "\n")
	if err := os.WriteFile(sessionPath, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}

	paths := config.Paths{SessionPatchDir: filepath.Join(root, "patches")}
	patch, _, err := PrepareTemporaryPatch(paths, sessionPath)
	if err != nil {
		t.Fatal(err)
	}
	if patch == nil {
		t.Fatal("expected patch")
	}
	if err := patch.Apply(); err != nil {
		t.Fatal(err)
	}

	appended := `{"message":{"role":"assistant","model":"claude-opus-4-1","content":[{"type":"text","text":"reply after crash"}]}}` + "\n"
	f, err := os.OpenFile(sessionPath, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(appended); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	if err := RestoreStale(paths); err != nil {
		t.Fatal(err)
	}
	restored, err := os.ReadFile(sessionPath)
	if err != nil {
		t.Fatal(err)
	}
	want := original + appended
	if string(restored) != want {
		t.Fatalf("stale restore should keep appended data\nwant:\n%s\ngot:\n%s", want, string(restored))
	}
}

func TestResumeIDParsesCommonForms(t *testing.T) {
	t.Parallel()

	cases := []struct {
		args []string
		want string
	}{
		{args: []string{"--resume", "abc"}, want: "abc"},
		{args: []string{"-r", "xyz"}, want: "xyz"},
		{args: []string{"--resume=foo"}, want: "foo"},
		{args: []string{"hello"}, want: ""},
	}
	for _, tc := range cases {
		if got := ResumeID(tc.args); got != tc.want {
			t.Fatalf("ResumeID(%v) = %q, want %q", tc.args, got, tc.want)
		}
	}
}
