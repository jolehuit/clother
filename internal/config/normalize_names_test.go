package config

import (
	"testing"

	"github.com/jolehuit/clother/internal/providers"
)

// TestNormalizeDropsPathTraversalNames locks the invariant that every name kept
// in the config file is a safe launcher file name. Before the fix, an alias
// such as "../../../outside/pwned" survived Normalize and made launchers.Sync
// create — and later remove — a symlink outside BinDir.
func TestNormalizeDropsPathTraversalNames(t *testing.T) {
	t.Parallel()

	catalog, err := providers.Load()
	if err != nil {
		t.Fatal(err)
	}

	cfg := &File{
		Version:           1,
		ProviderOverrides: map[string]ProviderOverride{},
		OpenRouterAliases: map[string]string{
			"../../../outside/pwned": "vendor/model",
			"sub/dir":                "vendor/model",
			"..":                     "vendor/model",
			"good-alias":             "vendor/model",
		},
		CustomProviders: map[string]CustomProvider{
			"../../../outside/custompwn": {Name: "x", BaseURL: "https://example.com", APIKeyEnv: "X_API_KEY"},
			"nested/name":                {Name: "y", BaseURL: "https://example.com", APIKeyEnv: "Y_API_KEY"},
			"good": {
				Name:      "good",
				BaseURL:   "https://api.example.com",
				APIKeyEnv: "GOOD_API_KEY",
			},
		},
	}

	cfg.Normalize(catalog)

	for name := range cfg.OpenRouterAliases {
		if !ValidName.MatchString(name) {
			t.Fatalf("alias %q survived Normalize but is not a safe launcher name", name)
		}
	}
	if cfg.OpenRouterAliases["good-alias"] != "vendor/model" {
		t.Fatalf("valid alias was dropped: %+v", cfg.OpenRouterAliases)
	}
	for name := range cfg.CustomProviders {
		if !ValidProviderName.MatchString(name) {
			t.Fatalf("custom provider %q survived Normalize but is not a safe launcher name", name)
		}
	}
	if _, ok := cfg.CustomProviders["good"]; !ok {
		t.Fatalf("valid custom provider was dropped: %+v", cfg.CustomProviders)
	}
}

// TestNormalizeDropsCustomProvidersThatCannotWork removes the entries that
// would either panic the test command (unparseable base URL) or make
// SaveSecrets fail after config.json has already been written (env key that
// starts with a digit).
func TestNormalizeDropsCustomProvidersThatCannotWork(t *testing.T) {
	t.Parallel()

	catalog, err := providers.Load()
	if err != nil {
		t.Fatal(err)
	}

	cfg := &File{
		Version:           1,
		ProviderOverrides: map[string]ProviderOverride{},
		OpenRouterAliases: map[string]string{},
		CustomProviders: map[string]CustomProvider{
			"digitkey":  {Name: "digitkey", BaseURL: "https://example.com", APIKeyEnv: "9GPT_API_KEY"},
			"9gpt":      {Name: "9gpt", BaseURL: "https://example.com", APIKeyEnv: "GPT_API_KEY"},
			"badport":   {Name: "badport", BaseURL: "http://example.com:port", APIKeyEnv: "BADPORT_API_KEY"},
			"spaceurl":  {Name: "spaceurl", BaseURL: "http://exa mple.com", APIKeyEnv: "SPACEURL_API_KEY"},
			"noscheme":  {Name: "noscheme", BaseURL: "example.com", APIKeyEnv: "NOSCHEME_API_KEY"},
			"emptyurl":  {Name: "emptyurl", BaseURL: "", APIKeyEnv: "EMPTYURL_API_KEY"},
			"trailing":  {Name: "trailing", BaseURL: "https://api.example.com/", APIKeyEnv: "TRAILING_API_KEY"},
			"nokeyvar":  {Name: "nokeyvar", BaseURL: "https://api.example.com"},
			"upperName": {Name: "upperName", BaseURL: "https://api.example.com", APIKeyEnv: "UPPERNAME_API_KEY"},
		},
	}

	cfg.Normalize(catalog)

	for _, dropped := range []string{"digitkey", "9gpt", "badport", "spaceurl", "noscheme", "emptyurl"} {
		if _, ok := cfg.CustomProviders[dropped]; ok {
			t.Fatalf("expected %q to be dropped, got %+v", dropped, cfg.CustomProviders[dropped])
		}
	}
	trailing, ok := cfg.CustomProviders["trailing"]
	if !ok || trailing.BaseURL != "https://api.example.com" {
		t.Fatalf("trailing slash should be trimmed and the entry kept, got %+v", cfg.CustomProviders)
	}
	nokeyvar, ok := cfg.CustomProviders["nokeyvar"]
	if !ok || nokeyvar.APIKeyEnv != "NOKEYVAR_API_KEY" {
		t.Fatalf("missing APIKeyEnv should be derived from the name, got %+v", cfg.CustomProviders)
	}
	if _, ok := cfg.CustomProviders["uppername"]; !ok {
		t.Fatalf("name should be lowercased, got %+v", cfg.CustomProviders)
	}
}

func TestValidBaseURL(t *testing.T) {
	t.Parallel()

	valid := []string{"http://localhost:1234", "https://api.example.com", "https://api.example.com/v1"}
	invalid := []string{"", "example.com", "ftp://example.com", "http://exa mple.com", "http://example.com:port", "http://", "http://exa\x7fmple.com"}

	for _, raw := range valid {
		if !ValidBaseURL(raw) {
			t.Fatalf("ValidBaseURL(%q) = false, want true", raw)
		}
	}
	for _, raw := range invalid {
		if ValidBaseURL(raw) {
			t.Fatalf("ValidBaseURL(%q) = true, want false", raw)
		}
	}
}
