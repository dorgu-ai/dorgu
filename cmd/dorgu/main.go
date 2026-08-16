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

	// Execute CLI. The exit code is part of the contract with scripts, so it is
	// derived from the error rather than always being 1 (see internal/cli/exit.go).
	if err := cli.Execute(); err != nil {
		os.Exit(cli.ExitCode(err))
	}
}
