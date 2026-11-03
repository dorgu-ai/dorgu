package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/dorgu-ai/dorgu/internal/output"
)

var clusterFlags struct {
	name       string
	kubeconfig string
}

var clusterCmd = &cobra.Command{
	Use: "cluster",
	// Reject a stray subcommand instead of printing help and exiting 0 (F-12).
	Args:  noUnknownSubcommand,
	RunE:  runSubcommandGroup,
	Short: "Manage ClusterPersona CRDs",
	Long: `View and manage ClusterPersona Custom Resources that represent
your Kubernetes cluster's identity and configuration.

Examples:
  # View cluster persona status
  dorgu cluster status

The operator creates the ClusterPersona on startup; nothing here writes to the
cluster.`,
}

var clusterStatusCmd = &cobra.Command{
	Use:   "status [name]",
	Short: "Display the status of the ClusterPersona",
	Long: `Retrieve and display the current status of the ClusterPersona
from the Kubernetes cluster, including node information, resource usage,
discovered add-ons, and application count.

Examples:
  dorgu cluster status
  dorgu cluster status my-cluster`,
	Args: cobra.MaximumNArgs(1),
	RunE: runClusterStatus,
}

func init() {
	// Status flags (name is optional, will list all if not provided)
	clusterStatusCmd.Flags().StringVarP(&clusterFlags.name, "name", "n", "", "ClusterPersona name (optional)")
	clusterStatusCmd.Flags().StringVar(&clusterFlags.kubeconfig, "kubeconfig", "", "path to kubeconfig (default: ~/.kube/config)")

	// Register subcommands
	clusterCmd.AddCommand(clusterStatusCmd)
}

func runClusterStatus(cmd *cobra.Command, args []string) error {
	// Check kubectl availability
	if _, err := exec.LookPath("kubectl"); err != nil {
		return fmt.Errorf("kubectl not found in PATH; required for cluster status")
	}

	var name string
	if len(args) > 0 {
		name = args[0]
	} else if clusterFlags.name != "" {
		name = clusterFlags.name
	}

	kubeconfig, err := validateKubeconfig(clusterFlags.kubeconfig)
	if err != nil {
		return err
	}

	if name == "" {
		// List all ClusterPersonas
		return listClusterPersonas(kubeconfig)
	}

	// Get specific ClusterPersona
	return getClusterPersonaStatus(kubeconfig, name)
}

func listClusterPersonas(kubeconfig string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	rawOutput, err := kubectlCmd(ctx, kubeconfig, "get", "clusterpersona", "-o", "json").CombinedOutput()
	if err != nil {
		outputStr := kubectlErrText(rawOutput)
		if strings.Contains(outputStr, "the server doesn't have a resource type") {
			return fmt.Errorf("ClusterPersona CRD is not installed on this cluster. Install the Dorgu Operator first: %s", operatorInstallCommand)
		}
		return fmt.Errorf("failed to list cluster personas: %s", outputStr)
	}

	var list clusterPersonaList
	if err := json.Unmarshal(rawOutput, &list); err != nil {
		// Fallback: dump raw output
		fmt.Println(string(rawOutput))
		return nil
	}

	if len(list.Items) == 0 {
		if output.IsJSON() {
			fmt.Println(string(rawOutput))
			return nil
		}
		output.Info("No ClusterPersona resources found. The Dorgu Operator creates one on startup; check that it is running.")
		return nil
	}

	if output.IsJSON() {
		fmt.Println(string(rawOutput))
		return nil
	}

	fmt.Println()
	fmt.Printf("  %-18s %-14s %-14s %-7s %-6s %s\n",
		"NAME", "ENVIRONMENT", "PHASE", "NODES", "APPS", "AGE")
	fmt.Printf("  %s\n", strings.Repeat("─", 70))

	for _, item := range list.Items {
		phaseStr := clusterPhaseDot(item.Status.Phase)
		// Pad phase column to consistent visual width (use lipgloss Width for ANSI-safe padding)
		phasePadded := lipgloss.NewStyle().Width(14).Render(phaseStr)
		env := item.Spec.Environment
		if env == "" {
			env = "-"
		}
		age := formatAge(item.Metadata.CreationTimestamp)

		fmt.Printf("  %-18s %-14s %s %-7d %-6d %s\n",
			item.Metadata.Name,
			env,
			phasePadded,
			len(item.Status.Nodes),
			item.Status.ApplicationCount,
			age)
	}
	fmt.Println()
	return nil
}

func getClusterPersonaStatus(kubeconfig, name string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	outputFormat := "yaml"
	if output.IsJSON() {
		outputFormat = "json"
	}

	rawOutput, err := kubectlCmd(ctx, kubeconfig, "get", "clusterpersona", name, "-o", outputFormat).CombinedOutput()
	if err != nil {
		outputStr := kubectlErrText(rawOutput)
		if strings.Contains(outputStr, "not found") {
			output.ErrorWithHint("ClusterPersona not found: "+name,
				"List available ClusterPersonas: dorgu cluster status")
			return errSilent
		}
		if strings.Contains(outputStr, "the server doesn't have a resource type") {
			return fmt.Errorf("ClusterPersona CRD is not installed on this cluster. Install the Dorgu Operator first: %s", operatorInstallCommand)
		}
		return fmt.Errorf("failed to get cluster persona: %s", outputStr)
	}

	if output.IsJSON() {
		fmt.Println(string(rawOutput))
		return nil
	}

	displayClusterPersonaStatus(name, string(rawOutput))
	return nil
}
