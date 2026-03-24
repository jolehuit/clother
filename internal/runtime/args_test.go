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
