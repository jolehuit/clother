package commands

import (
	"context"

	"github.com/jolehuit/clother/internal/config"
	"github.com/jolehuit/clother/internal/profiles"
	"github.com/jolehuit/clother/internal/runtime"
)

func RunLauncher(ctx context.Context, paths config.Paths, secrets config.Secrets, target profiles.Target, args []string, noBanner bool) (int, error) {
	env, err := runtime.BuildEnv(target, secrets)
	if err != nil {
		// `clother-zai --help` used to die on "ZAI_API_KEY not configured — run
		// `clother config zai`", which is the one message that hides the help
		// explaining how to configure the key. Help and version make no API
		// call, so they are served without a credential.
		if !runtime.IsInfoOnlyInvocation(args) {
			return 1, err
		}
		env, err = runtime.BuildEnvWithoutCredential(target)
		if err != nil {
			return 1, err
		}
	}
	return runtime.Launch(ctx, paths, target, args, env, runtime.RunOptions{NoBanner: noBanner})
}
