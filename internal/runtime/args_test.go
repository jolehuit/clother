package runtime

import (
	"reflect"
	"testing"
)

func TestNormalizeClaudeArgsRewritesYolo(t *testing.T) {
	t.Parallel()

	got := NormalizeClaudeArgs([]string{"--yolo", "--resume", "abc"})
	want := []string{"--dangerously-skip-permissions", "--resume", "abc"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("NormalizeClaudeArgs() = %#v, want %#v", got, want)
	}
}

func TestNormalizeClaudeArgsAvoidsDuplicateDangerousFlag(t *testing.T) {
	t.Parallel()

	got := NormalizeClaudeArgs([]string{"--dangerously-skip-permissions", "--yolo"})
	want := []string{"--dangerously-skip-permissions"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("NormalizeClaudeArgs() = %#v, want %#v", got, want)
	}
}

func TestModelOverridePrefersExplicitFlagValue(t *testing.T) {
	t.Parallel()

	got := ModelOverride([]string{"--model", "glm-5", "--resume", "abc"})
	if got != "glm-5" {
		t.Fatalf("ModelOverride() = %q, want %q", got, "glm-5")
	}
}

func TestModelOverrideSupportsEqualsSyntax(t *testing.T) {
	t.Parallel()

	got := ModelOverride([]string{"--model=MiniMax-M2.7"})
	if got != "MiniMax-M2.7" {
		t.Fatalf("ModelOverride() = %q, want %q", got, "MiniMax-M2.7")
	}
}

func TestModelOverrideReturnsEmptyWhenMissingValue(t *testing.T) {
	t.Parallel()

	if got := ModelOverride([]string{"--model"}); got != "" {
		t.Fatalf("ModelOverride() = %q, want empty", got)
	}
}

// D3: `--yolo` used to be rewritten wherever the literal token appeared,
// including after the end-of-options separator — the very form a user types to
// say "this is data, not a flag".
func TestNormalizeClaudeArgsOnlyRewritesFlagPositions(t *testing.T) {
	t.Parallel()

	cases := map[string][]string{
		"after end-of-flags":  {"--", "--yolo"},
		"model value":         {"--model", "--yolo"},
		"system prompt value": {"--append-system-prompt", "--yolo"},
		"deep after --":       {"-p", "--", "hello", "--yolo"},
	}
	for name, args := range cases {
		got := NormalizeClaudeArgs(append([]string{}, args...))
		if !reflect.DeepEqual(got, args) {
			t.Fatalf("%s: NormalizeClaudeArgs(%#v) = %#v, want it untouched", name, args, got)
		}
	}
}

func TestNormalizeClaudeArgsStillRewritesRealFlag(t *testing.T) {
	t.Parallel()

	got := NormalizeClaudeArgs([]string{"-p", "hello", "--yolo"})
	want := []string{"-p", "hello", "--dangerously-skip-permissions"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("NormalizeClaudeArgs() = %#v, want %#v", got, want)
	}
}

// D11: ModelOverride drives six env vars and the settings.json overlay.
func TestModelOverrideIgnoresDataPositions(t *testing.T) {
	t.Parallel()

	cases := map[string][]string{
		"after end-of-flags": {"--", "--model", "attacker/model"},
		"prompt file value":  {"--append-system-prompt", "--model", "attacker/model"},
	}
	for name, args := range cases {
		if got := ModelOverride(args); got != "" {
			t.Fatalf("%s: ModelOverride(%#v) = %q, want empty", name, args, got)
		}
	}
}

// D11: the id ResumeOverride returns drives an in-place rewrite of a transcript
// on disk, so it must never come from a data token.
func TestResumeOverrideIgnoresDataPositions(t *testing.T) {
	t.Parallel()

	cases := map[string][]string{
		"after end-of-flags":  {"--", "--resume", "11111111-2222-3333-4444-555555555555"},
		"system prompt value": {"--append-system-prompt", "--resume", "victim-session"},
	}
	for name, args := range cases {
		if got := ResumeOverride(args); got != "" {
			t.Fatalf("%s: ResumeOverride(%#v) = %q, want empty", name, args, got)
		}
	}
	if got := ResumeOverride([]string{"--resume", "real-session"}); got != "real-session" {
		t.Fatalf("ResumeOverride() = %q, want real-session", got)
	}
	if got := ResumeOverride([]string{"--resume", "--print"}); got != "" {
		t.Fatalf("ResumeOverride() = %q, want empty: --print is a flag, not a session id", got)
	}
}
