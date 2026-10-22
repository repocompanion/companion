package main

import (
	"fmt"
	"os"

	"github.com/bolacha/docsync/internal/cli"
)

func main() {
	root := cli.NewRootCommand()
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "docsync:", err)
		os.Exit(1)
	}
}
