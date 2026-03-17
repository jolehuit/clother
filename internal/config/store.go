package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/jolehuit/clother/internal/providers"
)

type ProviderOverride struct {
	Model string `json:"model,omitempty"`
}

type CustomProvider struct {
	Name         string `json:"name"`
	DisplayName  string `json:"display_name"`
	BaseURL      string `json:"base_url"`
	APIKeyEnv    string `json:"api_key_env"`
	DefaultModel string `json:"default_model,omitempty"`
}

type File struct {
	Version           int                         `json:"version"`
	ProviderOverrides map[string]ProviderOverride `json:"provider_overrides,omitempty"`
	OpenRouterAliases map[string]string           `json:"openrouter_aliases,omitempty"`
	CustomProviders   map[string]CustomProvider   `json:"custom_providers,omitempty"`
}

func LoadConfig(path string) (*File, error) {
	cfg := &File{
		Version:           1,
		ProviderOverrides: map[string]ProviderOverride{},
		OpenRouterAliases: map[string]string{},
		CustomProviders:   map[string]CustomProvider{},
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	if cfg.ProviderOverrides == nil {
		cfg.ProviderOverrides = map[string]ProviderOverride{}
	}
	if cfg.OpenRouterAliases == nil {
		cfg.OpenRouterAliases = map[string]string{}
	}
	if cfg.CustomProviders == nil {
		cfg.CustomProviders = map[string]CustomProvider{}
	}
	return cfg, nil
}

func SaveConfig(path string, cfg *File) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return writeAtomic(path, data, 0o644)
}

func (cfg *File) ApplyLegacySecrets(secrets Secrets, catalog providers.Catalog) {
	builtinSecretKeys := catalog.BuiltinSecretKeys()
	for key, value := range secrets {
		if strings.HasPrefix(key, "OPENROUTER_MODEL_") {
			name := normalizeOpenRouterAliasName(strings.ToLower(strings.ReplaceAll(strings.TrimPrefix(key, "OPENROUTER_MODEL_"), "_", "-")))
			value = strings.TrimSpace(value)
			if name != "" && !looksLikeLauncherName(value) {
				if _, ok := cfg.OpenRouterAliases[name]; !ok && value != "" {
					cfg.OpenRouterAliases[name] = value
				}
			}
			continue
		}
		if !strings.HasSuffix(key, "_API_KEY") {
			continue
		}
		if _, ok := builtinSecretKeys[key]; ok {
			continue
		}
		baseURLKey := "CLOTHER_" + key + "_BASE_URL"
		baseURL := secrets[baseURLKey]
		if baseURL == "" {
			continue
		}
		name := strings.ToLower(strings.ReplaceAll(strings.TrimSuffix(key, "_API_KEY"), "_", "-"))
		if _, exists := cfg.CustomProviders[name]; exists {
			continue
		}
		cfg.CustomProviders[name] = CustomProvider{
			Name:        name,
			DisplayName: name,
			BaseURL:     baseURL,
			APIKeyEnv:   key,
		}
	}
}

func (cfg *File) Normalize(catalog providers.Catalog) {
	if cfg.ProviderOverrides == nil {
		cfg.ProviderOverrides = map[string]ProviderOverride{}
	}
	if cfg.OpenRouterAliases == nil {
		cfg.OpenRouterAliases = map[string]string{}
	}
	if cfg.CustomProviders == nil {
		cfg.CustomProviders = map[string]CustomProvider{}
	}

	for id, override := range cfg.ProviderOverrides {
		if strings.TrimSpace(override.Model) == "" {
			delete(cfg.ProviderOverrides, id)
			continue
		}
		provider, ok := catalog.Get(id)
		if !ok {
			continue
		}
		model := normalizeProviderOverrideModel(provider, override.Model)
		if model == "" || model == provider.DefaultModel {
			delete(cfg.ProviderOverrides, id)
			continue
		}
		cfg.ProviderOverrides[id] = ProviderOverride{Model: model}
	}

	normalizedAliases := map[string]string{}
	for name, model := range cfg.OpenRouterAliases {
		name = normalizeOpenRouterAliasName(name)
		model = strings.TrimSpace(model)
		if name == "" || model == "" || looksLikeLauncherName(model) {
			continue
		}
		if _, exists := normalizedAliases[name]; !exists {
			normalizedAliases[name] = model
		}
	}
	cfg.OpenRouterAliases = normalizedAliases
}

func normalizeProviderOverrideModel(provider providers.Provider, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if idx, err := strconv.Atoi(value); err == nil {
		if idx >= 1 && idx <= len(provider.ModelChoices) {
			return provider.ModelChoices[idx-1].ID
		}
	}
	return value
}

func normalizeOpenRouterAliasName(name string) string {
	name = strings.TrimSpace(strings.ToLower(name))
	for strings.HasPrefix(name, "clother-or-") {
		name = strings.TrimPrefix(name, "clother-or-")
	}
	name = strings.Trim(name, "-")
	return name
}

func looksLikeLauncherName(value string) bool {
	value = strings.TrimSpace(strings.ToLower(value))
	return strings.HasPrefix(value, "clother-")
}

func (cfg *File) OpenRouterNames() []string {
	return mapKeys(cfg.OpenRouterAliases)
}

func (cfg *File) CustomProviderNames() []string {
	return mapKeys(cfg.CustomProviders)
}

func mapKeys[T any](input map[string]T) []string {
	keys := make([]string, 0, len(input))
	for key := range input {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
