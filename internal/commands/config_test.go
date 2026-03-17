package commands

import (
	"testing"

	"github.com/jolehuit/clother/internal/providers"
)

func TestResolveModelChoiceMapsNumericSelections(t *testing.T) {
	t.Parallel()

	choices := []providers.ModelChoice{
		{ID: "glm-5"},
		{ID: "glm-4.7"},
	}

	if got := resolveModelChoice("1", choices); got != "glm-5" {
		t.Fatalf("resolveModelChoice(1) = %q, want glm-5", got)
	}
	if got := resolveModelChoice("glm-4.7", choices); got != "glm-4.7" {
		t.Fatalf("resolveModelChoice(glm-4.7) = %q", got)
	}
}
