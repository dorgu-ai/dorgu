package cli

import (
	"bufio"
	"fmt"

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

func runGitOpsSetup(reader *bufio.Reader, ex setup.Executor, personaName, environment string, selected []setup.ComponentConfig) error {
	var repoURL string
	if !clusterSetupFlags.dryRun {
		var err error
		repoURL, err = setup.PromptGitRepoURL(reader)
		if err != nil {
			return fmt.Errorf("GitOps setup cancelled: %w", err)
		}
	}
	gitopsDir := setup.PromptGitOpsOutputDir(reader, clusterSetupFlags.gitopsOutputDir)

	// ArgoCD bootstrap check (only in non-dry-run mode)
	if !clusterSetupFlags.dryRun {
		argoCDInstalled := setup.IsArgoCDInstalled(ex)
		if !argoCDInstalled {
			action := setup.PromptArgoCDBootstrap(reader)
			switch action {
			case setup.BootstrapActionInstall:
				output.Info("Installing ArgoCD via Helm...")
				if err := setup.InstallArgoCDBootstrap(ex); err != nil {
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
		setup.ConfirmGitOpsPush(reader, repoURL, gitopsDir)
	}
	return nil
}
