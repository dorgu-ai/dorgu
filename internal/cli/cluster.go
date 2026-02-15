package cli

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dorgu-ai/dorgu/internal/output"
)

var clusterFlags struct {
	name        string
	environment string
	dryRun      bool
}

var clusterCmd = &cobra.Command{
	Use:   "cluster",
	Short: "Manage ClusterPersona CRDs",
	Long: `View and manage ClusterPersona Custom Resources that represent
your Kubernetes cluster's identity and configuration.

Examples:
  # View cluster persona status
  dorgu cluster status

  # Initialize a new cluster persona
  dorgu cluster init --name my-cluster --environment production`,
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
	clusterInitCmd.MarkFlagRequired("name")

	// Register subcommands
	clusterCmd.AddCommand(clusterStatusCmd)
	clusterCmd.AddCommand(clusterInitCmd)
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
	kubectlCmd := exec.Command("kubectl", "get", "clusterpersona", "-o", "wide")
	rawOutput, err := kubectlCmd.CombinedOutput()
	if err != nil {
		outputStr := strings.TrimSpace(string(rawOutput))
		if strings.Contains(outputStr, "the server doesn't have a resource type") {
			return fmt.Errorf("ClusterPersona CRD is not installed on this cluster. Install the Dorgu Operator first")
		}
		if strings.Contains(outputStr, "No resources found") {
			output.Info("No ClusterPersona resources found. Create one with: dorgu cluster init --name <name>")
			return nil
		}
		return fmt.Errorf("failed to list cluster personas: %s", outputStr)
	}

	output.Header("ClusterPersonas")
	fmt.Println(string(rawOutput))
	return nil
}

func getClusterPersonaStatus(name string) error {
	kubectlCmd := exec.Command("kubectl", "get", "clusterpersona", name, "-o", "yaml")
	rawOutput, err := kubectlCmd.CombinedOutput()
	if err != nil {
		outputStr := strings.TrimSpace(string(rawOutput))
		if strings.Contains(outputStr, "not found") {
			return fmt.Errorf("ClusterPersona '%s' not found", name)
		}
		if strings.Contains(outputStr, "the server doesn't have a resource type") {
			return fmt.Errorf("ClusterPersona CRD is not installed on this cluster. Install the Dorgu Operator first")
		}
		return fmt.Errorf("failed to get cluster persona: %s", outputStr)
	}

	displayClusterPersonaStatus(name, string(rawOutput))
	return nil
}

func displayClusterPersonaStatus(name string, rawYAML string) {
	output.Header(fmt.Sprintf("ClusterPersona: %s", name))

	lines := strings.Split(rawYAML, "\n")

	// Extract key information
	var phase, kubeVersion, platform string
	var nodeCount, appCount, runningPods int
	var addons []string

	inStatus := false
	inNodes := false
	inAddons := false
	inResourceSummary := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if trimmed == "status:" {
			inStatus = true
			continue
		}

		if inStatus {
			if strings.HasPrefix(trimmed, "phase:") {
				phase = strings.TrimPrefix(trimmed, "phase:")
				phase = strings.TrimSpace(phase)
			}
			if strings.HasPrefix(trimmed, "kubernetesVersion:") {
				kubeVersion = strings.TrimPrefix(trimmed, "kubernetesVersion:")
				kubeVersion = strings.TrimSpace(kubeVersion)
			}
			if strings.HasPrefix(trimmed, "platform:") {
				platform = strings.TrimPrefix(trimmed, "platform:")
				platform = strings.TrimSpace(platform)
			}
			if strings.HasPrefix(trimmed, "applicationCount:") {
				fmt.Sscanf(trimmed, "applicationCount: %d", &appCount)
			}
			if trimmed == "nodes:" {
				inNodes = true
				continue
			}
			if inNodes && strings.HasPrefix(trimmed, "- name:") {
				nodeCount++
			}
			if inNodes && !strings.HasPrefix(trimmed, "-") && !strings.HasPrefix(trimmed, " ") && trimmed != "" {
				inNodes = false
			}
			if trimmed == "addons:" {
				inAddons = true
				continue
			}
			if inAddons && strings.HasPrefix(trimmed, "- name:") {
				addonName := strings.TrimPrefix(trimmed, "- name:")
				addonName = strings.TrimSpace(addonName)
				addons = append(addons, addonName)
			}
			if inAddons && !strings.HasPrefix(trimmed, "-") && !strings.HasPrefix(trimmed, " ") && trimmed != "" {
				inAddons = false
			}
			if trimmed == "resourceSummary:" {
				inResourceSummary = true
				continue
			}
			if inResourceSummary && strings.HasPrefix(trimmed, "runningPods:") {
				fmt.Sscanf(trimmed, "runningPods: %d", &runningPods)
			}
		}
	}

	// Display summary
	fmt.Println()
	output.Info("Cluster Overview")
	fmt.Printf("  Phase:              %s\n", colorPhase(phase))
	fmt.Printf("  Kubernetes Version: %s\n", kubeVersion)
	fmt.Printf("  Platform:           %s\n", platform)
	fmt.Printf("  Nodes:              %d\n", nodeCount)
	fmt.Printf("  Running Pods:       %d\n", runningPods)
	fmt.Printf("  Applications:       %d\n", appCount)

	if len(addons) > 0 {
		fmt.Println()
		output.Info("Discovered Add-ons")
		for _, addon := range addons {
			fmt.Printf("  • %s\n", addon)
		}
	}

	fmt.Println()
	output.Dim("Use 'kubectl get clusterpersona " + name + " -o yaml' for full details")
}

func colorPhase(phase string) string {
	switch phase {
	case "Ready":
		return output.Green(phase)
	case "Degraded":
		return output.Yellow(phase)
	case "Discovering":
		return output.Blue(phase)
	default:
		return phase
	}
}

func runClusterInit(cmd *cobra.Command, args []string) error {
	// Check kubectl availability
	if _, err := exec.LookPath("kubectl"); err != nil {
		return fmt.Errorf("kubectl not found in PATH; required for cluster init")
	}

	// Generate ClusterPersona YAML
	clusterPersonaYAML := generateClusterPersonaYAML(clusterFlags.name, clusterFlags.environment)

	if clusterFlags.dryRun {
		fmt.Println(clusterPersonaYAML)
		return nil
	}

	// Apply via kubectl
	output.Info("Creating ClusterPersona...")
	kubectlCmd := exec.Command("kubectl", "apply", "-f", "-")
	kubectlCmd.Stdin = bytes.NewBufferString(clusterPersonaYAML)
	kubectlCmd.Stdout = os.Stdout
	kubectlCmd.Stderr = os.Stderr
	if err := kubectlCmd.Run(); err != nil {
		return fmt.Errorf("kubectl apply failed: %w", err)
	}

	output.Success(fmt.Sprintf("ClusterPersona '%s' created successfully", clusterFlags.name))
	output.Info("The Dorgu Operator will now discover cluster state. Check status with: dorgu cluster status " + clusterFlags.name)
	return nil
}

func generateClusterPersonaYAML(name, environment string) string {
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
  conventions:
    requiredLabels:
      - app.kubernetes.io/name
      - app.kubernetes.io/version
  defaults:
    namespace: default
`, name, name, environment)
}
