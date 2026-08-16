package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/dorgu-ai/dorgu/internal/output"
)

var validDNSName = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{0,252}$`)

var clusterFlags struct {
	name        string
	environment string
	dryRun      bool
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

  # Initialize a new cluster persona
  dorgu cluster init --name my-cluster --environment production

  # Install a curated production-ready stack
  dorgu cluster setup`,
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

var clusterInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize a ClusterPersona for the current cluster",
	Long: `Create a ClusterPersona CRD for the current Kubernetes cluster.
This establishes the cluster's identity and allows the Dorgu Operator
to discover and track cluster state.

Examples:
  dorgu cluster init --name production-cluster --environment production
  dorgu cluster init --name dev-cluster --environment development --dry-run`,
	RunE: runClusterInit,
}

func init() {
	// Status flags (name is optional, will list all if not provided)
	clusterStatusCmd.Flags().StringVarP(&clusterFlags.name, "name", "n", "", "ClusterPersona name (optional)")

	// Init flags
	clusterInitCmd.Flags().StringVar(&clusterFlags.name, "name", "", "cluster name (required)")
	clusterInitCmd.Flags().StringVar(&clusterFlags.environment, "environment", "development", "cluster environment (development, staging, production, sandbox)")
	clusterInitCmd.Flags().BoolVar(&clusterFlags.dryRun, "dry-run", false, "print to stdout without applying")
	_ = clusterInitCmd.MarkFlagRequired("name")

	// Register subcommands
	clusterCmd.AddCommand(clusterStatusCmd)
	clusterCmd.AddCommand(clusterInitCmd)
	clusterCmd.AddCommand(clusterSetupCmd)
	clusterCmd.AddCommand(clusterInfoCmd)
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

	if name == "" {
		// List all ClusterPersonas
		return listClusterPersonas()
	}

	// Get specific ClusterPersona
	return getClusterPersonaStatus(name)
}

func listClusterPersonas() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	kubectlCmd := exec.CommandContext(ctx, "kubectl", "get", "clusterpersona", "-o", "json")
	rawOutput, err := kubectlCmd.CombinedOutput()
	if err != nil {
		outputStr := kubectlErrText(rawOutput)
		if strings.Contains(outputStr, "the server doesn't have a resource type") {
			return fmt.Errorf("ClusterPersona CRD is not installed on this cluster. Install the Dorgu Operator first")
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
		output.Info("No ClusterPersona resources found. Create one with: dorgu cluster init --name <name>")
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

func getClusterPersonaStatus(name string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	outputFormat := "yaml"
	if output.IsJSON() {
		outputFormat = "json"
	}

	kubectlCmd := exec.CommandContext(ctx, "kubectl", "get", "clusterpersona", name, "-o", outputFormat)
	rawOutput, err := kubectlCmd.CombinedOutput()
	if err != nil {
		outputStr := kubectlErrText(rawOutput)
		if strings.Contains(outputStr, "not found") {
			output.ErrorWithHint("ClusterPersona not found: "+name,
				"List available ClusterPersonas: dorgu cluster status",
				"Create one: dorgu cluster init --name <name> --environment <env>")
			return errSilent
		}
		if strings.Contains(outputStr, "the server doesn't have a resource type") {
			return fmt.Errorf("ClusterPersona CRD is not installed on this cluster. Install the Dorgu Operator first")
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

func runClusterInit(cmd *cobra.Command, args []string) error {
	// Check kubectl availability
	if _, err := exec.LookPath("kubectl"); err != nil {
		return fmt.Errorf("kubectl not found in PATH; required for cluster init")
	}

	personaYAML, err := generateClusterPersonaYAML(clusterFlags.name, clusterFlags.environment)
	if err != nil {
		return err
	}

	if clusterFlags.dryRun {
		fmt.Println(personaYAML)
		return nil
	}

	// Apply via kubectl
	output.Info("Creating ClusterPersona...")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	kubectlCmd := exec.CommandContext(ctx, "kubectl", "apply", "-f", "-")
	kubectlCmd.Stdin = bytes.NewBufferString(personaYAML)
	kubectlCmd.Stdout = os.Stdout
	kubectlCmd.Stderr = os.Stderr
	if err := kubectlCmd.Run(); err != nil {
		return fmt.Errorf("kubectl apply failed: %w", err)
	}

	output.Success(fmt.Sprintf("ClusterPersona '%s' created successfully", clusterFlags.name))
	output.Info("The Dorgu Operator will now discover cluster state. Check status with: dorgu cluster status " + clusterFlags.name)
	return nil
}

func generateClusterPersonaYAML(name, environment string) (string, error) {
	if !validDNSName.MatchString(name) {
		return "", fmt.Errorf("invalid ClusterPersona name %q: must match RFC 1123 (lowercase alphanumeric, hyphens, dots)", name)
	}
	if !validDNSName.MatchString(environment) {
		return "", fmt.Errorf("invalid environment %q: must match RFC 1123 (lowercase alphanumeric, hyphens, dots)", environment)
	}
	return fmt.Sprintf(`apiVersion: dorgu.io/v1
kind: ClusterPersona
metadata:
  name: %s
spec:
  name: %s
  description: "Kubernetes cluster managed by Dorgu"
  environment: %s
  policies:
    security:
      enforceNonRoot: true
      disallowPrivileged: true
      podSecurityStandard: baseline
    selfHealing:
      mode: observe
      trustLevel: 2
  conventions:
    requiredLabels:
      - app.kubernetes.io/name
      - app.kubernetes.io/version
  defaults:
    namespace: default
`, name, name, environment), nil
}
