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
	Short:         "Open-source AI SRE for Kubernetes",
	Long: `Dorgu detects what is wrong in your cluster, diagnoses the root cause,
proposes a reviewable fix, and heals the workload once you approve it.

The operator watches the cluster and writes proposals. This CLI is how you read
them and apply them: the workload change is made here, with your credentials,
and nothing is applied without your approval.

The self-healing loop:
  # What is wrong right now
  dorgu health

  # What was detected, and why
  dorgu incidents list
  dorgu incidents describe <incident> -n <namespace>

  # Read the proposed fix, then approve or reject it
  dorgu remediation list
  dorgu remediation diff <remediation> -n <namespace>
  dorgu remediation approve <remediation> -n <namespace>

Dorgu only watches workloads that have an ApplicationPersona. On a cluster that
already has apps, create them from the running Deployments:
  dorgu persona import -n <namespace> --all

Manifest generation is also here, as a secondary capability. It analyzes an
app's Dockerfile, Compose file and source, and emits Kubernetes manifests,
ArgoCD config, CI workflows and a persona:
  dorgu generate ./my-app

Requires kubectl on your PATH for every cluster command. Set DORGU_DEBUG=1 to
see kubectl's raw output in error messages.`,
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
