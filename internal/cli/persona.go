package cli

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/briandowns/spinner"
	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"

	"github.com/dorgu-ai/dorgu/internal/analyzer"
	"github.com/dorgu-ai/dorgu/internal/config"
	"github.com/dorgu-ai/dorgu/internal/generator"
	"github.com/dorgu-ai/dorgu/internal/output"
)

var personaFlags struct {
	namespace   string
	outputDir   string
	dryRun      bool
	llmProvider string
	name        string
}

var personaCmd = &cobra.Command{
	Use:   "persona",
	Short: "Manage ApplicationPersona CRDs",
	Long: `Generate, apply, and inspect ApplicationPersona Custom Resources
for your applications on Kubernetes.

Examples:
  # Generate persona YAML from application analysis
  dorgu persona generate ./my-app

  # Generate and apply to cluster
  dorgu persona apply ./my-app --namespace commerce

  # Check persona status on cluster
  dorgu persona status order-service -n commerce`,
}

var personaGenerateCmd = &cobra.Command{
	Use:   "generate [path]",
	Short: "Generate an ApplicationPersona CRD YAML from application analysis",
	Long: `Analyze an application directory and output a structured
ApplicationPersona CRD YAML that can be applied to a Kubernetes cluster
with the Dorgu Operator installed.

Examples:
  dorgu persona generate .
  dorgu persona generate ./my-app --namespace production
  dorgu persona generate ./my-app --dry-run
  dorgu persona generate ./my-app -o ./manifests`,
	Args: cobra.MaximumNArgs(1),
	RunE: runPersonaGenerate,
}

var personaApplyCmd = &cobra.Command{
	Use:   "apply [path]",
	Short: "Generate and apply an ApplicationPersona to the cluster",
	Long: `Analyze an application, generate the ApplicationPersona CRD YAML,
and apply it to the current Kubernetes cluster using kubectl.

Requires:
  - kubectl configured and accessible
  - ApplicationPersona CRD installed on the cluster (via Dorgu Operator)

Examples:
  dorgu persona apply ./my-app --namespace commerce
  dorgu persona apply ./my-app -n default`,
	Args: cobra.MaximumNArgs(1),
	RunE: runPersonaApply,
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
	// Generate flags
	personaGenerateCmd.Flags().StringVarP(&personaFlags.namespace, "namespace", "n", "default", "target Kubernetes namespace")
	personaGenerateCmd.Flags().StringVarP(&personaFlags.outputDir, "output", "o", ".", "output directory for persona.yaml")
	personaGenerateCmd.Flags().BoolVar(&personaFlags.dryRun, "dry-run", false, "print to stdout without writing files")
	personaGenerateCmd.Flags().StringVar(&personaFlags.llmProvider, "llm-provider", "", "LLM provider for analysis")
	personaGenerateCmd.Flags().StringVar(&personaFlags.name, "name", "", "override application name")

	// Apply flags
	personaApplyCmd.Flags().StringVarP(&personaFlags.namespace, "namespace", "n", "default", "target Kubernetes namespace")
	personaApplyCmd.Flags().StringVar(&personaFlags.llmProvider, "llm-provider", "", "LLM provider for analysis")
	personaApplyCmd.Flags().StringVar(&personaFlags.name, "name", "", "override application name")

	// Status flags
	personaStatusCmd.Flags().StringVarP(&personaFlags.namespace, "namespace", "n", "default", "Kubernetes namespace")

	// Register subcommands
	personaCmd.AddCommand(personaGenerateCmd)
	personaCmd.AddCommand(personaApplyCmd)
	personaCmd.AddCommand(personaStatusCmd)
}

func runPersonaGenerate(cmd *cobra.Command, args []string) error {
	targetPath := "."
	if len(args) > 0 {
		targetPath = args[0]
	}

	personaYAML, err := generatePersonaFromPath(targetPath)
	if err != nil {
		return err
	}

	if personaFlags.dryRun {
		fmt.Println(personaYAML)
		return nil
	}

	// Write to file
	outputPath := filepath.Join(personaFlags.outputDir, "persona.yaml")
	if err := os.MkdirAll(personaFlags.outputDir, 0o755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}
	if err := os.WriteFile(outputPath, []byte(personaYAML), 0o644); err != nil {
		return fmt.Errorf("failed to write persona.yaml: %w", err)
	}

	output.Success(fmt.Sprintf("Generated persona: %s", outputPath))
	return nil
}

func runPersonaApply(cmd *cobra.Command, args []string) error {
	targetPath := "."
	if len(args) > 0 {
		targetPath = args[0]
	}

	// Check kubectl availability
	if _, err := exec.LookPath("kubectl"); err != nil {
		return fmt.Errorf("kubectl not found in PATH; required for persona apply")
	}

	personaYAML, err := generatePersonaFromPath(targetPath)
	if err != nil {
		return err
	}

	// Apply via kubectl — capture stderr to detect schema errors while still streaming to user
	output.Info("Applying ApplicationPersona to cluster...")
	var stderrCapture bytes.Buffer
	kubectlCmd := exec.Command("kubectl", "apply", "-f", "-", "-n", personaFlags.namespace)
	kubectlCmd.Stdin = bytes.NewBufferString(personaYAML)
	kubectlCmd.Stdout = os.Stdout
	kubectlCmd.Stderr = io.MultiWriter(os.Stderr, &stderrCapture)
	if err := kubectlCmd.Run(); err != nil {
		stderrStr := stderrCapture.String()
		if strings.Contains(stderrStr, "strict decoding error") || strings.Contains(stderrStr, "ValidationError") {
			output.ErrorWithHint("ApplicationPersona rejected by cluster (schema mismatch)",
				"Try regenerating: dorgu persona generate . -o ./k8s-out",
				"Then re-apply: dorgu persona apply . -n <namespace>")
			return errSilent
		}
		return fmt.Errorf("kubectl apply failed: %w", err)
	}

	output.Success("ApplicationPersona applied successfully")
	return nil
}

func runPersonaStatus(cmd *cobra.Command, args []string) error {
	name := args[0]

	// Check kubectl availability
	if _, err := exec.LookPath("kubectl"); err != nil {
		return fmt.Errorf("kubectl not found in PATH; required for persona status")
	}

	// Get the persona resource (Kubernetes resource name is DNS-safe; try sanitized if name has underscores)
	tryGet := func(n string) ([]byte, error) {
		kubectlCmd := exec.Command("kubectl", "get", "applicationpersona", n,
			"-n", personaFlags.namespace, "-o", "yaml")
		return kubectlCmd.CombinedOutput()
	}
	rawOutput, err := tryGet(name)
	if err != nil {
		outputStr := strings.TrimSpace(string(rawOutput))
		if strings.Contains(outputStr, "not found") && strings.Contains(name, "_") {
			// App names with underscores are sanitized to hyphens in the cluster
			sanitized := generator.ToDNSSubdomain(name)
			rawOutput, err = tryGet(sanitized)
			if err == nil {
				name = sanitized
			}
		}
	}
	if err != nil {
		outputStr := strings.TrimSpace(string(rawOutput))
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

// generatePersonaFromPath runs the analysis pipeline and generates persona YAML.
func generatePersonaFromPath(targetPath string) (string, error) {
	absPath, err := filepath.Abs(targetPath)
	if err != nil {
		return "", fmt.Errorf("failed to resolve path: %w", err)
	}
	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		return "", fmt.Errorf("path does not exist: %s", absPath)
	}

	// Load config chain
	globalCfg, err := config.LoadGlobalConfig()
	if err != nil {
		globalCfg = config.DefaultGlobalConfig()
	}
	cfg, err := config.Load()
	if err != nil {
		cfg = config.Default()
	}
	if cfg.CI.Registry == "" && globalCfg.Defaults.Registry != "" {
		cfg.CI.Registry = globalCfg.Defaults.Registry
	}

	effectiveProvider := globalCfg.GetEffectiveProvider(personaFlags.llmProvider)
	if effectiveProvider == "" {
		effectiveProvider = cfg.LLM.Provider
	}

	s := spinner.New(spinner.CharSets[14], 100*time.Millisecond)
	s.Suffix = " Analyzing application..."
	s.Start()

	analysis, err := analyzer.Analyze(absPath, effectiveProvider)
	if err != nil {
		s.Stop()
		return "", fmt.Errorf("analysis failed: %w", err)
	}

	// Git repo auto-detect
	if analysis.Repository == "" {
		if gitURL := analyzer.DetectGitRemoteURL(absPath); gitURL != "" {
			analysis.Repository = gitURL
		}
	}

	if personaFlags.name != "" {
		analysis.Name = personaFlags.name
	}

	s.Suffix = " Generating persona..."

	personaYAML, err := generator.GeneratePersonaYAML(analysis, personaFlags.namespace, cfg)
	s.Stop()
	if err != nil {
		return "", fmt.Errorf("persona generation failed: %w", err)
	}

	return personaYAML, nil
}

// personaStatus represents the parsed ApplicationPersona for display.
type personaStatus struct {
	Metadata struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	} `json:"metadata"`
	Spec struct {
		Name string `json:"name"`
		Type string `json:"type"`
		Tier string `json:"tier"`
	} `json:"spec"`
	Status struct {
		Phase       string `json:"phase"`
		LastUpdated string `json:"lastUpdated"`
		Health      *struct {
			Status      string `json:"status"`
			LastCheck   string `json:"lastCheck"`
			Message     string `json:"message"`
			PodFailures []struct {
				PodName   string `json:"podName"`
				Container string `json:"container"`
				Reason    string `json:"reason"`
				Message   string `json:"message"`
			} `json:"podFailures"`
		} `json:"health"`
		Validation *struct {
			Passed      bool   `json:"passed"`
			LastChecked string `json:"lastChecked"`
			Issues      []struct {
				Severity   string `json:"severity"`
				Field      string `json:"field"`
				Message    string `json:"message"`
				Suggestion string `json:"suggestion"`
			} `json:"issues"`
		} `json:"validation"`
		Deployments *struct {
			Current        string `json:"current"`
			LastSuccessful string `json:"lastSuccessful"`
			LastFailed     string `json:"lastFailed"`
		} `json:"deployments"`
		ArgoCD *struct {
			SyncStatus   string `json:"syncStatus"`
			HealthStatus string `json:"healthStatus"`
			LastSyncTime string `json:"lastSyncTime"`
			Revision     string `json:"revision"`
		} `json:"argoCD"`
		Conditions []struct {
			Type    string `json:"type"`
			Status  string `json:"status"`
			Reason  string `json:"reason"`
			Message string `json:"message"`
		} `json:"conditions"`
		Learned *struct {
			ResourceBaseline *struct {
				AvgCPU     string `json:"avgCPU"`
				AvgMemory  string `json:"avgMemory"`
				PeakCPU    string `json:"peakCPU"`
				PeakMemory string `json:"peakMemory"`
			} `json:"resourceBaseline"`
		} `json:"learned"`
	} `json:"status"`
}

// displayPersonaStatus formats and prints persona status information.
func displayPersonaStatus(name string, rawYAML string) {
	var persona personaStatus
	if err := yaml.Unmarshal([]byte(rawYAML), &persona); err != nil {
		output.Error(fmt.Sprintf("Failed to parse persona YAML: %v", err))
		fmt.Println(rawYAML)
		return
	}

	// Header
	output.Header(fmt.Sprintf("ApplicationPersona: %s", name))

	// Check if status exists
	if persona.Status.Phase == "" && persona.Status.Health == nil && persona.Status.Validation == nil {
		fmt.Println(output.Dim("  No status available yet. The Dorgu Operator may not have reconciled this persona."))
		return
	}

	// Phase with color
	phaseDisplay := formatPhase(persona.Status.Phase)
	fmt.Printf("  %-14s %s\n", "Phase:", phaseDisplay)

	// Health status
	if persona.Status.Health != nil {
		healthDisplay := formatHealth(persona.Status.Health.Status, persona.Status.Health.Message)
		fmt.Printf("  %-14s %s\n", "Health:", healthDisplay)
	}

	// Validation status
	if persona.Status.Validation != nil {
		validationDisplay := formatValidation(persona.Status.Validation.Passed)
		fmt.Printf("  %-14s %s\n", "Validation:", validationDisplay)
	}

	fmt.Println()

	// Conditions section
	if len(persona.Status.Conditions) > 0 {
		fmt.Println("  Conditions:")
		for _, cond := range persona.Status.Conditions {
			condStatus := formatConditionStatus(cond.Status)
			fmt.Printf("    %-12s %s - %s\n", cond.Type+":", condStatus, cond.Message)
		}
		fmt.Println()
	}

	// Deployment info
	if persona.Status.Deployments != nil && persona.Status.Deployments.Current != "" {
		fmt.Println("  Deployment:")
		fmt.Printf("    Current Image: %s\n", persona.Status.Deployments.Current)
		if persona.Status.Deployments.LastSuccessful != "" {
			fmt.Printf("    Last Successful: %s\n", persona.Status.Deployments.LastSuccessful)
		}
		if persona.Status.Deployments.LastFailed != "" {
			fmt.Printf("    Last Failed: %s\n", output.Red(persona.Status.Deployments.LastFailed))
		}
		fmt.Println()
	}

	// Pod failures (if any)
	if persona.Status.Health != nil && len(persona.Status.Health.PodFailures) > 0 {
		fmt.Println("  " + output.Red("Pod Failures:"))
		for _, pf := range persona.Status.Health.PodFailures {
			fmt.Printf("    %s/%s: %s\n", pf.PodName, pf.Container, output.Red(pf.Reason))
			if pf.Message != "" {
				fmt.Printf("      %s\n", output.Dim(pf.Message))
			}
		}
		fmt.Println()
	}

	// Validation issues (if any)
	if persona.Status.Validation != nil && len(persona.Status.Validation.Issues) > 0 {
		fmt.Println("  Validation Issues:")
		for _, issue := range persona.Status.Validation.Issues {
			icon := getIssueSeverityIcon(issue.Severity)
			fmt.Printf("    %s [%s] %s\n", icon, issue.Field, issue.Message)
			if issue.Suggestion != "" {
				fmt.Printf("       Suggestion: %s\n", output.Dim(issue.Suggestion))
			}
		}
		fmt.Println()
	}

	// ArgoCD status (if present)
	if persona.Status.ArgoCD != nil && persona.Status.ArgoCD.SyncStatus != "" {
		fmt.Println("  ArgoCD:")
		syncDisplay := formatArgoSyncStatus(persona.Status.ArgoCD.SyncStatus)
		fmt.Printf("    Sync Status: %s\n", syncDisplay)
		if persona.Status.ArgoCD.HealthStatus != "" {
			healthDisplay := formatArgoHealthStatus(persona.Status.ArgoCD.HealthStatus)
			fmt.Printf("    Health: %s\n", healthDisplay)
		}
		if persona.Status.ArgoCD.Revision != "" {
			fmt.Printf("    Revision: %s\n", truncateRevision(persona.Status.ArgoCD.Revision))
		}
		fmt.Println()
	}

	// Learned patterns (if present)
	if persona.Status.Learned != nil && persona.Status.Learned.ResourceBaseline != nil {
		rb := persona.Status.Learned.ResourceBaseline
		if rb.AvgCPU != "" || rb.AvgMemory != "" {
			fmt.Println("  Resource Baseline (learned):")
			if rb.AvgCPU != "" {
				fmt.Printf("    Avg CPU: %s", rb.AvgCPU)
				if rb.PeakCPU != "" {
					fmt.Printf(" (peak: %s)", rb.PeakCPU)
				}
				fmt.Println()
			}
			if rb.AvgMemory != "" {
				fmt.Printf("    Avg Memory: %s", rb.AvgMemory)
				if rb.PeakMemory != "" {
					fmt.Printf(" (peak: %s)", rb.PeakMemory)
				}
				fmt.Println()
			}
			fmt.Println()
		}
	}

	// Last updated
	if persona.Status.LastUpdated != "" {
		fmt.Printf("  Last Updated: %s\n", output.Dim(persona.Status.LastUpdated))
	}
}

// formatPhase returns a colored phase string.
func formatPhase(phase string) string {
	switch phase {
	case "Active":
		return output.Green(phase)
	case "Degraded":
		return output.Yellow(phase)
	case "Failed":
		return output.Red(phase)
	case "Pending":
		return output.Blue(phase)
	default:
		return phase
	}
}

// formatHealth returns a colored health status with message.
func formatHealth(status, message string) string {
	var colored string
	switch status {
	case "Healthy":
		colored = output.Green(status)
	case "Degraded":
		colored = output.Yellow(status)
	case "Unhealthy":
		colored = output.Red(status)
	default:
		colored = status
	}
	if message != "" {
		return fmt.Sprintf("%s (%s)", colored, message)
	}
	return colored
}

// formatValidation returns a colored validation status.
func formatValidation(passed bool) string {
	if passed {
		return output.Green("Passed")
	}
	return output.Red("Failed")
}

// formatConditionStatus returns a colored condition status.
func formatConditionStatus(status string) string {
	switch status {
	case "True":
		return output.Green(status)
	case "False":
		return output.Red(status)
	default:
		return output.Yellow(status)
	}
}

// getIssueSeverityIcon returns an icon for the issue severity.
func getIssueSeverityIcon(severity string) string {
	switch severity {
	case "error":
		return output.Red("✗")
	case "warning":
		return output.Yellow("⚠")
	case "info":
		return output.Blue("ℹ")
	default:
		return "•"
	}
}

// formatArgoSyncStatus returns a colored ArgoCD sync status.
func formatArgoSyncStatus(status string) string {
	switch status {
	case "Synced":
		return output.Green(status)
	case "OutOfSync":
		return output.Yellow(status)
	default:
		return status
	}
}

// formatArgoHealthStatus returns a colored ArgoCD health status.
func formatArgoHealthStatus(status string) string {
	switch status {
	case "Healthy":
		return output.Green(status)
	case "Degraded", "Progressing":
		return output.Yellow(status)
	case "Suspended", "Missing":
		return output.Red(status)
	default:
		return status
	}
}

// truncateRevision shortens a git revision for display.
func truncateRevision(rev string) string {
	if len(rev) > 8 {
		return rev[:8]
	}
	return rev
}
