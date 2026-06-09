package main

import (
	"fmt"
	"os"

	"mewcode/internal/app"
	"mewcode/internal/cli"
)

func main() {
	root := cli.NewRootCommand(app.New())
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
