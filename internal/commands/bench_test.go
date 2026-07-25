package commands

import (
	"testing"

	"github.com/jolehuit/clother/internal/profiles"
)

func TestBenchModelFallsBackToTiers(t *testing.T) {
	t.Parallel()

	explicit := profiles.Target{Model: "glm-5.2", ModelTiers: map[string]string{"sonnet": "other"}}
	if got := benchModel(explicit); got != "glm-5.2" {
		t.Fatalf("benchModel(explicit) = %q, want glm-5.2", got)
	}

	// OpenRouter aliases resolve with an empty Model and tier-only mapping.
	alias := profiles.Target{ModelTiers: map[string]string{
		"haiku":  "minimax/minimax-m2.5:free",
		"sonnet": "minimax/minimax-m2.5:free",
	}}
	if got := benchModel(alias); got != "minimax/minimax-m2.5:free" {
		t.Fatalf("benchModel(alias) = %q, want the tier model", got)
	}

	empty := profiles.Target{}
	if got := benchModel(empty); got != "" {
		t.Fatalf("benchModel(empty) = %q, want empty", got)
	}
}
