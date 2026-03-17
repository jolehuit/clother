package cli

import (
	"fmt"
	"io"
	"sort"

	"github.com/jolehuit/clother/internal/providers"
	"github.com/jolehuit/clother/internal/version"
)

func ShowBrief(w io.Writer) {
	fmt.Fprintf(w, "Clother v%s - Multi-provider launcher for Claude CLI\n\n", version.Value)
	fmt.Fprintln(w, "Usage: clother [options] <command>")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Commands:")
	fmt.Fprintln(w, "  config       Configure a provider")
	fmt.Fprintln(w, "  list         List profiles")
	fmt.Fprintln(w, "  info         Provider details")
	fmt.Fprintln(w, "  test         Test providers")
	fmt.Fprintln(w, "  status       Show installation state")
	fmt.Fprintln(w, "  uninstall    Remove Clother")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Tip: add --yolo to a launcher command to skip permission prompts.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Run clother --help for full help.")
}

func ShowFull(w io.Writer, catalog providers.Catalog) {
	fmt.Fprintf(w, "Clother v%s\n", version.Value)
	fmt.Fprintln(w, "Multi-provider launcher for Claude CLI")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  clother [options] <command> [args]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Commands:")
	fmt.Fprintln(w, "  config [provider]")
	fmt.Fprintln(w, "  list")
	fmt.Fprintln(w, "  info <provider>")
	fmt.Fprintln(w, "  test [provider]")
	fmt.Fprintln(w, "  status")
	fmt.Fprintln(w, "  install")
	fmt.Fprintln(w, "  uninstall")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Options:")
	fmt.Fprintln(w, "  -h, --help")
	fmt.Fprintln(w, "  -V, --version")
	fmt.Fprintln(w, "  -v, --verbose")
	fmt.Fprintln(w, "  -d, --debug")
	fmt.Fprintln(w, "  -q, --quiet")
	fmt.Fprintln(w, "  -y, --yes")
	fmt.Fprintln(w, "  --bin-dir <path>")
	fmt.Fprintln(w, "  --no-input")
	fmt.Fprintln(w, "  --no-banner")
	fmt.Fprintln(w, "  --json")
	fmt.Fprintln(w, "  --plain")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Launcher tips:")
	fmt.Fprintln(w, "  clother-zai --yolo       skip permission prompts")
	fmt.Fprintln(w, "  claude --yolo            same behavior via the Clother shim")
	fmt.Fprintln(w, "  --yolo                   shorthand for --dangerously-skip-permissions")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Providers:")
	for _, category := range catalog.Categories() {
		fmt.Fprintf(w, "  %s\n", category)
		providersInCategory := catalog.ProvidersByCategory(category)
		sort.SliceStable(providersInCategory, func(i, j int) bool {
			return providersInCategory[i].ID < providersInCategory[j].ID
		})
		for _, provider := range providersInCategory {
			fmt.Fprintf(w, "    %-12s %s\n", provider.ID, provider.Description)
		}
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Advanced:")
	fmt.Fprintln(w, "    openrouter   100+ models via native API")
	fmt.Fprintln(w, "    custom       Anthropic-compatible endpoint")
}
