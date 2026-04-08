package setup

import (
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

// PrintInstallPlan prints the installation plan table with box-drawing characters.
func PrintInstallPlan(cfg SetupConfig) {
	printInstallPlanTo(os.Stdout, cfg)
}

func printInstallPlanTo(w io.Writer, cfg SetupConfig) {
	colComp := 22
	colVer := 11
	colNs := 16

	divComp := strings.Repeat("─", colComp+2)
	divVer := strings.Repeat("─", colVer+2)
	divNs := strings.Repeat("─", colNs+2)

	fmt.Fprintln(w)
	fmt.Fprintln(w, "  Installation Plan")
	fmt.Fprintln(w)
	fmt.Fprintf(w, "  ┌%s┬%s┬%s┐\n", divComp, divVer, divNs)
	fmt.Fprintf(w, "  │ %-*s │ %-*s │ %-*s │\n", colComp, "Component", colVer, "Version", colNs, "Namespace")
	fmt.Fprintf(w, "  ├%s┼%s┼%s┤\n", divComp, divVer, divNs)

	for _, c := range cfg.Components {
		version := c.Version
		if cfg.VersionOverrides != nil {
			if v, ok := cfg.VersionOverrides[c.ID]; ok && v != "" {
				version = v
			}
		}
		name := c.DisplayName
		runes := []rune(name)
		if len(runes) > colComp {
			name = string(runes[:colComp-1]) + "…"
		}
		ns := c.Namespace
		nsRunes := []rune(ns)
		if len(nsRunes) > colNs {
			ns = string(nsRunes[:colNs-1]) + "…"
		}
		fmt.Fprintf(w, "  │ %-*s │ %-*s │ %-*s │\n", colComp, name, colVer, version, colNs, ns)
	}
	fmt.Fprintf(w, "  └%s┴%s┴%s┘\n", divComp, divVer, divNs)
	fmt.Fprintln(w)
	fmt.Fprintf(w, "  Environment: %s\n", cfg.Environment)
	if cfg.ClusterPersonaName != "" {
		fmt.Fprintf(w, "  ClusterPersona: %s\n", cfg.ClusterPersonaName)
	}
	fmt.Fprintln(w)
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
	if len(vrs) > 0 && succeeded > 0 {
		allSkipped := true
		healthy := 0
		for _, vr := range vrs {
			if vr.Message != "skipped" {
				allSkipped = false
				if vr.Healthy {
					healthy++
				}
			}
		}
		if cfg.SkipValidation || allSkipped {
			fmt.Fprintf(w, "  Pods healthy: — (validation skipped)\n")
		} else {
			fmt.Fprintf(w, "  Pods healthy: %d/%d\n", healthy, succeeded)
		}
		fmt.Fprintln(w)
	}

	fmt.Fprintf(w, "  Next Steps\n")
	fmt.Fprintf(w, "  %s\n", strings.Repeat("─", 38))
	fmt.Fprintf(w, "  %s Check cluster status:   dorgu cluster status\n", output.Dim("→"))
	if cfg.ClusterPersonaName != "" {
		fmt.Fprintf(w, "  %s View ClusterPersona:    kubectl get clusterpersona %s -o yaml\n", output.Dim("→"), cfg.ClusterPersonaName)
	}
	fmt.Fprintf(w, "  %s Access component UIs:   dorgu cluster info\n", output.Dim("→"))
	fmt.Fprintf(w, "  %s Monitor with ArgoCD:    dorgu sync\n", output.Dim("→"))
	fmt.Fprintln(w)
}
