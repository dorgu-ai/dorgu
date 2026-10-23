package cli

import (
	"errors"

	"github.com/spf13/cobra"

	"github.com/dorgu-ai/dorgu/internal/output"
)

// errSilent is returned by command handlers that have already printed their own
// formatted error (e.g. via output.ErrorWithHint). Execute() will propagate the
// non-zero exit but will not double-print the message.
var errSilent = errors.New("")

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
	// Global flags
	rootCmd.PersistentFlags().Bool("no-color", false, "disable colored output")
	rootCmd.PersistentFlags().Bool("json", false, "output in JSON format (for scripting and agents)")

	// Add subcommands
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(personaCmd)
	rootCmd.AddCommand(clusterCmd)
	rootCmd.AddCommand(newHealthCmd())
	rootCmd.AddCommand(newIncidentsCmd())
	rootCmd.AddCommand(newRemediationCmd())
}
