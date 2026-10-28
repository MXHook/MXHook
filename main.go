package main

import (
	"os"

	"github.com/mxhook/mxhook/cli"
)

func main() {
	if err := cli.Root.Execute(); err != nil {
		os.Exit(1)
	}
}
