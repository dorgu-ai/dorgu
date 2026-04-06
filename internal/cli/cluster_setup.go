package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/dorgu-ai/dorgu/internal/output"
	"github.com/dorgu-ai/dorgu/internal/setup"
)

var clusterSetupFlags struct {
	clusterPersonaName string
	environment        string
	dryRun             bool
	skipValidation     bool
	driver             string
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

Use --driver to select the installation strategy:
  helm   — install components imperatively via Helm (default)
  gitops — scaffold a GitOps repository with ArgoCD Application manifests

Examples:
  dorgu cluster setup
  dorgu cluster setup --cluster-persona my-cluster --environment production
  dorgu cluster setup --dry-run
  dorgu cluster setup --driver gitops --gitops-output ./my-cluster-gitops`,
	RunE: runClusterSetup,
}

// clusterSetupShouldValidateExplicitClusterPersona reports whether cluster setup should run
// ValidateClusterPersonaExists for a user-provided --cluster-persona name before proceeding.
// Regression coverage: BUG-4-1 — this must be true in dry-run when the flag is set; current
// behavior skips validation in dry-run (returns false when dryRun is true).
func clusterSetupShouldValidateExplicitClusterPersona(dryRun bool, explicitNameFromFlag string) bool {
	if explicitNameFromFlag == "" {
		return false
	}
	return !dryRun
}

func init() {
	clusterSetupCmd.Flags().StringVar(&clusterSetupFlags.clusterPersonaName, "cluster-persona", "", "ClusterPersona name (auto-detected if not set)")
	clusterSetupCmd.Flags().StringVar(&clusterSetupFlags.environment, "environment", "", "environment override: development, staging, production")
	clusterSetupCmd.Flags().BoolVar(&clusterSetupFlags.dryRun, "dry-run", false, "print helm commands without executing them")
	clusterSetupCmd.Flags().BoolVar(&clusterSetupFlags.skipValidation, "skip-validation", false, "skip post-install pod health checks")
	clusterSetupCmd.Flags().StringVar(&clusterSetupFlags.driver, "driver", "helm", "installation driver: helm (default) or gitops")
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
		ctxTimeout, cancelCtx := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancelCtx()
		if _, err := exec.CommandContext(ctxTimeout, "kubectl", "config", "use-context", clusterSetupFlags.kubeContext).CombinedOutput(); err != nil {
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
				if output.IsInteractive() {
					var proceed bool
					if err := huh.NewForm(
						huh.NewGroup(
							huh.NewConfirm().
								Title("Are you sure you want to proceed with this context?").
								Value(&proceed),
						),
					).Run(); err != nil {
						output.Info("Aborted.")
						return nil
					}
					if !proceed {
						output.Info("Aborted.")
						return nil
					}
				} else {
					output.Info("Non-interactive mode: aborting due to risky kube-context")
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
		if clusterSetupShouldValidateExplicitClusterPersona(clusterSetupFlags.dryRun, clusterSetupFlags.clusterPersonaName) {
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
	environment := clusterSetupFlags.environment
	if environment == "" {
		environment = setup.PromptEnvironment()
	}

	// 7. Component selection
	allComponents := stack.Components()
	var selected []setup.ComponentConfig
	for i, c := range allComponents {
		if setup.PromptComponentSelection(c, i+1, len(allComponents)) {
			selected = append(selected, c)
		}
	}

	if len(selected) == 0 {
		output.Warn("No components selected. Exiting.")
		return nil
	}

	// 8. Dispatch based on driver
	driver, err := resolveDriver()
	if err != nil {
		return err
	}

	switch driver {
	case "gitops":
		return runGitOpsSetup(ex, personaName, environment, selected)
	case "helm":
		return runHelmSetup(ex, personaName, environment, selected)
	default:
		return fmt.Errorf("unsupported driver: %s", driver)
	}
}
