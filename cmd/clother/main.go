package main

import (
	"context"
	"fmt"
	"os"
	goruntime "runtime"

	"github.com/jolehuit/clother/internal/app"
)

func main() {
	if !platformSupported {
		fmt.Fprintf(os.Stderr,
			"clother: unsupported platform %s/%s.\n"+
				"Clother only runs on macOS and Linux: it installs launchers as symlinks and\n"+
				"reads API keys from /dev/tty with stty, none of which exist elsewhere.\n",
			goruntime.GOOS, goruntime.GOARCH)
		os.Exit(2)
	}

	code, err := app.Run(context.Background(), os.Args[1:], os.Args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
	if err != nil && code == 0 {
		code = 1
	}
	// os.Exit truncates to 8 bits, so a stray -1 or 256 would surface as 255 or
	// 0 — the latter turning a failure into a success for every caller.
	if code < 0 || code > 255 {
		code = 1
	}
	os.Exit(code)
}
