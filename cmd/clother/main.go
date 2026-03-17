package main

import (
	"context"
	"fmt"
	"os"

	"github.com/jolehuit/clother/internal/app"
)

func main() {
	code, err := app.Run(context.Background(), os.Args[1:], os.Args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
	if err != nil && code == 0 {
		code = 1
	}
	os.Exit(code)
}
