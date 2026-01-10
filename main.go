package main

import (
	"os"

	"github.com/newhook/autoclaude/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
