package runtime

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jolehuit/clother/internal/config"
	"github.com/jolehuit/clother/internal/profiles"
	"github.com/jolehuit/clother/internal/providers"
)

const (
	overlayPrefix    = "clother-claude-config-"
	overlayOwnerFile = ".clother-owner"
	// An overlay whose owner process is gone and that has not been touched for
	// this long is a leftover from a SIGKILL/crash: it still holds a patched
	// settings.json, so it is purged on the next launch.
	overlayMaxAge = 24 * time.Hour
)

// overlayModelKeys are the only environment variables copied into the overlay
// settings.json. The auth token is deliberately absent: it already reaches the
// child process through its environment (exec.go sets cmd.Env), so writing it
// to disk buys nothing and leaves a credential in $TMPDIR after a hard kill.
var overlayModelKeys = []string{
	"ANTHROPIC_MODEL",
	"ANTHROPIC_DEFAULT_HAIKU_MODEL",
	"ANTHROPIC_DEFAULT_SONNET_MODEL",
	"ANTHROPIC_DEFAULT_OPUS_MODEL",
	// BuildEnv posts the fable tier for every non-strict family; leaving it out
	// here would let `--model <concrete id>` overwrite the five other tiers and
	// keep fable pointed at the provider default.
	"ANTHROPIC_DEFAULT_FABLE_MODEL",
	"ANTHROPIC_SMALL_FAST_MODEL",
	"CLAUDE_CODE_SUBAGENT_MODEL",
}

// credentialSettingsKeys make Claude Code fetch an Anthropic credential
// (keychain helper, AWS SSO refresh, login method). They are stripped from the
// overlay so a session pointed at a third-party endpoint never carries the
// user's Anthropic credential machinery.
var credentialSettingsKeys = []string{"apiKeyHelper", "awsAuthRefresh", "forceLoginMethod"}

// clotherSettingsKeys are written by clother itself and must never be written
// back into the user's own settings.json when the session ends.
var clotherSettingsKeys = map[string]bool{"model": true, "env": true}

// modelAliasTiers maps the documented `--model` aliases to the tier they
// resolve through. Pinning the alias literally (ANTHROPIC_MODEL=opus) both
// sends a non-existent model id to the provider and destroys the tier mapping.
var modelAliasTiers = map[string]string{
	"default":    "",
	"best":       "",
	"fable":      "",
	"opus":       "opus",
	"opusplan":   "opus",
	"opus[1m]":   "opus",
	"sonnet":     "sonnet",
	"sonnet[1m]": "sonnet",
	"haiku":      "haiku",
}

func PrepareClaudeConfigOverlay(target profiles.Target, args []string, env []string) ([]string, func(), error) {
	if target.Family == providers.FamilyClaudeStrict {
		return env, func() {}, nil
	}

	envMap := envSliceToMap(env)
	aliasModel := ""
	if overrideModel := ModelOverride(args); overrideModel != "" {
		if resolved, isAlias := resolveModelAlias(target, overrideModel); isAlias {
			// `--model opus` is a tier alias: Claude Code resolves it through
			// ANTHROPIC_DEFAULT_OPUS_MODEL, so the tier variables are left
			// untouched and only the pinned session model is resolved here.
			aliasModel = resolved
		} else {
			for _, key := range overlayModelKeys {
				envMap[key] = overrideModel
			}
		}
	}

	claudeEnv := anthropicEnv(envMap)
	if len(claudeEnv) == 0 {
		return flattenEnv(envMap), func() {}, nil
	}

	sessionModel := aliasModel
	if sessionModel == "" {
		sessionModel = effectiveSessionModel(target, claudeEnv)
	}
	if sessionModel == "" {
		return flattenEnv(envMap), func() {}, nil
	}

	sourceDir := envMap["CLAUDE_CONFIG_DIR"]
	if sourceDir == "" {
		sourceDir = filepath.Join(userHomeDir(), ".claude")
	}

	purgeStaleOverlays(time.Now())

	overlayDir, err := os.MkdirTemp("", overlayPrefix+"*")
	if err != nil {
		return nil, nil, err
	}
	discard := func() {
		_ = os.RemoveAll(overlayDir)
	}
	markOverlayOwner(overlayDir)

	if err := mirrorClaudeConfigDir(sourceDir, overlayDir); err != nil {
		discard()
		return nil, nil, err
	}
	if err := mirrorClaudeStateFile(sourceDir, overlayDir); err != nil {
		discard()
		return nil, nil, err
	}
	if err := writePatchedClaudeSettings(sourceDir, overlayDir, sessionModel, envMap); err != nil {
		discard()
		return nil, nil, err
	}

	cleanup := func() {
		writeBackOverlay(sourceDir, overlayDir)
		_ = os.RemoveAll(overlayDir)
	}

	envMap["CLAUDE_CONFIG_DIR"] = overlayDir
	return flattenEnv(envMap), cleanup, nil
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

func anthropicEnv(envMap map[string]string) map[string]string {
	out := map[string]string{}
	for key, value := range envMap {
		if isAnthropicSettingKey(key) {
			out[key] = value
		}
	}
	return out
}

func isAnthropicSettingKey(key string) bool {
	return strings.HasPrefix(key, "ANTHROPIC_") || key == "CLAUDE_CODE_SUBAGENT_MODEL"
}

// looksLikeCredentialKey reports whether an env key carried by the user's
// settings.json smells like a secret. Those are dropped from the overlay: the
// session is pointed at a third-party endpoint.
func looksLikeCredentialKey(key string) bool {
	upper := strings.ToUpper(key)
	for _, suffix := range []string{"_API_KEY", "_TOKEN", "_SECRET", "_PASSWORD", "_CREDENTIAL", "_CREDENTIALS"} {
		if strings.HasSuffix(upper, suffix) {
			return true
		}
	}
	switch upper {
	case "API_KEY", "TOKEN", "SECRET", "PASSWORD":
		return true
	}
	return false
}

func resolveModelAlias(target profiles.Target, value string) (string, bool) {
	tier, ok := modelAliasTiers[strings.ToLower(strings.TrimSpace(value))]
	if !ok {
		return "", false
	}
	if tier != "" {
		if model := strings.TrimSpace(target.ModelTiers[tier]); model != "" {
			return model, true
		}
	}
	return strings.TrimSpace(target.Model), true
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
	for _, key := range []string{"opus", "sonnet", "haiku", "fable", "small"} {
		if model := strings.TrimSpace(target.ModelTiers[key]); model != "" {
			return model
		}
	}
	return ""
}

// skipMirror lists the entries never symlinked into the overlay.
//
//   - settings.json is rewritten as a patched copy;
//   - .claude.json is the state file, handled by mirrorClaudeStateFile;
//   - .credentials.json is the OAuth store: CLAUDE_CONFIG_DIR relocates it on
//     Linux and Windows, so a token refresh performed against a third-party
//     provider would overwrite the user's Anthropic subscription credential.
func skipMirror(name string) bool {
	switch name {
	case "settings.json", ".claude.json", ".credentials.json", overlayOwnerFile:
		return true
	}
	return false
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
		if skipMirror(entry.Name()) {
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

// mirrorClaudeStateFile links the .claude.json Claude Code will read. With
// CLAUDE_CONFIG_DIR set, the state file lives inside the config dir, so that
// copy wins; the home-level one is the fallback for the default ~/.claude
// layout.
func mirrorClaudeStateFile(sourceDir, overlayDir string) error {
	for _, statePath := range []string{
		filepath.Join(sourceDir, ".claude.json"),
		filepath.Join(filepath.Dir(sourceDir), ".claude.json"),
	} {
		info, err := os.Stat(statePath)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if info.IsDir() {
			continue
		}
		return os.Symlink(statePath, filepath.Join(overlayDir, ".claude.json"))
	}
	// No state file at all: without one, Claude Code replays its onboarding
	// wizard inside the throwaway overlay on every single launch. Seed it; the
	// write-back promotes the file to sourceDir when the session ends.
	encoded, err := json.MarshalIndent(map[string]any{"hasCompletedOnboarding": true}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(overlayDir, ".claude.json"), append(encoded, '\n'), 0o600)
}

func writePatchedClaudeSettings(sourceDir, overlayDir, sessionModel string, envMap map[string]string) error {
	settings := map[string]any{}
	sourceSettings := filepath.Join(sourceDir, "settings.json")
	data, err := os.ReadFile(sourceSettings)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &settings); err != nil {
			return fmt.Errorf("decode %s: %w", sourceSettings, err)
		}
	}

	for _, key := range credentialSettingsKeys {
		delete(settings, key)
	}

	settings["model"] = sessionModel
	settingsEnv := map[string]any{}
	if existing, ok := settings["env"].(map[string]any); ok {
		for key, value := range existing {
			if isAnthropicSettingKey(key) || looksLikeCredentialKey(key) {
				continue
			}
			settingsEnv[key] = value
		}
	}
	for _, key := range overlayModelKeys {
		if value := strings.TrimSpace(envMap[key]); value != "" {
			settingsEnv[key] = value
		}
	}
	settings["env"] = settingsEnv

	encoded, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	return os.WriteFile(filepath.Join(overlayDir, "settings.json"), encoded, 0o600)
}

// writeBackOverlay promotes everything Claude Code created inside the throwaway
// overlay back into the real config dir. Symlinked entries wrote through to
// sourceDir already and are left alone; anything else was created during the
// session and would otherwise die with the temp directory.
func writeBackOverlay(sourceDir, overlayDir string) {
	entries, err := os.ReadDir(overlayDir)
	if err != nil {
		return
	}
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		return
	}
	for _, entry := range entries {
		name := entry.Name()
		src := filepath.Join(overlayDir, name)
		info, err := os.Lstat(src)
		if err != nil {
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		switch name {
		case overlayOwnerFile:
			continue
		case "settings.json":
			mergeBackSettings(sourceDir, src)
			continue
		case ".credentials.json":
			// Written by claude against a third-party provider: it must never
			// land on the user's Anthropic credential store.
			continue
		}
		dst := filepath.Join(sourceDir, name)
		if _, err := os.Lstat(dst); err == nil {
			// The source already owns this name (it was skipped by the mirror
			// on purpose): never clobber it.
			continue
		}
		if err := movePath(src, dst); err != nil {
			fmt.Fprintf(os.Stderr, "clother: could not keep %q created during the session: %v\n", name, err)
		}
	}
}

// mergeBackSettings replays, key by key, whatever claude changed in the overlay
// settings.json onto the user's own settings.json, skipping the keys clother
// itself wrote.
//
// The whole read-merge-write runs under an exclusive lock on the target file.
// The rename at the end is atomic, but the cycle around it was not: several
// sessions ending at the same time all read the same starting version and the
// last one to rename dropped the settings the others had just brought back.
// Measured at 6 to 8 keys surviving out of 8 with eight concurrent sessions —
// and those are theme, permissions and hooks that claude wrote during the
// session, disappearing without a word.
func mergeBackSettings(sourceDir, overlayPath string) {
	sourcePath := filepath.Join(sourceDir, "settings.json")
	err := config.WithFileLock(sourcePath, func() error {
		mergeBackSettingsLocked(sourcePath, overlayPath)
		return nil
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "clother: could not save the settings changed during the session: %v\n", err)
	}
}

func mergeBackSettingsLocked(sourcePath, overlayPath string) {
	overlayData, err := os.ReadFile(overlayPath)
	if err != nil {
		return
	}
	var overlaySettings map[string]any
	if err := json.Unmarshal(overlayData, &overlaySettings); err != nil {
		return
	}

	// Re-read inside the lock: the copy this process loaded before taking it
	// would already be stale.
	sourceSettings := map[string]any{}
	mode := os.FileMode(0o600)
	data, err := os.ReadFile(sourcePath)
	switch {
	case err == nil:
		if len(data) > 0 {
			if err := json.Unmarshal(data, &sourceSettings); err != nil {
				fmt.Fprintf(os.Stderr, "clother: %s is not valid JSON, keeping it untouched: %v\n", sourcePath, err)
				return
			}
		}
		if info, statErr := os.Stat(sourcePath); statErr == nil {
			mode = info.Mode().Perm()
		}
	case os.IsNotExist(err):
	default:
		return
	}

	changed := false
	for key, value := range overlaySettings {
		if clotherSettingsKeys[key] {
			continue
		}
		if existing, ok := sourceSettings[key]; ok && jsonEqual(existing, value) {
			continue
		}
		sourceSettings[key] = value
		changed = true
	}
	if overlayEnv, ok := overlaySettings["env"].(map[string]any); ok {
		sourceEnv := map[string]any{}
		if existing, ok := sourceSettings["env"].(map[string]any); ok {
			for key, value := range existing {
				sourceEnv[key] = value
			}
		}
		envChanged := false
		for key, value := range overlayEnv {
			if isOverlayModelKey(key) {
				continue
			}
			if existing, ok := sourceEnv[key]; ok && jsonEqual(existing, value) {
				continue
			}
			sourceEnv[key] = value
			envChanged = true
		}
		if envChanged {
			sourceSettings["env"] = sourceEnv
			changed = true
		}
	}
	if !changed {
		return
	}

	encoded, err := json.MarshalIndent(sourceSettings, "", "  ")
	if err != nil {
		return
	}
	if err := config.WriteFileAtomic(sourcePath, append(encoded, '\n'), mode); err != nil {
		fmt.Fprintf(os.Stderr, "clother: could not save the settings changed during the session: %v\n", err)
	}
}

func isOverlayModelKey(key string) bool {
	for _, candidate := range overlayModelKeys {
		if candidate == key {
			return true
		}
	}
	return false
}

func jsonEqual(left, right any) bool {
	leftData, err := json.Marshal(left)
	if err != nil {
		return false
	}
	rightData, err := json.Marshal(right)
	if err != nil {
		return false
	}
	return string(leftData) == string(rightData)
}

func movePath(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	// $TMPDIR and the config dir often live on different filesystems.
	if err := copyPath(src, dst); err != nil {
		return err
	}
	return os.RemoveAll(src)
}

func copyPath(src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		target, err := os.Readlink(src)
		if err != nil {
			return err
		}
		return os.Symlink(target, dst)
	case info.IsDir():
		if err := os.MkdirAll(dst, info.Mode().Perm()); err != nil {
			return err
		}
		entries, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if err := copyPath(filepath.Join(src, entry.Name()), filepath.Join(dst, entry.Name())); err != nil {
				return err
			}
		}
		return nil
	case info.Mode().IsRegular():
		in, err := os.Open(src)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, in); err != nil {
			out.Close()
			return err
		}
		return out.Close()
	default:
		return fmt.Errorf("unsupported file type %s", info.Mode())
	}
}

func markOverlayOwner(overlayDir string) {
	_ = os.WriteFile(filepath.Join(overlayDir, overlayOwnerFile), []byte(strconv.Itoa(os.Getpid())+"\n"), 0o600)
}

// purgeStaleOverlays removes the overlays left behind by a SIGKILL or a crash:
// the deferred cleanup never runs in those cases and the patched settings.json
// stays in $TMPDIR forever.
func purgeStaleOverlays(now time.Time) {
	matches, err := filepath.Glob(filepath.Join(os.TempDir(), overlayPrefix+"*"))
	if err != nil {
		return
	}
	for _, dir := range matches {
		info, err := os.Lstat(dir)
		if err != nil || !info.IsDir() {
			continue
		}
		if now.Sub(info.ModTime()) <= overlayMaxAge {
			continue
		}
		if owner, ok := overlayOwner(dir); ok && processAlive(owner) {
			continue
		}
		_ = os.RemoveAll(dir)
	}
}

func overlayOwner(dir string) (int, bool) {
	data, err := os.ReadFile(filepath.Join(dir, overlayOwnerFile))
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return 0, false
	}
	return pid, true
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = process.Signal(syscall.Signal(0))
	if err == nil {
		return true
	}
	if errors.Is(err, os.ErrProcessDone) {
		return false
	}
	var errno syscall.Errno
	if errors.As(err, &errno) {
		return errno == syscall.EPERM
	}
	return false
}
