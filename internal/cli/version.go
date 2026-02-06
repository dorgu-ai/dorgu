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
		fmt.Printf("dorgu %s\n", versionInfo.Version)
		fmt.Printf("Commit: %s\n", versionInfo.Commit)
		fmt.Printf("Built: %s\n", versionInfo.Date)
	},
}

// versionCmd is added to rootCmd in root.go init()
