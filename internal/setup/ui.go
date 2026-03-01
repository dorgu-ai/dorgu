package setup

import (
	"bufio"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/briandowns/spinner"
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

// PromptEnvironment prompts: "? Environment [development/staging/production]:"
// Returns the entered value (trimmed). Default: "development".
func PromptEnvironment(r *bufio.Reader) string {
	fmt.Printf("? Environment [development/staging/production]: ")
	input, _ := r.ReadString('\n')
	input = strings.TrimSpace(input)
	if input == "" {
		return "development"
	}
	return input
}

// PromptComponentSelection prints the component's WhyItMatters, chart info, and (if not Required)
// prompts: "Install <DisplayName>? [y/N]:". Returns true if the component should be installed.
// For Required components, prints "[Required — will be installed]" and returns true without prompting.
// stepNum and totalSteps are for "── Step N of M: <title> ──" header.
func PromptComponentSelection(r *bufio.Reader, c ComponentConfig, stepNum, totalSteps int) bool {
	header := fmt.Sprintf("── Step %d of %d: %s ──────────────────────────────────", stepNum, totalSteps, c.DisplayName)
	// Trim to ~65 runes for consistent display (rune-safe to avoid splitting multi-byte characters)
	runes := []rune(header)
	if len(runes) > 65 {
		header = string(runes[:65])
	}
	fmt.Println()
	fmt.Println(output.Blue(header))
	fmt.Println()
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

// PrintInstallPlan prints the installation plan table.
func PrintInstallPlan(cfg SetupConfig) {
	fmt.Println()
	output.Header("Installation Plan")
	fmt.Printf("  %-28s %-12s %s\n", "Component", "Version", "Namespace")
	fmt.Printf("  %-28s %-12s %s\n", strings.Repeat("─", 28), strings.Repeat("─", 12), strings.Repeat("─", 20))
	for _, c := range cfg.Components {
		version := c.Version
		if cfg.VersionOverrides != nil {
			if v, ok := cfg.VersionOverrides[c.ID]; ok && v != "" {
				version = v
			}
		}
		fmt.Printf("  %-28s %-12s %s\n", c.DisplayName, version, c.Namespace)
	}
	fmt.Println()
	fmt.Printf("  Environment: %s\n", cfg.Environment)
	if cfg.ClusterPersonaName != "" {
		fmt.Printf("  ClusterPersona: %s\n", cfg.ClusterPersonaName)
	}
	fmt.Println()
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
// Shows a spinner with "Installing <DisplayName>...".
// Accepts an io.Writer for the spinner output (use os.Stderr in production, a buffer in tests).
func PrintComponentProgress(w io.Writer, c ComponentConfig, stepNum, totalSteps int) func() {
	s := spinner.New(spinner.CharSets[14], 100*time.Millisecond, spinner.WithWriter(w))
	s.Suffix = fmt.Sprintf(" [%d/%d] Installing %s...", stepNum, totalSteps, c.DisplayName)
	s.Start()
	return func() { s.Stop() }
}

// PrintComponentResult prints ✓ / ✗ result for a single component install.
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
		output.Error(fmt.Sprintf("%s failed: %v", r.Component.DisplayName, r.Error))
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

// PrintFinalSummary prints the completion table and next steps.
func PrintFinalSummary(results []InstallResult, vrs []ValidationResult, cfg SetupConfig) {
	fmt.Println()
	output.Header("Setup Complete")

	succeeded := 0
	failed := 0
	skipped := 0
	for _, r := range results {
		switch {
		case r.Skipped:
			skipped++
		case r.Succeeded:
			succeeded++
		default:
			failed++
		}
	}

	fmt.Printf("  Installed: %d  Skipped: %d  Failed: %d\n", succeeded, skipped, failed)
	fmt.Println()

	// Health summary
	healthy := 0
	for _, vr := range vrs {
		if vr.Healthy && vr.Message != "skipped" {
			healthy++
		}
	}
	if len(vrs) > 0 {
		fmt.Printf("  Pods healthy: %d/%d\n", healthy, succeeded)
		fmt.Println()
	}

	output.Header("Next Steps")
	output.Info("Check cluster status:   dorgu cluster status")
	if cfg.ClusterPersonaName != "" {
		output.Info(fmt.Sprintf("View ClusterPersona:    kubectl get clusterpersona %s -o yaml", cfg.ClusterPersonaName))
	}
	output.Info("Monitor with ArgoCD:    dorgu sync")
	fmt.Println()
}
