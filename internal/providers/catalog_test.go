package providers

import (
	"encoding/json"
	"strings"
	"testing"
)

// goldenIDs pins the exact provider list. Any addition or removal has to show
// up in the diff of this file, so a provider can never disappear from a release
// unnoticed and a new endpoint always gets a reviewer's eye.
var goldenIDs = []string{
	"native",
	"zai",
	"minimax",
	"kimi",
	"moonshot",
	"deepseek",
	"mimo",
	"zai-cn",
	"minimax-cn",
	"alibaba",
	"alibaba-token-plan",
	"ve",
	"openrouter",
	"vercel-ai-gateway",
	"fireworks",
	"sambanova",
	"ollama",
	"lmstudio",
	"llamacpp",
	"vllm",
}

func loadCatalog(t *testing.T) Catalog {
	t.Helper()
	cat, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cat.All()) == 0 {
		t.Fatal("Load() returned an empty catalog")
	}
	return cat
}

func TestCatalogLoads(t *testing.T) {
	cat := loadCatalog(t)
	for _, id := range cat.IDs() {
		if _, ok := cat.Get(id); !ok {
			t.Errorf("Get(%q) missing although IDs() lists it", id)
		}
	}
}

// TestEmbeddedCatalogIsValid is the enforcement point of validate.go: every
// semantic rule is checked here against the data that actually ships.
func TestEmbeddedCatalogIsValid(t *testing.T) {
	cat := loadCatalog(t)
	for _, err := range Validate(cat) {
		t.Errorf("catalog invariant violated: %v", err)
	}
}

func TestCatalogIDsGolden(t *testing.T) {
	got := loadCatalog(t).IDs()
	if len(got) != len(goldenIDs) {
		t.Fatalf("catalog has %d providers %v, golden list has %d %v", len(got), got, len(goldenIDs), goldenIDs)
	}
	for i := range got {
		if got[i] != goldenIDs[i] {
			t.Fatalf("provider %d = %q, want %q (full list: %v)", i, got[i], goldenIDs[i], got)
		}
	}
}

// TestParseRejectsDuplicateID is the non-regression test for the silent
// hijack: before the fix, a second entry reusing an existing id overwrote the
// base URL and the model of the first one in the lookup map while both kept
// showing up in listings, and nothing in the build or the test suite noticed.
func TestParseRejectsDuplicateID(t *testing.T) {
	payload := []byte(`{"providers":[
		{"id":"zai","display_name":"Z.AI","category":"international","family":"anthropic_compatible_non_claude","auth_mode":"secret","key_var":"ZAI_API_KEY","base_url":"https://api.z.ai/api/anthropic","test_url":"https://api.z.ai/api/anthropic","doc_url":"https://docs.z.ai/devpack/latest-model","verified_at":"2026-08-15"},
		{"id":"zai","display_name":"Z.AI","category":"international","family":"anthropic_compatible_non_claude","auth_mode":"secret","key_var":"ZAI_API_KEY","base_url":"https://not-z-ai.example/anthropic","test_url":"https://not-z-ai.example/anthropic","doc_url":"https://docs.z.ai/devpack/latest-model","verified_at":"2026-08-15"}
	]}`)
	cat, err := Parse(payload)
	if err == nil {
		provider, _ := cat.Get("zai")
		t.Fatalf("Parse() accepted a duplicate id; zai now resolves to %q", provider.BaseURL)
	}
	if !strings.Contains(err.Error(), "duplicate provider id") {
		t.Fatalf("Parse() error = %v, want it to name the duplicate id", err)
	}
}

func TestParseRejectsEmptyID(t *testing.T) {
	_, err := Parse([]byte(`{"providers":[{"id":"","display_name":"x"}]}`))
	if err == nil {
		t.Fatal("Parse() accepted an entry with an empty id")
	}
}

// TestParseCorruptedCatalog checks the requirement that a broken catalogue
// produces a readable error and never a panic.
func TestParseCorruptedCatalog(t *testing.T) {
	cases := map[string]string{
		"truncated":       `{"providers":[{"id":"zai"`,
		"empty":           ``,
		"not an object":   `[]`,
		"providers a map": `{"providers":{"zai":{}}}`,
		"wrong type":      `{"providers":[{"id":42}]}`,
		"binary garbage":  "\x00\xff\xfe not json at all",
	}
	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("Parse() panicked on a corrupted catalog: %v", r)
				}
			}()
			cat, err := Parse([]byte(payload))
			if err == nil {
				t.Fatalf("Parse() accepted a corrupted catalog, got %d providers", len(cat.All()))
			}
			if !strings.Contains(err.Error(), "providers catalog") {
				t.Fatalf("error %q does not say which file is at fault", err)
			}
		})
	}
}

// TestParseTrimsTrailingSlash pins the normalisation: the Kimi Code base URL is
// published with a trailing slash, and exporting it verbatim makes Claude Code
// build ".../coding//v1/messages".
func TestParseTrimsTrailingSlash(t *testing.T) {
	cat, err := Parse([]byte(`{"providers":[{"id":"kimi","base_url":"https://api.kimi.com/coding/","test_url":"https://api.kimi.com/coding/"}]}`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	provider, _ := cat.Get("kimi")
	if provider.BaseURL != "https://api.kimi.com/coding" {
		t.Errorf("BaseURL = %q, want the trailing slash trimmed", provider.BaseURL)
	}
	if provider.TestURL != "https://api.kimi.com/coding" {
		t.Errorf("TestURL = %q, want the trailing slash trimmed", provider.TestURL)
	}
}

// TestRawCatalogURLsHaveNoTrailingSlash keeps the source data honest too:
// Parse would hide the defect, and the next reader would copy the bad shape.
func TestRawCatalogURLsHaveNoTrailingSlash(t *testing.T) {
	var payload struct {
		Providers []map[string]any `json:"providers"`
	}
	if err := json.Unmarshal(RawCatalog(), &payload); err != nil {
		t.Fatalf("unmarshal raw catalog: %v", err)
	}
	for _, entry := range payload.Providers {
		for _, field := range []string{"base_url", "test_url"} {
			value, _ := entry[field].(string)
			if value != "" && strings.HasSuffix(value, "/") {
				t.Errorf("provider %v: %s = %q ends with a slash", entry["id"], field, value)
			}
		}
	}
}

// TestEveryTargetExportsACredential is the non-regression test for llama.cpp
// declaring auth_mode "none": a base URL without a credential variable leaves
// the saved claude.ai login as the active credential, so the user's
// subscription token is what reaches the third-party or local endpoint.
func TestEveryTargetExportsACredential(t *testing.T) {
	for _, provider := range loadCatalog(t).All() {
		if provider.BaseURL == "" {
			continue
		}
		switch provider.AuthMode {
		case AuthSecret:
			if provider.KeyVar == "" {
				t.Errorf("provider %q: auth_mode secret without key_var", provider.ID)
			}
		case AuthLiteral:
			if provider.LiteralAuthToken == "" {
				t.Errorf("provider %q: auth_mode literal without literal_auth_token", provider.ID)
			}
		default:
			t.Errorf("provider %q: base_url %q with auth_mode %q exports no credential",
				provider.ID, provider.BaseURL, provider.AuthMode)
		}
	}
}

// TestModelTiersAreComplete is the non-regression test for the half-broken
// providers: a profile that pins ANTHROPIC_MODEL but leaves a tier empty sends
// a claude-* identifier to an endpoint that has never heard of it as soon as
// the user types /model or a subagent starts.
func TestModelTiersAreComplete(t *testing.T) {
	for _, provider := range loadCatalog(t).All() {
		if provider.DefaultModel == "" {
			continue
		}
		for _, tier := range ModelTierKeys {
			if provider.ModelTiers[tier] == "" {
				t.Errorf("provider %q: default_model %q but tier %q is unset",
					provider.ID, provider.DefaultModel, tier)
			}
		}
	}
}

// TestDefaultModelComesWithChoices is the non-regression test for the empty
// model picker: `clother config ve` printed "Choose model:" followed by nothing.
func TestDefaultModelComesWithChoices(t *testing.T) {
	for _, provider := range loadCatalog(t).All() {
		if provider.DefaultModel != "" && len(provider.ModelChoices) == 0 {
			t.Errorf("provider %q: default_model %q with an empty model_choices list",
				provider.ID, provider.DefaultModel)
		}
	}
}

// TestKeyVarsAreNotShared keeps two profiles from overwriting each other's
// secret: the three Alibaba variants used to share ALIBABA_API_KEY, so
// configuring one silently destroyed the credential of the other two.
func TestKeyVarsAreNotShared(t *testing.T) {
	owner := map[string]string{}
	for _, provider := range loadCatalog(t).All() {
		if provider.KeyVar == "" {
			continue
		}
		if previous, ok := owner[provider.KeyVar]; ok {
			t.Errorf("providers %q and %q share key_var %s: configuring one overwrites the other",
				previous, provider.ID, provider.KeyVar)
			continue
		}
		owner[provider.KeyVar] = provider.ID
	}
}

// TestCompatibilityEnvIsDeclared: every endpoint that is not api.anthropic.com
// needs the Claude Code compatibility knobs, and every entry needs a source.
func TestCompatibilityEnvIsDeclared(t *testing.T) {
	for _, provider := range loadCatalog(t).All() {
		if provider.DocURL == "" || provider.VerifiedAt == "" {
			t.Errorf("provider %q: doc_url/verified_at are mandatory", provider.ID)
		}
		if provider.Family == FamilyClaudeStrict {
			continue
		}
		if len(provider.ExtraEnv) == 0 {
			t.Errorf("provider %q: no extra_env, so every compatibility break stays a code change", provider.ID)
		}
		if provider.ExtraEnv["CLAUDE_CODE_SUBPROCESS_ENV_SCRUB"] != "1" {
			t.Errorf("provider %q: CLAUDE_CODE_SUBPROCESS_ENV_SCRUB must be 1 so the provider key stays out of Bash/hook/MCP subprocesses", provider.ID)
		}
	}
}

func TestCredentialEnvVar(t *testing.T) {
	cat := loadCatalog(t)
	kimi, ok := cat.Get("kimi")
	if !ok {
		t.Fatal("kimi missing from the catalog")
	}
	if got := kimi.CredentialEnvVar(); got != "ANTHROPIC_API_KEY" {
		t.Errorf("kimi credential var = %q, want ANTHROPIC_API_KEY (Kimi Code rejects a bearer token)", got)
	}
	moonshot, ok := cat.Get("moonshot")
	if !ok {
		t.Fatal("moonshot missing from the catalog")
	}
	if got := moonshot.CredentialEnvVar(); got != DefaultCredentialEnvVar {
		t.Errorf("moonshot credential var = %q, want %q", got, DefaultCredentialEnvVar)
	}
}

func TestValidateReportsEveryViolation(t *testing.T) {
	cat, err := Parse([]byte(`{"providers":[{
		"id":"BAD_ID",
		"display_name":"",
		"category":"",
		"family":"anthropic_compat",
		"auth_mode":"api_key",
		"base_url":"http://evil.example/anthropic",
		"default_model":"ghost",
		"model_tiers":{"opus":"ghost","nope":"ghost"},
		"model_choices":[{"id":"real"}],
		"test_url":"ftp://nope",
		"extra_env":{"lowercase":"1"},
		"doc_url":""
	}]}`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	errs := Validate(cat)
	joined := ""
	for _, e := range errs {
		joined += e.Error() + "\n"
	}
	for _, want := range []string{
		"id must match",
		"display_name is empty",
		"category is empty",
		"unknown family",
		"unknown auth_mode",
		"must use https",
		"absent from model_choices",
		"unknown tier",
		"extra_env key",
		"doc_url is required",
		"verified_at is required",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("Validate() did not report %q; got:\n%s", want, joined)
		}
	}
}

func TestValidateAcceptsLoopbackHTTPForLocalOnly(t *testing.T) {
	ok, err := Parse([]byte(`{"providers":[{"id":"local","display_name":"L","category":"local","family":"local","auth_mode":"literal","literal_auth_token":"x","base_url":"http://127.0.0.1:8080","test_url":"http://127.0.0.1:8080","doc_url":"https://example.com/doc","verified_at":"2026-08-15"}]}`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if errs := Validate(ok); len(errs) != 0 {
		t.Errorf("Validate() rejected a loopback local backend: %v", errs)
	}

	remote, err := Parse([]byte(`{"providers":[{"id":"local","display_name":"L","category":"local","family":"local","auth_mode":"literal","literal_auth_token":"x","base_url":"http://198.51.100.7:8080","test_url":"http://198.51.100.7:8080","doc_url":"https://example.com/doc","verified_at":"2026-08-15"}]}`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	errs := Validate(remote)
	if len(errs) == 0 {
		t.Fatal("Validate() accepted plain http to a non-loopback host")
	}
}

func TestSortChoicesDoesNotMutateInput(t *testing.T) {
	input := []ModelChoice{{ID: "b"}, {ID: "a"}}
	got := SortChoices(input)
	if input[0].ID != "b" {
		t.Errorf("SortChoices mutated its input: %v", input)
	}
	if got[0].ID != "a" || got[1].ID != "b" {
		t.Errorf("SortChoices() = %v, want sorted by id", got)
	}
}
