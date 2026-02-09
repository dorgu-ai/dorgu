package cli

import (
	"fmt"
	"os"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	// Styles for terminal output
	successStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))  // Green
	errorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("196")) // Red
	warnStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("214")) // Yellow
	infoStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("39"))  // Blue

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

// Helper functions for styled output
func printSuccess(msg string) {
	fmt.Println(successStyle.Render("✓ " + msg))
}

func printError(msg string) {
	fmt.Println(errorStyle.Render("✗ " + msg))
}

func printWarn(msg string) {
	fmt.Println(warnStyle.Render("⚠ " + msg))
}

func printInfo(msg string) {
	fmt.Println(infoStyle.Render("ℹ " + msg))
}
