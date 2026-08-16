package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jolehuit/clother/internal/cli"
	"github.com/jolehuit/clother/internal/config"
)

// fakeKey is the value planted in the sandboxed secrets file. It is not a real
// credential; the leak test greps every command output for it.
const fakeKey = "sk-clother-test-not-a-real-key-0123456789"

type sandbox struct {
	root    string
	binDir  string
	pathDir string
	dumpDir string
	paths   config.Paths
}

// newSandbox isolates HOME, the Clother directories and PATH, and installs a
// fake `claude` that records the environment and the arguments it received.
func newSandbox(t *testing.T) *sandbox {
	t.Helper()
	root := t.TempDir()

	sb := &sandbox{
		root:    root,
		binDir:  filepath.Join(root, "bin"),
		pathDir: filepath.Join(root, "path"),
		dumpDir: filepath.Join(root, "dump"),
	}
	for _, dir := range []string{sb.binDir, sb.pathDir, sb.dumpDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	script := "#!/bin/sh\n" +
		"env > \"$CLOTHER_TEST_DUMP/env\"\n" +
		"printf '%s\\n' \"$@\" > \"$CLOTHER_TEST_DUMP/args\"\n" +
		"exit 0\n"
	if err := os.WriteFile(filepath.Join(sb.pathDir, "claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, ".local", "share"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, ".cache"))
	t.Setenv("CLOTHER_CONFIG_DIR", filepath.Join(root, "config"))
	t.Setenv("CLOTHER_DATA_DIR", filepath.Join(root, "data"))
	t.Setenv("CLOTHER_CACHE_DIR", filepath.Join(root, "cache"))
	t.Setenv("CLOTHER_BIN", sb.binDir)
	t.Setenv("CLOTHER_NO_UPDATE_CHECK", "1")
	t.Setenv("CLOTHER_TEST_DUMP", sb.dumpDir)
	t.Setenv("PATH", sb.pathDir+string(os.PathListSeparator)+"/usr/bin"+string(os.PathListSeparator)+"/bin")
	// Anything left over from the parent environment must be purged by Clother.
	t.Setenv("ANTHROPIC_API_KEY", "leftover-anthropic-key")
	t.Setenv("ANTHROPIC_BASE_URL", "https://leftover.example")
	t.Setenv("ANTHROPIC_CUSTOM_HEADERS", "leftover: header")

	paths, err := config.Detect("")
	if err != nil {
		t.Fatal(err)
	}
	sb.paths = paths
	return sb
}

func (sb *sandbox) writeSecrets(t *testing.T, secrets map[string]string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(sb.paths.SecretsFile), 0o700); err != nil {
		t.Fatal(err)
	}
	var buf strings.Builder
	for key, value := range secrets {
		fmt.Fprintf(&buf, "%s=%s\n", key, value)
	}
	if err := os.WriteFile(sb.paths.SecretsFile, []byte(buf.String()), 0o600); err != nil {
		t.Fatal(err)
	}
}

func (sb *sandbox) writeConfig(t *testing.T, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(sb.paths.ConfigFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sb.paths.ConfigFile, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// claudeEnv returns the environment the fake claude was launched with.
func (sb *sandbox) claudeEnv(t *testing.T) map[string]string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(sb.dumpDir, "env"))
	if err != nil {
		t.Fatalf("fake claude was never launched: %v", err)
	}
	env := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		if key, value, ok := strings.Cut(line, "="); ok {
			env[key] = value
		}
	}
	return env
}

func (sb *sandbox) claudeArgs(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(sb.dumpDir, "args"))
	if err != nil {
		t.Fatalf("fake claude was never launched: %v", err)
	}
	var args []string
	for _, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		if line != "" {
			args = append(args, line)
		}
	}
	return args
}

// capture redirects the process stdout and stderr into a file for the duration
// of fn. Clother resolves its writers from os.Stdout at startup, so the swap has
// to happen before Run is called.
func capture(t *testing.T, fn func()) string {
	t.Helper()
	file, err := os.CreateTemp(t.TempDir(), "capture")
	if err != nil {
		t.Fatal(err)
	}
	oldStdout, oldStderr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = file, file
	defer func() {
		os.Stdout, os.Stderr = oldStdout, oldStderr
		_ = file.Close()
	}()

	fn()

	if _, err := file.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(file.Name())
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// captureStreams is capture() with the two streams kept apart, so a test can
// assert that a diagnostic went to stderr and left stdout untouched.
func captureStreams(t *testing.T, fn func()) (string, string) {
	t.Helper()
	dir := t.TempDir()
	outFile, err := os.CreateTemp(dir, "stdout")
	if err != nil {
		t.Fatal(err)
	}
	errFile, err := os.CreateTemp(dir, "stderr")
	if err != nil {
		t.Fatal(err)
	}
	oldStdout, oldStderr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outFile, errFile
	defer func() {
		os.Stdout, os.Stderr = oldStdout, oldStderr
		_ = outFile.Close()
		_ = errFile.Close()
	}()

	fn()

	read := func(file *os.File) string {
		data, readErr := os.ReadFile(file.Name())
		if readErr != nil {
			t.Fatal(readErr)
		}
		return string(data)
	}
	return read(outFile), read(errFile)
}

// Regression: -v and -d were parsed and read nowhere, so `clother -v status`
// printed exactly what `clother status` printed.
func TestVerboseAndDebugAreWiredToStderr(t *testing.T) {
	newSandbox(t)

	quietOut, quietErr := captureStreams(t, func() {
		if code, err := Run(context.Background(), []string{"list"}, "clother"); err != nil || code != 0 {
			t.Fatalf("clother list = %d, %v", code, err)
		}
	})
	if strings.Contains(quietErr, "clother: running") {
		t.Fatalf("plain run already prints diagnostics: %q", quietErr)
	}

	verboseOut, verboseErr := captureStreams(t, func() {
		if code, err := Run(context.Background(), []string{"-v", "list"}, "clother"); err != nil || code != 0 {
			t.Fatalf("clother -v list = %d, %v", code, err)
		}
	})
	if !strings.Contains(verboseErr, "clother: running \"list\"") {
		t.Fatalf("-v produced no diagnostic on stderr: %q", verboseErr)
	}
	if verboseOut != quietOut {
		t.Fatalf("-v changed stdout:\n%q\nvs\n%q", verboseOut, quietOut)
	}

	_, debugErr := captureStreams(t, func() {
		if code, err := Run(context.Background(), []string{"-d", "list"}, "clother"); err != nil || code != 0 {
			t.Fatalf("clother -d list = %d, %v", code, err)
		}
	})
	if !strings.Contains(debugErr, "clother: debug: command=\"list\"") {
		t.Fatalf("-d produced no debug trace on stderr: %q", debugErr)
	}
	if !strings.Contains(debugErr, "clother: running \"list\"") {
		t.Fatalf("-d did not imply -v: %q", debugErr)
	}
}

// The core promise of the product: `clother-zai` launches claude against Z.AI
// with the right base URL, model and token, and forwards the arguments.
func TestLauncherSendsTheRightProviderToClaude(t *testing.T) {
	sb := newSandbox(t)
	sb.writeSecrets(t, map[string]string{"ZAI_API_KEY": fakeKey})

	code, err := Run(context.Background(), []string{"-p", "hello"}, filepath.Join(sb.binDir, "clother-zai"))
	if err != nil || code != 0 {
		t.Fatalf("Run(clother-zai) = %d, %v", code, err)
	}

	env := sb.claudeEnv(t)
	if got := env["ANTHROPIC_BASE_URL"]; got != "https://api.z.ai/api/anthropic" {
		t.Errorf("ANTHROPIC_BASE_URL = %q", got)
	}
	if got := env["ANTHROPIC_AUTH_TOKEN"]; got != fakeKey {
		t.Errorf("ANTHROPIC_AUTH_TOKEN = %q, want the configured key", got)
	}
	if got := env["ANTHROPIC_API_KEY"]; got != "" {
		t.Errorf("ANTHROPIC_API_KEY = %q, want the inherited one to be purged", got)
	}
	if got := env["ANTHROPIC_MODEL"]; got == "" {
		t.Error("ANTHROPIC_MODEL not set")
	}
	if args := sb.claudeArgs(t); len(args) != 2 || args[0] != "-p" || args[1] != "hello" {
		t.Errorf("claude args = %q, want [-p hello]", args)
	}
}

// The `or` gateway consumes exactly one argument, the alias, and forwards the rest.
func TestLauncherOpenRouterGateway(t *testing.T) {
	sb := newSandbox(t)
	sb.writeSecrets(t, map[string]string{"OPENROUTER_API_KEY": fakeKey})
	sb.writeConfig(t, `{"version":1,"openrouter_aliases":{"fast":"vendor/model-fast"}}`)

	code, err := Run(context.Background(), []string{"fast", "-p", "hello"}, filepath.Join(sb.binDir, "clother-or"))
	if err != nil || code != 0 {
		t.Fatalf("Run(clother-or fast) = %d, %v", code, err)
	}

	env := sb.claudeEnv(t)
	if got := env["ANTHROPIC_BASE_URL"]; got != "https://openrouter.ai/api" {
		t.Errorf("ANTHROPIC_BASE_URL = %q", got)
	}
	if got := env["ANTHROPIC_DEFAULT_OPUS_MODEL"]; got != "vendor/model-fast" {
		t.Errorf("alias model = %q", got)
	}
	if got := env["ANTHROPIC_AUTH_TOKEN"]; got != fakeKey {
		t.Errorf("ANTHROPIC_AUTH_TOKEN = %q", got)
	}
	if args := sb.claudeArgs(t); len(args) != 2 || args[0] != "-p" {
		t.Errorf("claude args = %q, want the alias consumed and the rest forwarded", args)
	}
}

// The `custom` gateway resolves a user-registered provider and refuses unknown ones.
func TestLauncherCustomGateway(t *testing.T) {
	sb := newSandbox(t)
	sb.writeSecrets(t, map[string]string{"MYPROV_API_KEY": fakeKey})
	sb.writeConfig(t, `{"version":1,"custom_providers":{"myprov":{"name":"myprov","display_name":"MyProv","base_url":"https://api.example.test/anthropic","api_key_env":"MYPROV_API_KEY","default_model":"my-model"}}}`)

	code, err := Run(context.Background(), []string{"myprov", "-p", "hello"}, filepath.Join(sb.binDir, "clother-custom"))
	if err != nil || code != 0 {
		t.Fatalf("Run(clother-custom myprov) = %d, %v", code, err)
	}
	env := sb.claudeEnv(t)
	if got := env["ANTHROPIC_BASE_URL"]; got != "https://api.example.test/anthropic" {
		t.Errorf("ANTHROPIC_BASE_URL = %q", got)
	}
	if got := env["ANTHROPIC_MODEL"]; got != "my-model" {
		t.Errorf("ANTHROPIC_MODEL = %q", got)
	}
}

func TestLauncherGatewayErrors(t *testing.T) {
	tests := []struct {
		name    string
		argv0   string
		args    []string
		wantErr string
	}{
		{name: "or without an alias", argv0: "clother-or"},
		{name: "or with a flag first", argv0: "clother-or", args: []string{"--yolo"}},
		{name: "custom without a name", argv0: "clother-custom"},
		{name: "custom with an unknown name", argv0: "clother-custom", args: []string{"nope"}, wantErr: "unknown custom provider"},
		{name: "unknown profile", argv0: "clother-nope", wantErr: "unknown profile"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			sb := newSandbox(t)
			var code int
			var err error
			capture(t, func() {
				code, err = Run(context.Background(), test.args, filepath.Join(sb.binDir, test.argv0))
			})
			if code == 0 {
				t.Fatalf("exit code = 0, want a failure (err=%v)", err)
			}
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("error = %v, want it to contain %q", err, test.wantErr)
				}
			}
			if _, statErr := os.Stat(filepath.Join(sb.dumpDir, "env")); statErr == nil {
				t.Fatal("claude was launched despite the gateway error")
			}
		})
	}
}

// argv[0] == claude falls through to the shim without any provider environment.
func TestClaudeShimForwardsWithoutProviderEnv(t *testing.T) {
	sb := newSandbox(t)

	code, err := Run(context.Background(), []string{"-p", "hello"}, filepath.Join(sb.binDir, "claude"))
	if err != nil || code != 0 {
		t.Fatalf("Run(claude) = %d, %v", code, err)
	}
	env := sb.claudeEnv(t)
	if got := env["ANTHROPIC_BASE_URL"]; got != "https://leftover.example" {
		t.Errorf("shim changed ANTHROPIC_BASE_URL to %q, want the inherited value", got)
	}
	if args := sb.claudeArgs(t); len(args) != 2 {
		t.Errorf("claude args = %q", args)
	}
}

// Regression: `clother bench --prompt "..."`, documented in the README and in
// --help, used to be rejected by the global parser with "unknown option".
func TestBenchPromptFlagIsAccepted(t *testing.T) {
	newSandbox(t)

	var code int
	var err error
	output := capture(t, func() {
		code, err = Run(context.Background(), []string{"bench", "--prompt", "Write a haiku"}, "clother")
	})
	if err != nil || code != 0 {
		t.Fatalf("clother bench --prompt = %d, %v (output: %s)", code, err, output)
	}
	if strings.Contains(output, "unknown option") {
		t.Fatalf("output still rejects the documented flag: %s", output)
	}
}

// Regression: `-h` after a command used to dump the global help.
func TestHelpAfterACommandIsAboutThatCommand(t *testing.T) {
	newSandbox(t)

	for _, args := range [][]string{{"-h", "status"}, {"status", "--help"}, {"config", "--help"}} {
		var code int
		var err error
		output := capture(t, func() {
			code, err = Run(context.Background(), args, "clother")
		})
		if err != nil || code != 0 {
			t.Fatalf("clother %v = %d, %v", args, code, err)
		}
		command := args[0]
		if command == "-h" {
			command = args[1]
		}
		if !strings.Contains(output, "clother "+command) {
			t.Errorf("help for %q does not name the command:\n%s", command, output)
		}
		if strings.Contains(output, "Providers:") {
			t.Errorf("help for %q dumped the global help:\n%s", command, output)
		}
	}
}

// An unreadable config file degrades Clother, it does not brick it.
func TestBrokenConfigDoesNotBrickTheCLI(t *testing.T) {
	sb := newSandbox(t)
	sb.writeConfig(t, "{ this is not json")

	for _, args := range [][]string{{"--help"}, {"list"}, {"status"}} {
		var code int
		var err error
		output := capture(t, func() {
			code, err = Run(context.Background(), args, "clother")
		})
		if err != nil || code != 0 {
			t.Fatalf("clother %v with a broken config = %d, %v (output: %s)", args, code, err, output)
		}
	}

	// But a command that would rewrite the file must refuse.
	var code int
	var err error
	capture(t, func() {
		code, err = Run(context.Background(), []string{"config", "zai", "--no-input"}, "clother")
	})
	if code == 0 || err == nil {
		t.Fatalf("clother config on a broken config = %d, %v; want a refusal", code, err)
	}
	if !strings.Contains(err.Error(), sb.paths.ConfigFile) {
		t.Errorf("refusal does not name the offending file: %v", err)
	}

	// `remove` rewrites config.json and secrets.env just like `config` does, so
	// it must refuse too. It was missing from commandsWritingConfig and would
	// have persisted the empty fallback, silently dropping every provider the
	// unreadable file still described.
	capture(t, func() {
		code, err = Run(context.Background(), []string{"remove", "zai", "--yes"}, "clother")
	})
	if code == 0 || err == nil {
		t.Fatalf("clother remove on a broken config = %d, %v; want a refusal", code, err)
	}
	if !strings.Contains(err.Error(), sb.paths.ConfigFile) {
		t.Errorf("refusal does not name the offending file: %v", err)
	}
}

// Regression: `clother update` delegates to runInstall, which writes config.json
// and secrets.env, but was absent from commandsWritingConfig — a table keyed by
// command name. On a corrupted config it overwrote every provider override,
// OpenRouter alias and custom provider with `{"version":1}` and exited 0. The
// guard now sits at the write itself, so the delegation cannot walk around it.
func TestUpdateRefusesToWriteOverAnUnreadableConfig(t *testing.T) {
	sb := newSandbox(t)
	t.Setenv("CLOTHER_SKIP_SELF_UPDATE", "1")
	const truncated = `{"version":1,"provider_overrides":{"zai":{"model":"glm-4.6"}},"openrouter_alias`
	sb.writeConfig(t, truncated)

	var code int
	var err error
	capture(t, func() {
		code, err = Run(context.Background(), []string{"update"}, "clother")
	})
	if code == 0 || err == nil {
		t.Fatalf("clother update on a broken config = %d, %v; want a refusal", code, err)
	}
	if !strings.Contains(err.Error(), sb.paths.ConfigFile) {
		t.Errorf("refusal does not name the offending file: %v", err)
	}

	got, readErr := os.ReadFile(sb.paths.ConfigFile)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != truncated {
		t.Fatalf("clother update rewrote the config it could not read:\n%s", got)
	}
}

// An unreadable secrets.env used to propagate out of New() and make every
// command exit 1 — `clother list`, `clother status` and even `clother help` —
// with nothing but "permission denied" and, in --json, an empty stdout.
func TestUnreadableSecretsDegradesInsteadOfBricking(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads a 0000 file regardless of its mode")
	}
	sb := newSandbox(t)
	sb.writeSecrets(t, map[string]string{"ZAI_API_KEY": fakeKey})
	if err := os.Chmod(sb.paths.SecretsFile, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(sb.paths.SecretsFile, 0o600) })

	for _, args := range [][]string{{"list"}, {"status"}, {"help"}} {
		var code int
		var err error
		output := capture(t, func() {
			code, err = Run(context.Background(), args, "clother")
		})
		if err != nil || code != 0 {
			t.Fatalf("clother %v with an unreadable secrets file = %d, %v (output: %s)", args, code, err, output)
		}
	}

	// The warning names the real next action, not "fix the JSON".
	output := capture(t, func() {
		_, _ = Run(context.Background(), []string{"list"}, "clother")
	})
	if !strings.Contains(output, "check the permissions of "+sb.paths.SecretsFile) {
		t.Errorf("the permission problem was not named as such:\n%s", output)
	}

	// And a command that would rewrite the file it could not read still refuses.
	var code int
	var err error
	capture(t, func() {
		code, err = Run(context.Background(), []string{"config", "zai", "--no-input"}, "clother")
	})
	if code == 0 || err == nil {
		t.Fatalf("clother config over an unreadable secrets file = %d, %v; want a refusal", code, err)
	}
}

// In --json every invocation must end with exactly one envelope on stdout, a
// failed startup included.
func TestUnreadableSecretsStillEmitsAnEnvelope(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads a 0000 file regardless of its mode")
	}
	sb := newSandbox(t)
	sb.writeSecrets(t, map[string]string{"ZAI_API_KEY": fakeKey})
	if err := os.Chmod(sb.paths.SecretsFile, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(sb.paths.SecretsFile, 0o600) })

	stdout, stderr := captureStreams(t, func() {
		_, _ = Run(context.Background(), []string{"--json", "list"}, "clother")
	})
	var envelope struct {
		SchemaVersion int  `json:"schema_version"`
		OK            bool `json:"ok"`
	}
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		t.Fatalf("stdout is not a JSON envelope: %v (stdout=%q stderr=%q)", err, stdout, stderr)
	}
	if envelope.SchemaVersion == 0 {
		t.Fatalf("envelope without a schema version: %q", stdout)
	}
	if !strings.Contains(stderr, "secrets file") {
		t.Errorf("the unreadable secrets file was not reported on stderr: %q", stderr)
	}
}

// The hint used to be hardcoded to "fix the JSON", which is the wrong action
// for a file whose JSON is perfectly valid and whose mode is 000.
func TestUnreadableConfigHintFollowsTheError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads a 0000 file regardless of its mode")
	}
	sb := newSandbox(t)
	sb.writeConfig(t, `{"version":1}`)
	if err := os.Chmod(sb.paths.ConfigFile, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(sb.paths.ConfigFile, 0o600) })

	output := capture(t, func() {
		_, _ = Run(context.Background(), []string{"list"}, "clother")
	})
	if strings.Contains(output, "fix the JSON") {
		t.Errorf("a permission error was reported as a syntax error:\n%s", output)
	}
	if !strings.Contains(output, "check the permissions of "+sb.paths.ConfigFile) {
		t.Errorf("the permission problem was not named as such:\n%s", output)
	}
}

func TestBrokenConfigReportsAnEnvelopeInJSONMode(t *testing.T) {
	sb := newSandbox(t)
	sb.writeConfig(t, "{ this is not json")

	var code int
	output := capture(t, func() {
		code, _ = Run(context.Background(), []string{"--json", "config", "zai"}, "clother")
	})
	if code == 0 {
		t.Fatal("exit code = 0, want a failure")
	}
	var envelope struct {
		SchemaVersion int  `json:"schema_version"`
		OK            bool `json:"ok"`
		Error         struct {
			Kind string `json:"kind"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(output), &envelope); err != nil {
		t.Fatalf("output is not a JSON envelope: %v (%s)", err, output)
	}
	if envelope.OK || envelope.Error.Kind != "config_invalid" {
		t.Fatalf("envelope = %+v", envelope)
	}
}

// No command may ever print a secret value, on stdout, on stderr, or in the
// error it returns.
func TestNoCommandLeaksASecret(t *testing.T) {
	sb := newSandbox(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `{"content":[{"type":"text","text":"ok"}]}`)
	}))
	defer server.Close()

	sb.writeSecrets(t, map[string]string{"LOCAL_API_KEY": fakeKey})
	sb.writeConfig(t, fmt.Sprintf(`{"version":1,"custom_providers":{"local":{"name":"local","display_name":"Local","base_url":%q,"api_key_env":"LOCAL_API_KEY","default_model":"local-model"}}}`, server.URL))

	invocations := [][]string{
		{},
		{"help"},
		{"--help"},
		{"list"},
		{"--json", "list"},
		{"--plain", "list"},
		{"info", "local"},
		{"--json", "info", "local"},
		{"status"},
		{"--json", "status"},
		{"test", "local"},
		{"bench", "--prompt", "hello"},
		{"config", "local", "--no-input"},
		{"install"},
		{"uninstall", "-y"},
		{"-d", "list"},
	}

	for _, args := range invocations {
		var err error
		output := capture(t, func() {
			_, err = Run(context.Background(), args, "clother")
		})
		if err != nil {
			output += "\n" + err.Error()
		}
		if strings.Contains(output, fakeKey) {
			t.Errorf("clother %v leaked the secret:\n%s", args, output)
		}
	}
}

// --debug traces the resolved target, the base URL and the environment, with the
// token masked.
func TestDebugTraceMasksTheToken(t *testing.T) {
	sb := newSandbox(t)
	sb.writeSecrets(t, map[string]string{"ZAI_API_KEY": fakeKey})
	t.Setenv("CLOTHER_DEBUG", "1")

	var code int
	var err error
	output := capture(t, func() {
		code, err = Run(context.Background(), []string{"-p", "hello"}, filepath.Join(sb.binDir, "clother-zai"))
	})
	if err != nil || code != 0 {
		t.Fatalf("Run(clother-zai) = %d, %v", code, err)
	}
	for _, needle := range []string{
		"target profile=\"zai\"",
		"base url=\"https://api.z.ai/api/anthropic\"",
		"env set ANTHROPIC_BASE_URL",
		"env purged ANTHROPIC_CUSTOM_HEADERS",
	} {
		if !strings.Contains(output, needle) {
			t.Errorf("debug trace misses %q:\n%s", needle, output)
		}
	}
	if strings.Contains(output, fakeKey) {
		t.Errorf("debug trace leaked the token:\n%s", output)
	}
	if !strings.Contains(output, config.MaskSecret(fakeKey)) {
		t.Errorf("debug trace does not show the masked token:\n%s", output)
	}
}

// --no-input must reach the prompter, which then fails instead of asking.
func TestNoInputIsWiredToThePrompter(t *testing.T) {
	newSandbox(t)

	for _, noInput := range []bool{true, false} {
		app, err := New(cli.Parsed{Options: cli.Options{Format: "human", NoInput: noInput}})
		if err != nil {
			t.Fatal(err)
		}
		if app.Prompt.NoInput != noInput {
			t.Fatalf("--no-input=%v produced Prompter.NoInput=%v", noInput, app.Prompt.NoInput)
		}
		if app.Output.Debug {
			t.Fatal("debug enabled without --debug")
		}
	}

	app, err := New(cli.Parsed{Options: cli.Options{Format: "human", Debug: true}})
	if err != nil {
		t.Fatal(err)
	}
	if !app.Output.Debug {
		t.Fatal("--debug did not reach the output")
	}

	// And end to end: a configuration that needs an answer must fail, not hang.
	var code int
	output := capture(t, func() {
		code, err = Run(context.Background(), []string{"config", "zai", "--no-input"}, "clother")
	})
	if code == 0 || err == nil {
		t.Fatalf("clother config --no-input = %d, %v; want a refusal (output: %s)", code, err, output)
	}
}
