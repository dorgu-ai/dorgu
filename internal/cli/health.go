package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dorgu-ai/dorgu/internal/output"
)

func newHealthCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "health",
		Short: "Show cluster health summary",
		Long: `Display a summary of cluster health including nodes, resource
saturation, control plane status, active incidents, and pending remediations.

Queries the Kubernetes API directly. When the Dorgu Operator is installed,
shows richer data from IncidentMemory and RemediationAction CRDs.

Examples:
  dorgu health
  dorgu health --json
  dorgu health -n production`,
		RunE: runHealth,
	}

	cmd.Flags().StringP("namespace", "n", "", "filter incidents by namespace")
	cmd.Flags().String("kubeconfig", "", "path to kubeconfig (default: ~/.kube/config)")

	return cmd
}

// healthSummary is the JSON output structure.
type healthSummary struct {
	Nodes               []healthNode        `json:"nodes"`
	ResourceSaturation  *resourceSaturation `json:"resourceSaturation,omitempty"`
	ControlPlane        *controlPlaneStatus `json:"controlPlane"`
	ActiveIncidents     *incidentsSummary   `json:"activeIncidents"`
	PendingRemediations *remediationSummary `json:"pendingRemediations"`
}

type healthNode struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Roles  string `json:"roles"`
	Age    string `json:"age"`
}

type resourceSaturation struct {
	CPU    *saturationDetail `json:"cpu,omitempty"`
	Memory *saturationDetail `json:"memory,omitempty"`
}

type saturationDetail struct {
	Percentage  string `json:"percentage"`
	Used        string `json:"used"`
	Allocatable string `json:"allocatable"`
}

type controlPlaneStatus struct {
	Healthy    bool                    `json:"healthy"`
	Components []controlPlaneComponent `json:"components"`
}

type controlPlaneComponent struct {
	Name    string `json:"name"`
	Healthy bool   `json:"healthy"`
}

type incidentsSummary struct {
	Count int             `json:"count"`
	Items []incidentBrief `json:"items"`
}

type incidentBrief struct {
	Severity  string `json:"severity"`
	Category  string `json:"category"`
	Signal    string `json:"signal"`
	Persona   string `json:"persona"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Age       string `json:"age"`
}

type remediationSummary struct {
	Count int `json:"count"`
}

func runHealth(cmd *cobra.Command, args []string) error {
	if _, err := exec.LookPath("kubectl"); err != nil {
		return fmt.Errorf("kubectl not found in PATH; required for health check")
	}

	namespace, _ := cmd.Flags().GetString("namespace")
	kubeconfigFlag, _ := cmd.Flags().GetString("kubeconfig")

	summary := &healthSummary{}

	// Collect data.
	summary.Nodes = fetchNodes(kubeconfigFlag)
	summary.ResourceSaturation = fetchResourceSaturation(kubeconfigFlag)
	summary.ControlPlane = fetchControlPlane(kubeconfigFlag)
	summary.ActiveIncidents = fetchIncidentsBrief(kubeconfigFlag, namespace)
	summary.PendingRemediations = fetchPendingRemediations(kubeconfigFlag, namespace)

	if output.IsJSON() {
		return output.PrintJSON(summary)
	}

	printHealthSummary(os.Stdout, summary)
	return nil
}

func printHealthSummary(w io.Writer, s *healthSummary) {
	fmt.Fprintln(w)
	fmt.Fprintln(w, output.Blue("Cluster Health Summary"))
	fmt.Fprintln(w, "══════════════════════")
	fmt.Fprintln(w)

	// Nodes
	fmt.Fprintln(w, "Nodes:")
	tbl := output.NewTable(w, "NAME", "STATUS", "ROLES", "AGE")
	for _, n := range s.Nodes {
		status := n.Status
		if status == "Ready" {
			status = output.Green(status)
		} else {
			status = output.Red(status)
		}
		tbl.AddRow(n.Name, status, n.Roles, n.Age)
	}
	tbl.Render()
	fmt.Fprintln(w)

	// Resource saturation
	if s.ResourceSaturation != nil {
		fmt.Fprintln(w, "Resource Saturation:")
		if s.ResourceSaturation.CPU != nil {
			cpu := s.ResourceSaturation.CPU
			fmt.Fprintf(w, "  CPU:    %s requests / allocatable (%s / %s)\n",
				cpu.Percentage, cpu.Used, cpu.Allocatable)
		}
		if s.ResourceSaturation.Memory != nil {
			mem := s.ResourceSaturation.Memory
			fmt.Fprintf(w, "  Memory: %s requests / allocatable (%s / %s)\n",
				mem.Percentage, mem.Used, mem.Allocatable)
		}
		fmt.Fprintln(w)
	}

	// Control plane
	if s.ControlPlane != nil {
		healthStr := output.HealthColor("Healthy")
		if !s.ControlPlane.Healthy {
			healthStr = output.HealthColor("Unhealthy")
		}
		fmt.Fprintf(w, "Control Plane: %s\n", healthStr)

		var parts []string
		for _, c := range s.ControlPlane.Components {
			icon := output.Green("✓")
			if !c.Healthy {
				icon = output.Red("✗")
			}
			parts = append(parts, fmt.Sprintf("%s %s", icon, c.Name))
		}
		if len(parts) > 0 {
			fmt.Fprintf(w, "  %s\n", strings.Join(parts, "    "))
		}
		fmt.Fprintln(w)
	}

	// Active incidents
	if s.ActiveIncidents != nil {
		fmt.Fprintf(w, "Active Incidents: %d\n", s.ActiveIncidents.Count)
		if s.ActiveIncidents.Count > 0 {
			itbl := output.NewTable(w, "SEVERITY", "CATEGORY", "SIGNAL", "PERSONA", "AGE")
			for _, inc := range s.ActiveIncidents.Items {
				persona := inc.Persona
				if inc.Namespace != "" {
					persona = inc.Namespace + "/" + persona
				}
				itbl.AddRow(
					output.SeverityColor(inc.Severity),
					inc.Category,
					inc.Signal,
					persona,
					inc.Age,
				)
			}
			itbl.Render()
		}
		fmt.Fprintln(w)
	}

	// Pending remediations
	if s.PendingRemediations != nil {
		fmt.Fprintf(w, "Pending Remediations: %d\n", s.PendingRemediations.Count)
	}
	fmt.Fprintln(w)
}

// kubectlArgs builds a kubectl command with optional kubeconfig.
func kubectlArgs(kubeconfig string, args ...string) *exec.Cmd {
	if kubeconfig != "" {
		args = append([]string{"--kubeconfig", kubeconfig}, args...)
	}
	return exec.Command("kubectl", args...)
}

// fetchNodes lists cluster nodes via kubectl.
func fetchNodes(kubeconfig string) []healthNode {
	out, err := kubectlArgs(kubeconfig, "get", "nodes", "-o", "json").CombinedOutput()
	if err != nil {
		return nil
	}

	var result struct {
		Items []struct {
			Metadata struct {
				Name              string            `json:"name"`
				CreationTimestamp string            `json:"creationTimestamp"`
				Labels            map[string]string `json:"labels"`
			} `json:"metadata"`
			Status struct {
				Conditions []struct {
					Type   string `json:"type"`
					Status string `json:"status"`
				} `json:"conditions"`
			} `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		return nil
	}

	var nodes []healthNode
	for _, item := range result.Items {
		status := "NotReady"
		for _, cond := range item.Status.Conditions {
			if cond.Type == "Ready" && cond.Status == "True" {
				status = "Ready"
				break
			}
		}

		roles := nodeRoles(item.Metadata.Labels)
		nodes = append(nodes, healthNode{
			Name:   item.Metadata.Name,
			Status: status,
			Roles:  roles,
			Age:    formatAge(item.Metadata.CreationTimestamp),
		})
	}
	return nodes
}

func nodeRoles(labels map[string]string) string {
	var roles []string
	for k := range labels {
		if role, ok := strings.CutPrefix(k, "node-role.kubernetes.io/"); ok && role != "" {
			roles = append(roles, role)
		}
	}
	if len(roles) == 0 {
		return "<none>"
	}
	return strings.Join(roles, ",")
}

// fetchResourceSaturation gets cluster resource saturation from ClusterPersona if available.
func fetchResourceSaturation(kubeconfig string) *resourceSaturation {
	out, err := kubectlArgs(kubeconfig, "get", "clusterpersona", "-o", "json").CombinedOutput()
	if err != nil {
		return nil
	}

	var list struct {
		Items []struct {
			Status struct {
				ResourceSummary struct {
					AllocatableCPU    string `json:"allocatableCPU"`
					AllocatableMemory string `json:"allocatableMemory"`
					UsedCPU           string `json:"usedCPU"`
					UsedMemory        string `json:"usedMemory"`
					CPUUtilization    string `json:"cpuUtilization"`
					MemoryUtilization string `json:"memoryUtilization"`
				} `json:"resourceSummary"`
			} `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal(out, &list); err != nil || len(list.Items) == 0 {
		return nil
	}

	rs := list.Items[0].Status.ResourceSummary
	if rs.AllocatableCPU == "" && rs.AllocatableMemory == "" {
		return nil
	}

	sat := &resourceSaturation{}
	if rs.AllocatableCPU != "" {
		pct := rs.CPUUtilization
		if pct == "" {
			pct = "-"
		}
		sat.CPU = &saturationDetail{
			Percentage:  pct,
			Used:        rs.UsedCPU,
			Allocatable: rs.AllocatableCPU,
		}
	}
	if rs.AllocatableMemory != "" {
		pct := rs.MemoryUtilization
		if pct == "" {
			pct = "-"
		}
		sat.Memory = &saturationDetail{
			Percentage:  pct,
			Used:        rs.UsedMemory,
			Allocatable: rs.AllocatableMemory,
		}
	}
	return sat
}

// fetchControlPlane checks control plane component health.
func fetchControlPlane(kubeconfig string) *controlPlaneStatus {
	componentNames := []string{"kube-apiserver", "kube-scheduler", "kube-controller-manager", "etcd"}

	// Check control plane pods in kube-system.
	out, err := kubectlArgs(kubeconfig, "get", "pods", "-n", "kube-system",
		"-l", "tier=control-plane", "-o", "json").CombinedOutput()
	if err != nil {
		// Fallback: mark all as unknown.
		cp := &controlPlaneStatus{Healthy: false}
		for _, name := range componentNames {
			cp.Components = append(cp.Components, controlPlaneComponent{Name: friendlyComponentName(name), Healthy: false})
		}
		return cp
	}

	var podList struct {
		Items []struct {
			Metadata struct {
				Labels map[string]string `json:"labels"`
			} `json:"metadata"`
			Status struct {
				Phase string `json:"phase"`
			} `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal(out, &podList); err != nil {
		return nil
	}

	// Map component name → running.
	running := make(map[string]bool)
	for _, pod := range podList.Items {
		comp := pod.Metadata.Labels["component"]
		if pod.Status.Phase == "Running" {
			running[comp] = true
		}
	}

	cp := &controlPlaneStatus{Healthy: true}
	for _, name := range componentNames {
		healthy := running[name]
		if !healthy {
			cp.Healthy = false
		}
		cp.Components = append(cp.Components, controlPlaneComponent{
			Name:    friendlyComponentName(name),
			Healthy: healthy,
		})
	}
	return cp
}

func friendlyComponentName(name string) string {
	switch name {
	case "kube-apiserver":
		return "API Server"
	case "kube-scheduler":
		return "Scheduler"
	case "kube-controller-manager":
		return "Controller Manager"
	case "etcd":
		return "etcd"
	default:
		return name
	}
}

// fetchIncidentsBrief lists active IncidentMemory CRDs.
func fetchIncidentsBrief(kubeconfig, namespace string) *incidentsSummary {
	args := []string{"get", "incidentmemory", "-o", "json"}
	if namespace != "" {
		args = append(args, "-n", namespace)
	} else {
		args = append(args, "--all-namespaces")
	}

	out, err := kubectlArgs(kubeconfig, args...).CombinedOutput()
	if err != nil {
		// CRD not installed — return zero incidents.
		return &incidentsSummary{Count: 0}
	}

	var list struct {
		Items []struct {
			Metadata struct {
				Name              string `json:"name"`
				Namespace         string `json:"namespace"`
				CreationTimestamp string `json:"creationTimestamp"`
			} `json:"metadata"`
			Spec struct {
				Category  string `json:"category"`
				Severity  string `json:"severity"`
				Detection struct {
					Signal string `json:"signal"`
				} `json:"detection"`
				PersonaRef struct {
					Name      string `json:"name"`
					Namespace string `json:"namespace"`
				} `json:"personaRef"`
			} `json:"spec"`
			Status struct {
				Phase string `json:"phase"`
			} `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal(out, &list); err != nil {
		return &incidentsSummary{Count: 0}
	}

	summary := &incidentsSummary{}
	for _, item := range list.Items {
		// Only active incidents (not Resolved).
		if item.Status.Phase == "Resolved" {
			continue
		}
		summary.Items = append(summary.Items, incidentBrief{
			Name:      item.Metadata.Name,
			Namespace: item.Metadata.Namespace,
			Severity:  item.Spec.Severity,
			Category:  item.Spec.Category,
			Signal:    item.Spec.Detection.Signal,
			Persona:   item.Spec.PersonaRef.Name,
			Age:       formatAge(item.Metadata.CreationTimestamp),
		})
	}
	summary.Count = len(summary.Items)
	return summary
}

// fetchPendingRemediations counts pending RemediationAction CRDs.
func fetchPendingRemediations(kubeconfig, namespace string) *remediationSummary {
	args := []string{"get", "remediationaction", "-o", "json"}
	if namespace != "" {
		args = append(args, "-n", namespace)
	} else {
		args = append(args, "--all-namespaces")
	}

	out, err := kubectlArgs(kubeconfig, args...).CombinedOutput()
	if err != nil {
		return &remediationSummary{Count: 0}
	}

	var list struct {
		Items []struct {
			Status struct {
				Phase string `json:"phase"`
			} `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal(out, &list); err != nil {
		return &remediationSummary{Count: 0}
	}

	count := 0
	for _, item := range list.Items {
		if item.Status.Phase == "Pending" || item.Status.Phase == "Approved" {
			count++
		}
	}
	return &remediationSummary{Count: count}
}
