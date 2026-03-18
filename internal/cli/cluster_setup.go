package cli

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"
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
	kubeContext        string
	verbose            bool
}

var clusterSetupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Interactively install a production-ready Kubernetes stack",
	Long: `Bootstrap your cluster with a curated, production-ready Kubernetes stack
via an interactive wizard. Each component is explained before installation.

The Blessed Stack includes:
  cert-manager     — automated TLS certificate management
  ingress-nginx    — HTTP/S ingress controller
  CloudNativePG    — PostgreSQL operator (required by OpenObserve)
  OpenObserve      — unified observability (logs, metrics, traces)
  Argo CD          — declarative GitOps continuous delivery
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
	clusterSetupCmd.Flags().StringVar(&clusterSetupFlags.kubeContext, "context", "", "kube-context to use (defaults to current-context)")
	clusterSetupCmd.Flags().BoolVar(&clusterSetupFlags.verbose, "verbose", false, "stream real-time Helm output during installation (dim styling)")
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

	// 1b. Kube-context safety guard
	if clusterSetupFlags.kubeContext != "" {
		if _, err := exec.Command("kubectl", "config", "use-context", clusterSetupFlags.kubeContext).CombinedOutput(); err != nil {
			return fmt.Errorf("failed to switch to kube-context %q: %w", clusterSetupFlags.kubeContext, err)
		}
		output.Success(fmt.Sprintf("Using kube-context: %q", clusterSetupFlags.kubeContext))
	}

	// Always show current context before proceeding
	{
		realEx := &setup.OSExecutor{}
		detected, err := setup.GetCurrentKubeContext(realEx)
		if err != nil {
			output.Warn("Could not detect kube-context — proceeding with default")
		} else {
			output.Success(fmt.Sprintf("kube-context: %q", detected))
			needsConfirm, warning := setup.ValidateKubeContext(detected)
			if needsConfirm {
				output.Warn(warning)
				confirmReader := bufio.NewReader(os.Stdin)
				fmt.Printf("Are you sure you want to proceed with this context? [y/N]: ")
				input, _ := confirmReader.ReadString('\n')
				input = strings.ToLower(strings.TrimSpace(input))
				if input != "y" && input != "yes" {
					output.Info("Aborted.")
					return nil
				}
			}
		}
	}

	// 1c. Operator readiness gate (skip in dry-run)
	if !clusterSetupFlags.dryRun {
		checkEx := &setup.OSExecutor{}
		if err := setup.CheckOperatorInstalled(checkEx); err != nil {
			output.ErrorWithHint("Dorgu Operator not detected on this cluster",
				err.Error(),
				"Install: helm install dorgu-operator oci://ghcr.io/dorgu-ai/dorgu-operator-charts/dorgu-operator -n dorgu-system --create-namespace")
			return errSilent
		}
		output.Success("Dorgu Operator detected")
	} else {
		output.Info("Dry-run mode: skipping operator readiness check")
	}

	// 2. Choose executor based on --dry-run / --verbose
	var ex setup.Executor
	if clusterSetupFlags.dryRun {
		ex = &setup.DryRunExecutor{}
	} else if clusterSetupFlags.verbose {
		ex = &setup.StreamingExecutor{
			StreamTo: os.Stderr,
			Dim:      true,
		}
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
		if !clusterSetupFlags.dryRun {
			checkEx := &setup.OSExecutor{}
			if err := setup.ValidateClusterPersonaExists(checkEx, personaName); err != nil {
				output.ErrorWithHint(
					fmt.Sprintf("ClusterPersona %q not found in cluster", personaName),
					"List available: dorgu cluster status",
					"Create one: dorgu cluster init --name <name> --environment <env>")
				return errSilent
			}
		}
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
		var repoURL string
		if !clusterSetupFlags.dryRun {
			var err error
			repoURL, err = setup.PromptGitRepoURL(reader)
			if err != nil {
				return fmt.Errorf("GitOps setup cancelled: %w", err)
			}
		}
		gitopsDir := setup.PromptGitOpsOutputDir(reader, clusterSetupFlags.gitopsOutputDir)

		err := setup.ScaffoldGitOpsRepo(setup.GitOpsConfig{
			OutputDir:          gitopsDir,
			ClusterPersonaName: personaName,
			Environment:        environment,
			Components:         selected,
			DryRun:             clusterSetupFlags.dryRun,
			RepoURL:            repoURL,
		})
		if err != nil {
			return err
		}
		if !clusterSetupFlags.dryRun {
			setup.ConfirmGitOpsPush(reader, repoURL, gitopsDir)
		}
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

	// 11. Install each component (with dependency enforcement)
	fmt.Println()
	output.Header("Installing components...")
	fmt.Println()

	var results []setup.InstallResult
	installed := make(map[setup.ComponentID]bool)

	for i, c := range cfg.Components {
		// Check dependencies: all DependsOn must be in the installed set
		depsMet := true
		var missingDep setup.ComponentID
		for _, dep := range c.DependsOn {
			if !installed[dep] {
				depsMet = false
				missingDep = dep
				break
			}
		}
		if !depsMet {
			result := setup.InstallResult{
				Component: c,
				Succeeded: false,
				Error:     fmt.Errorf("dependency %q not installed — skipping %s", missingDep, c.DisplayName),
			}
			setup.PrintComponentResult(result)
			results = append(results, result)
			continue
		}

		stop := setup.PrintComponentProgress(os.Stderr, c, i+1, len(cfg.Components))
		result := setup.InstallComponent(ex, c, cfg)
		stop()
		setup.PrintComponentResult(result)
		results = append(results, result)

		if result.Succeeded {
			installed[c.ID] = true
		}
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
