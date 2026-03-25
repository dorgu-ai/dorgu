package cli

import (
	"fmt"

	"sigs.k8s.io/yaml"

	"github.com/dorgu-ai/dorgu/internal/output"
)

// personaStatus represents the parsed ApplicationPersona for display.
type personaStatus struct {
	Metadata struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	} `json:"metadata"`
	Spec struct {
		Name string `json:"name"`
		Type string `json:"type"`
		Tier string `json:"tier"`
	} `json:"spec"`
	Status struct {
		Phase       string `json:"phase"`
		LastUpdated string `json:"lastUpdated"`
		Health      *struct {
			Status      string `json:"status"`
			LastCheck   string `json:"lastCheck"`
			Message     string `json:"message"`
			PodFailures []struct {
				PodName   string `json:"podName"`
				Container string `json:"container"`
				Reason    string `json:"reason"`
				Message   string `json:"message"`
			} `json:"podFailures"`
		} `json:"health"`
		Validation *struct {
			Passed      bool   `json:"passed"`
			LastChecked string `json:"lastChecked"`
			Issues      []struct {
				Severity   string `json:"severity"`
				Field      string `json:"field"`
				Message    string `json:"message"`
				Suggestion string `json:"suggestion"`
			} `json:"issues"`
		} `json:"validation"`
		Deployments *struct {
			Current        string `json:"current"`
			LastSuccessful string `json:"lastSuccessful"`
			LastFailed     string `json:"lastFailed"`
		} `json:"deployments"`
		ArgoCD *struct {
			SyncStatus   string `json:"syncStatus"`
			HealthStatus string `json:"healthStatus"`
			LastSyncTime string `json:"lastSyncTime"`
			Revision     string `json:"revision"`
		} `json:"argoCD"`
		Conditions []struct {
			Type    string `json:"type"`
			Status  string `json:"status"`
			Reason  string `json:"reason"`
			Message string `json:"message"`
		} `json:"conditions"`
		Learned *struct {
			ResourceBaseline *struct {
				AvgCPU     string `json:"avgCPU"`
				AvgMemory  string `json:"avgMemory"`
				PeakCPU    string `json:"peakCPU"`
				PeakMemory string `json:"peakMemory"`
			} `json:"resourceBaseline"`
		} `json:"learned"`
	} `json:"status"`
}

// displayPersonaStatus formats and prints persona status information.
func displayPersonaStatus(name string, rawYAML string) {
	var persona personaStatus
	if err := yaml.Unmarshal([]byte(rawYAML), &persona); err != nil {
		output.Error(fmt.Sprintf("Failed to parse persona YAML: %v", err))
		fmt.Println(rawYAML)
		return
	}

	if output.IsJSON() {
		output.PrintJSON(persona)
		return
	}

	// Header
	output.Header(fmt.Sprintf("ApplicationPersona: %s", name))

	// Check if status exists
	if persona.Status.Phase == "" && persona.Status.Health == nil && persona.Status.Validation == nil {
		fmt.Println(output.Dim("  No status available yet. The Dorgu Operator may not have reconciled this persona."))
		return
	}

	// Phase with color
	phaseDisplay := output.FormatPhase(persona.Status.Phase)
	fmt.Printf("  %-14s %s\n", "Phase:", phaseDisplay)

	// Health status
	if persona.Status.Health != nil {
		healthDisplay := formatHealth(persona.Status.Health.Status, persona.Status.Health.Message)
		fmt.Printf("  %-14s %s\n", "Health:", healthDisplay)
	}

	// Validation status
	if persona.Status.Validation != nil {
		validationDisplay := formatValidation(persona.Status.Validation.Passed)
		fmt.Printf("  %-14s %s\n", "Validation:", validationDisplay)
	}

	fmt.Println()

	// Conditions section
	if len(persona.Status.Conditions) > 0 {
		fmt.Println("  Conditions:")
		for _, cond := range persona.Status.Conditions {
			condStatus := formatConditionStatus(cond.Status)
			fmt.Printf("    %-12s %s - %s\n", cond.Type+":", condStatus, cond.Message)
		}
		fmt.Println()
	}

	// Deployment info
	if persona.Status.Deployments != nil && persona.Status.Deployments.Current != "" {
		fmt.Println("  Deployment:")
		fmt.Printf("    Current Image: %s\n", persona.Status.Deployments.Current)
		if persona.Status.Deployments.LastSuccessful != "" {
			fmt.Printf("    Last Successful: %s\n", persona.Status.Deployments.LastSuccessful)
		}
		if persona.Status.Deployments.LastFailed != "" {
			fmt.Printf("    Last Failed: %s\n", output.Red(persona.Status.Deployments.LastFailed))
		}
		fmt.Println()
	}

	// Pod failures (if any)
	if persona.Status.Health != nil && len(persona.Status.Health.PodFailures) > 0 {
		fmt.Println("  " + output.Red("Pod Failures:"))
		for _, pf := range persona.Status.Health.PodFailures {
			fmt.Printf("    %s/%s: %s\n", pf.PodName, pf.Container, output.Red(pf.Reason))
			if pf.Message != "" {
				fmt.Printf("      %s\n", output.Dim(pf.Message))
			}
		}
		fmt.Println()
	}

	// Validation issues (if any)
	if persona.Status.Validation != nil && len(persona.Status.Validation.Issues) > 0 {
		fmt.Println("  Validation Issues:")
		for _, issue := range persona.Status.Validation.Issues {
			icon := getIssueSeverityIcon(issue.Severity)
			fmt.Printf("    %s [%s] %s\n", icon, issue.Field, issue.Message)
			if issue.Suggestion != "" {
				fmt.Printf("       Suggestion: %s\n", output.Dim(issue.Suggestion))
			}
		}
		fmt.Println()
	}

	// ArgoCD status (if present)
	if persona.Status.ArgoCD != nil && persona.Status.ArgoCD.SyncStatus != "" {
		fmt.Println("  ArgoCD:")
		syncDisplay := formatArgoSyncStatus(persona.Status.ArgoCD.SyncStatus)
		fmt.Printf("    Sync Status: %s\n", syncDisplay)
		if persona.Status.ArgoCD.HealthStatus != "" {
			healthDisplay := formatArgoHealthStatus(persona.Status.ArgoCD.HealthStatus)
			fmt.Printf("    Health: %s\n", healthDisplay)
		}
		if persona.Status.ArgoCD.Revision != "" {
			fmt.Printf("    Revision: %s\n", truncateRevision(persona.Status.ArgoCD.Revision))
		}
		fmt.Println()
	}

	// Learned patterns (if present)
	if persona.Status.Learned != nil && persona.Status.Learned.ResourceBaseline != nil {
		rb := persona.Status.Learned.ResourceBaseline
		if rb.AvgCPU != "" || rb.AvgMemory != "" {
			fmt.Println("  Resource Baseline (learned):")
			if rb.AvgCPU != "" {
				fmt.Printf("    Avg CPU: %s", rb.AvgCPU)
				if rb.PeakCPU != "" {
					fmt.Printf(" (peak: %s)", rb.PeakCPU)
				}
				fmt.Println()
			}
			if rb.AvgMemory != "" {
				fmt.Printf("    Avg Memory: %s", rb.AvgMemory)
				if rb.PeakMemory != "" {
					fmt.Printf(" (peak: %s)", rb.PeakMemory)
				}
				fmt.Println()
			}
			fmt.Println()
		}
	}

	// Last updated
	if persona.Status.LastUpdated != "" {
		fmt.Printf("  Last Updated: %s\n", output.Dim(persona.Status.LastUpdated))
	}
}

// formatHealth returns a colored health status with optional message.
// Core coloring delegates to output.FormatHealth.
func formatHealth(status, message string) string {
	colored := output.FormatHealth(status)
	if message != "" {
		return fmt.Sprintf("%s (%s)", colored, message)
	}
	return colored
}

// formatValidation returns a colored validation status.
func formatValidation(passed bool) string {
	if passed {
		return output.Green("Passed")
	}
	return output.Red("Failed")
}

// formatConditionStatus returns a colored condition status.
func formatConditionStatus(status string) string {
	switch status {
	case "True":
		return output.Green(status)
	case "False":
		return output.Red(status)
	default:
		return output.Yellow(status)
	}
}

// getIssueSeverityIcon returns an icon for the issue severity.
func getIssueSeverityIcon(severity string) string {
	switch severity {
	case "error":
		return output.Red("✗")
	case "warning":
		return output.Yellow("⚠")
	case "info":
		return output.Blue("ℹ")
	default:
		return "•"
	}
}

// formatArgoSyncStatus returns a colored ArgoCD sync status.
func formatArgoSyncStatus(status string) string {
	switch status {
	case "Synced":
		return output.Green(status)
	case "OutOfSync":
		return output.Yellow(status)
	default:
		return status
	}
}

// formatArgoHealthStatus returns a colored ArgoCD health status.
func formatArgoHealthStatus(status string) string {
	switch status {
	case "Healthy":
		return output.Green(status)
	case "Degraded", "Progressing":
		return output.Yellow(status)
	case "Suspended", "Missing":
		return output.Red(status)
	default:
		return status
	}
}

// truncateRevision shortens a git revision for display.
func truncateRevision(rev string) string {
	if len(rev) > 8 {
		return rev[:8]
	}
	return rev
}
