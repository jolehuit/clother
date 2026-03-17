package providers

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
)

//go:embed catalog.json
var rawCatalog []byte

type AuthMode string

const (
	AuthNone    AuthMode = "none"
	AuthSecret  AuthMode = "secret"
	AuthLiteral AuthMode = "literal"
)

type Family string

const (
	FamilyClaudeStrict                 Family = "claude_strict"
	FamilyAnthropicCompatibleNonClaude Family = "anthropic_compatible_non_claude"
	FamilyLocal                        Family = "local"
	FamilyOpenRouter                   Family = "openrouter"
	FamilyCustomUnknown                Family = "custom_unknown"
)

type ModelChoice struct {
	ID          string `json:"id"`
	Description string `json:"description"`
}

type Provider struct {
	ID               string            `json:"id"`
	DisplayName      string            `json:"display_name"`
	Description      string            `json:"description"`
	Category         string            `json:"category"`
	Family           Family            `json:"family"`
	AuthMode         AuthMode          `json:"auth_mode"`
	KeyVar           string            `json:"key_var,omitempty"`
	LiteralAuthToken string            `json:"literal_auth_token,omitempty"`
	BaseURL          string            `json:"base_url"`
	DefaultModel     string            `json:"default_model"`
	ModelTiers       map[string]string `json:"model_tiers"`
	ModelChoices     []ModelChoice     `json:"model_choices"`
	TestURL          string            `json:"test_url"`
	Setup            []string          `json:"setup"`
	Usage            []string          `json:"usage"`
}

type Catalog struct {
	ordered []Provider
	byID    map[string]Provider
}

func Load() (Catalog, error) {
	var payload struct {
		Providers []Provider `json:"providers"`
	}
	if err := json.Unmarshal(rawCatalog, &payload); err != nil {
		return Catalog{}, fmt.Errorf("decode providers catalog: %w", err)
	}
	cat := Catalog{
		ordered: make([]Provider, 0, len(payload.Providers)),
		byID:    make(map[string]Provider, len(payload.Providers)),
	}
	for _, provider := range payload.Providers {
		if provider.ModelTiers == nil {
			provider.ModelTiers = map[string]string{}
		}
		cat.ordered = append(cat.ordered, provider)
		cat.byID[provider.ID] = provider
	}
	return cat, nil
}

func (c Catalog) All() []Provider {
	out := make([]Provider, len(c.ordered))
	copy(out, c.ordered)
	return out
}

func (c Catalog) IDs() []string {
	ids := make([]string, 0, len(c.ordered))
	for _, provider := range c.ordered {
		ids = append(ids, provider.ID)
	}
	return ids
}

func (c Catalog) Get(id string) (Provider, bool) {
	provider, ok := c.byID[id]
	return provider, ok
}

func (c Catalog) Categories() []string {
	seen := map[string]struct{}{}
	var categories []string
	for _, provider := range c.ordered {
		if _, ok := seen[provider.Category]; ok {
			continue
		}
		seen[provider.Category] = struct{}{}
		categories = append(categories, provider.Category)
	}
	return categories
}

func (c Catalog) ProvidersByCategory(category string) []Provider {
	var out []Provider
	for _, provider := range c.ordered {
		if provider.Category == category {
			out = append(out, provider)
		}
	}
	return out
}

func (c Catalog) BuiltinSecretKeys() map[string]struct{} {
	keys := map[string]struct{}{}
	for _, provider := range c.ordered {
		if provider.KeyVar != "" {
			keys[provider.KeyVar] = struct{}{}
		}
	}
	return keys
}

func SortChoices(choices []ModelChoice) []ModelChoice {
	out := make([]ModelChoice, len(choices))
	copy(out, choices)
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})
	return out
}
