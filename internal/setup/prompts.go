package setup

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/huh"

	"github.com/dorgu-ai/dorgu/internal/output"
)

// PromptEnvironment prompts the user to select an environment.
// Returns "development" when non-interactive.
func PromptEnvironment() string {
	if !output.IsInteractive() {
		return "development"
	}
	var env string = "development"
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Environment").
				Options(
					huh.NewOption("Development", "development"),
					huh.NewOption("Staging", "staging"),
					huh.NewOption("Production", "production"),
					huh.NewOption("Sandbox", "sandbox"),
				).
				Value(&env),
		),
	)
	if err := form.Run(); err != nil {
		return "development"
	}
	return env
}

// PromptGitRepoURL prompts for the Git repository URL and validates it.
// Returns an error when non-interactive.
func PromptGitRepoURL() (string, error) {
	if !output.IsInteractive() {
		return "", fmt.Errorf("non-interactive mode: provide repository URL via flag")
	}
	var url string
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Git repository URL").
				Description("e.g., https://github.com/org/repo.git").
				Value(&url).
				Validate(func(s string) error {
					if s == "" {
						return fmt.Errorf("repository URL is required")
					}
					if !strings.HasPrefix(s, "https://") && !strings.HasPrefix(s, "git@") && !strings.HasPrefix(s, "ssh://") {
						return fmt.Errorf("must start with https://, git@, or ssh://")
					}
					return nil
				}),
		),
	)
	if err := form.Run(); err != nil {
		return "", fmt.Errorf("cancelled: %w", err)
	}
	return url, nil
}

// PromptGitOpsOutputDir prompts for the output directory with a default.
func PromptGitOpsOutputDir(defaultDir string) string {
	if !output.IsInteractive() {
		return defaultDir
	}
	dir := defaultDir
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Output directory").
				Value(&dir),
		),
	)
	if err := form.Run(); err != nil {
		return defaultDir
	}
	if dir == "" {
		return defaultDir
	}
	return dir
}

// ConfirmGitOpsPush prints push instructions after scaffolding.
func ConfirmGitOpsPush(repoURL, outputDir string) {
	fmt.Println()
	output.Header("Next: Push to Git and apply")
	fmt.Println()
	output.DimPrint("  cd " + outputDir)
	output.DimPrint("  git init && git add -A && git commit -m 'Initial cluster GitOps scaffold'")
	output.DimPrint("  git remote add origin " + repoURL)
	output.DimPrint("  git push -u origin main")
	output.DimPrint("  kubectl apply -f argocd/root-app.yaml")
	fmt.Println()
}

// BootstrapAction represents user's choice for ArgoCD bootstrap.
type BootstrapAction string

const (
	BootstrapActionInstall BootstrapAction = "install"
	BootstrapActionSkip    BootstrapAction = "skip"
	BootstrapActionAbort   BootstrapAction = "abort"
)

// PromptArgoCDBootstrap asks user how to handle missing ArgoCD in GitOps mode.
func PromptArgoCDBootstrap() BootstrapAction {
	if !output.IsInteractive() {
		return BootstrapActionAbort
	}

	fmt.Println()
	output.Warn("ArgoCD is required to apply the GitOps scaffold")
	fmt.Println()

	var action string = "install"
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Choose bootstrap method").
				Options(
					huh.NewOption("Install ArgoCD via Helm now (recommended)", "install"),
					huh.NewOption("Continue without ArgoCD — I will install it manually", "skip"),
					huh.NewOption("Abort", "abort"),
				).
				Value(&action),
		),
	)
	if err := form.Run(); err != nil {
		return BootstrapActionAbort
	}
	return BootstrapAction(action)
}

// PromptComponentSelection prints the step header, component description, and
// prompts the user to confirm installation. Returns true if the component should be installed.
// For Required components, returns true without prompting.
func PromptComponentSelection(c ComponentConfig, stepNum, totalSteps int) bool {
	printStepHeader(os.Stdout, c, stepNum, totalSteps)

	fmt.Println(c.WhyItMatters)
	fmt.Println()
	output.DimPrint(fmt.Sprintf("  Chart:     %s", c.HelmChart))
	output.DimPrint(fmt.Sprintf("  Version:   %s", c.Version))
	output.DimPrint(fmt.Sprintf("  Namespace: %s", c.Namespace))
	fmt.Println()

	if c.Required {
		output.Info("[Required — will be installed]")
		return true
	}

	if !output.IsInteractive() {
		return c.DefaultEnabled
	}

	install := c.DefaultEnabled
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title(fmt.Sprintf("Install %s?", c.DisplayName)).
				Value(&install),
		),
	)
	if err := form.Run(); err != nil {
		return c.DefaultEnabled
	}
	return install
}

// ConfirmProceed prompts the user to confirm proceeding.
// Returns false when non-interactive (safe default).
func ConfirmProceed() bool {
	if !output.IsInteractive() {
		return false
	}
	var proceed bool
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Proceed?").
				Value(&proceed),
		),
	)
	if err := form.Run(); err != nil {
		return false
	}
	return proceed
}

// FailedComponentAction represents user's choice when a component fails.
type FailedComponentAction string

const (
	ActionRetry FailedComponentAction = "retry"
	ActionSkip  FailedComponentAction = "skip"
	ActionAbort FailedComponentAction = "abort"
)

// PromptFailedComponentAction asks user what to do when a component fails.
func PromptFailedComponentAction(c ComponentConfig, classifiedErr *ClassifiedError) FailedComponentAction {
	if !output.IsInteractive() {
		return ActionAbort
	}

	fmt.Println()
	output.Error(fmt.Sprintf("✗ %s installation failed", c.DisplayName))

	switch classifiedErr.Category {
	case ErrorCategoryTransient:
		output.Info("  Error appears to be transient (network/timeout)")
	case ErrorCategoryConfiguration:
		output.Warn("  Error appears to be a configuration issue (requires manual fix)")
	case ErrorCategoryUnknown:
		output.Info("  Error type unknown")
	}
	fmt.Println()

	var action string = "abort"
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("What would you like to do?").
				Options(
					huh.NewOption("Retry — try installing this component again", "retry"),
					huh.NewOption("Skip — skip this component and continue", "skip"),
					huh.NewOption("Abort — cancel the entire installation", "abort"),
				).
				Value(&action),
		),
	)
	if err := form.Run(); err != nil {
		return ActionAbort
	}
	return FailedComponentAction(action)
}
