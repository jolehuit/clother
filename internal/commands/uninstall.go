package commands

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jolehuit/clother/internal/launchers"
)

func runUninstall(_ context.Context, c Context) (int, error) {
	if !c.Options.Yes {
		ok, err := c.Prompt.Confirm("Remove all Clother files?", false)
		if err != nil {
			return 1, err
		}
		if !ok {
			return 0, nil
		}
	}
	manifest, _ := launchers.LoadManifest(c.Paths.ManifestFile)
	for _, name := range manifest.Launchers {
		_ = os.Remove(filepath.Join(c.Paths.BinDir, name))
	}
	_ = os.Remove(filepath.Join(c.Paths.BinDir, "claude"))
	_ = os.Remove(filepath.Join(c.Paths.BinDir, "clother"))
	_ = os.RemoveAll(c.Paths.ConfigDir)
	_ = os.RemoveAll(c.Paths.DataDir)
	_ = os.RemoveAll(c.Paths.CacheDir)
	fmt.Fprintln(c.Output.Stdout, "Clother uninstalled")
	return 0, nil
}
