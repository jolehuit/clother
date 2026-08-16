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
// The decision is made on the resolved path of the executable ONLY. The
// HOMEBREW_PREFIX env var is not a signal on its own: `brew shellenv`, which
// Homebrew tells every Apple Silicon and Linuxbrew user to put in their shell
// profile, exports it for every process. Trusting it made every curl-installed
// binary claim to be a Homebrew one, which routed `clother update` to
// `brew upgrade clother` and made `clother install` skip the download.
func IsHomebrew() bool {
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		resolved = exe
	}
	if prefix := os.Getenv("HOMEBREW_PREFIX"); prefix != "" {
		cellar := filepath.Join(prefix, "Cellar") + string(os.PathSeparator)
		if strings.HasPrefix(resolved, cellar) {
			return true
		}
	}
	return strings.Contains(resolved, "/Cellar/")
}

// inheritedRoutingKeys are the variables that let an inherited shell
// configuration redirect Claude Code somewhere else than the provider Clother
// just selected, or pin a model the provider does not serve. They are dropped
// for every family but claude_strict, where the user's own Anthropic routing is
// legitimate.
var inheritedRoutingKeys = []string{
	"API_TIMEOUT_MS",
	"AWS_BEARER_TOKEN_BEDROCK",
	"CLAUDE_CODE_MAX_OUTPUT_TOKENS",
	"CLAUDE_CODE_SKIP_BEDROCK_AUTH",
	"CLAUDE_CODE_SKIP_VERTEX_AUTH",
	"CLAUDE_CODE_SUBAGENT_MODEL",
	"CLAUDE_CODE_USE_BEDROCK",
	"CLAUDE_CODE_USE_FOUNDRY",
	"CLAUDE_CODE_USE_VERTEX",
	"CLOUD_ML_REGION",
}

// subprocessScrubKey hides the injected credential from every Bash command,
// hook and MCP server of the session. It is a security decision, not a
// preference, so it is forced rather than defaulted — see enforceCredentialScrub.
const subprocessScrubKey = "CLAUDE_CODE_SUBPROCESS_ENV_SCRUB"

// sessionHardeningDefaults are posted for every non-claude_strict family.
//
// They keep a session that talks to a third party from also talking to
// Anthropic's telemetry endpoints. Each key is only posted when it is absent
// from the inherited environment, so `DISABLE_TELEMETRY= clother-zai` (or any
// explicit value) opts out — which is reasonable for telemetry.
var sessionHardeningDefaults = map[string]string{
	"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1",
	"DISABLE_TELEMETRY":                        "1",
	"DISABLE_ERROR_REPORTING":                  "1",
}

// hardeningWarn reports a hardening decision to the user. Overridden in tests.
var hardeningWarn = func(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "clother: "+format+"\n", args...)
}

// enforceCredentialScrub pins the subprocess scrub on for every non-strict
// family.
//
// It used to sit in sessionHardeningDefaults, i.e. it was only posted when the
// key was absent from the inherited environment — and it is not in
// inheritedRoutingKeys, so it was not purged first either. Any inherited value
// (a shell profile, a repository .envrc, a setting left by an older version)
// therefore switched the protection off, silently, and the credential Clother
// had just injected became readable by every command of the session.
func enforceCredentialScrub(envMap map[string]string) {
	if inherited, ok := envMap[subprocessScrubKey]; ok && inherited != "1" {
		hardeningWarn("ignoring inherited %s=%q: the credential injected for this session must stay hidden from its subprocesses",
			subprocessScrubKey, inherited)
	}
	envMap[subprocessScrubKey] = "1"
}

// modelTierEnv maps a tier name to the variable claude reads for it.
// ANTHROPIC_SMALL_FAST_MODEL is deprecated in favour of
// ANTHROPIC_DEFAULT_HAIKU_MODEL and is only kept as a compatibility alias.
var modelTierEnv = []struct {
	tier string
	key  string
}{
	{"haiku", "ANTHROPIC_DEFAULT_HAIKU_MODEL"},
	{"sonnet", "ANTHROPIC_DEFAULT_SONNET_MODEL"},
	{"opus", "ANTHROPIC_DEFAULT_OPUS_MODEL"},
	{"fable", "ANTHROPIC_DEFAULT_FABLE_MODEL"},
	{"small", "ANTHROPIC_SMALL_FAST_MODEL"},
}

func BuildEnv(target profiles.Target, secrets config.Secrets) ([]string, error) {
	return buildEnv(target, secrets, true)
}

// BuildEnvWithoutCredential builds the same child environment as BuildEnv but
// tolerates a provider whose key has not been configured yet. It is only for
// invocations that make no API call (`claude --help`, `claude --version`):
// nothing is authenticated, so nothing can leak, and the user gets the help
// that tells them how to configure the key.
func BuildEnvWithoutCredential(target profiles.Target) ([]string, error) {
	return buildEnv(target, config.Secrets{}, false)
}

func buildEnv(target profiles.Target, secrets config.Secrets, requireCredential bool) ([]string, error) {
	if err := profiles.Validate(target); err != nil {
		return nil, err
	}

	envMap := map[string]string{}
	for _, pair := range os.Environ() {
		key, value, ok := splitEnv(pair)
		if ok {
			envMap[key] = value
		}
	}
	strict := target.Family == providers.FamilyClaudeStrict
	clearInheritedProviderEnv(envMap, target, strict)

	if !strict {
		for key, value := range sessionHardeningDefaults {
			if _, ok := envMap[key]; !ok {
				envMap[key] = value
			}
		}
		enforceCredentialScrub(envMap)
	}

	if target.BaseURL != "" {
		envMap["ANTHROPIC_BASE_URL"] = target.BaseURL
	}
	if target.Model != "" {
		envMap["ANTHROPIC_MODEL"] = target.Model
	}
	applyModelTiers(envMap, target, strict)

	switch target.AuthMode {
	case providers.AuthNone:
	case providers.AuthLiteral:
		exportCredential(envMap, target, target.LiteralAuthToken, strict)
	case providers.AuthSecret:
		value := secrets[target.SecretKey]
		if value == "" {
			if requireCredential {
				return nil, fmt.Errorf("%s not configured — run `clother config %s`", target.SecretKey, target.Profile)
			}
			break
		}
		exportCredential(envMap, target, value, strict)
	default:
		return nil, fmt.Errorf("unsupported auth mode %q", target.AuthMode)
	}

	// Provider-specific tuning declared by the catalog (context window,
	// timeouts, betas to disable). Applied last so a provider entry can
	// override any default posted above.
	for key, value := range target.ExtraEnv {
		if key != "" {
			envMap[key] = value
		}
	}

	return flattenEnv(envMap), nil
}

// exportCredential publishes the credential through the variable the provider
// documents, and explicitly blanks the other one.
//
// Claude Code reads both ANTHROPIC_AUTH_TOKEN and ANTHROPIC_API_KEY. Most
// third-party endpoints want the bearer token, but Kimi Code and SambaNova
// document ANTHROPIC_API_KEY and reject a bearer token — the catalog carries
// that per provider through auth_env_var, and this is where it is honoured.
// The unused variable is set to the empty string rather than left absent
// because several providers document that the two coexisting produces a 401 or
// a silent fallback to api.anthropic.com.
func exportCredential(envMap map[string]string, target profiles.Target, value string, strict bool) {
	chosen := target.CredentialEnvVar
	if chosen == "" {
		chosen = providers.DefaultCredentialEnvVar
	}
	envMap[chosen] = value
	if strict {
		// claude_strict talks to Anthropic itself; the user's own key variable
		// is legitimate there and must not be blanked.
		return
	}
	for _, key := range providers.CredentialEnvVars {
		if key != chosen {
			envMap[key] = ""
		}
	}
}

func applyModelTiers(envMap map[string]string, target profiles.Target, strict bool) {
	if strict {
		// claude_strict talks to Anthropic, which resolves its own aliases.
		for tier, value := range target.ModelTiers {
			for _, entry := range modelTierEnv {
				if entry.tier == tier && value != "" {
					envMap[entry.key] = value
				}
			}
		}
		return
	}

	fallback := strings.TrimSpace(target.Model)
	if fallback == "" {
		fallback = firstConfiguredTier(target)
	}
	for _, entry := range modelTierEnv {
		model := strings.TrimSpace(target.ModelTiers[entry.tier])
		if model == "" {
			// No third-party provider knows the Claude aliases, so an
			// unmapped tier must never be left to claude's built-in ids: it
			// would send claude-sonnet-* to the provider and 404.
			model = fallback
		}
		if model != "" {
			envMap[entry.key] = model
		}
	}
}

// firstConfiguredTier is the fallback when a target declares tiers but no
// default model of its own.
func firstConfiguredTier(target profiles.Target) string {
	for _, tier := range []string{"opus", "sonnet", "haiku", "fable", "small"} {
		if model := strings.TrimSpace(target.ModelTiers[tier]); model != "" {
			return model
		}
	}
	return ""
}

// clearInheritedProviderEnv removes everything the parent shell could use to
// redirect the session or to leak another provider's credential into it.
func clearInheritedProviderEnv(envMap map[string]string, target profiles.Target, strict bool) {
	for key := range envMap {
		if strings.HasPrefix(key, "ANTHROPIC_") {
			delete(envMap, key)
		}
	}
	if !strict {
		for _, key := range inheritedRoutingKeys {
			delete(envMap, key)
		}
	}
	for key := range foreignSecretKeys(target) {
		delete(envMap, key)
	}
}

// foreignSecretKeys is the set of provider credentials that must not reach the
// child: every key of the builtin catalog and of the user's custom providers,
// minus the one the current target actually needs.
func foreignSecretKeys(target profiles.Target) map[string]struct{} {
	keys := map[string]struct{}{"OPENROUTER_API_KEY": {}}
	if catalog, err := providers.Load(); err == nil {
		for key := range catalog.BuiltinSecretKeys() {
			keys[key] = struct{}{}
		}
	}
	for _, key := range target.ForeignSecretKeys {
		if key != "" {
			keys[key] = struct{}{}
		}
	}
	delete(keys, target.SecretKey)
	return keys
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
