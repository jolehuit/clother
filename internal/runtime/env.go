package runtime

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jolehuit/clother/internal/config"
	"github.com/jolehuit/clother/internal/profiles"
	"github.com/jolehuit/clother/internal/providers"
)

// IsHomebrew reports whether the running binary is managed by Homebrew.
//
// It first checks the HOMEBREW_PREFIX env var (set during `brew install`),
// then falls back to inspecting whether the resolved executable path lives
// inside a Homebrew Cellar directory — which is the case when the user runs
// a Homebrew-installed binary from their normal shell session.
func IsHomebrew() bool {
	if os.Getenv("HOMEBREW_PREFIX") != "" {
		return true
	}
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		resolved = exe
	}
	return strings.Contains(resolved, "/Cellar/")
}

// tierEnvVars maps each tier to the variable Claude Code reads it from.
var tierEnvVars = map[string]string{
	providers.TierOpus:     "ANTHROPIC_DEFAULT_OPUS_MODEL",
	providers.TierSonnet:   "ANTHROPIC_DEFAULT_SONNET_MODEL",
	providers.TierHaiku:    "ANTHROPIC_DEFAULT_HAIKU_MODEL",
	providers.TierFable:    "ANTHROPIC_DEFAULT_FABLE_MODEL",
	providers.TierSmall:    "ANTHROPIC_SMALL_FAST_MODEL",
	providers.TierSubagent: "CLAUDE_CODE_SUBAGENT_MODEL",
}

// inheritedRoutingKeys are dropped from the parent environment for every
// family but claude_strict. Each of them can reroute a third-party session:
// the cloud-provider switches send it to Bedrock/Vertex/Foundry, a stale
// subagent model or context size was meant for another provider, and
// CLAUDE_SECURESTORAGE_CONFIG_DIR would point the keychain lookup back at the
// user's own Anthropic credentials.
var inheritedRoutingKeys = []string{
	"CLAUDE_CODE_SUBAGENT_MODEL",
	"CLAUDE_CODE_USE_BEDROCK",
	"CLAUDE_CODE_USE_VERTEX",
	"CLAUDE_CODE_USE_FOUNDRY",
	"CLAUDE_CODE_AUTO_COMPACT_WINDOW",
	"CLAUDE_CODE_MAX_CONTEXT_TOKENS",
	"CLAUDE_SECURESTORAGE_CONFIG_DIR",
}

func BuildEnv(target profiles.Target, secrets config.Secrets) ([]string, error) {
	envMap := map[string]string{}
	for _, pair := range os.Environ() {
		key, value, ok := splitEnv(pair)
		if ok {
			envMap[key] = value
		}
	}
	clearAnthropicEnv(envMap)
	strict := target.Family == providers.FamilyClaudeStrict
	if !strict {
		for _, key := range inheritedRoutingKeys {
			delete(envMap, key)
		}
	}

	if target.BaseURL != "" {
		envMap["ANTHROPIC_BASE_URL"] = target.BaseURL
	}
	if target.Model != "" {
		envMap["ANTHROPIC_MODEL"] = target.Model
	}
	for tier, model := range profiles.EffectiveTiers(target) {
		if key, ok := tierEnvVars[tier]; ok {
			envMap[key] = model
		}
	}

	switch target.AuthMode {
	case providers.AuthNone:
	case providers.AuthLiteral:
		token := target.LiteralAuthToken
		if target.SecretKey != "" && secrets[target.SecretKey] != "" {
			token = secrets[target.SecretKey]
		}
		exportCredential(envMap, target, token)
	case providers.AuthSecret:
		value := secrets[target.SecretKey]
		if value == "" {
			return nil, fmt.Errorf("%s not configured", target.SecretKey)
		}
		exportCredential(envMap, target, value)
	default:
		return nil, fmt.Errorf("unsupported auth mode %q", target.AuthMode)
	}

	if !strict {
		for key, value := range target.ExtraEnv {
			envMap[key] = value
		}
		for key, value := range target.ModelEnv[target.Model] {
			envMap[key] = value
		}
	}

	return flattenEnv(envMap), nil
}

// exportCredential publishes the credential through the variable the provider
// documents and blanks the other one, so a key exported by the user's shell
// can never ride along to the third party.
func exportCredential(envMap map[string]string, target profiles.Target, value string) {
	chosen := target.CredentialEnvVar
	if chosen == "" {
		chosen = providers.AuthTokenEnvVar
	}
	envMap[chosen] = value
	for _, key := range []string{providers.AuthTokenEnvVar, providers.APIKeyEnvVar} {
		if key != chosen {
			envMap[key] = ""
		}
	}
}

func clearAnthropicEnv(envMap map[string]string) {
	for key := range envMap {
		if strings.HasPrefix(key, "ANTHROPIC_") {
			delete(envMap, key)
		}
	}
}

func splitEnv(pair string) (string, string, bool) {
	for i := 0; i < len(pair); i++ {
		if pair[i] == '=' {
			return pair[:i], pair[i+1:], true
		}
	}
	return "", "", false
}

func flattenEnv(envMap map[string]string) []string {
	env := make([]string, 0, len(envMap))
	for key, value := range envMap {
		env = append(env, key+"="+value)
	}
	return env
}
