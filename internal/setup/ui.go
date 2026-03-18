package setup

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/briandowns/spinner"
	"github.com/charmbracelet/lipgloss"
	"github.com/dorgu-ai/dorgu/internal/output"
)

// PrintWelcomeBanner prints the top banner box and preamble.
func PrintWelcomeBanner() {
	fmt.Println()
	fmt.Println("┌─────────────────────────────────────────────────────────────┐")
	fmt.Println("│          dorgu cluster setup — Blessed Stack Wizard          │")
	fmt.Println("└─────────────────────────────────────────────────────────────┘")
	fmt.Println()
	output.Info("This wizard installs a curated, production-ready Kubernetes stack.")
	output.Info("Each component will be explained before you are prompted to install it.")
	fmt.Println()
}

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

// printStepHeader writes a lipgloss-styled step header box to w.
// Using lipgloss eliminates the byte-slicing truncation bug that could
// corrupt multibyte UTF-8 characters.
func printStepHeader(w io.Writer, c ComponentConfig, stepNum, totalSteps int) {
	headerStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("39")).
		Padding(0, 1).
		Width(45)

	stepLine := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")).
		Render(fmt.Sprintf("Step %d of %d", stepNum, totalSteps))
	titleLine := lipgloss.NewStyle().Bold(true).Render(c.DisplayName)
	descLine := lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render(c.Description)

	content := fmt.Sprintf("%s\n%s\n%s", stepLine, titleLine, descLine)
	fmt.Fprintln(w)
	fmt.Fprintln(w, headerStyle.Render(content))
	fmt.Fprintln(w)
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

// PrintInstallPlan prints the installation plan table with box-drawing characters.
func PrintInstallPlan(cfg SetupConfig) {
	printInstallPlanTo(os.Stdout, cfg)
}

func printInstallPlanTo(w io.Writer, cfg SetupConfig) {
	colComp := 22
	colVer := 11
	colNs := 16

	divComp := strings.Repeat("─", colComp)
	divVer := strings.Repeat("─", colVer)
	divNs := strings.Repeat("─", colNs)

	fmt.Fprintln(w)
	fmt.Fprintln(w, "  Installation Plan")
	fmt.Fprintln(w)
	fmt.Fprintf(w, "  ┌%s┬%s┬%s┐\n", strings.Repeat("─", colComp+2), strings.Repeat("─", colVer+2), strings.Repeat("─", colNs+2))
	fmt.Fprintf(w, "  │ %-*s │ %-*s │ %-*s │\n", colComp, "Component", colVer, "Version", colNs, "Namespace")
	fmt.Fprintf(w, "  ├%s┼%s┼%s┤\n", strings.Repeat("─", colComp+2), strings.Repeat("─", colVer+2), strings.Repeat("─", colNs+2))
	_ = divComp
	_ = divVer
	_ = divNs

	for _, c := range cfg.Components {
		version := c.Version
		if cfg.VersionOverrides != nil {
			if v, ok := cfg.VersionOverrides[c.ID]; ok && v != "" {
				version = v
			}
		}
		name := c.DisplayName
		if len(name) > colComp {
			name = name[:colComp-1] + "…"
		}
		ns := c.Namespace
		if len(ns) > colNs {
			ns = ns[:colNs-1] + "…"
		}
		fmt.Fprintf(w, "  │ %-*s │ %-*s │ %-*s │\n", colComp, name, colVer, version, colNs, ns)
	}
	fmt.Fprintf(w, "  └%s┴%s┴%s┘\n", strings.Repeat("─", colComp+2), strings.Repeat("─", colVer+2), strings.Repeat("─", colNs+2))
	fmt.Fprintln(w)
	fmt.Fprintf(w, "  Environment: %s\n", cfg.Environment)
	if cfg.ClusterPersonaName != "" {
		fmt.Fprintf(w, "  ClusterPersona: %s\n", cfg.ClusterPersonaName)
	}
	fmt.Fprintln(w)
}

// ConfirmProceed prompts: "Proceed? [y/N]:" and returns true if user confirmed.
func ConfirmProceed(r *bufio.Reader) bool {
	fmt.Printf("Proceed? [y/N]: ")
	input, _ := r.ReadString('\n')
	input = strings.ToLower(strings.TrimSpace(input))
	return input == "y" || input == "yes"
}

// PrintComponentProgress returns a stop() function. Call it as:
//
//	stop := PrintComponentProgress(w, c, stepNum, totalSteps)
//	defer stop()
//
// Shows a spinner with "[N/M] Installing <DisplayName>... (elapsed)".
// Accepts an io.Writer for the spinner output (use os.Stderr in production, a buffer in tests).
func PrintComponentProgress(w io.Writer, c ComponentConfig, stepNum, totalSteps int) func() {
	s := spinner.New(spinner.CharSets[14], 100*time.Millisecond, spinner.WithWriter(w))
	start := time.Now()
	s.Suffix = fmt.Sprintf(" [%d/%d] Installing %s...", stepNum, totalSteps, c.DisplayName)
	s.Start()

	done := make(chan struct{})
	go func() {
		tick := time.NewTicker(time.Second)
		defer tick.Stop()
		for {
			select {
			case <-done:
				return
			case <-tick.C:
				elapsed := time.Since(start).Round(time.Second)
				s.Suffix = fmt.Sprintf(" [%d/%d] Installing %s... (%s)",
					stepNum, totalSteps, c.DisplayName, elapsed)
			}
		}
	}()

	return func() {
		close(done)
		s.Stop()
	}
}

// PrintComponentResult prints ✓ / ✗ result for a single component install.
// On failure, prints inline diagnostic hints to help the user debug.
func PrintComponentResult(r InstallResult) {
	if r.Skipped {
		output.Warn(fmt.Sprintf("Skipped %s", r.Component.DisplayName))
		return
	}
	if r.Succeeded {
		output.Success(fmt.Sprintf("%s installed (%s)", r.Component.DisplayName, r.Duration.Round(time.Second)))
		if r.Component.PostInstallMessage != "" {
			output.Info(r.Component.PostInstallMessage)
		}
	} else {
		// Extract a concise error message
		errMsg := ""
		if r.Error != nil {
			errMsg = r.Error.Error()
			// Use only the first line for the inline summary
			if idx := strings.Index(errMsg, "\n"); idx >= 0 {
				errMsg = strings.TrimSpace(errMsg[:idx])
			}
			if len(errMsg) > 80 {
				errMsg = errMsg[:77] + "..."
			}
		}
		output.Error(fmt.Sprintf("%s failed (%s)", r.Component.DisplayName, errMsg))

		// Always show full Helm output on failure for debugging
		if r.HelmOutput != "" {
			output.DimPrint("  Full Helm output:")
			for _, line := range strings.Split(r.HelmOutput, "\n") {
				if strings.TrimSpace(line) != "" {
					output.DimPrint("    " + line)
				}
			}
		}

		// Inline diagnostic hints
		output.DimPrint(fmt.Sprintf("  → Check pod status: kubectl get pods -n %s", r.Component.Namespace))
		output.DimPrint(fmt.Sprintf("  → Check events: kubectl get events -n %s --sort-by=.lastTimestamp", r.Component.Namespace))
		output.DimPrint("  → Retry: dorgu cluster setup --cluster-persona <name>")
	}
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

// PrintValidationResults prints ✓ / ✗ for each ValidationResult.
func PrintValidationResults(vrs []ValidationResult) {
	fmt.Println()
	output.Header("Validation Results")
	for _, vr := range vrs {
		if vr.Healthy {
			output.Success(fmt.Sprintf("%-28s %s", string(vr.ComponentID), vr.Message))
		} else {
			output.Error(fmt.Sprintf("%-28s %s", string(vr.ComponentID), vr.Message))
		}
	}
	fmt.Println()
}

// PrintFinalSummary prints the completion box and next steps.
func PrintFinalSummary(results []InstallResult, vrs []ValidationResult, cfg SetupConfig) {
	printFinalSummaryTo(os.Stdout, results, vrs, cfg)
}

func printFinalSummaryTo(w io.Writer, results []InstallResult, vrs []ValidationResult, cfg SetupConfig) {
	succeeded := 0
	failed := 0
	skipped := 0
	var totalDuration time.Duration
	for _, r := range results {
		totalDuration += r.Duration
		switch {
		case r.Skipped:
			skipped++
		case r.Succeeded:
			succeeded++
		default:
			failed++
		}
	}

	// Choose border color: red if any failures, green otherwise
	borderColor := lipgloss.Color("42") // green
	if failed > 0 {
		borderColor = lipgloss.Color("196") // red
	}

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Padding(0, 1).
		Width(44)

	installed := output.Green(fmt.Sprintf("✓ Installed: %d", succeeded))
	skippedStr := output.Dim(fmt.Sprintf("⊘ Skipped: %d", skipped))
	failedStr := output.Red(fmt.Sprintf("✗ Failed: %d", failed))
	elapsed := fmt.Sprintf("⏱ Total: %s", totalDuration.Round(time.Second))

	line1 := fmt.Sprintf("%-30s %s", installed, skippedStr)
	line2 := fmt.Sprintf("%-30s %s", failedStr, elapsed)

	title := lipgloss.NewStyle().Bold(true).Render("Setup Complete")
	content := fmt.Sprintf("%s\n%s\n%s", title, line1, line2)

	fmt.Fprintln(w)
	fmt.Fprintln(w, boxStyle.Render(content))
	fmt.Fprintln(w)

	// Health summary
	healthy := 0
	for _, vr := range vrs {
		if vr.Healthy && vr.Message != "skipped" {
			healthy++
		}
	}
	if len(vrs) > 0 && succeeded > 0 {
		fmt.Fprintf(w, "  Pods healthy: %d/%d\n", healthy, succeeded)
		fmt.Fprintln(w)
	}

	fmt.Fprintf(w, "  Next Steps\n")
	fmt.Fprintf(w, "  %s\n", strings.Repeat("─", 38))
	fmt.Fprintf(w, "  %s Check cluster status:   dorgu cluster status\n", output.Dim("→"))
	if cfg.ClusterPersonaName != "" {
		fmt.Fprintf(w, "  %s View ClusterPersona:    kubectl get clusterpersona %s -o yaml\n", output.Dim("→"), cfg.ClusterPersonaName)
	}
	fmt.Fprintf(w, "  %s Monitor with ArgoCD:    dorgu sync\n", output.Dim("→"))
	fmt.Fprintln(w)
}
