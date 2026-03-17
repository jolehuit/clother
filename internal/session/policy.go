package session

import "github.com/jolehuit/clother/internal/providers"

func RequiresClaudeSanitization(family providers.Family) bool {
	return family == providers.FamilyClaudeStrict
}
