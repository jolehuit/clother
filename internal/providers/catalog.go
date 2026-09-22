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

// Claude Code reads a credential from two variables: ANTHROPIC_AUTH_TOKEN is
// sent as "Authorization: Bearer", ANTHROPIC_API_KEY as "x-api-key".
const (
	AuthTokenEnvVar = "ANTHROPIC_AUTH_TOKEN"
	APIKeyEnvVar    = "ANTHROPIC_API_KEY"
)

// Tier names a catalog entry may map to a model. "small" is the deprecated
// ANTHROPIC_SMALL_FAST_MODEL, which Claude Code still reads and prefers over
// the haiku tier for background work.
const (
	TierOpus     = "opus"
	TierSonnet   = "sonnet"
	TierHaiku    = "haiku"
	TierFable    = "fable"
	TierSmall    = "small"
	TierSubagent = "subagent"
)

type ModelChoice struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	// Env only applies while this model is the session model: context window
	// sizes differ from one model to the next, so they cannot be set per
	// provider without breaking the other models of the same provider.
	Env map[string]string `json:"env,omitempty"`
}

type Provider struct {
	ID          string   `json:"id"`
	DisplayName string   `json:"display_name"`
	Description string   `json:"description"`
	Category    string   `json:"category"`
	Family      Family   `json:"family"`
	AuthMode    AuthMode `json:"auth_mode"`
	// KeyVar names the secret holding the credential. With auth_mode
	// "literal" it is optional: the secret replaces the literal token when
	// set (an LM Studio server with "Require Authentication" enabled).
	KeyVar string `json:"key_var,omitempty"`
	// AuthEnvVar is the variable the credential is exported through. Empty
	// means ANTHROPIC_AUTH_TOKEN; Kimi Code documents ANTHROPIC_API_KEY.
	AuthEnvVar       string            `json:"auth_env_var,omitempty"`
	LiteralAuthToken string            `json:"literal_auth_token,omitempty"`
	BaseURL          string            `json:"base_url"`
	DefaultModel     string            `json:"default_model"`
	ModelTiers       map[string]string `json:"model_tiers"`
	ModelChoices     []ModelChoice     `json:"model_choices"`
	// ExtraEnv is provider-wide tuning from the vendor's Claude Code guide
	// (timeouts, effort level). Per-model values belong on ModelChoice.Env.
	ExtraEnv map[string]string `json:"extra_env,omitempty"`
	TestURL  string            `json:"test_url"`
	Setup    []string          `json:"setup"`
	Usage    []string          `json:"usage"`
}

// CredentialEnvVar returns the variable the provider's credential is exported
// through.
func (p Provider) CredentialEnvVar() string {
	if p.AuthEnvVar != "" {
		return p.AuthEnvVar
	}
	return AuthTokenEnvVar
}

// ModelEnv maps each model choice to the environment it needs.
func (p Provider) ModelEnv() map[string]map[string]string {
	out := map[string]map[string]string{}
	for _, choice := range p.ModelChoices {
		if len(choice.Env) > 0 {
			out[choice.ID] = choice.Env
		}
	}
	return out
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
		if provider.ExtraEnv == nil {
			provider.ExtraEnv = map[string]string{}
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
