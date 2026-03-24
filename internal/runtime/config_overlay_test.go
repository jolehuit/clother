package runtime

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/jolehuit/clother/internal/profiles"
	"github.com/jolehuit/clother/internal/providers"
)

func TestPrepareClaudeConfigOverlayMirrorsConfigAndPinsModel(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	claudeDir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(filepath.Join(claudeDir, "teams"), 0o755); err != nil {
		t.Fatal(err)
	}
	originalSettings := []byte("{\n  \"model\": \"opus[1m]\",\n  \"env\": {\n    \"KEEP_ME\": \"1\",\n    \"ANTHROPIC_MODEL\": \"claude-opus-4-6\"\n  }\n}\n")
	if err := os.WriteFile(filepath.Join(claudeDir, "settings.json"), originalSettings, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(claudeDir, "teams", "marker.txt"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}

	target := profiles.Target{
		Family: providers.FamilyAnthropicCompatibleNonClaude,
		Model:  "qwen3.5-plus",
	}
	env, cleanup, err := PrepareClaudeConfigOverlay(target, []string{"--model", "glm-5"}, []string{
		"PATH=/usr/bin",
		"ANTHROPIC_BASE_URL=https://coding-intl.dashscope.aliyuncs.com/apps/anthropic",
		"ANTHROPIC_AUTH_TOKEN=secret-token",
		"ANTHROPIC_MODEL=qwen3.5-plus",
		"ANTHROPIC_DEFAULT_OPUS_MODEL=qwen3.5-plus",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)

	overlayDir := envSliceToMap(env)["CLAUDE_CONFIG_DIR"]
	if overlayDir == "" {
		t.Fatal("expected CLAUDE_CONFIG_DIR override")
	}

	settingsData, err := os.ReadFile(filepath.Join(overlayDir, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var settings map[string]any
	if err := json.Unmarshal(settingsData, &settings); err != nil {
		t.Fatal(err)
	}
	if settings["model"] != "glm-5" {
		t.Fatalf("patched model = %v, want glm-5", settings["model"])
	}
	settingsEnv, ok := settings["env"].(map[string]any)
	if !ok {
		t.Fatal("expected env object in patched settings")
	}
	if settingsEnv["KEEP_ME"] != "1" {
		t.Fatalf("patched env lost KEEP_ME: %+v", settingsEnv)
	}
	if settingsEnv["ANTHROPIC_MODEL"] != "glm-5" {
		t.Fatalf("patched ANTHROPIC_MODEL = %v, want glm-5", settingsEnv["ANTHROPIC_MODEL"])
	}
	if settingsEnv["ANTHROPIC_DEFAULT_OPUS_MODEL"] != "glm-5" {
		t.Fatalf("patched ANTHROPIC_DEFAULT_OPUS_MODEL = %v, want glm-5", settingsEnv["ANTHROPIC_DEFAULT_OPUS_MODEL"])
	}
	if settingsEnv["CLAUDE_CODE_SUBAGENT_MODEL"] != "glm-5" {
		t.Fatalf("patched CLAUDE_CODE_SUBAGENT_MODEL = %v, want glm-5", settingsEnv["CLAUDE_CODE_SUBAGENT_MODEL"])
	}
	markerPath := filepath.Join(overlayDir, "teams")
	info, err := os.Lstat(markerPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("expected %s to be a symlink", markerPath)
	}

	cleanup()
	if _, err := os.Stat(overlayDir); !os.IsNotExist(err) {
		t.Fatalf("expected cleanup to remove overlay, stat err=%v", err)
	}
}

func TestPrepareClaudeConfigOverlaySkipsNativeClaude(t *testing.T) {
	env, cleanup, err := PrepareClaudeConfigOverlay(profiles.Target{Family: providers.FamilyClaudeStrict}, nil, []string{"PATH=/usr/bin"})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if got := envSliceToMap(env)["CLAUDE_CONFIG_DIR"]; got != "" {
		t.Fatalf("CLAUDE_CONFIG_DIR = %q, want empty", got)
	}
}
