package commands

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jolehuit/clother/internal/config"
	"github.com/jolehuit/clother/internal/providers"
	"github.com/jolehuit/clother/internal/ui"
)

// sandboxContext points every Clother directory inside t.TempDir() so no test
// ever touches the real HOME.
func sandboxContext(t *testing.T) (Context, string) {
	t.Helper()

	root := t.TempDir()
	home := filepath.Join(root, "home")
	binDir := filepath.Join(root, "bin")

	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("CLOTHER_BIN", binDir)
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}

	paths, err := config.Detect("")
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := providers.Load()
	if err != nil {
		t.Fatal(err)
	}
	return Context{
		Paths:   paths,
		Config:  &config.File{Version: 1, ProviderOverrides: map[string]config.ProviderOverride{}, OpenRouterAliases: map[string]string{}, CustomProviders: map[string]config.CustomProvider{}},
		Secrets: config.Secrets{},
		Catalog: catalog,
		Output:  &ui.Output{Stdout: io.Discard, Stderr: io.Discard, Format: ui.FormatHuman},
	}, binDir
}

// A failed download used to be degraded into a warning, after which install
// still printed "OK installed Clother v3.0.10" and exited 0. That is what makes
// a frozen or hijacked update channel indistinguishable from a real upgrade.
func TestRunInstallFailsWhenTheUpdateCouldNotBeDownloaded(t *testing.T) {
	c, binDir := sandboxContext(t)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	var stdout bytes.Buffer
	c.Output = &ui.Output{Stdout: &stdout, Stderr: io.Discard, Format: ui.FormatHuman}

	downloadFailure := errors.New("release metadata unreachable")
	original := downloadLatestBinary
	downloadLatestBinary = func(_ context.Context, _ string) (string, string, func(), error) {
		return "", "", nil, downloadFailure
	}
	t.Cleanup(func() { downloadLatestBinary = original })

	code, err := runInstall(context.Background(), c)
	if code == 0 {
		t.Fatalf("a failed update must not exit 0, got code %d", code)
	}
	if err == nil {
		t.Fatal("a failed update must report an error")
	}
	if !errors.Is(err, downloadFailure) {
		t.Fatalf("the underlying failure should survive, got %v", err)
	}
	if strings.Contains(stdout.String(), "installed Clother") {
		t.Fatalf("no success line may be printed when nothing was installed:\n%s", stdout.String())
	}
}

// `clother help bench` is advertised in the same table as `clother bench
// --help`; it used to print the 60-line global help instead.
func TestHelpWithACommandNameShowsThatCommandsHelp(t *testing.T) {
	c, _ := sandboxContext(t)
	var stdout bytes.Buffer
	c.Output = &ui.Output{Stdout: &stdout, Stderr: io.Discard, Format: ui.FormatHuman}

	code, err := Dispatch(context.Background(), c, "help", []string{"bench"})
	if err != nil || code != 0 {
		t.Fatalf("help bench = (%d, %v)", code, err)
	}
	out := stdout.String()
	if !strings.Contains(out, "bench") {
		t.Fatalf("help bench should describe bench:\n%s", out)
	}
	// The global help lists every command; the per-command help does not.
	if strings.Contains(out, "uninstall") {
		t.Fatalf("help bench dumped the global help instead of the command help:\n%s", out)
	}
}

func TestHelpWithAnUnknownCommandFails(t *testing.T) {
	c, _ := sandboxContext(t)
	c.Output = &ui.Output{Stdout: io.Discard, Stderr: io.Discard, Format: ui.FormatHuman}

	code, err := Dispatch(context.Background(), c, "help", []string{"nope"})
	if code == 0 || err == nil {
		t.Fatalf("help nope = (%d, %v), want a non-zero code and an error", code, err)
	}
}

func TestHelpWithoutArgumentStillShowsTheGlobalHelp(t *testing.T) {
	c, _ := sandboxContext(t)
	var stdout bytes.Buffer
	c.Output = &ui.Output{Stdout: &stdout, Stderr: io.Discard, Format: ui.FormatHuman}

	code, err := Dispatch(context.Background(), c, "help", nil)
	if err != nil || code != 0 {
		t.Fatalf("help = (%d, %v)", code, err)
	}
	if !strings.Contains(stdout.String(), "uninstall") {
		t.Fatalf("the global help should list every command:\n%s", stdout.String())
	}
}

// The catalog carries warnings that cost money when ignored (Alibaba's sk-sp-
// key, VolcEngine's /api/v3 path). Nothing displayed them.
func TestInfoShowsTheCatalogSetupNotesAndDocURL(t *testing.T) {
	c, _ := sandboxContext(t)
	var stdout bytes.Buffer
	c.Output = &ui.Output{Stdout: &stdout, Stderr: io.Discard, Format: ui.FormatHuman}

	provider, ok := c.Catalog.Get("alibaba")
	if !ok {
		t.Skip("catalog no longer carries the alibaba entry")
	}
	if len(provider.Setup) == 0 && provider.DocURL == "" {
		t.Skip("alibaba entry carries neither setup notes nor a doc URL")
	}

	code, err := runInfo(context.Background(), c, []string{"alibaba"})
	if err != nil || code != 0 {
		t.Fatalf("info alibaba = (%d, %v)", code, err)
	}
	out := stdout.String()
	if provider.DocURL != "" && !strings.Contains(out, provider.DocURL) {
		t.Fatalf("info should print the doc URL %q:\n%s", provider.DocURL, out)
	}
	for _, line := range provider.Setup {
		if !strings.Contains(out, line) {
			t.Fatalf("info dropped the setup note %q:\n%s", line, out)
		}
	}
}
