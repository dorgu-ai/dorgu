package main

import (
	"os"

	"github.com/dorgu-ai/dorgu/internal/cli"
)

// Build-time variables (set via ldflags)
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	// Set version info for CLI
	cli.SetVersionInfo(version, commit, date)

	// Execute CLI
	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}
