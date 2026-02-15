package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	// Config file path
	cfgFile string
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "dorgu",
	Short: "AI-powered Kubernetes application onboarding",
	Long: `Dorgu analyzes your containerized applications and generates 
production-ready Kubernetes manifests, CI/CD pipelines, and documentation.

Examples:
  # Generate manifests for an application
  dorgu generate ./my-app

  # Generate with custom output directory
  dorgu generate ./my-app --output ./manifests

  # Initialize org standards config
  dorgu init`,
}

// Execute adds all child commands to the root command and sets flags appropriately.
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	cobra.OnInitialize(initConfig)

	// Global flags
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is .dorgu.yaml)")
	rootCmd.PersistentFlags().Bool("no-color", false, "disable colored output")

	// Bind to viper
	viper.BindPFlag("no-color", rootCmd.PersistentFlags().Lookup("no-color"))

	// Add subcommands
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(generateCmd)
	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(configCmd)
	rootCmd.AddCommand(personaCmd)
	rootCmd.AddCommand(clusterCmd)
	rootCmd.AddCommand(watchCmd)
	rootCmd.AddCommand(syncCmd)
}

// initConfig reads in config file and ENV variables if set.
func initConfig() {
	if cfgFile != "" {
		// Use config file from the flag
		viper.SetConfigFile(cfgFile)
	} else {
		// Search for config in current directory
		viper.AddConfigPath(".")
		viper.SetConfigType("yaml")
		viper.SetConfigName(".dorgu")
	}

	// Read in environment variables that match
	viper.SetEnvPrefix("DORGU")
	viper.AutomaticEnv()

	// If a config file is found, read it in
	if err := viper.ReadInConfig(); err == nil {
		fmt.Fprintln(os.Stderr, "Using config file:", viper.ConfigFileUsed())
	}
}
