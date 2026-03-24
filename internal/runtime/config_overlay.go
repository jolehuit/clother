package runtime

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jolehuit/clother/internal/profiles"
	"github.com/jolehuit/clother/internal/providers"
)

func PrepareClaudeConfigOverlay(target profiles.Target, args []string, env []string) ([]string, func(), error) {
	if target.Family == providers.FamilyClaudeStrict {
		return env, func() {}, nil
	}

	envMap := envSliceToMap(env)
	overrideModel := ModelOverride(args)
	if overrideModel != "" {
		envMap["ANTHROPIC_MODEL"] = overrideModel
		for _, key := range []string{
			"ANTHROPIC_DEFAULT_HAIKU_MODEL",
			"ANTHROPIC_DEFAULT_SONNET_MODEL",
			"ANTHROPIC_DEFAULT_OPUS_MODEL",
			"ANTHROPIC_SMALL_FAST_MODEL",
			"CLAUDE_CODE_SUBAGENT_MODEL",
		} {
			envMap[key] = overrideModel
		}
	}

	claudeEnv := anthropicEnv(envMap)
	if len(claudeEnv) == 0 {
		return flattenEnv(envMap), func() {}, nil
	}

	sessionModel := effectiveSessionModel(target, claudeEnv)
	if sessionModel == "" {
		return flattenEnv(envMap), func() {}, nil
	}

	sourceDir := envMap["CLAUDE_CONFIG_DIR"]
	if sourceDir == "" {
		sourceDir = filepath.Join(userHomeDir(), ".claude")
	}

	overlayDir, err := os.MkdirTemp("", "clother-claude-config-*")
	if err != nil {
		return nil, nil, err
	}
	cleanup := func() {
		_ = os.RemoveAll(overlayDir)
	}

	if err := mirrorClaudeConfigDir(sourceDir, overlayDir); err != nil {
		cleanup()
		return nil, nil, err
	}
	if err := mirrorClaudeStateFile(sourceDir, overlayDir); err != nil {
		cleanup()
		return nil, nil, err
	}
	if err := writePatchedClaudeSettings(sourceDir, overlayDir, sessionModel, claudeEnv); err != nil {
		cleanup()
		return nil, nil, err
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
		if strings.HasPrefix(key, "ANTHROPIC_") || strings.HasPrefix(key, "CLAUDE_CODE_SUBAGENT_MODEL") {
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
	for _, key := range []string{"opus", "sonnet", "haiku", "small"} {
		if model := strings.TrimSpace(target.ModelTiers[key]); model != "" {
			return model
		}
	}
	return ""
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
		if entry.Name() == "settings.json" {
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

func mirrorClaudeStateFile(sourceDir, overlayDir string) error {
	statePath := filepath.Join(filepath.Dir(sourceDir), ".claude.json")
	if _, err := os.Stat(statePath); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	return os.Symlink(statePath, filepath.Join(overlayDir, ".claude.json"))
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

	settings["model"] = sessionModel
	settingsEnv := map[string]any{}
	if existing, ok := settings["env"].(map[string]any); ok {
		for key, value := range existing {
			settingsEnv[key] = value
		}
		for _, key := range anthropicEnvKeys(settingsEnv) {
			delete(settingsEnv, key)
		}
	}
	for key, value := range envMap {
		settingsEnv[key] = value
	}
	settings["env"] = settingsEnv

	encoded, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	return os.WriteFile(filepath.Join(overlayDir, "settings.json"), encoded, 0o644)
}

func anthropicEnvKeys(env map[string]any) []string {
	keys := make([]string, 0, len(env))
	for key := range env {
		if strings.HasPrefix(key, "ANTHROPIC_") || key == "CLAUDE_CODE_SUBAGENT_MODEL" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}
