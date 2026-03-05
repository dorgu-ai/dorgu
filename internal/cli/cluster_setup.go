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
	gitops             bool
	gitopsOutputDir    string
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
  Argo CD          — declarative GitOps continuous delivery (optional, default on)
  External Secrets — secret sync from cloud stores (optional)

The result is recorded as annotations on your ClusterPersona CRD. The Dorgu
Operator will discover the installed components within 5 minutes.

Use --gitops to scaffold a GitOps repository with ArgoCD Application manifests
instead of installing components imperatively.

Examples:
  dorgu cluster setup
  dorgu cluster setup --cluster-persona my-cluster --environment production
  dorgu cluster setup --dry-run
  dorgu cluster setup --gitops --gitops-output ./my-cluster-gitops`,
	RunE: runClusterSetup,
}

func init() {
	clusterSetupCmd.Flags().StringVar(&clusterSetupFlags.clusterPersonaName, "cluster-persona", "", "ClusterPersona name (auto-detected if not set)")
	clusterSetupCmd.Flags().StringVar(&clusterSetupFlags.environment, "environment", "", "environment override: development, staging, production")
	clusterSetupCmd.Flags().BoolVar(&clusterSetupFlags.dryRun, "dry-run", false, "print helm commands without executing them")
	clusterSetupCmd.Flags().BoolVar(&clusterSetupFlags.skipValidation, "skip-validation", false, "skip post-install pod health checks")
	clusterSetupCmd.Flags().BoolVar(&clusterSetupFlags.gitops, "gitops", false, "scaffold a GitOps repository instead of installing imperatively")
	clusterSetupCmd.Flags().StringVar(&clusterSetupFlags.gitopsOutputDir, "gitops-output", "./dorgu-cluster-gitops", "output directory for GitOps repo scaffold")
}

func runClusterSetup(cmd *cobra.Command, args []string) error {
	// 1. Preflight: kubectl and helm must be in PATH
	fmt.Println()
	output.Info("Preflight checks...")
	if _, err := exec.LookPath("kubectl"); err != nil {
		return fmt.Errorf("kubectl not found in PATH — required for cluster setup")
	}
	output.Success("kubectl found")
	if _, err := exec.LookPath("helm"); err != nil {
		return fmt.Errorf("helm not found in PATH — install from https://helm.sh/docs/intro/install/")
	}
	output.Success("helm found")

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
			output.Info("Dry-run mode: skipping ClusterPersona detection")
		} else {
			var err error
			personaName, err = setup.AutoDetectClusterPersonaName(ex)
			if err != nil {
				output.ErrorWithHint("No ClusterPersona found in cluster",
					"Create one first: dorgu cluster init --name <cluster-name> --environment <env>",
					"Or specify one: dorgu cluster setup --cluster-persona <name>")
				return errSilent
			}
			output.Success(fmt.Sprintf("ClusterPersona detected: %q", personaName))
		}
	} else {
		output.Success(fmt.Sprintf("ClusterPersona: %q (from --cluster-persona flag)", personaName))
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

	// GitOps mode: scaffold a repo instead of imperatively installing
	if clusterSetupFlags.gitops {
		return setup.ScaffoldGitOpsRepo(setup.GitOpsConfig{
			OutputDir:          clusterSetupFlags.gitopsOutputDir,
			ClusterPersonaName: personaName,
			Environment:        environment,
			Components:         selected,
			DryRun:             clusterSetupFlags.dryRun,
		})
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

	// 10. Add all helm repos and update indices
	fmt.Println()
	output.Header("Preparing Helm repositories...")
	added := make(map[string]bool)
	for _, c := range cfg.Components {
		if added[c.HelmRepoName] {
			continue
		}
		if err := setup.AddHelmRepo(ex, c.HelmRepoName, c.HelmRepo); err != nil {
			output.Warn(fmt.Sprintf("helm repo add %s: %v (continuing)", c.HelmRepoName, err))
		}
		added[c.HelmRepoName] = true
	}
	if err := setup.UpdateHelmRepos(ex); err != nil {
		output.Warn(fmt.Sprintf("helm repo update: %v (continuing)", err))
	}
	output.Success("Helm repositories ready")

	// 10b. Preflight: verify chart versions exist
	if err := setup.CheckChartAvailability(ex, cfg.Components); err != nil {
		return fmt.Errorf("preflight chart check failed: %w", err)
	}

	// 11. Install each component
	fmt.Println()
	output.Header("Installing components...")
	fmt.Println()

	var results []setup.InstallResult
	for i, c := range cfg.Components {
		stop := setup.PrintComponentProgress(os.Stderr, c, i+1, len(cfg.Components))
		result := setup.InstallComponent(ex, c, cfg)
		stop()
		setup.PrintComponentResult(result)
		results = append(results, result)
	}

	// 12. Validate
	fmt.Println()
	output.Header("Validating installation...")
	vrs := setup.ValidateAll(ex, results, cfg.SkipValidation)
	setup.PrintValidationResults(vrs)

	// 13. Annotate ClusterPersona (records setup intent)
	fmt.Printf("Annotating ClusterPersona %q... ", personaName)
	if err := setup.AnnotateClusterPersona(ex, personaName, cfg); err != nil {
		output.Error(fmt.Sprintf("annotation failed: %v", err))
	} else {
		output.Success("done")
	}

	// 14. Final summary
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
