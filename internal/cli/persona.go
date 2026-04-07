package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/briandowns/spinner"
	"github.com/spf13/cobra"

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
	personaCmd.AddCommand(personaListCmd)
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
	applyCtx, applyCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer applyCancel()
	var stderrCapture bytes.Buffer
	kubectlCmd := exec.CommandContext(applyCtx, "kubectl", "apply", "-f", "-", "-n", personaFlags.namespace)
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
	getCtx, getCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer getCancel()
	tryGet := func(n string) ([]byte, error) {
		kubectlCmd := exec.CommandContext(getCtx, "kubectl", "get", "applicationpersona", n,
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

	var s *spinner.Spinner
	if output.IsTTY() {
		s = spinner.New(spinner.CharSets[14], 100*time.Millisecond)
		s.Suffix = " Analyzing application..."
		s.Start()
	}

	analysis, err := analyzer.Analyze(absPath, effectiveProvider)
	if err != nil {
		if s != nil {
			s.Stop()
		}
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

	if s != nil {
		s.Suffix = " Generating persona..."
	}

	personaYAML, err := generator.GeneratePersonaYAML(analysis, personaFlags.namespace, cfg)
	if s != nil {
		s.Stop()
	}
	if err != nil {
		return "", fmt.Errorf("persona generation failed: %w", err)
	}

	return personaYAML, nil
}
