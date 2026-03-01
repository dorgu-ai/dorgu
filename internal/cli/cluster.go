package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	k8syaml "sigs.k8s.io/yaml"

	"github.com/dorgu-ai/dorgu/internal/output"
)

// clusterPersonaYAML is used to parse a single ClusterPersona YAML response from kubectl.
type clusterPersonaYAML struct {
	Metadata struct {
		Name              string            `json:"name"`
		CreationTimestamp string            `json:"creationTimestamp"`
		Annotations       map[string]string `json:"annotations"`
	} `json:"metadata"`
	Spec struct {
		Environment string `json:"environment"`
	} `json:"spec"`
	Status struct {
		Phase             string `json:"phase"`
		KubernetesVersion string `json:"kubernetesVersion"`
		Platform          string `json:"platform"`
		ApplicationCount  int    `json:"applicationCount"`
		Nodes             []struct {
			Name  string `json:"name"`
			Ready bool   `json:"ready"`
			Role  string `json:"role"`
		} `json:"nodes"`
		Addons []struct {
			Name      string `json:"name"`
			Type      string `json:"type"`
			Installed bool   `json:"installed"`
			Healthy   bool   `json:"healthy"`
			Version   string `json:"version"`
			Namespace string `json:"namespace"`
		} `json:"addons"`
		ResourceSummary struct {
			TotalCPU          string `json:"totalCPU"`
			TotalMemory       string `json:"totalMemory"`
			AllocatableCPU    string `json:"allocatableCPU"`
			AllocatableMemory string `json:"allocatableMemory"`
			RunningPods       int    `json:"runningPods"`
			TotalPods         int    `json:"totalPods"`
		} `json:"resourceSummary"`
	} `json:"status"`
}

// clusterPersonaList wraps the kubectl JSON list response.
type clusterPersonaList struct {
	Items []clusterPersonaYAML `json:"items"`
}

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
	kubectlCmd := exec.Command("kubectl", "get", "clusterpersona", "-o", "json")
	rawOutput, err := kubectlCmd.CombinedOutput()
	if err != nil {
		outputStr := strings.TrimSpace(string(rawOutput))
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
		output.Info("No ClusterPersona resources found. Create one with: dorgu cluster init --name <name>")
		return nil
	}

	fmt.Println()
	fmt.Printf("  %-18s %-14s %-14s %-7s %-6s %s\n",
		"NAME", "ENVIRONMENT", "PHASE", "NODES", "APPS", "AGE")
	fmt.Printf("  %s\n", strings.Repeat("─", 70))

	headerStyle := lipgloss.NewStyle()
	for _, item := range list.Items {
		phaseStr := clusterPhaseDot(item.Status.Phase)
		// Pad phase column to consistent visual width (use lipgloss Width for ANSI-safe padding)
		phasePadded := lipgloss.NewStyle().Width(14).Render(phaseStr)
		env := item.Spec.Environment
		if env == "" {
			env = "-"
		}
		age := formatAge(item.Metadata.CreationTimestamp)
		_ = headerStyle

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
	kubectlCmd := exec.Command("kubectl", "get", "clusterpersona", name, "-o", "yaml")
	rawOutput, err := kubectlCmd.CombinedOutput()
	if err != nil {
		outputStr := strings.TrimSpace(string(rawOutput))
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

	displayClusterPersonaStatus(name, string(rawOutput))
	return nil
}

func displayClusterPersonaStatus(name string, rawYAML string) {
	displayClusterPersonaStatusTo(os.Stdout, name, rawYAML)
}

func displayClusterPersonaStatusTo(w io.Writer, name string, rawYAML string) {
	var cp clusterPersonaYAML
	if err := k8syaml.Unmarshal([]byte(rawYAML), &cp); err != nil {
		output.Warn(fmt.Sprintf("Could not parse ClusterPersona YAML: %v", err))
		fmt.Fprintln(w, rawYAML)
		return
	}

	s := cp.Status

	// --- Header box ---
	headerBoxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("39")).
		Padding(0, 1).
		Width(52)

	env := cp.Spec.Environment
	if env == "" {
		env = "-"
	}
	phaseDot := clusterPhaseDot(s.Phase)
	headerContent := fmt.Sprintf("ClusterPersona: %s\nEnvironment: %-16s Phase: %s", name, env, phaseDot)
	fmt.Fprintln(w)
	fmt.Fprintln(w, headerBoxStyle.Render(headerContent))
	fmt.Fprintln(w)

	// --- Key-value overview ---
	platform := s.Platform
	if platform == "" {
		platform = "-"
	}
	k8sVer := s.KubernetesVersion
	if k8sVer == "" {
		k8sVer = "-"
	}
	totalNodes := len(s.Nodes)
	readyNodes := 0
	for _, n := range s.Nodes {
		if n.Ready {
			readyNodes++
		}
	}
	fmt.Fprintf(w, "  %-16s %s\n", "Platform", platform)
	fmt.Fprintf(w, "  %-16s %s\n", "K8s Version", k8sVer)
	fmt.Fprintf(w, "  %-16s %d (%d ready)\n", "Nodes", totalNodes, readyNodes)
	fmt.Fprintf(w, "  %-16s %d\n", "Running Pods", s.ResourceSummary.RunningPods)

	// --- Resources section ---
	rs := s.ResourceSummary
	if rs.TotalCPU != "" || rs.TotalMemory != "" {
		fmt.Fprintln(w)
		fmt.Fprintf(w, "  Resources\n")
		fmt.Fprintf(w, "  %s\n", strings.Repeat("─", 38))
		if rs.TotalCPU != "" {
			fmt.Fprintf(w, "  %-8s %s allocated / %s total\n", "CPU", rs.AllocatableCPU, rs.TotalCPU)
		}
		if rs.TotalMemory != "" {
			fmt.Fprintf(w, "  %-8s %s allocated / %s total\n", "Memory", rs.AllocatableMemory, rs.TotalMemory)
		}
	}

	// --- Installed Add-ons section ---
	if len(s.Addons) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintf(w, "  Installed Add-ons\n")
		fmt.Fprintf(w, "  %s\n", strings.Repeat("─", 38))
		for _, addon := range s.Addons {
			var icon, healthStr string
			if !addon.Installed {
				icon = output.Red("✗")
				healthStr = "not installed"
			} else if !addon.Healthy {
				icon = output.Yellow("⚠")
				healthStr = "unhealthy"
			} else {
				icon = output.Green("✓")
				healthStr = "healthy"
			}
			ver := addon.Version
			if ver == "" {
				ver = "—"
			}
			fmt.Fprintf(w, "  %s %-20s %-10s %s\n", icon, addon.Name, ver, healthStr)
		}
	}

	// --- Next Steps section ---
	fmt.Fprintln(w)
	fmt.Fprintf(w, "  Next Steps\n")
	fmt.Fprintf(w, "  %s\n", strings.Repeat("─", 38))

	hasUninstalled := false
	for _, addon := range s.Addons {
		if !addon.Installed {
			hasUninstalled = true
			break
		}
	}
	if hasUninstalled {
		fmt.Fprintf(w, "  %s Run 'dorgu cluster setup' to install missing components\n", output.Dim("→"))
	}
	if s.ApplicationCount == 0 {
		fmt.Fprintf(w, "  %s Run 'dorgu persona apply' to onboard an application\n", output.Dim("→"))
	} else {
		fmt.Fprintf(w, "  %s Run 'dorgu persona apply' to onboard additional applications\n", output.Dim("→"))
	}
	if s.Phase == "Degraded" {
		fmt.Fprintf(w, "  %s Check nodes: kubectl get nodes\n", output.Dim("→"))
	}
	fmt.Fprintln(w)
}

// clusterPhaseDot returns a colored "● Phase" string for the given phase.
func clusterPhaseDot(phase string) string {
	dot := "●"
	switch phase {
	case "Ready":
		return output.Green(dot + " " + phase)
	case "Degraded":
		return output.Yellow(dot + " " + phase)
	case "Discovering":
		return output.Blue(dot + " " + phase)
	default:
		if phase == "" {
			return dot + " Unknown"
		}
		return dot + " " + phase
	}
}

// colorPhase returns a colored phase string (without the dot prefix).
// Used by sync.go and watch.go for persona/cluster event display.
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

// formatAge returns a human-readable age string from an RFC3339 timestamp.
func formatAge(ts string) string {
	if ts == "" {
		return "?"
	}
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return "?"
	}
	age := time.Since(t)
	switch {
	case age < time.Hour:
		return fmt.Sprintf("%dm", int(age.Minutes()))
	case age < 24*time.Hour:
		return fmt.Sprintf("%dh", int(age.Hours()))
	default:
		return fmt.Sprintf("%dd", int(age.Hours()/24))
	}
}

func runClusterInit(cmd *cobra.Command, args []string) error {
	// Check kubectl availability
	if _, err := exec.LookPath("kubectl"); err != nil {
		return fmt.Errorf("kubectl not found in PATH; required for cluster init")
	}

	// Generate ClusterPersona YAML
	personaYAML := generateClusterPersonaYAML(clusterFlags.name, clusterFlags.environment)

	if clusterFlags.dryRun {
		fmt.Println(personaYAML)
		return nil
	}

	// Apply via kubectl
	output.Info("Creating ClusterPersona...")
	kubectlCmd := exec.Command("kubectl", "apply", "-f", "-")
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
