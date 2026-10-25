package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/repocompanion/companion/internal/cli"
)

func main() {
	root := cli.NewRootCommand()
	if err := root.Execute(); err != nil {
		if !errors.Is(err, cli.ErrPending) {
			fmt.Fprintln(os.Stderr, "companion:", err)
		}
		os.Exit(cli.ExitCode(err))
	}
}
