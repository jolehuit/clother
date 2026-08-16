package runtime

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jolehuit/clother/internal/profiles"
	"github.com/jolehuit/clother/internal/providers"
)

const testAuthToken = "not-a-real-token-0000"

func newOverlayTarget() profiles.Target {
	return profiles.Target{
		Family: providers.FamilyAnthropicCompatibleNonClaude,
		Model:  "glm-5.2",
		ModelTiers: map[string]string{
			"opus":   "glm-5.2",
			"sonnet": "glm-5.2",
			"haiku":  "glm-5.2-air",
		},
	}
}

func overlaySandbox(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	// Keep the overlay (and the orphan sweep) inside the sandbox.
	t.Setenv("TMPDIR", t.TempDir())
	return home
}

func readJSONFile(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return decoded
}

// TestOverlayKeepsWhatClaudeWroteDuringTheSession is the write-back
// regression: everything Claude Code created in the throwaway config dir used
// to be deleted with it, including the settings the user believed they had
// saved.
func TestOverlayKeepsWhatClaudeWroteDuringTheSession(t *testing.T) {
	home := overlaySandbox(t)
	claudeDir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(filepath.Join(claudeDir, "projects"), 0o755); err != nil {
		t.Fatal(err)
	}
	sourceSettings := `{
  "apiKeyHelper": "/usr/bin/security find-generic-password -s anthropic -w",
  "permissions": {"allow": ["Bash(ls:*)"]}
}
`
	if err := os.WriteFile(filepath.Join(claudeDir, "settings.json"), []byte(sourceSettings), 0o644); err != nil {
		t.Fatal(err)
	}
	realCredentials := `{"claudeAiOauth":{"accessToken":"subscription-credential"}}` + "\n"
	credentialsPath := filepath.Join(claudeDir, ".credentials.json")
	if err := os.WriteFile(credentialsPath, []byte(realCredentials), 0o600); err != nil {
		t.Fatal(err)
	}

	env, cleanup, err := PrepareClaudeConfigOverlay(newOverlayTarget(), nil, []string{
		"PATH=/usr/bin",
		"ANTHROPIC_BASE_URL=https://api.z.ai/api/anthropic",
		"ANTHROPIC_AUTH_TOKEN=" + testAuthToken,
		"ANTHROPIC_MODEL=glm-5.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	overlayDir := envSliceToMap(env)["CLAUDE_CONFIG_DIR"]
	if overlayDir == "" {
		t.Fatal("expected CLAUDE_CONFIG_DIR override")
	}

	// The credential store must not be reachable from the overlay at all.
	if _, err := os.Lstat(filepath.Join(overlayDir, ".credentials.json")); !os.IsNotExist(err) {
		t.Fatalf("the overlay must not expose .credentials.json, stat err=%v", err)
	}
	// The keychain helper must not follow the session to a third-party endpoint.
	if _, ok := readJSONFile(t, filepath.Join(overlayDir, "settings.json"))["apiKeyHelper"]; ok {
		t.Fatal("apiKeyHelper must be stripped from the overlay settings")
	}

	// --- what claude does during the session ---
	if err := os.MkdirAll(filepath.Join(overlayDir, "todos"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(overlayDir, "todos", "t.json"), []byte(`[{"id":1}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(overlayDir, "history.jsonl"), []byte("{\"display\":\"hi\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A token refresh performed against the third-party provider.
	if err := os.WriteFile(filepath.Join(overlayDir, ".credentials.json"), []byte(`{"thirdParty":"refreshed"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	// A write inside an already existing (symlinked) subdirectory.
	if err := os.WriteFile(filepath.Join(overlayDir, "projects", "x.txt"), []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}
	// "Always allow" recorded through /config, plus a new top-level setting.
	sessionSettings := readJSONFile(t, filepath.Join(overlayDir, "settings.json"))
	sessionSettings["permissions"] = map[string]any{"allow": []any{"Bash(ls:*)", "Bash(rm:*)"}}
	sessionSettings["statusLine"] = map[string]any{"type": "command", "command": "echo hi"}
	encoded, err := json.MarshalIndent(sessionSettings, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(overlayDir, "settings.json"), encoded, 0o600); err != nil {
		t.Fatal(err)
	}

	cleanup()

	if _, err := os.Stat(overlayDir); !os.IsNotExist(err) {
		t.Fatalf("cleanup should remove the overlay, stat err=%v", err)
	}
	for _, rel := range []string{
		filepath.Join("todos", "t.json"),
		"history.jsonl",
		filepath.Join("projects", "x.txt"),
	} {
		if _, err := os.Stat(filepath.Join(claudeDir, rel)); err != nil {
			t.Fatalf("%s written during the session was lost: %v", rel, err)
		}
	}

	credentials, err := os.ReadFile(credentialsPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(credentials) != realCredentials {
		t.Fatalf("the subscription credential was overwritten: %s", credentials)
	}

	saved := readJSONFile(t, filepath.Join(claudeDir, "settings.json"))
	permissions, ok := saved["permissions"].(map[string]any)
	if !ok {
		t.Fatalf("permissions lost: %+v", saved)
	}
	allow, _ := permissions["allow"].([]any)
	if len(allow) != 2 || allow[1] != "Bash(rm:*)" {
		t.Fatalf("the permission added during the session was lost: %+v", allow)
	}
	if _, ok := saved["statusLine"]; !ok {
		t.Fatalf("statusLine set during the session was lost: %+v", saved)
	}
	if saved["apiKeyHelper"] != "/usr/bin/security find-generic-password -s anthropic -w" {
		t.Fatalf("the user's own apiKeyHelper must be preserved: %+v", saved["apiKeyHelper"])
	}
	if _, ok := saved["model"]; ok {
		t.Fatalf("the model pinned by clother must not leak into the user's settings: %+v", saved)
	}
	if data, err := os.ReadFile(filepath.Join(claudeDir, "settings.json")); err != nil {
		t.Fatal(err)
	} else if strings.Contains(string(data), testAuthToken) {
		t.Fatal("the auth token must never reach the user's settings.json")
	}
}

// TestOverlayNeverWritesTheAuthTokenToDisk locks C3/C8: the token travels
// through the child process environment, never through $TMPDIR.
func TestOverlayNeverWritesTheAuthTokenToDisk(t *testing.T) {
	home := overlaySandbox(t)
	claudeDir := filepath.Join(home, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	source := `{"env":{"KEEP_ME":"1","MY_CORP_TOKEN":"corp-secret-123"}}` + "\n"
	if err := os.WriteFile(filepath.Join(claudeDir, "settings.json"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}

	env, cleanup, err := PrepareClaudeConfigOverlay(newOverlayTarget(), nil, []string{
		"PATH=/usr/bin",
		"ANTHROPIC_BASE_URL=https://api.z.ai/api/anthropic",
		"ANTHROPIC_AUTH_TOKEN=" + testAuthToken,
		"ANTHROPIC_MODEL=glm-5.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	overlayDir := envSliceToMap(env)["CLAUDE_CONFIG_DIR"]

	settingsPath := filepath.Join(overlayDir, "settings.json")
	info, err := os.Stat(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("overlay settings.json mode = %v, want 0600", perm)
	}
	err = filepath.WalkDir(overlayDir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		if strings.Contains(string(data), testAuthToken) {
			t.Errorf("%s contains the auth token in clear text", path)
		}
		if strings.Contains(string(data), "corp-secret-123") {
			t.Errorf("%s carries a secret taken from the user's settings env", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	settingsEnv, ok := readJSONFile(t, settingsPath)["env"].(map[string]any)
	if !ok {
		t.Fatal("expected an env object")
	}
	if settingsEnv["KEEP_ME"] != "1" {
		t.Fatalf("non-secret settings env keys must be kept: %+v", settingsEnv)
	}
	if _, ok := settingsEnv["ANTHROPIC_BASE_URL"]; ok {
		t.Fatalf("only model keys belong in the overlay env: %+v", settingsEnv)
	}
	if settingsEnv["ANTHROPIC_MODEL"] != "glm-5.2" {
		t.Fatalf("the model must still be pinned: %+v", settingsEnv)
	}
	// The token still has to reach claude through its environment.
	if envSliceToMap(env)["ANTHROPIC_AUTH_TOKEN"] != testAuthToken {
		t.Fatal("the auth token must stay in the process environment")
	}
}

// TestOverlayResolvesModelAliases locks C6: `--model opus` is a tier alias, not
// a model id.
func TestOverlayResolvesModelAliases(t *testing.T) {
	home := overlaySandbox(t)
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}

	env, cleanup, err := PrepareClaudeConfigOverlay(newOverlayTarget(), []string{"--model", "opus"}, []string{
		"PATH=/usr/bin",
		"ANTHROPIC_BASE_URL=https://api.z.ai/api/anthropic",
		"ANTHROPIC_MODEL=glm-5.2",
		"ANTHROPIC_DEFAULT_OPUS_MODEL=glm-5.2",
		"ANTHROPIC_DEFAULT_HAIKU_MODEL=glm-5.2-air",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	envMap := envSliceToMap(env)
	for key, want := range map[string]string{
		"ANTHROPIC_MODEL":               "glm-5.2",
		"ANTHROPIC_DEFAULT_OPUS_MODEL":  "glm-5.2",
		"ANTHROPIC_DEFAULT_HAIKU_MODEL": "glm-5.2-air",
	} {
		if envMap[key] != want {
			t.Errorf("%s = %q, want %q (an alias must not be pinned literally)", key, envMap[key], want)
		}
	}
	settings := readJSONFile(t, filepath.Join(envMap["CLAUDE_CONFIG_DIR"], "settings.json"))
	if settings["model"] != "glm-5.2" {
		t.Fatalf("settings model = %v, want the resolved tier glm-5.2", settings["model"])
	}

	// A concrete id keeps pinning every tier.
	env, cleanup2, err := PrepareClaudeConfigOverlay(newOverlayTarget(), []string{"--model=glm-4.7"}, []string{
		"PATH=/usr/bin",
		"ANTHROPIC_BASE_URL=https://api.z.ai/api/anthropic",
		"ANTHROPIC_MODEL=glm-5.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup2()
	if got := envSliceToMap(env)["ANTHROPIC_DEFAULT_OPUS_MODEL"]; got != "glm-4.7" {
		t.Fatalf("explicit model id should pin the tiers, got %q", got)
	}
}

// TestOverlayMirrorsStateFileFromConfigDir locks C5: with CLAUDE_CONFIG_DIR
// set, the state file lives inside that directory.
func TestOverlayMirrorsStateFileFromConfigDir(t *testing.T) {
	home := overlaySandbox(t)
	configDir := filepath.Join(home, ".config", "claude")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(configDir, ".claude.json")
	if err := os.WriteFile(statePath, []byte(`{"mcpServers":{"x":{}}}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	env, cleanup, err := PrepareClaudeConfigOverlay(newOverlayTarget(), nil, []string{
		"PATH=/usr/bin",
		"CLAUDE_CONFIG_DIR=" + configDir,
		"ANTHROPIC_BASE_URL=https://api.z.ai/api/anthropic",
		"ANTHROPIC_MODEL=glm-5.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	overlayState := filepath.Join(envSliceToMap(env)["CLAUDE_CONFIG_DIR"], ".claude.json")
	resolved, err := filepath.EvalSymlinks(overlayState)
	if err != nil {
		t.Fatalf("overlay .claude.json missing, claude would start on a virgin config: %v", err)
	}
	wanted, err := filepath.EvalSymlinks(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if resolved != wanted {
		t.Fatalf("overlay state resolves to %q, want %q", resolved, wanted)
	}
}

// TestOverlaySeedsOnboardingOnAFreshMachine covers the case where nothing
// exists yet: without a state file claude replays its onboarding wizard inside
// a directory that is deleted right after.
func TestOverlaySeedsOnboardingOnAFreshMachine(t *testing.T) {
	home := overlaySandbox(t)

	env, cleanup, err := PrepareClaudeConfigOverlay(newOverlayTarget(), nil, []string{
		"PATH=/usr/bin",
		"ANTHROPIC_BASE_URL=https://api.z.ai/api/anthropic",
		"ANTHROPIC_MODEL=glm-5.2",
	})
	if err != nil {
		t.Fatal(err)
	}
	overlayDir := envSliceToMap(env)["CLAUDE_CONFIG_DIR"]
	state := readJSONFile(t, filepath.Join(overlayDir, ".claude.json"))
	if state["hasCompletedOnboarding"] != true {
		t.Fatalf("first launch should skip onboarding: %+v", state)
	}

	cleanup()
	promoted := readJSONFile(t, filepath.Join(home, ".claude", ".claude.json"))
	if promoted["hasCompletedOnboarding"] != true {
		t.Fatalf("the seeded state file should be promoted to the config dir: %+v", promoted)
	}
}

func TestPurgeStaleOverlays(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)

	makeOverlay := func(name string, age time.Duration, owner string) string {
		dir := filepath.Join(tmp, name)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
		if owner != "" {
			if err := os.WriteFile(filepath.Join(dir, overlayOwnerFile), []byte(owner), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		stamp := time.Now().Add(-age)
		if err := os.Chtimes(dir, stamp, stamp); err != nil {
			t.Fatal(err)
		}
		return dir
	}

	orphan := makeOverlay(overlayPrefix+"orphan", 48*time.Hour, strconv.Itoa(999999))
	fresh := makeOverlay(overlayPrefix+"fresh", time.Minute, strconv.Itoa(os.Getpid()))
	live := makeOverlay(overlayPrefix+"live", 48*time.Hour, strconv.Itoa(os.Getpid()))
	unrelated := filepath.Join(tmp, "something-else")
	if err := os.MkdirAll(unrelated, 0o700); err != nil {
		t.Fatal(err)
	}

	purgeStaleOverlays(time.Now())

	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatalf("a day-old overlay whose owner is gone must be purged, stat err=%v", err)
	}
	for _, dir := range []string{fresh, live, unrelated} {
		if _, err := os.Stat(dir); err != nil {
			t.Fatalf("%s should have been left alone: %v", dir, err)
		}
	}
}
