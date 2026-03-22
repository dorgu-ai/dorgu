package cli

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	k8syaml "sigs.k8s.io/yaml"

	"github.com/dorgu-ai/dorgu/internal/output"
)

// clusterPersonaYAML is used to parse a single ClusterPersona YAML response from kubectl.
type clusterPersonaYAML struct {
	Metadata struct {
		Name              string            `json:"name"`
		CreationTimestamp string            `json:"creationTimestamp"`
		Annotations       map[string]string `json:"annotations"`
	} `json:"metadata"`
	Spec struct {
		Environment string `json:"environment"`
	} `json:"spec"`
	Status struct {
		Phase             string `json:"phase"`
		KubernetesVersion string `json:"kubernetesVersion"`
		Platform          string `json:"platform"`
		ApplicationCount  int    `json:"applicationCount"`
		Nodes             []struct {
			Name  string `json:"name"`
			Ready bool   `json:"ready"`
			Role  string `json:"role"`
		} `json:"nodes"`
		Addons []struct {
			Name      string `json:"name"`
			Type      string `json:"type"`
			Installed bool   `json:"installed"`
			Healthy   bool   `json:"healthy"`
			Version   string `json:"version"`
			Namespace string `json:"namespace"`
		} `json:"addons"`
		ResourceSummary struct {
			TotalCPU          string `json:"totalCPU"`
			TotalMemory       string `json:"totalMemory"`
			AllocatableCPU    string `json:"allocatableCPU"`
			AllocatableMemory string `json:"allocatableMemory"`
			RunningPods       int    `json:"runningPods"`
			TotalPods         int    `json:"totalPods"`
		} `json:"resourceSummary"`
	} `json:"status"`
}

// clusterPersonaList wraps the kubectl JSON list response.
type clusterPersonaList struct {
	Items []clusterPersonaYAML `json:"items"`
}

func displayClusterPersonaStatus(name string, rawYAML string) {
	displayClusterPersonaStatusTo(os.Stdout, name, rawYAML)
}

func displayClusterPersonaStatusTo(w io.Writer, name string, rawYAML string) {
	var cp clusterPersonaYAML
	if err := k8syaml.Unmarshal([]byte(rawYAML), &cp); err != nil {
		output.Warn(fmt.Sprintf("Could not parse ClusterPersona YAML: %v", err))
		fmt.Fprintln(w, rawYAML)
		return
	}

	s := cp.Status

	// --- Header box ---
	headerBoxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("39")).
		Padding(0, 1).
		Width(52)

	env := cp.Spec.Environment
	if env == "" {
		env = "-"
	}
	phaseDot := clusterPhaseDot(s.Phase)
	headerContent := fmt.Sprintf("ClusterPersona: %s\nEnvironment: %-16s Phase: %s", name, env, phaseDot)
	fmt.Fprintln(w)
	fmt.Fprintln(w, headerBoxStyle.Render(headerContent))
	fmt.Fprintln(w)

	// --- Key-value overview ---
	platform := s.Platform
	if platform == "" {
		platform = "-"
	}
	k8sVer := s.KubernetesVersion
	if k8sVer == "" {
		k8sVer = "-"
	}
	totalNodes := len(s.Nodes)
	readyNodes := 0
	for _, n := range s.Nodes {
		if n.Ready {
			readyNodes++
		}
	}
	fmt.Fprintf(w, "  %-16s %s\n", "Platform", platform)
	fmt.Fprintf(w, "  %-16s %s\n", "K8s Version", k8sVer)
	fmt.Fprintf(w, "  %-16s %d (%d ready)\n", "Nodes", totalNodes, readyNodes)
	fmt.Fprintf(w, "  %-16s %d\n", "Running Pods", s.ResourceSummary.RunningPods)

	// --- Resources section ---
	rs := s.ResourceSummary
	if rs.TotalCPU != "" || rs.TotalMemory != "" {
		fmt.Fprintln(w)
		fmt.Fprintf(w, "  Resources\n")
		fmt.Fprintf(w, "  %s\n", strings.Repeat("─", 38))
		if rs.TotalCPU != "" {
			fmt.Fprintf(w, "  %-8s %s allocated / %s total\n", "CPU", rs.AllocatableCPU, rs.TotalCPU)
		}
		if rs.TotalMemory != "" {
			fmt.Fprintf(w, "  %-8s %s allocated / %s total\n", "Memory", rs.AllocatableMemory, rs.TotalMemory)
		}
	}

	// --- Installed Add-ons section ---
	if len(s.Addons) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintf(w, "  Installed Add-ons\n")
		fmt.Fprintf(w, "  %s\n", strings.Repeat("─", 38))
		for _, addon := range s.Addons {
			var icon, healthStr string
			if !addon.Installed {
				icon = output.Red("✗")
				healthStr = "not installed"
			} else if !addon.Healthy {
				icon = output.Yellow("⚠")
				healthStr = "unhealthy"
			} else {
				icon = output.Green("✓")
				healthStr = "healthy"
			}
			ver := addon.Version
			if ver == "" {
				ver = "—"
			}
			fmt.Fprintf(w, "  %s %-20s %-10s %s\n", icon, addon.Name, ver, healthStr)
		}
	}

	// --- Next Steps section ---
	fmt.Fprintln(w)
	fmt.Fprintf(w, "  Next Steps\n")
	fmt.Fprintf(w, "  %s\n", strings.Repeat("─", 38))

	hasUninstalled := false
	for _, addon := range s.Addons {
		if !addon.Installed {
			hasUninstalled = true
			break
		}
	}
	if hasUninstalled {
		fmt.Fprintf(w, "  %s Run 'dorgu cluster setup' to install missing components\n", output.Dim("→"))
	}
	if s.ApplicationCount == 0 {
		fmt.Fprintf(w, "  %s Run 'dorgu persona apply' to onboard an application\n", output.Dim("→"))
	} else {
		fmt.Fprintf(w, "  %s Run 'dorgu persona apply' to onboard additional applications\n", output.Dim("→"))
	}
	if s.Phase == "Degraded" {
		fmt.Fprintf(w, "  %s Check nodes: kubectl get nodes\n", output.Dim("→"))
	}
	fmt.Fprintln(w)
}

// clusterPhaseDot returns a colored "● Phase" string for the given phase.
func clusterPhaseDot(phase string) string {
	dot := "●"
	if phase == "" {
		return dot + " Unknown"
	}
	return dot + " " + output.FormatPhase(phase)
}

// formatAge returns a human-readable age string from an RFC3339 timestamp.
func formatAge(ts string) string {
	if ts == "" {
		return "?"
	}
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return "?"
	}
	age := time.Since(t)
	switch {
	case age < time.Hour:
		return fmt.Sprintf("%dm", int(age.Minutes()))
	case age < 24*time.Hour:
		return fmt.Sprintf("%dh", int(age.Hours()))
	default:
		return fmt.Sprintf("%dd", int(age.Hours()/24))
	}
}
