package cli

import (
	"fmt"
	"os"
	"time"

	"github.com/dorgu-ai/dorgu/internal/output"
	"github.com/dorgu-ai/dorgu/internal/setup"
)

// resolveDriver determines the installation driver based on flags.
func resolveDriver() (string, error) {
	switch clusterSetupFlags.driver {
	case "helm", "gitops":
		return clusterSetupFlags.driver, nil
	default:
		return "", fmt.Errorf("unknown driver %q — supported drivers: helm, gitops", clusterSetupFlags.driver)
	}
}

func runGitOpsSetup(ex setup.Executor, personaName, environment string, selected []setup.ComponentConfig) error {
	var repoURL string
	if !clusterSetupFlags.dryRun {
		var err error
		repoURL, err = setup.PromptGitRepoURL()
		if err != nil {
			return fmt.Errorf("GitOps setup cancelled: %w", err)
		}
	}
	gitopsDir := setup.PromptGitOpsOutputDir(clusterSetupFlags.gitopsOutputDir)

	// ArgoCD bootstrap check (only in non-dry-run mode)
	if !clusterSetupFlags.dryRun {
		argoCDInstalled := setup.IsArgoCDInstalled(ex)
		if !argoCDInstalled {
			action := setup.PromptArgoCDBootstrap()
			switch action {
			case setup.BootstrapActionInstall:
				output.Info("Installing ArgoCD via Helm...")
				var argoCDEx setup.Executor
				if clusterSetupFlags.verbose {
					argoCDEx = &setup.StreamingExecutor{
						StreamTo: os.Stderr,
						Dim:      true,
					}
				} else {
					argoCDEx = &setup.TailExecutor{
						StreamTo:  os.Stderr,
						Dim:       true,
						TailLines: 5,
					}
				}
				if err := setup.InstallArgoCDBootstrap(argoCDEx); err != nil {
					return fmt.Errorf("ArgoCD bootstrap failed: %w", err)
				}
				output.Success("ArgoCD installed successfully")

			case setup.BootstrapActionSkip:
				output.Warn("Proceeding without ArgoCD — you must install ArgoCD before applying root-app.yaml")
				output.Info("To install ArgoCD manually: helm install argocd argo/argo-cd -n argocd --create-namespace")

			case setup.BootstrapActionAbort:
				output.Info("Setup aborted")
				return nil
			}
		} else {
			output.Success("ArgoCD is already installed")
		}
	}

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
		// Record scaffolded components on the ClusterPersona so 'dorgu cluster info'
		// can report them without waiting for ArgoCD to sync.
		results := make([]setup.InstallResult, len(selected))
		for i, c := range selected {
			results[i] = setup.InstallResult{Component: c, Succeeded: true}
		}
		setupCfg := setup.SetupConfig{
			ClusterPersonaName: personaName,
			Environment:        environment,
			Timestamp:          time.Now(),
		}
		if annotateErr := setup.AnnotateClusterPersona(ex, personaName, setupCfg, results); annotateErr != nil {
			output.Warn("Could not annotate ClusterPersona with setup info: " + annotateErr.Error())
		}
		if driverErr := setup.AnnotateDriver(ex, personaName, "gitops"); driverErr != nil {
			output.Warn("Could not record setup driver on ClusterPersona: " + driverErr.Error())
		}
		setup.ConfirmGitOpsPush(repoURL, gitopsDir)
	}
	return nil
}
