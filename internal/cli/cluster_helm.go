package cli

import (
	"fmt"
	"os"
	"time"

	"github.com/dorgu-ai/dorgu/internal/output"
	"github.com/dorgu-ai/dorgu/internal/setup"
)

func runHelmSetup(ex setup.Executor, personaName, environment string, selected []setup.ComponentConfig) error {
	cfg := setup.SetupConfig{
		ClusterPersonaName: personaName,
		Environment:        environment,
		Components:         selected,
		Timestamp:          time.Now(),
		SkipValidation:     clusterSetupFlags.skipValidation || clusterSetupFlags.dryRun,
	}

	setup.PrintInstallPlan(cfg)
	if !setup.ConfirmProceed() {
		output.Info("Aborted.")
		return nil
	}

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

	if err := setup.CheckChartAvailability(ex, cfg.Components); err != nil {
		return fmt.Errorf("preflight chart check failed: %w", err)
	}

	fmt.Println()
	output.Header("Installing components...")
	fmt.Println()

	var results []setup.InstallResult
	installed := make(map[setup.ComponentID]bool)

	for i, c := range cfg.Components {
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

		if !result.Succeeded {
			if classifiedErr, ok := result.Error.(*setup.ClassifiedError); ok {
				setup.PrintComponentResult(result)
				action := setup.PromptFailedComponentAction(c, classifiedErr)

				switch action {
				case setup.ActionRetry:
					output.Info(fmt.Sprintf("Retrying %s...", c.DisplayName))
					stop = setup.PrintComponentProgress(os.Stderr, c, i+1, len(cfg.Components))
					result = setup.InstallComponent(ex, c, cfg)
					stop()
					setup.PrintComponentResult(result)

				case setup.ActionSkip:
					output.Info(fmt.Sprintf("Skipping %s", c.DisplayName))
					result.Skipped = true

				case setup.ActionAbort:
					output.Warn("Installation aborted by user")
					return fmt.Errorf("installation aborted at component %s", c.DisplayName)
				}
			} else {
				setup.PrintComponentResult(result)
			}
		} else {
			setup.PrintComponentResult(result)
		}

		results = append(results, result)

		if result.Succeeded {
			installed[c.ID] = true
		}
	}

	fmt.Println()
	output.Header("Validating installation...")
	vrs := setup.ValidateAll(ex, results, cfg.SkipValidation)
	setup.PrintValidationResults(vrs)

	fmt.Printf("Annotating ClusterPersona %q... ", personaName)
	if err := setup.AnnotateClusterPersona(ex, personaName, cfg); err != nil {
		output.Error(fmt.Sprintf("annotation failed: %v", err))
	} else {
		output.Success("done")
	}

	setup.PrintFinalSummary(results, vrs, cfg)

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
