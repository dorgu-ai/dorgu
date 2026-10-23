package cli

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

var personaFlags struct {
	namespace string
}

var personaCmd = &cobra.Command{
	Use: "persona",
	// Reject a stray subcommand instead of printing help and exiting 0 (F-12).
	Args:  noUnknownSubcommand,
	RunE:  runSubcommandGroup,
	Short: "Manage ApplicationPersona CRDs",
	Long: `Import and inspect ApplicationPersona Custom Resources for the
applications running on your cluster. Dorgu only watches workloads that have
an ApplicationPersona.

Examples:
  # Import personas for apps already running in the cluster
  dorgu persona import -n apps --all

  # List personas
  dorgu persona list -n apps

  # Check persona status on cluster
  dorgu persona status order-service -n commerce`,
}

var personaStatusCmd = &cobra.Command{
	Use:   "status [name]",
	Short: "Display the status of an ApplicationPersona on the cluster",
	Long: `Retrieve and display the current status of an ApplicationPersona
from the Kubernetes cluster, including validation results, health status,
learned patterns, and recommendations.

The name must be the Kubernetes resource name (DNS-safe). If your app directory
has underscores (e.g. sample_app_go_net_http), the cluster resource name is
sanitized to use hyphens (e.g. sample-app-go-net-http). You can pass either;
the CLI will try the sanitized form if the name contains underscores and the
first lookup fails.

Examples:
  dorgu persona status order-service -n commerce
  dorgu persona status sample-app-go-net-http -n default`,
	Args: cobra.ExactArgs(1),
	RunE: runPersonaStatus,
}

func init() {
	// Status flags
	personaStatusCmd.Flags().StringVarP(&personaFlags.namespace, "namespace", "n", "default", "Kubernetes namespace")

	// Register subcommands
	personaCmd.AddCommand(personaStatusCmd)
	personaCmd.AddCommand(personaListCmd)
	personaCmd.AddCommand(newPersonaImportCmd())
}

func runPersonaStatus(cmd *cobra.Command, args []string) error {
	name := args[0]

	// Check kubectl availability
	if _, err := exec.LookPath("kubectl"); err != nil {
		return fmt.Errorf("kubectl not found in PATH; required for persona status")
	}

	// Get the persona resource (Kubernetes resource name is DNS-safe; try sanitized if name has underscores)
	getCtx, getCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer getCancel()
	tryGet := func(n string) ([]byte, error) {
		kubectlCmd := exec.CommandContext(getCtx, "kubectl", "get", "applicationpersona", n,
			"-n", personaFlags.namespace, "-o", "yaml")
		return kubectlCmd.CombinedOutput()
	}
	rawOutput, err := tryGet(name)
	if err != nil {
		outputStr := kubectlErrText(rawOutput)
		if strings.Contains(outputStr, "not found") && strings.Contains(name, "_") {
			// App names with underscores are sanitized to hyphens in the cluster
			sanitized := strings.ReplaceAll(strings.ToLower(name), "_", "-")
			rawOutput, err = tryGet(sanitized)
			if err == nil {
				name = sanitized
			}
		}
	}
	if err != nil {
		outputStr := kubectlErrText(rawOutput)
		if strings.Contains(outputStr, "not found") {
			return fmt.Errorf("ApplicationPersona '%s' not found in namespace '%s' (hint: use DNS-safe name, e.g. sample-app-go-net-http)", name, personaFlags.namespace)
		}
		if strings.Contains(outputStr, "the server doesn't have a resource type") {
			return fmt.Errorf("ApplicationPersona CRD is not installed on this cluster. Install the Dorgu Operator first")
		}
		return fmt.Errorf("failed to get persona: %s", outputStr)
	}

	// Parse and display in a human-friendly format
	displayPersonaStatus(name, string(rawOutput))
	return nil
}
