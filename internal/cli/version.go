package cli

import (
	"fmt"
	"runtime/debug"
	"strings"

	"github.com/spf13/cobra"
)

var (
	// Version info set at build time
	versionInfo struct {
		Version string
		Commit  string
		Date    string
	}
)

// SetVersionInfo sets the version information (called from main)
func SetVersionInfo(version, commit, date string) {
	versionInfo.Version = version
	versionInfo.Commit = commit
	versionInfo.Date = date
}

// effectiveVersion returns the version to display. When built with ldflags (e.g. GoReleaser),
// versionInfo.Version is set. When built via "go install ...@v0.2.1", ldflags are not passed
// but the Go toolchain embeds the module version in build info; we use that as fallback.
func effectiveVersion() string {
	v := versionInfo.Version
	if v != "" && v != "dev" {
		return v
	}
	info, ok := debug.ReadBuildInfo()
	if !ok || info.Main.Version == "" || info.Main.Version == "(devel)" {
		return "dev"
	}
	// Module version can be "v0.2.1" or a pseudo-version; use as-is if it looks like a tag
	if strings.HasPrefix(info.Main.Version, "v") && len(info.Main.Version) > 1 {
		return info.Main.Version
	}
	return "dev"
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version number of dorgu",
	Long:  `Display the version, commit hash, and build date of dorgu.`,
	Run: func(cmd *cobra.Command, args []string) {
		version := effectiveVersion()
		if version == "dev" {
			fmt.Println("dorgu dev")
		} else {
			displayVersion := version
			if version[0] != 'v' {
				displayVersion = "v" + version
			}
			fmt.Printf("dorgu version %s\n", displayVersion)
		}
		commit, date := versionInfo.Commit, versionInfo.Date
		if commit != "" && commit != "none" && date != "" && date != "unknown" {
			fmt.Printf("Build: %s @ %s\n", commit, date)
		} else {
			fmt.Println("Build: from source")
		}
	},
}

// versionCmd is added to rootCmd in root.go init()
