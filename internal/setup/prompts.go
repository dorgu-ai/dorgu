package setup

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/dorgu-ai/dorgu/internal/output"
)

// PromptEnvironment prompts: "? Environment [development/staging/production/sandbox]:"
// Validates against known environments; retries on invalid input.
// Default: "development".
func PromptEnvironment(r *bufio.Reader) string {
	validEnvs := map[string]bool{"development": true, "staging": true, "production": true, "sandbox": true}
	for {
		fmt.Printf("? Environment [development/staging/production/sandbox]: ")
		input, _ := r.ReadString('\n')
		input = strings.TrimSpace(input)
		if input == "" {
			return "development"
		}
		if validEnvs[input] {
			return input
		}
		fmt.Printf("  Invalid environment %q. Choose: development, staging, production, sandbox\n", input)
	}
}

// PromptGitRepoURL prompts for the Git repository URL and validates it.
// Retries up to 3 times on invalid input.
func PromptGitRepoURL(r *bufio.Reader) (string, error) {
	for attempt := 0; attempt < 3; attempt++ {
		fmt.Printf("? Git repository URL (e.g., https://github.com/org/repo.git): ")
		input, _ := r.ReadString('\n')
		input = strings.TrimSpace(input)
		if input == "" {
			fmt.Println("  Repository URL is required.")
			continue
		}
		if !strings.HasPrefix(input, "https://") && !strings.HasPrefix(input, "git@") && !strings.HasPrefix(input, "ssh://") {
			fmt.Printf("  Invalid URL %q — must start with https://, git@, or ssh://\n", input)
			continue
		}
		return input, nil
	}
	return "", fmt.Errorf("no valid repository URL provided after 3 attempts")
}

// PromptGitOpsOutputDir prompts for the output directory with a default.
func PromptGitOpsOutputDir(r *bufio.Reader, defaultDir string) string {
	fmt.Printf("? Output directory [%s]: ", defaultDir)
	input, _ := r.ReadString('\n')
	input = strings.TrimSpace(input)
	if input == "" {
		return defaultDir
	}
	return input
}

// ConfirmGitOpsPush prints push instructions after scaffolding.
func ConfirmGitOpsPush(r *bufio.Reader, repoURL, outputDir string) {
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
func PromptArgoCDBootstrap(r *bufio.Reader) BootstrapAction {
	fmt.Println()
	output.Warn("ArgoCD is required to apply the GitOps scaffold")
	fmt.Println()
	fmt.Println("  Choose bootstrap method:")
	fmt.Println("    [1] Install ArgoCD via Helm now (recommended for first-time setup)")
	fmt.Println("    [2] Continue without ArgoCD — I will install it manually")
	fmt.Println("    [3] Abort")
	fmt.Println()

	for {
		fmt.Printf("  Choice [1/2/3]: ")
		input, _ := r.ReadString('\n')
		input = strings.TrimSpace(input)

		switch input {
		case "1", "install":
			return BootstrapActionInstall
		case "2", "skip", "continue":
			return BootstrapActionSkip
		case "3", "abort":
			return BootstrapActionAbort
		default:
			fmt.Printf("  Invalid choice %q. Enter 1, 2, or 3.\n", input)
		}
	}
}

// PromptComponentSelection prints the step header, component description, and (if not Required)
// prompts: "Install <DisplayName>? [y/N]:". Returns true if the component should be installed.
// For Required components, prints "[Required — will be installed]" and returns true without prompting.
func PromptComponentSelection(r *bufio.Reader, c ComponentConfig, stepNum, totalSteps int) bool {
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

	defaultStr := "y/N"
	if c.DefaultEnabled {
		defaultStr = "Y/n"
	}
	fmt.Printf("Install %s? [%s]: ", c.DisplayName, defaultStr)
	input, _ := r.ReadString('\n')
	input = strings.ToLower(strings.TrimSpace(input))

	if input == "" {
		return c.DefaultEnabled
	}
	return input == "y" || input == "yes"
}

// ConfirmProceed prompts: "Proceed? [y/N]:" and returns true if user confirmed.
func ConfirmProceed(r *bufio.Reader) bool {
	fmt.Printf("Proceed? [y/N]: ")
	input, _ := r.ReadString('\n')
	input = strings.ToLower(strings.TrimSpace(input))
	return input == "y" || input == "yes"
}

// FailedComponentAction represents user's choice when a component fails.
type FailedComponentAction string

const (
	ActionRetry FailedComponentAction = "retry"
	ActionSkip  FailedComponentAction = "skip"
	ActionAbort FailedComponentAction = "abort"
)

// PromptFailedComponentAction asks user what to do when a component fails.
// Returns: retry, skip, or abort.
func PromptFailedComponentAction(r *bufio.Reader, c ComponentConfig, classifiedErr *ClassifiedError) FailedComponentAction {
	fmt.Println()
	output.Error(fmt.Sprintf("✗ %s installation failed", c.DisplayName))

	// Show error category context
	switch classifiedErr.Category {
	case ErrorCategoryTransient:
		output.Info("  Error appears to be transient (network/timeout)")
	case ErrorCategoryConfiguration:
		output.Warn("  Error appears to be a configuration issue (requires manual fix)")
	case ErrorCategoryUnknown:
		output.Info("  Error type unknown")
	}

	fmt.Println()
	fmt.Println("  What would you like to do?")
	fmt.Println("    [R]etry - try installing this component again")
	fmt.Println("    [S]kip  - skip this component and continue with remaining components")
	fmt.Println("    [A]bort - cancel the entire installation")
	fmt.Println()

	for {
		fmt.Printf("  Choice [R/S/A]: ")
		input, _ := r.ReadString('\n')
		input = strings.ToLower(strings.TrimSpace(input))

		switch input {
		case "r", "retry":
			return ActionRetry
		case "s", "skip":
			return ActionSkip
		case "a", "abort":
			return ActionAbort
		default:
			fmt.Printf("  Invalid choice %q. Enter R, S, or A.\n", input)
		}
	}
}
