package runtime

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"

	"github.com/jolehuit/clother/internal/config"
	"github.com/jolehuit/clother/internal/profiles"
	"github.com/jolehuit/clother/internal/providers"
)

// overlayMarker records, inside an overlay, which Claude config it mirrors. A
// launcher started from inside a Clother session sees CLAUDE_CONFIG_DIR point
// at the parent's overlay and resolves the real config through it, instead of
// mirroring a mirror.
const overlayMarker = ".clother-overlay.json"

// strippedStateKeys never reach a third-party session. primaryApiKey is the
// Console key saved by /login: Claude Code sends it as x-api-key next to the
// provider's bearer token (issue #40). apiKey is its legacy spelling and
// oauthAccount belongs to the user's Anthropic subscription.
var strippedStateKeys = map[string]bool{
	"primaryApiKey": true,
	"apiKey":        true,
	"oauthAccount":  true,
}

// strippedCredentialKeys are removed from the copy of .credentials.json, the
// plaintext credential store Claude Code uses on Linux: claudeAiOauth is the
// claude.ai subscription token. The rest (MCP servers' OAuth tokens) stays.
var strippedCredentialKeys = map[string]bool{
	"claudeAiOauth": true,
}

const credentialsFile = ".credentials.json"

// skippedConfigEntries are not symlinked into the overlay: settings.json is
// rewritten, and the state and credential files are copied without the
// user's Anthropic credentials.
var skippedConfigEntries = map[string]bool{
	"settings.json": true,
	".claude.json":  true,
	".config.json":  true,
	credentialsFile: true,
	overlayMarker:   true,
}

// credentialSettingsKeys produce an Anthropic key from settings.json: the
// output of apiKeyHelper is sent as x-api-key exactly like primaryApiKey.
var credentialSettingsKeys = []string{"apiKeyHelper"}

// modelAliases are the --model values Claude Code resolves through the tier
// variables, mapped to the tier they read. Overwriting the tiers with the alias
// itself would send "opus" to the provider.
var modelAliases = map[string]string{
	"default":      "",
	"opus":         providers.TierOpus,
	"opus[1m]":     providers.TierOpus,
	"opusplan":     providers.TierOpus,
	"opusplan[1m]": providers.TierOpus,
	"sonnet":       providers.TierSonnet,
	"sonnet[1m]":   providers.TierSonnet,
	"haiku":        providers.TierHaiku,
	"fable":        providers.TierFable,
	"fable[1m]":    providers.TierFable,
	"best":         providers.TierFable,
}

type claudeSource struct {
	ConfigDir string `json:"config_dir"`
	StateFile string `json:"state_file"`
}

// PrepareClaudeConfigOverlay points a third-party session at a temporary
// CLAUDE_CONFIG_DIR that mirrors the user's config without any Anthropic
// credential in it. The overlay is built for every family but claude_strict,
// with or without a model: Claude Code derives its keychain entry names from
// CLAUDE_CONFIG_DIR, so the overlay is also what keeps a keychain-stored key
// out of the session.
func PrepareClaudeConfigOverlay(target profiles.Target, args []string, env []string) ([]string, func(), error) {
	if target.Family == providers.FamilyClaudeStrict {
		return env, func() {}, nil
	}

	envMap := envSliceToMap(env)
	sessionModel := applyModelOverride(target, ModelOverride(args), envMap)
	source := resolveClaudeSource(envMap["CLAUDE_CONFIG_DIR"])

	overlayDir, err := os.MkdirTemp("", "clother-claude-config-*")
	if err != nil {
		return nil, nil, err
	}
	discard := func() {
		_ = os.RemoveAll(overlayDir)
	}

	if err := writeOverlayMarker(overlayDir, source); err != nil {
		discard()
		return nil, nil, err
	}
	if err := mirrorClaudeConfigDir(source.ConfigDir, overlayDir); err != nil {
		discard()
		return nil, nil, err
	}
	state, err := copySanitized(source.StateFile, filepath.Join(overlayDir, filepath.Base(source.StateFile)), strippedStateKeys, map[string]any{"hasCompletedOnboarding": true})
	if err != nil {
		discard()
		return nil, nil, err
	}
	credentials, err := copySanitized(filepath.Join(source.ConfigDir, credentialsFile), filepath.Join(overlayDir, credentialsFile), strippedCredentialKeys, nil)
	if err != nil {
		discard()
		return nil, nil, err
	}
	if err := writePatchedClaudeSettings(source.ConfigDir, overlayDir, sessionModel, envMap, managedEnvKeys(target, envMap)); err != nil {
		discard()
		return nil, nil, err
	}

	var once sync.Once
	cleanup := func() {
		once.Do(func() {
			state.writeBack()
			credentials.writeBack()
			discard()
		})
	}
	envMap["CLAUDE_CONFIG_DIR"] = overlayDir
	return flattenEnv(envMap), cleanup, nil
}

// applyModelOverride applies a `--model` argument and returns the model the
// session starts on.
func applyModelOverride(target profiles.Target, override string, envMap map[string]string) string {
	if override == "" {
		return effectiveSessionModel(target, envMap)
	}
	if tier, isAlias := modelAliases[strings.ToLower(override)]; isAlias {
		// Claude Code resolves the alias through the tier variables BuildEnv
		// posted; only the model-scoped env follows the resolved model.
		model := target.Model
		if tier != "" {
			model = profiles.EffectiveTiers(target)[tier]
		}
		if model == "" {
			return effectiveSessionModel(target, envMap)
		}
		swapModelEnv(target, envMap, model)
		return model
	}
	envMap["ANTHROPIC_MODEL"] = override
	for _, key := range tierEnvVars {
		envMap[key] = override
	}
	swapModelEnv(target, envMap, override)
	return override
}

// swapModelEnv replaces the default model's scoped env (its context window)
// with the one of the model actually used. A model the catalog does not
// describe gets none, which leaves Claude Code to its own assumption: 200K,
// or 1M when the ID carries a [1m] suffix.
func swapModelEnv(target profiles.Target, envMap map[string]string, model string) {
	if model == target.Model {
		return
	}
	for key := range target.ModelEnv[target.Model] {
		delete(envMap, key)
	}
	for key, value := range target.ModelEnv[model] {
		envMap[key] = value
	}
}

func envSliceToMap(env []string) map[string]string {
	out := make(map[string]string, len(env))
	for _, pair := range env {
		key, value, ok := splitEnv(pair)
		if ok {
			out[key] = value
		}
	}
	return out
}

func effectiveSessionModel(target profiles.Target, envMap map[string]string) string {
	for _, key := range []string{
		"ANTHROPIC_MODEL",
		"ANTHROPIC_DEFAULT_OPUS_MODEL",
		"ANTHROPIC_DEFAULT_SONNET_MODEL",
		"ANTHROPIC_DEFAULT_HAIKU_MODEL",
		"ANTHROPIC_DEFAULT_FABLE_MODEL",
		"ANTHROPIC_SMALL_FAST_MODEL",
		"CLAUDE_CODE_SUBAGENT_MODEL",
	} {
		if model := strings.TrimSpace(envMap[key]); model != "" {
			return model
		}
	}
	if model := strings.TrimSpace(target.Model); model != "" {
		return model
	}
	for _, key := range []string{providers.TierOpus, providers.TierSonnet, providers.TierHaiku, providers.TierFable, providers.TierSmall} {
		if model := strings.TrimSpace(target.ModelTiers[key]); model != "" {
			return model
		}
	}
	return ""
}

// resolveClaudeSource finds the config dir and state file Claude Code would
// use with this CLAUDE_CONFIG_DIR: the state file lives in CLAUDE_CONFIG_DIR
// when it is set and next to ~/.claude otherwise, and a legacy .config.json in
// the config dir wins over both.
func resolveClaudeSource(configDirEnv string) claudeSource {
	if configDirEnv != "" {
		if parent, ok := readOverlayMarker(configDirEnv); ok {
			return parent
		}
	}
	configDir, stateDir := configDirEnv, configDirEnv
	if configDir == "" {
		home := userHomeDir()
		configDir = filepath.Join(home, ".claude")
		stateDir = home
	}
	source := claudeSource{ConfigDir: configDir, StateFile: filepath.Join(stateDir, ".claude.json")}
	legacy := filepath.Join(configDir, ".config.json")
	if info, err := os.Stat(legacy); err == nil && !info.IsDir() {
		source.StateFile = legacy
	}
	return source
}

func writeOverlayMarker(overlayDir string, source claudeSource) error {
	encoded, err := json.Marshal(source)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(overlayDir, overlayMarker), encoded, 0o600)
}

func readOverlayMarker(dir string) (claudeSource, bool) {
	data, err := os.ReadFile(filepath.Join(dir, overlayMarker))
	if err != nil {
		return claudeSource{}, false
	}
	var source claudeSource
	if err := json.Unmarshal(data, &source); err != nil || source.ConfigDir == "" || source.StateFile == "" {
		return claudeSource{}, false
	}
	return source, true
}

func mirrorClaudeConfigDir(sourceDir, overlayDir string) error {
	entries, err := os.ReadDir(sourceDir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if skippedConfigEntries[entry.Name()] {
			continue
		}
		src := filepath.Join(sourceDir, entry.Name())
		dst := filepath.Join(overlayDir, entry.Name())
		if err := os.Symlink(src, dst); err != nil {
			return fmt.Errorf("symlink %s: %w", entry.Name(), err)
		}
	}
	return nil
}

// fileSync carries what the session changes in its copy of a state or
// credential file (projects, onboarding flags, MCP tokens...) back to the
// user's file when it ends.
type fileSync struct {
	realPath    string
	overlayPath string
	stripped    map[string]bool
	baseline    map[string]any
	enabled     bool
}

// copySanitized writes a JSON file into the overlay without its stripped
// keys. It is a copy rather than a symlink so that nothing the session writes
// can drop the stripped keys from, or add them back to, the user's own file.
// A missing file is seeded with seed, or not created at all when seed is nil;
// either way a file the session creates is carried back.
func copySanitized(realPath, overlayPath string, stripped map[string]bool, seed map[string]any) (*fileSync, error) {
	copied := &fileSync{realPath: realPath, overlayPath: overlayPath, stripped: stripped, baseline: map[string]any{}}
	data, err := os.ReadFile(realPath)
	switch {
	case errors.Is(err, os.ErrNotExist):
		copied.enabled = true
		if seed == nil {
			return copied, nil
		}
		// Without a state file Claude Code replays its onboarding in every
		// throwaway overlay; the write-back creates the real file.
		for key, value := range seed {
			copied.baseline[key] = value
		}
	case err != nil:
		return nil, err
	default:
		// An unreadable file is left alone: the session starts from an empty
		// copy and nothing is written back over the user's file.
		if decoded, decodeErr := decodeState(data); decodeErr == nil {
			copied.baseline = decoded
			copied.enabled = true
		}
	}
	for key := range stripped {
		delete(copied.baseline, key)
	}
	encoded, err := json.MarshalIndent(copied.baseline, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(overlayPath, append(encoded, '\n'), 0o600); err != nil {
		return nil, err
	}
	return copied, nil
}

func (s *fileSync) writeBack() {
	if s == nil || !s.enabled {
		return
	}
	data, err := os.ReadFile(s.overlayPath)
	if err != nil {
		return
	}
	session, err := decodeState(data)
	if err != nil || reflect.DeepEqual(session, s.baseline) {
		return
	}
	path := s.realPath
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	current := map[string]any{}
	mode := os.FileMode(0o600)
	if data, err := os.ReadFile(path); err == nil {
		if current, err = decodeState(data); err != nil {
			return
		}
		if info, err := os.Stat(path); err == nil {
			mode = info.Mode().Perm()
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return
	}
	encoded, err := json.MarshalIndent(mergeState(s.baseline, session, current, s.stripped), "", "  ")
	if err != nil {
		return
	}
	_ = config.WriteFileAtomic(path, append(encoded, '\n'), mode)
}

// mergeState applies what the session changed (base to session) onto the file
// as it is now (current), so that writes made meanwhile by other Claude Code
// sessions survive. Objects merge key by key; any other value the session
// changed replaces the current one. The top-level stripped keys always keep
// the current file's value.
func mergeState(base, session, current map[string]any, stripped map[string]bool) map[string]any {
	out := make(map[string]any, len(current))
	for key, value := range current {
		out[key] = value
	}
	keys := map[string]struct{}{}
	for key := range base {
		keys[key] = struct{}{}
	}
	for key := range session {
		keys[key] = struct{}{}
	}
	for key := range keys {
		if stripped[key] {
			continue
		}
		before, hadBefore := base[key]
		after, hasAfter := session[key]
		if hadBefore && hasAfter && reflect.DeepEqual(before, after) {
			continue
		}
		if !hasAfter {
			delete(out, key)
			continue
		}
		beforeMap, beforeIsMap := before.(map[string]any)
		afterMap, afterIsMap := after.(map[string]any)
		currentMap, currentIsMap := out[key].(map[string]any)
		if beforeIsMap && afterIsMap && currentIsMap {
			out[key] = mergeState(beforeMap, afterMap, currentMap, nil)
			continue
		}
		out[key] = after
	}
	return out
}

// decodeState keeps numbers as json.Number so that large integers in the
// state file survive the round trip unchanged.
func decodeState(data []byte) (map[string]any, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var state map[string]any
	if err := decoder.Decode(&state); err != nil {
		return nil, err
	}
	if state == nil {
		return nil, errors.New("state file is not a JSON object")
	}
	return state, nil
}

// managedEnvKeys lists every settings.json env key the launcher owns: the
// user's value is replaced by the launcher's, or removed when the launcher
// posts none.
func managedEnvKeys(target profiles.Target, envMap map[string]string) []string {
	seen := map[string]bool{}
	var keys []string
	add := func(key string) {
		if key != "" && !seen[key] {
			seen[key] = true
			keys = append(keys, key)
		}
	}
	for key := range envMap {
		if strings.HasPrefix(key, "ANTHROPIC_") {
			add(key)
		}
	}
	for _, key := range tierEnvVars {
		add(key)
	}
	for _, key := range inheritedRoutingKeys {
		add(key)
	}
	for key := range target.ExtraEnv {
		add(key)
	}
	for _, env := range target.ModelEnv {
		for key := range env {
			add(key)
		}
	}
	return keys
}

func writePatchedClaudeSettings(sourceDir, overlayDir, sessionModel string, envMap map[string]string, managed []string) error {
	settings := map[string]any{}
	sourceSettings := filepath.Join(sourceDir, "settings.json")
	data, err := os.ReadFile(sourceSettings)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if len(bytes.TrimSpace(data)) > 0 {
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.UseNumber()
		if err := decoder.Decode(&settings); err != nil {
			return fmt.Errorf("decode %s: %w", sourceSettings, err)
		}
		if settings == nil {
			settings = map[string]any{}
		}
	}

	if sessionModel != "" {
		settings["model"] = sessionModel
	} else {
		// A Claude model pinned in the user's settings would be sent to the
		// third party.
		delete(settings, "model")
	}
	for _, key := range credentialSettingsKeys {
		delete(settings, key)
	}

	settingsEnv := map[string]any{}
	if existing, ok := settings["env"].(map[string]any); ok {
		for key, value := range existing {
			if !strings.HasPrefix(key, "ANTHROPIC_") {
				settingsEnv[key] = value
			}
		}
	}
	for _, key := range managed {
		delete(settingsEnv, key)
		if value, ok := envMap[key]; ok {
			settingsEnv[key] = value
		}
	}
	// The credential reaches the child through its environment only: written
	// here it would outlive the session in $TMPDIR after a hard kill.
	delete(settingsEnv, providers.AuthTokenEnvVar)
	delete(settingsEnv, providers.APIKeyEnvVar)
	settings["env"] = settingsEnv

	encoded, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(overlayDir, "settings.json"), append(encoded, '\n'), 0o600)
}
