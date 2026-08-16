package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/jolehuit/clother/internal/providers"
)

func loadCatalog(t *testing.T) providers.Catalog {
	t.Helper()
	catalog, err := providers.Load()
	if err != nil {
		t.Fatalf("providers.Load(): %v", err)
	}
	return catalog
}

// The two help screens are generated from the same table, so a command can no
// longer be listed in one and missing from the other.
func TestHelpScreensListEveryCommand(t *testing.T) {
	catalog := loadCatalog(t)

	var brief, full bytes.Buffer
	ShowBrief(&brief)
	ShowFull(&full, catalog)

	for _, command := range Commands() {
		if !strings.Contains(brief.String(), command.Name) {
			t.Errorf("ShowBrief does not mention %q", command.Name)
		}
		if !strings.Contains(full.String(), command.Name) {
			t.Errorf("ShowFull does not mention %q", command.Name)
		}
	}
	for _, name := range []string{"install", "help"} {
		if _, ok := CommandByName(name); !ok {
			t.Errorf("command %q missing from the table", name)
		}
	}
}

func TestShowCommandIsAboutThatCommand(t *testing.T) {
	catalog := loadCatalog(t)

	var out bytes.Buffer
	if !ShowCommand(&out, "bench", catalog) {
		t.Fatal("ShowCommand(bench) reported an unknown command")
	}
	text := out.String()
	if !strings.Contains(text, "clother bench") {
		t.Errorf("bench help does not name the command:\n%s", text)
	}
	if !strings.Contains(text, "--prompt") {
		t.Errorf("bench help does not document --prompt:\n%s", text)
	}
	// The whole point of the finding: a per-command page, not the global dump.
	if strings.Contains(text, "Providers:") {
		t.Errorf("bench help dumped the global provider catalog:\n%s", text)
	}

	var unknown bytes.Buffer
	if ShowCommand(&unknown, "nope", catalog) {
		t.Error("ShowCommand accepted an unknown command")
	}
	if unknown.Len() != 0 {
		t.Errorf("ShowCommand wrote %q for an unknown command", unknown.String())
	}
}

func TestShowFullDocumentsOptionsGatewaysAndEnvironment(t *testing.T) {
	catalog := loadCatalog(t)

	var out bytes.Buffer
	ShowFull(&out, catalog)
	text := out.String()

	for _, needle := range []string{
		"--no-input", "--debug", "--verbose", "--bin-dir",
		"clother-or <alias>", "clother-custom <name>",
		"CLOTHER_CONFIG_DIR", "CLOTHER_DATA_DIR", "CLOTHER_CACHE_DIR", "CLOTHER_BIN",
		"CLOTHER_NO_UPDATE_CHECK", "CLOTHER_UPDATE_URL", "NO_COLOR",
	} {
		if !strings.Contains(text, needle) {
			t.Errorf("ShowFull does not document %q", needle)
		}
	}

	// Every option must carry a description, not just a flag name.
	for _, option := range globalOptions {
		if strings.TrimSpace(option.Description) == "" {
			t.Errorf("option %q has no description", option.Flags)
		}
	}
}
