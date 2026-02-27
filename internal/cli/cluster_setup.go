package cli

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/spf13/cobra"

	"github.com/dorgu-ai/dorgu/internal/output"
	"github.com/dorgu-ai/dorgu/internal/setup"
)

var clusterSetupFlags struct {
	clusterPersonaName string
	environment        string
	dryRun             bool
	skipValidation     bool
}

var clusterSetupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Interactively install a production-ready Kubernetes stack",
	Long: `Bootstrap your cluster with a curated, production-ready Kubernetes stack
via an interactive wizard. Each component is explained before installation.

The Blessed Stack includes:
  cert-manager     — automated TLS certificate management
  ingress-nginx    — HTTP/S ingress controller
  OpenObserve      — unified observability (logs, metrics, traces)
  External Secrets — secret sync from cloud stores (optional)

The result is recorded as annotations on your ClusterPersona CRD. The Dorgu
Operator will discover the installed components within 5 minutes.

Examples:
  dorgu cluster setup
  dorgu cluster setup --cluster-persona my-cluster --environment production
  dorgu cluster setup --dry-run`,
	RunE: runClusterSetup,
}

func init() {
	clusterSetupCmd.Flags().StringVar(&clusterSetupFlags.clusterPersonaName, "cluster-persona", "", "ClusterPersona name (auto-detected if not set)")
	clusterSetupCmd.Flags().StringVar(&clusterSetupFlags.environment, "environment", "", "environment override: development, staging, production")
	clusterSetupCmd.Flags().BoolVar(&clusterSetupFlags.dryRun, "dry-run", false, "print helm commands without executing them")
	clusterSetupCmd.Flags().BoolVar(&clusterSetupFlags.skipValidation, "skip-validation", false, "skip post-install pod health checks")
}

func runClusterSetup(cmd *cobra.Command, args []string) error {
	// 1. Preflight: kubectl and helm must be in PATH
	if _, err := exec.LookPath("kubectl"); err != nil {
		return fmt.Errorf("kubectl not found in PATH — required for cluster setup")
	}
	if _, err := exec.LookPath("helm"); err != nil {
		return fmt.Errorf("helm not found in PATH — install from https://helm.sh/docs/intro/install/")
	}

	// 2. Choose executor based on --dry-run
	var ex setup.Executor
	if clusterSetupFlags.dryRun {
		ex = &setup.DryRunExecutor{}
	} else {
		ex = &setup.OSExecutor{}
	}

	// 3. Resolve ClusterPersona name
	personaName := clusterSetupFlags.clusterPersonaName
	if personaName == "" {
		if clusterSetupFlags.dryRun {
			personaName = "<cluster-persona>" // placeholder for dry-run annotation output
		} else {
			var err error
			personaName, err = setup.AutoDetectClusterPersonaName(ex)
			if err != nil {
				return err
			}
		}
	}

	// 4. Get the active stack (BlessedStack for MVP; extensible via StackProvider)
	stack := setup.DefaultStack()

	// 5. Welcome banner
	setup.PrintWelcomeBanner()
	if clusterSetupFlags.dryRun {
		output.Warn("Dry-run mode: helm commands will be logged, not executed.")
		fmt.Println()
	}

	// 6. Environment: use flag value or prompt interactively
	reader := bufio.NewReader(os.Stdin)
	environment := clusterSetupFlags.environment
	if environment == "" {
		environment = setup.PromptEnvironment(reader)
	}

	// 7. Component selection
	allComponents := stack.Components()
	var selected []setup.ComponentConfig
	for i, c := range allComponents {
		if setup.PromptComponentSelection(reader, c, i+1, len(allComponents)) {
			selected = append(selected, c)
		}
	}

	if len(selected) == 0 {
		output.Warn("No components selected. Exiting.")
		return nil
	}

	// 8. Build SetupConfig
	cfg := setup.SetupConfig{
		ClusterPersonaName: personaName,
		Environment:        environment,
		Components:         selected,
		Timestamp:          time.Now(),
		// Skip validation in dry-run (nothing was actually installed)
		SkipValidation: clusterSetupFlags.skipValidation || clusterSetupFlags.dryRun,
	}

	// 9. Print plan and confirm
	setup.PrintInstallPlan(cfg)
	if !setup.ConfirmProceed(reader) {
		output.Info("Aborted.")
		return nil
	}

	// 10. Install each component
	fmt.Println()
	output.Header("Installing components...")
	fmt.Println()

	var results []setup.InstallResult
	for i, c := range cfg.Components {
		if err := setup.AddHelmRepo(ex, c.HelmRepoName, c.HelmRepo); err != nil {
			output.Warn(fmt.Sprintf("helm repo add %s: %v (continuing)", c.HelmRepoName, err))
		}
		stop := setup.PrintComponentProgress(os.Stderr, c, i+1, len(cfg.Components))
		result := setup.InstallComponent(ex, c, cfg)
		stop()
		setup.PrintComponentResult(result)
		results = append(results, result)
	}

	// 11. Validate
	fmt.Println()
	output.Header("Validating installation...")
	vrs := setup.ValidateAll(ex, results, cfg.SkipValidation)
	setup.PrintValidationResults(vrs)

	// 12. Annotate ClusterPersona (records setup intent)
	fmt.Printf("Annotating ClusterPersona %q... ", personaName)
	if err := setup.AnnotateClusterPersona(ex, personaName, cfg); err != nil {
		output.Error(fmt.Sprintf("annotation failed: %v", err))
	} else {
		output.Success("done")
	}

	// 13. Final summary
	setup.PrintFinalSummary(results, vrs, cfg)

	// In dry-run mode, show the command log
	if drex, ok := ex.(*setup.DryRunExecutor); ok && len(drex.Log) > 0 {
		fmt.Println()
		output.Header("Dry-run command log")
		for _, entry := range drex.Log {
			output.DimPrint("  " + entry)
		}
		fmt.Println()
	}

	return nil
}
