package runtime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jolehuit/clother/internal/profiles"
	"github.com/jolehuit/clother/internal/providers"
)

func thirdPartyTarget() profiles.Target {
	return profiles.Target{
		Profile: "zai",
		Family:  providers.FamilyAnthropicCompatibleNonClaude,
		Model:   "glm-5.3[1m]",
	}
}

func thirdPartyEnv() []string {
	return []string{
		"PATH=/usr/bin",
		"ANTHROPIC_BASE_URL=https://api.z.ai/api/anthropic",
		"ANTHROPIC_AUTH_TOKEN=provider-token",
		"ANTHROPIC_API_KEY=",
		"ANTHROPIC_MODEL=glm-5.3[1m]",
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func prepareOverlay(t *testing.T, target profiles.Target, args []string, env []string) (string, func()) {
	t.Helper()
	out, cleanup, err := PrepareClaudeConfigOverlay(target, args, env)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	overlayDir := envSliceToMap(out)["CLAUDE_CONFIG_DIR"]
	if overlayDir == "" {
		t.Fatal("expected a CLAUDE_CONFIG_DIR overlay")
	}
	return overlayDir, cleanup
}

// Issue #40: the Console key saved by /login must never be visible to a
// session pointed at a third party, where Claude Code would send it as
// x-api-key.
func TestOverlayStripsAnthropicCredentialsFromState(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	homeState := filepath.Join(home, ".claude.json")
	original := `{"primaryApiKey":"sk-ant-api03-secret","apiKey":"sk-ant-legacy","oauthAccount":{"emailAddress":"me@example.com"},"theme":"dark"}`
	writeFile(t, homeState, original)

	overlayDir, _ := prepareOverlay(t, thirdPartyTarget(), nil, thirdPartyEnv())

	copied, err := os.ReadFile(filepath.Join(overlayDir, ".claude.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, leaked := range []string{"sk-ant-api03-secret", "sk-ant-legacy", "primaryApiKey", "oauthAccount", "me@example.com"} {
		if strings.Contains(string(copied), leaked) {
			t.Fatalf("overlay state still contains %q:\n%s", leaked, copied)
		}
	}
	if state := readJSONFile(t, filepath.Join(overlayDir, ".claude.json")); state["theme"] != "dark" {
		t.Fatalf("overlay state lost unrelated settings: %+v", state)
	}
	if data, _ := os.ReadFile(homeState); string(data) != original {
		t.Fatalf("home state file was modified:\n%s", data)
	}
}

func TestOverlayWriteBackMergesSessionChangesAndKeepsCredentials(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	homeState := filepath.Join(home, ".claude.json")
	writeFile(t, homeState, `{"primaryApiKey":"sk-ant-api03-secret","oauthAccount":{"emailAddress":"me@example.com"},"numStartups":3,"projects":{"/a":{"lastSessionId":"old"}},"userID":"12345678901234567890123"}`)

	overlayDir, cleanup := prepareOverlay(t, thirdPartyTarget(), nil, thirdPartyEnv())

	// The session writes its state, including a key it must not persist...
	writeFile(t, filepath.Join(overlayDir, ".claude.json"), `{"numStartups":4,"projects":{"/a":{"lastSessionId":"new"},"/b":{"lastSessionId":"b1"}},"primaryApiKey":"sk-ant-api03-other","userID":"12345678901234567890123"}`)
	// ...while another Claude Code session updates the real file.
	writeFile(t, homeState, `{"primaryApiKey":"sk-ant-api03-secret","oauthAccount":{"emailAddress":"me@example.com"},"numStartups":3,"projects":{"/a":{"lastSessionId":"old"},"/c":{"lastSessionId":"c1"}},"tipsHistory":{"x":1},"userID":"12345678901234567890123"}`)

	cleanup()

	state := readJSONFile(t, homeState)
	if state["primaryApiKey"] != "sk-ant-api03-secret" {
		t.Fatalf("primaryApiKey = %v, want the user's own key back", state["primaryApiKey"])
	}
	if account, _ := state["oauthAccount"].(map[string]any); account["emailAddress"] != "me@example.com" {
		t.Fatalf("oauthAccount lost: %+v", state["oauthAccount"])
	}
	if state["numStartups"] != float64(4) {
		t.Fatalf("numStartups = %v, want the session's 4", state["numStartups"])
	}
	projects, _ := state["projects"].(map[string]any)
	for path, want := range map[string]string{"/a": "new", "/b": "b1", "/c": "c1"} {
		project, _ := projects[path].(map[string]any)
		if project["lastSessionId"] != want {
			t.Fatalf("projects[%s] = %+v, want lastSessionId %q (projects: %+v)", path, project, want, projects)
		}
	}
	if state["tipsHistory"] == nil {
		t.Fatal("a concurrent write to the real file was lost")
	}
	if data, _ := os.ReadFile(homeState); !strings.Contains(string(data), "12345678901234567890123") {
		t.Fatalf("large number was not preserved verbatim:\n%s", data)
	}
}

func TestOverlayWriteBackLeavesUntouchedStateAlone(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	homeState := filepath.Join(home, ".claude.json")
	original := "{\"theme\":\"dark\",   \"primaryApiKey\":\"sk-ant-api03-secret\"}\n"
	writeFile(t, homeState, original)

	_, cleanup := prepareOverlay(t, thirdPartyTarget(), nil, thirdPartyEnv())
	cleanup()

	if data, _ := os.ReadFile(homeState); string(data) != original {
		t.Fatalf("an unchanged session rewrote the state file:\n%s", data)
	}
}

func TestOverlaySeedsAndCreatesMissingState(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")

	overlayDir, cleanup := prepareOverlay(t, thirdPartyTarget(), nil, thirdPartyEnv())
	if state := readJSONFile(t, filepath.Join(overlayDir, ".claude.json")); state["hasCompletedOnboarding"] != true {
		t.Fatalf("missing state was not seeded: %+v", state)
	}
	writeFile(t, filepath.Join(overlayDir, ".claude.json"), `{"hasCompletedOnboarding":true,"numStartups":1}`)
	cleanup()

	if state := readJSONFile(t, filepath.Join(home, ".claude.json")); state["numStartups"] != float64(1) {
		t.Fatalf("session state was not promoted to the home file: %+v", state)
	}
}

func TestOverlayReadsStateFromCustomConfigDir(t *testing.T) {
	home := t.TempDir()
	configDir := filepath.Join(t.TempDir(), "claude-work")
	t.Setenv("HOME", home)
	writeFile(t, filepath.Join(home, ".claude.json"), `{"which":"home"}`)
	writeFile(t, filepath.Join(configDir, ".claude.json"), `{"which":"config-dir","primaryApiKey":"sk-ant-api03-secret"}`)

	env := append(thirdPartyEnv(), "CLAUDE_CONFIG_DIR="+configDir)
	overlayDir, _ := prepareOverlay(t, thirdPartyTarget(), nil, env)

	state := readJSONFile(t, filepath.Join(overlayDir, ".claude.json"))
	if state["which"] != "config-dir" || state["primaryApiKey"] != nil {
		t.Fatalf("overlay state = %+v, want the sanitized CLAUDE_CONFIG_DIR state", state)
	}
}

func TestOverlaySanitizesLegacyConfigJSON(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	writeFile(t, filepath.Join(home, ".claude", ".config.json"), `{"primaryApiKey":"sk-ant-api03-secret","legacy":true}`)

	overlayDir, _ := prepareOverlay(t, thirdPartyTarget(), nil, thirdPartyEnv())

	legacy := filepath.Join(overlayDir, ".config.json")
	if info, err := os.Lstat(legacy); err != nil || !info.Mode().IsRegular() {
		t.Fatalf("legacy state must be a regular copy, stat err=%v", err)
	}
	if state := readJSONFile(t, legacy); state["legacy"] != true || state["primaryApiKey"] != nil {
		t.Fatalf("legacy state = %+v", state)
	}
}

func TestOverlayNestedLaunchResolvesTheRealConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	writeFile(t, filepath.Join(home, ".claude.json"), `{"which":"home"}`)
	writeFile(t, filepath.Join(home, ".claude", "settings.json"), `{}`)

	parent, _ := prepareOverlay(t, thirdPartyTarget(), nil, thirdPartyEnv())
	child, _ := prepareOverlay(t, thirdPartyTarget(), nil, append(thirdPartyEnv(), "CLAUDE_CONFIG_DIR="+parent))

	source, ok := readOverlayMarker(child)
	if !ok {
		t.Fatal("nested overlay has no marker")
	}
	if source.ConfigDir != filepath.Join(home, ".claude") || source.StateFile != filepath.Join(home, ".claude.json") {
		t.Fatalf("nested overlay mirrors %+v, want the user's own config", source)
	}
}

func TestOverlayKeepsAnthropicCredentialsOutOfTheSession(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	claudeDir := filepath.Join(home, ".claude")
	writeFile(t, filepath.Join(claudeDir, ".credentials.json"), `{"claudeAiOauth":{"accessToken":"sk-ant-oat01-secret"},"mcpOAuth":{"server":{"accessToken":"mcp-token"}}}`)
	writeFile(t, filepath.Join(claudeDir, "settings.json"), `{"apiKeyHelper":"security find-generic-password -s anthropic -w","model":"claude-opus-5-5","env":{"ANTHROPIC_API_KEY":"sk-ant-stale","ANTHROPIC_DEFAULT_HAIKU_MODEL_NAME":"x","KEEP_ME":"1"}}`)

	target := profiles.Target{Profile: "ollama", Family: providers.FamilyLocal}
	overlayDir, _ := prepareOverlay(t, target, nil, []string{
		"ANTHROPIC_BASE_URL=http://localhost:11434",
		"ANTHROPIC_AUTH_TOKEN=ollama",
		"ANTHROPIC_API_KEY=",
	})

	credentials := filepath.Join(overlayDir, ".credentials.json")
	if info, err := os.Lstat(credentials); err != nil || !info.Mode().IsRegular() {
		t.Fatalf("credentials must be a sanitized regular copy (err=%v)", err)
	}
	if data, _ := os.ReadFile(credentials); strings.Contains(string(data), "sk-ant-oat01-secret") {
		t.Fatalf("the claude.ai OAuth token reached the overlay:\n%s", data)
	}
	settings := readJSONFile(t, filepath.Join(overlayDir, "settings.json"))
	if _, ok := settings["apiKeyHelper"]; ok {
		t.Fatal("apiKeyHelper survived: its output would be sent as x-api-key")
	}
	if _, ok := settings["model"]; ok {
		t.Fatalf("a Claude model stayed pinned without a provider model: %v", settings["model"])
	}
	env, _ := settings["env"].(map[string]any)
	for _, key := range []string{"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_DEFAULT_HAIKU_MODEL_NAME"} {
		if _, ok := env[key]; ok {
			t.Fatalf("settings env still carries %s: %+v", key, env)
		}
	}
	if env["KEEP_ME"] != "1" || env["ANTHROPIC_BASE_URL"] != "http://localhost:11434" {
		t.Fatalf("settings env = %+v", env)
	}
	info, err := os.Stat(filepath.Join(overlayDir, "settings.json"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("patched settings mode = %v (err=%v), want 0600", info.Mode().Perm(), err)
	}
}

func TestOverlayModelAliasResolvesThroughTiers(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	target := profiles.Target{
		Profile:    "zai",
		Family:     providers.FamilyAnthropicCompatibleNonClaude,
		Model:      "glm-5.3[1m]",
		ModelTiers: map[string]string{"haiku": "glm-5.3-flash[1m]"},
	}
	env := append(thirdPartyEnv(),
		"ANTHROPIC_DEFAULT_OPUS_MODEL=glm-5.3[1m]",
		"ANTHROPIC_DEFAULT_HAIKU_MODEL=glm-5.3-flash[1m]",
	)
	out, cleanup, err := PrepareClaudeConfigOverlay(target, []string{"--model", "haiku"}, env)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	got := envSliceToMap(out)
	if got["ANTHROPIC_DEFAULT_OPUS_MODEL"] != "glm-5.3[1m]" || got["ANTHROPIC_DEFAULT_HAIKU_MODEL"] != "glm-5.3-flash[1m]" {
		t.Fatalf("an alias overwrote the tier mapping: %+v", got)
	}
	if got["ANTHROPIC_MODEL"] != "glm-5.3[1m]" {
		t.Fatalf("ANTHROPIC_MODEL = %q, want the provider default kept", got["ANTHROPIC_MODEL"])
	}
	if settings := readJSONFile(t, filepath.Join(got["CLAUDE_CONFIG_DIR"], "settings.json")); settings["model"] != "glm-5.3-flash[1m]" {
		t.Fatalf("session model = %v, want the haiku mapping", settings["model"])
	}
}

func TestOverlayConcreteModelSwapsContextSettings(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	target := profiles.Target{
		Profile: "kimi",
		Family:  providers.FamilyAnthropicCompatibleNonClaude,
		Model:   "k3-256k",
		ModelEnv: map[string]map[string]string{
			"k3-256k": {"CLAUDE_CODE_AUTO_COMPACT_WINDOW": "262144", "CLAUDE_CODE_MAX_CONTEXT_TOKENS": "262144"},
			"k3[1m]":  {"CLAUDE_CODE_AUTO_COMPACT_WINDOW": "1048576", "CLAUDE_CODE_MAX_CONTEXT_TOKENS": "1048576"},
		},
	}
	base := append(thirdPartyEnv(), "CLAUDE_CODE_AUTO_COMPACT_WINDOW=262144", "CLAUDE_CODE_MAX_CONTEXT_TOKENS=262144")

	out, cleanup, err := PrepareClaudeConfigOverlay(target, []string{"--model", "k3[1m]"}, base)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	if got := envSliceToMap(out); got["CLAUDE_CODE_MAX_CONTEXT_TOKENS"] != "1048576" || got["CLAUDE_CODE_AUTO_COMPACT_WINDOW"] != "1048576" {
		t.Fatalf("context settings did not follow the chosen model: %+v", got)
	}

	out, cleanup, err = PrepareClaudeConfigOverlay(target, []string{"--model=some-other-model"}, base)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	got := envSliceToMap(out)
	if _, ok := got["CLAUDE_CODE_MAX_CONTEXT_TOKENS"]; ok {
		t.Fatalf("an unknown model inherited the default model's context size: %+v", got)
	}
	if got["ANTHROPIC_DEFAULT_FABLE_MODEL"] != "some-other-model" || got["CLAUDE_CODE_SUBAGENT_MODEL"] != "some-other-model" {
		t.Fatalf("--model did not pin every tier: %+v", got)
	}
}

// MCP servers' OAuth tokens live next to the claude.ai token in the Linux
// credential store: they must survive, and a refresh during the session must
// reach the user's file, without the claude.ai token ever entering the copy.
func TestOverlayKeepsMCPTokensAndWritesTheirRefreshBack(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	realCredentials := filepath.Join(home, ".claude", ".credentials.json")
	writeFile(t, realCredentials, `{"claudeAiOauth":{"accessToken":"sk-ant-oat01-secret"},"mcpOAuth":{"server":{"accessToken":"old"}}}`)

	overlayDir, cleanup := prepareOverlay(t, thirdPartyTarget(), nil, thirdPartyEnv())
	copied := readJSONFile(t, filepath.Join(overlayDir, ".credentials.json"))
	if _, ok := copied["claudeAiOauth"]; ok {
		t.Fatal("the claude.ai OAuth token reached the overlay")
	}
	if servers, _ := copied["mcpOAuth"].(map[string]any); servers["server"] == nil {
		t.Fatalf("MCP tokens were dropped: %+v", copied)
	}

	writeFile(t, filepath.Join(overlayDir, ".credentials.json"), `{"mcpOAuth":{"server":{"accessToken":"refreshed"}}}`)
	cleanup()

	final := readJSONFile(t, realCredentials)
	if oauth, _ := final["claudeAiOauth"].(map[string]any); oauth["accessToken"] != "sk-ant-oat01-secret" {
		t.Fatalf("the user's claude.ai token was lost: %+v", final)
	}
	servers, _ := final["mcpOAuth"].(map[string]any)
	if server, _ := servers["server"].(map[string]any); server["accessToken"] != "refreshed" {
		t.Fatalf("the refreshed MCP token was not written back: %+v", final)
	}
}

func TestOverlayDoesNotCreateAMissingCredentialStore(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")

	overlayDir, cleanup := prepareOverlay(t, thirdPartyTarget(), nil, thirdPartyEnv())
	if _, err := os.Stat(filepath.Join(overlayDir, ".credentials.json")); !os.IsNotExist(err) {
		t.Fatalf("an empty credential store was created in the overlay (err=%v)", err)
	}
	cleanup()
	if _, err := os.Stat(filepath.Join(home, ".claude", ".credentials.json")); !os.IsNotExist(err) {
		t.Fatalf("a credential store appeared in ~/.claude (err=%v)", err)
	}
}
