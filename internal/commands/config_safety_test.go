package commands

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jolehuit/clother/internal/cli"
	"github.com/jolehuit/clother/internal/config"
	"github.com/jolehuit/clother/internal/providers"
	"github.com/jolehuit/clother/internal/ui"
)

// newSandboxContext builds a Context whose HOME, bin dir and PATH all live in a
// temporary directory: no test may ever touch the real installation.
func newSandboxContext(t *testing.T, stdin string) (Context, *bytes.Buffer) {
	t.Helper()

	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, ".local", "share"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, ".cache"))
	t.Setenv("CLOTHER_BIN", binDir)
	t.Setenv("PATH", binDir)
	t.Setenv("HOMEBREW_PREFIX", "")
	t.Setenv(envConfigAPIKey, "")
	os.Unsetenv(envConfigAPIKey)
	t.Setenv(envConfigModel, "")
	t.Setenv(envConfigBaseURL, "")
	t.Setenv(envConfigAlias, "")
	t.Setenv(envConfigProviderName, "")

	paths, err := config.Detect("")
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := providers.Load()
	if err != nil {
		t.Fatal(err)
	}
	stdout := &bytes.Buffer{}
	ctx := Context{
		Paths:   paths,
		Config:  &config.File{Version: 1, ProviderOverrides: map[string]config.ProviderOverride{}, OpenRouterAliases: map[string]string{}, CustomProviders: map[string]config.CustomProvider{}},
		Secrets: config.Secrets{},
		Catalog: catalog,
		Output:  &ui.Output{Stdout: stdout, Stderr: io.Discard, Format: ui.FormatHuman},
		Prompt:  ui.NewPrompter(strings.NewReader(stdin), io.Discard),
		Options: cli.Options{Format: "human"},
	}
	return ctx, stdout
}

// TestPersistConfigPreservesRealClaudeBinary is the non-regression test of the
// critical finding: `clother config <provider>` used to delete the real claude
// binary sitting in BinDir and replace it with a symlink to clother, without
// ever creating the claude-real fallback.
func TestPersistConfigPreservesRealClaudeBinary(t *testing.T) {
	c, _ := newSandboxContext(t, "")

	realClaude := filepath.Join(c.Paths.BinDir, "claude")
	const marker = "#!/bin/sh\necho \"I am the REAL claude code binary\"\n"
	if err := os.WriteFile(realClaude, []byte(marker), 0o755); err != nil {
		t.Fatal(err)
	}

	c.Secrets["ZAI_API_KEY"] = "test-key-not-a-real-credential"
	code, err := persistConfig(c, "ZAI_API_KEY")
	if err != nil || code != 0 {
		t.Fatalf("persistConfig() = %d, %v", code, err)
	}

	preserved := filepath.Join(c.Paths.BinDir, "claude-real")
	data, err := os.ReadFile(preserved)
	if err != nil {
		t.Fatalf("the real claude binary was destroyed instead of preserved: %v", err)
	}
	if string(data) != marker {
		t.Fatalf("claude-real content = %q, want the original binary", data)
	}
	info, err := os.Stat(preserved)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o100 == 0 {
		t.Fatalf("claude-real mode = %v, want it to stay executable", info.Mode().Perm())
	}
	shim, err := os.Lstat(realClaude)
	if err != nil {
		t.Fatalf("claude shim missing: %v", err)
	}
	if shim.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("expected BinDir/claude to be the clother shim symlink, got mode %v", shim.Mode())
	}
}

// TestPersistConfigPreservesUnderAStaleBackup pins persistConfig's OWN
// protection rather than the defence launchers.Sync applies as well.
//
// When BinDir/claude-real is a stale clother shim left by a previous install,
// only runtime.PreserveRealClaude drops that shim and archives the genuine
// binary under the name FindRealClaude falls back to. Sync alone would keep the
// stale shim and park the real binary under claude.bak, leaving claude-real
// pointing back at clother.
func TestPersistConfigPreservesUnderAStaleBackup(t *testing.T) {
	c, _ := newSandboxContext(t, "")

	realClaude := filepath.Join(c.Paths.BinDir, "claude")
	const marker = "#!/bin/sh\necho \"I am the REAL claude code binary\"\n"
	if err := os.WriteFile(realClaude, []byte(marker), 0o755); err != nil {
		t.Fatal(err)
	}
	// A stale backup from an earlier install: a symlink naming the clother binary.
	stale := filepath.Join(c.Paths.BinDir, "claude-real")
	if err := os.Symlink("clother", stale); err != nil {
		t.Fatal(err)
	}

	c.Secrets["ZAI_API_KEY"] = "test-key-not-a-real-credential"
	if code, err := persistConfig(c, "ZAI_API_KEY"); err != nil || code != 0 {
		t.Fatalf("persistConfig() = %d, %v", code, err)
	}

	info, err := os.Lstat(stale)
	if err != nil {
		t.Fatalf("claude-real missing: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatal("claude-real is still the stale clother shim: the real binary was not archived under the name FindRealClaude looks for")
	}
	data, err := os.ReadFile(stale)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != marker {
		t.Fatalf("claude-real content = %q, want the original binary", data)
	}
}

// TestRunConfigWithoutTerminalFails covers the false success: a piped
// `clother config zai` printed "configuration saved" and exited 0 while
// writing nothing.
func TestRunConfigWithoutTerminalFails(t *testing.T) {
	c, stdout := newSandboxContext(t, "")
	stdinIsTerminal = func() bool { return false }
	t.Cleanup(func() { stdinIsTerminal = defaultStdinIsTerminal })

	code, err := runConfig(context.Background(), c, []string{"zai"})
	if err == nil {
		t.Fatal("expected an explicit error when stdin is not a terminal")
	}
	if code == 0 {
		t.Fatalf("exit code = %d, want non-zero", code)
	}
	if strings.Contains(stdout.String(), "configuration saved") {
		t.Fatalf("stdout claimed success: %q", stdout.String())
	}
	if _, statErr := os.Stat(c.Paths.SecretsFile); statErr == nil {
		t.Fatal("secrets file was written despite the failure")
	}
	if c.Secrets["ZAI_API_KEY"] != "" {
		t.Fatal("a key was recorded even though none was provided")
	}
}

func TestRunConfigNoInputFlagFailsEvenOnATerminal(t *testing.T) {
	c, _ := newSandboxContext(t, "")
	stdinIsTerminal = func() bool { return true }
	t.Cleanup(func() { stdinIsTerminal = defaultStdinIsTerminal })
	c.Options.NoInput = true

	code, err := runConfig(context.Background(), c, []string{"zai"})
	if err == nil || code == 0 {
		t.Fatalf("--no-input without a key should fail, got code=%d err=%v", code, err)
	}
}

// TestRunConfigNonInteractiveWritesKey documents the scriptable path.
func TestRunConfigNonInteractiveWritesKey(t *testing.T) {
	c, _ := newSandboxContext(t, "")
	stdinIsTerminal = func() bool { return false }
	t.Cleanup(func() { stdinIsTerminal = defaultStdinIsTerminal })
	t.Setenv(envConfigAPIKey, "sandbox-key-0123456789")

	code, err := runConfig(context.Background(), c, []string{"zai"})
	if err != nil || code != 0 {
		t.Fatalf("runConfig() = %d, %v", code, err)
	}
	loaded, err := config.LoadSecrets(c.Paths.SecretsFile)
	if err != nil {
		t.Fatal(err)
	}
	if loaded["ZAI_API_KEY"] != "sandbox-key-0123456789" {
		t.Fatalf("secrets file = %+v, want the key written", loaded)
	}
}

func TestRunConfigNonInteractiveReadsKeyFromStdin(t *testing.T) {
	c, _ := newSandboxContext(t, "")
	stdinIsTerminal = func() bool { return false }
	t.Setenv(envConfigAPIKey, "-")
	stdinReader = strings.NewReader("piped-key-0123456789\n")
	t.Cleanup(func() {
		stdinIsTerminal = defaultStdinIsTerminal
		stdinReader = os.Stdin
	})

	code, err := runConfig(context.Background(), c, []string{"kimi"})
	if err != nil || code != 0 {
		t.Fatalf("runConfig() = %d, %v", code, err)
	}
	loaded, err := config.LoadSecrets(c.Paths.SecretsFile)
	if err != nil {
		t.Fatal(err)
	}
	if loaded["KIMI_API_KEY"] != "piped-key-0123456789" {
		t.Fatalf("secrets file = %+v, want the piped key", loaded)
	}
}

// TestNullDeviceIsNotATerminal covers the detection itself: /dev/null is a
// character device, so the usual ModeCharDevice test alone reports the classic
// `clother config zai </dev/null` of a Dockerfile as an interactive session.
func TestNullDeviceIsNotATerminal(t *testing.T) {
	devNull, err := os.Open(os.DevNull)
	if err != nil {
		t.Skipf("no %s on this system: %v", os.DevNull, err)
	}
	defer devNull.Close()

	info, err := devNull.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeCharDevice == 0 {
		t.Skip("the null device is not a character device here, nothing to guard against")
	}
	if !isNullDevice(devNull) {
		t.Fatal("the null device was taken for a terminal a human types into")
	}
}

// TestScriptedInputShortCircuitsTerminalDetection makes the scriptable path
// reachable whatever stdin is attached to: an exported CLOTHER_API_KEY is an
// explicit statement that nobody is going to answer a prompt.
func TestScriptedInputShortCircuitsTerminalDetection(t *testing.T) {
	c, _ := newSandboxContext(t, "")
	stdinIsTerminal = func() bool { return true }
	t.Cleanup(func() { stdinIsTerminal = defaultStdinIsTerminal })
	t.Setenv(envConfigAPIKey, "sandbox-key-0123456789")

	if code, err := runConfig(context.Background(), c, []string{"zai"}); err != nil || code != 0 {
		t.Fatalf("runConfig() = %d, %v", code, err)
	}
	loaded, err := config.LoadSecrets(c.Paths.SecretsFile)
	if err != nil {
		t.Fatal(err)
	}
	if loaded["ZAI_API_KEY"] != "sandbox-key-0123456789" {
		t.Fatalf("the exported key was ignored: %+v", loaded)
	}
}

func TestRunConfigRejectsExtraArguments(t *testing.T) {
	c, _ := newSandboxContext(t, "")
	if code, err := runConfig(context.Background(), c, []string{"zai", "kimi"}); err == nil || code == 0 {
		t.Fatalf("expected extra arguments to be refused, got code=%d err=%v", code, err)
	}
}

func TestChooseProviderRetriesAcceptsNamesAndCancels(t *testing.T) {
	// A typo is retried instead of aborting the command.
	c, _ := newSandboxContext(t, "99\nzai\n")
	got, err := chooseProvider(c)
	if err != nil {
		t.Fatalf("chooseProvider() error = %v", err)
	}
	if got != "zai" {
		t.Fatalf("chooseProvider() = %q, want zai after a retry", got)
	}

	// The provider name shown in the menu is accepted, not only its number.
	c, _ = newSandboxContext(t, "custom\n")
	if got, err := chooseProvider(c); err != nil || got != "custom" {
		t.Fatalf("chooseProvider() = %q, %v; want custom", got, err)
	}

	// An empty line cancels instead of being reported as an invalid choice.
	c, _ = newSandboxContext(t, "\n")
	got, err = chooseProvider(c)
	if err != nil || got != "" {
		t.Fatalf("chooseProvider() = %q, %v; want a silent cancel", got, err)
	}

	// Three invalid answers still give up, with an error.
	c, _ = newSandboxContext(t, "98\n99\n100\n")
	if got, err := chooseProvider(c); err == nil || got != "" {
		t.Fatalf("chooseProvider() = %q, %v; want an error after three tries", got, err)
	}
}

// TestConfigCustomRejectsUnusableNamesBeforeWriting covers the half-written
// state: a name starting with a digit produced a valid config.json entry and
// then failed on SaveSecrets with "invalid secret key".
func TestConfigCustomRejectsUnusableNamesBeforeWriting(t *testing.T) {
	for _, name := range []string{"9gpt", "../../evil", "Upper Case", "-dash"} {
		c, _ := newSandboxContext(t, name+"\nhttps://api.example.com\n\n")
		code, err := configCustom(c)
		if err == nil || code == 0 {
			t.Fatalf("configCustom(%q) = %d, %v; want a refusal", name, code, err)
		}
		if len(c.Config.CustomProviders) != 0 {
			t.Fatalf("configCustom(%q) wrote %+v before failing", name, c.Config.CustomProviders)
		}
		if _, statErr := os.Stat(c.Paths.ConfigFile); statErr == nil {
			t.Fatalf("configCustom(%q) persisted a config file", name)
		}
	}
}

func TestConfigCustomRejectsInvalidBaseURL(t *testing.T) {
	c, _ := newSandboxContext(t, "evil\nhttp://exa mple.com\n\n")
	code, err := configCustom(c)
	if err == nil || code == 0 {
		t.Fatalf("configCustom() = %d, %v; want the malformed URL to be refused", code, err)
	}
	if len(c.Config.CustomProviders) != 0 {
		t.Fatalf("a custom provider was stored: %+v", c.Config.CustomProviders)
	}
}

// TestRunRemoveDropsASingleProviderKey covers the missing removal path: a key
// could only be added, or wiped together with the whole installation.
func TestRunRemoveDropsASingleProviderKey(t *testing.T) {
	c, _ := newSandboxContext(t, "")
	c.Options.Yes = true
	c.Secrets["ZAI_API_KEY"] = "zai-key-0123456789"
	c.Secrets["KIMI_API_KEY"] = "kimi-key-0123456789"
	c.Config.ProviderOverrides["zai"] = config.ProviderOverride{Model: "glm-4.7"}

	code, err := runRemove(context.Background(), c, []string{"zai"})
	if err != nil || code != 0 {
		t.Fatalf("runRemove() = %d, %v", code, err)
	}
	loaded, err := config.LoadSecrets(c.Paths.SecretsFile)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := loaded["ZAI_API_KEY"]; ok {
		t.Fatalf("ZAI_API_KEY survived the removal: %+v", loaded)
	}
	if loaded["KIMI_API_KEY"] != "kimi-key-0123456789" {
		t.Fatalf("the other provider key was lost: %+v", loaded)
	}
	if _, ok := c.Config.ProviderOverrides["zai"]; ok {
		t.Fatalf("the provider override survived: %+v", c.Config.ProviderOverrides)
	}
}

func TestRunRemoveNeedsConfirmation(t *testing.T) {
	c, _ := newSandboxContext(t, "")
	stdinIsTerminal = func() bool { return false }
	t.Cleanup(func() { stdinIsTerminal = defaultStdinIsTerminal })
	c.Secrets["ZAI_API_KEY"] = "zai-key-0123456789"

	code, err := runRemove(context.Background(), c, []string{"zai"})
	if err == nil || code == 0 {
		t.Fatalf("expected a refusal without --yes, got code=%d err=%v", code, err)
	}
	if c.Secrets["ZAI_API_KEY"] == "" {
		t.Fatal("the key was removed despite the refusal")
	}
}

func TestRunRemoveDropsCustomProviderAndAlias(t *testing.T) {
	c, _ := newSandboxContext(t, "")
	c.Options.Yes = true
	c.Config.CustomProviders["acme"] = config.CustomProvider{Name: "acme", DisplayName: "acme", BaseURL: "https://api.acme.test", APIKeyEnv: "ACME_API_KEY"}
	c.Secrets["ACME_API_KEY"] = "acme-key-0123456789"
	c.Config.OpenRouterAliases["kimi"] = "moonshotai/kimi-k2"

	if code, err := runRemove(context.Background(), c, []string{"acme"}); err != nil || code != 0 {
		t.Fatalf("runRemove(acme) = %d, %v", code, err)
	}
	if _, ok := c.Config.CustomProviders["acme"]; ok {
		t.Fatalf("custom provider survived: %+v", c.Config.CustomProviders)
	}
	if _, ok := c.Secrets["ACME_API_KEY"]; ok {
		t.Fatalf("custom provider key survived: %+v", c.Secrets)
	}

	if code, err := runRemove(context.Background(), c, []string{"or-kimi"}); err != nil || code != 0 {
		t.Fatalf("runRemove(or-kimi) = %d, %v", code, err)
	}
	if _, ok := c.Config.OpenRouterAliases["kimi"]; ok {
		t.Fatalf("alias survived: %+v", c.Config.OpenRouterAliases)
	}
}

func TestDispatchRejectsSilentlyIgnoredArguments(t *testing.T) {
	c, _ := newSandboxContext(t, "")
	for _, command := range []string{"status", "list"} {
		code, err := Dispatch(context.Background(), c, command, []string{"extraarg"})
		if err == nil || code == 0 {
			t.Fatalf("Dispatch(%q, [extraarg]) = %d, %v; want a refusal", command, code, err)
		}
	}
}

func TestRunInfoHandlesEveryArgument(t *testing.T) {
	c, stdout := newSandboxContext(t, "")
	if code, err := runInfo(context.Background(), c, []string{"zai", "kimi"}); err != nil || code != 0 {
		t.Fatalf("runInfo() = %d, %v", code, err)
	}
	out := stdout.String()
	if !strings.Contains(out, "zai") || !strings.Contains(out, "kimi") {
		t.Fatalf("runInfo output dropped a provider: %q", out)
	}
}

func TestResolveTargetArgExplainsOpenRouter(t *testing.T) {
	c, _ := newSandboxContext(t, "")
	if _, err := resolveTargetArg(c, "openrouter"); err == nil || !strings.Contains(err.Error(), "alias") {
		t.Fatalf("resolveTargetArg(openrouter) error = %v, want guidance about aliases", err)
	}
}

// The "the API key will travel unencrypted" warning existed at exactly one of
// the four places a base URL is validated: the interactive creation of a custom
// provider. Redirecting a BUILT-IN provider to a cleartext host answered "OK
// configuration saved" without a word, and the key was then exported into
// ANTHROPIC_AUTH_TOKEN and sent in the clear to that host.
func TestCleartextBaseURLIsAlwaysReported(t *testing.T) {
	cases := []struct {
		name      string
		baseURL   string
		wantWarn  bool
		provider  string
		configure func(t *testing.T)
	}{
		{name: "builtin override to a cleartext host", provider: "zai", baseURL: "http://attacker.example.com/v1", wantWarn: true},
		{name: "builtin override to https", provider: "zai", baseURL: "https://api.example.com/v1", wantWarn: false},
		{name: "loopback backend stays quiet", provider: "zai", baseURL: "http://127.0.0.1:11434/v1", wantWarn: false},
		{name: "localhost backend stays quiet", provider: "zai", baseURL: "http://localhost:1234/v1", wantWarn: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := newSandboxContext(t, "")
			warnings := &bytes.Buffer{}
			c.Output = &ui.Output{Stdout: io.Discard, Stderr: warnings, Format: ui.FormatHuman}

			t.Setenv(envConfigAPIKey, "test-key-not-a-real-credential")
			t.Setenv(envConfigBaseURL, tc.baseURL)

			code, err := configNonInteractive(c, tc.provider)
			if err != nil || code != 0 {
				t.Fatalf("configNonInteractive() = %d, %v", code, err)
			}

			warned := strings.Contains(warnings.String(), "unencrypted")
			if warned != tc.wantWarn {
				t.Fatalf("warned=%v, want %v (stderr: %q)", warned, tc.wantWarn, warnings.String())
			}
		})
	}
}

// A custom provider configured through the scriptable path gets the same
// warning as the interactive one.
func TestCleartextCustomProviderIsReportedNonInteractively(t *testing.T) {
	c, _ := newSandboxContext(t, "")
	warnings := &bytes.Buffer{}
	c.Output = &ui.Output{Stdout: io.Discard, Stderr: warnings, Format: ui.FormatHuman}

	t.Setenv(envConfigAPIKey, "test-key-not-a-real-credential")
	t.Setenv(envConfigProviderName, "mygateway")
	t.Setenv(envConfigBaseURL, "http://gateway.example.com/v1")

	code, err := configNonInteractive(c, "custom")
	if err != nil || code != 0 {
		t.Fatalf("configNonInteractive() = %d, %v", code, err)
	}
	if !strings.Contains(warnings.String(), "unencrypted") {
		t.Fatalf("no cleartext warning for a custom provider: %q", warnings.String())
	}
}

// Every refusal in Clother names the command to run next. The `remove` path for
// an unknown OpenRouter alias was the last dead end left: the launcher gateway
// answered "run `clother config openrouter` to add one" for the very same
// mistake while this one stopped at the diagnosis.
func TestUnknownRemovalTargetsNameTheNextCommand(t *testing.T) {
	c, _ := newSandboxContext(t, "")
	c.Options.Yes = true
	c.Config.OpenRouterAliases["kimi"] = "moonshotai/kimi-k2"

	for _, name := range []string{"or-nosuch", "nosuchprovider"} {
		code, err := runRemove(context.Background(), c, []string{name})
		if err == nil || code == 0 {
			t.Fatalf("runRemove(%q) = %d, %v; want a refusal", name, code, err)
		}
		if !strings.Contains(err.Error(), "clother ") {
			t.Errorf("runRemove(%q) gives no next command: %q", name, err.Error())
		}
	}
}

// Same rule on the way in: `clother config nosuchprovider` has to say where the
// list of providers is, like every other refusal does.
func TestUnknownConfigTargetNamesTheNextCommand(t *testing.T) {
	c, _ := newSandboxContext(t, "")
	stdinIsTerminal = func() bool { return false }
	t.Cleanup(func() { stdinIsTerminal = defaultStdinIsTerminal })
	t.Setenv(envConfigAPIKey, "sk-fake-0123456789")

	code, err := runConfig(context.Background(), c, []string{"nosuchprovider"})
	if err == nil || code == 0 {
		t.Fatalf("runConfig(nosuchprovider) = %d, %v; want a refusal", code, err)
	}
	if !strings.Contains(err.Error(), "clother ") {
		t.Errorf("runConfig gives no next command: %q", err.Error())
	}
}
