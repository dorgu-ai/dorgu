package cli

import (
	"fmt"

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

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version number of dorgu",
	Long:  `Display the version, commit hash, and build date of dorgu.`,
	Run: func(cmd *cobra.Command, args []string) {
		version := versionInfo.Version
		if version == "dev" {
			fmt.Println("dorgu dev")
		} else {
			displayVersion := version
			if version != "" && version[0] != 'v' {
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
