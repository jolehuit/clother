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
