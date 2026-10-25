package main

import (
	"fmt"
	"os"

	"github.com/repocompanion/companion/internal/cli"
)

func main() {
	root := cli.NewRootCommand()
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "companion:", err)
		os.Exit(1)
	}
}
