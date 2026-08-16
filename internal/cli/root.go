package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/dorgu-ai/dorgu/internal/output"
)

// errSilent is returned by command handlers that have already printed their own
// formatted error (e.g. via output.ErrorWithHint). Execute() will propagate the
// non-zero exit but will not double-print the message.
var errSilent = errors.New("")

var (
	// Config file path
	cfgFile string
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:           "dorgu",
	SilenceErrors: true,
	Short:         "AI-powered Kubernetes application onboarding",
	Long: `Dorgu analyzes your containerized applications and generates
production-ready Kubernetes manifests, CI/CD pipelines, and documentation.

Examples:
  # Generate manifests for an application
  dorgu generate ./my-app

  # Generate with custom output directory
  dorgu generate ./my-app --output ./manifests

  # Initialize org standards config
  dorgu init`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// Flag parsing and argument validation both happen before this hook, so
		// they keep their usage output. Anything that fails from here on is a
		// runtime failure, where 15 lines of manual bury the one line that says
		// what went wrong (F-13).
		cmd.SilenceUsage = true

		jsonFlag, _ := cmd.Flags().GetBool("json")
		noColor, _ := cmd.Flags().GetBool("no-color")
		output.Init(jsonFlag, noColor)
		return nil
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
func Execute() error {
	err := rootCmd.Execute()
	if err != nil && !errors.Is(err, errSilent) {
		output.Error(err.Error())
	}
	return err
}

func init() {
	cobra.OnInitialize(initConfig)

	// Global flags
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is .dorgu.yaml)")
	rootCmd.PersistentFlags().Bool("no-color", false, "disable colored output")
	rootCmd.PersistentFlags().Bool("json", false, "output in JSON format (for scripting and agents)")

	// Bind to viper
	_ = viper.BindPFlag("no-color", rootCmd.PersistentFlags().Lookup("no-color"))
	_ = viper.BindPFlag("json", rootCmd.PersistentFlags().Lookup("json"))

	// Add subcommands
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(generateCmd)
	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(configCmd)
	rootCmd.AddCommand(personaCmd)
	rootCmd.AddCommand(clusterCmd)
	rootCmd.AddCommand(watchCmd)
	rootCmd.AddCommand(syncCmd)
	rootCmd.AddCommand(platformCmd)
	rootCmd.AddCommand(newHealthCmd())
	rootCmd.AddCommand(newIncidentsCmd())
	rootCmd.AddCommand(newRemediationCmd())
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
