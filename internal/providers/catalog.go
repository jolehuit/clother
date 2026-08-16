package providers

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

//go:embed catalog.json
var rawCatalog []byte

// RawCatalog returns the embedded catalog bytes. Tests use it to assert
// invariants that only exist in the source data (formatting, key ordering)
// and that normalisation in Parse would otherwise hide.
func RawCatalog() []byte {
	out := make([]byte, len(rawCatalog))
	copy(out, rawCatalog)
	return out
}

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

// DefaultCredentialEnvVar is the Claude Code environment variable that carries
// the provider credential unless the provider documents another one. It is
// preferred over ANTHROPIC_API_KEY: it takes precedence immediately and never
// triggers the persisted approval prompt Claude Code shows for an API key.
const DefaultCredentialEnvVar = "ANTHROPIC_AUTH_TOKEN"

// CredentialEnvVars lists the Claude Code variables a provider may legitimately
// use to carry its credential.
var CredentialEnvVars = []string{"ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_API_KEY"}

// ModelTierKeys are the tiers Claude Code resolves through a dedicated
// ANTHROPIC_DEFAULT_*_MODEL variable ("small" being the legacy
// ANTHROPIC_SMALL_FAST_MODEL, deprecated in favour of the haiku tier).
var ModelTierKeys = []string{"haiku", "sonnet", "opus", "fable", "small"}

type ModelChoice struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	// Note carries a caveat about availability ("coding plan only", "sunset")
	// so the catalogue can ship a model id without implying every key can call it.
	Note string `json:"note,omitempty"`
}

type Provider struct {
	ID               string   `json:"id"`
	DisplayName      string   `json:"display_name"`
	Description      string   `json:"description"`
	Category         string   `json:"category"`
	Family           Family   `json:"family"`
	AuthMode         AuthMode `json:"auth_mode"`
	KeyVar           string   `json:"key_var,omitempty"`
	LiteralAuthToken string   `json:"literal_auth_token,omitempty"`
	// AuthEnvVar names the Claude Code variable the credential must be exported
	// as. Empty means DefaultCredentialEnvVar. Kimi Code and SambaNova document
	// ANTHROPIC_API_KEY and reject a bearer token, hence the per-provider field.
	AuthEnvVar   string            `json:"auth_env_var,omitempty"`
	BaseURL      string            `json:"base_url"`
	DefaultModel string            `json:"default_model"`
	ModelTiers   map[string]string `json:"model_tiers"`
	ModelChoices []ModelChoice     `json:"model_choices"`
	TestURL      string            `json:"test_url"`
	// ExtraEnv carries the Claude Code compatibility knobs this endpoint needs
	// (auto-compaction window, timeouts, experimental betas, attribution header,
	// streaming watchdog). Keeping them in data turns a compatibility break into
	// a catalogue patch instead of a code change.
	ExtraEnv map[string]string `json:"extra_env,omitempty"`
	// DocURL is the official page that justifies BaseURL and the model ids.
	DocURL string `json:"doc_url"`
	// VerifiedAt is the day DocURL was last read, as YYYY-MM-DD.
	VerifiedAt string   `json:"verified_at"`
	Setup      []string `json:"setup"`
	Usage      []string `json:"usage"`
}

// CredentialEnvVar returns the Claude Code variable carrying this provider's
// credential.
func (p Provider) CredentialEnvVar() string {
	if p.AuthEnvVar != "" {
		return p.AuthEnvVar
	}
	return DefaultCredentialEnvVar
}

type Catalog struct {
	ordered []Provider
	byID    map[string]Provider
}

func Load() (Catalog, error) {
	return Parse(rawCatalog)
}

// Parse decodes a catalogue document. It rejects the two data faults that are
// invisible at runtime but reroute a provider: a duplicate id (the second entry
// silently wins in the lookup map while both show up in listings) and an entry
// that carries no id at all. Everything else is normalised, never rejected:
// Load runs at the start of every command, so a cosmetic defect must not turn
// into a binary that refuses to start. The full rule set lives in Validate and
// is enforced by the catalogue test.
func Parse(data []byte) (Catalog, error) {
	var payload struct {
		Providers []Provider `json:"providers"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return Catalog{}, fmt.Errorf("decode providers catalog: %w", err)
	}
	cat := Catalog{
		ordered: make([]Provider, 0, len(payload.Providers)),
		byID:    make(map[string]Provider, len(payload.Providers)),
	}
	for index, provider := range payload.Providers {
		if provider.ID == "" {
			return Catalog{}, fmt.Errorf("providers catalog: entry %d has an empty id", index)
		}
		if _, dup := cat.byID[provider.ID]; dup {
			return Catalog{}, fmt.Errorf("providers catalog: duplicate provider id %q", provider.ID)
		}
		normalize(&provider)
		cat.ordered = append(cat.ordered, provider)
		cat.byID[provider.ID] = provider
	}
	return cat, nil
}

func normalize(provider *Provider) {
	if provider.ModelTiers == nil {
		provider.ModelTiers = map[string]string{}
	}
	if provider.ExtraEnv == nil {
		provider.ExtraEnv = map[string]string{}
	}
	// A trailing slash makes Claude Code build ".../coding//v1/messages"; the
	// rest of the codebase already trims in the other direction.
	provider.BaseURL = strings.TrimRight(provider.BaseURL, "/")
	provider.TestURL = strings.TrimRight(provider.TestURL, "/")
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
