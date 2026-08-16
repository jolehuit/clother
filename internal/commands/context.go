package commands

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"

	"github.com/jolehuit/clother/internal/cli"
	"github.com/jolehuit/clother/internal/config"
	"github.com/jolehuit/clother/internal/providers"
	"github.com/jolehuit/clother/internal/ui"
)

type Context struct {
	Paths   config.Paths
	Config  *config.File
	Secrets config.Secrets
	Catalog providers.Catalog
	Output  *ui.Output
	Prompt  *ui.Prompter
	Options cli.Options

	// ConfigErr and SecretsErr carry the failure of the corresponding disk load.
	// Both files degrade the CLI instead of bricking it, so every command runs
	// with an empty fallback — which is precisely why no command may write that
	// fallback back over the file it failed to read.
	ConfigErr  error
	SecretsErr error

	// ConfigBaseline and SecretsBaseline are the state as it was read from disk
	// at startup. The save path replays the baseline -> in-memory diff onto a
	// fresh read, so two concurrent commands no longer erase each other.
	ConfigBaseline  *config.File
	SecretsBaseline config.Secrets
}

// refuseUnreadableState is the guard every write path calls before touching
// config.json or secrets.env.
//
// It used to live in internal/app, keyed by command NAME. `update` was not in
// that table and delegates to runInstall, which writes both files: on a
// corrupted config.json `clother update` persisted the empty fallback over the
// user's providers and exited 0. The guard belongs where the write happens, so
// no present or future delegation can walk around it.
func (c Context) refuseUnreadableState() error {
	if c.ConfigErr != nil {
		return fmt.Errorf("config file %s cannot be read: %w — %s",
			c.Paths.ConfigFile, c.ConfigErr, UnreadableStateHint(c.Paths.ConfigFile, c.ConfigErr))
	}
	if c.SecretsErr != nil {
		return fmt.Errorf("secrets file %s cannot be read: %w — %s",
			c.Paths.SecretsFile, c.SecretsErr, UnreadableStateHint(c.Paths.SecretsFile, c.SecretsErr))
	}
	return nil
}

// UnreadableStateHint is the next action to suggest for a state file that could
// not be read.
//
// A permission error is not a syntax error: the file the user was told to "fix
// the JSON" of was perfectly valid JSON with mode 000, and following the advice
// meant hunting for a typo that does not exist.
func UnreadableStateHint(path string, err error) string {
	if errors.Is(err, fs.ErrPermission) {
		return fmt.Sprintf("check the permissions of %s (it must be readable by your user)", path)
	}
	if filepath.Ext(path) == ".json" {
		return fmt.Sprintf("fix the JSON in %s, or move it aside to start fresh", path)
	}
	return fmt.Sprintf("fix %s, or move it aside to start fresh", path)
}
