package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/dorgu-ai/dorgu/internal/output"
	"github.com/dorgu-ai/dorgu/internal/ws"
)

// healthCmdTimeout is the maximum time to wait for each kubectl call.
const healthCmdTimeout = 30 * time.Second

func newHealthCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "health",
		Short: "Show cluster health summary",
		Long: `Display a summary of cluster health including nodes, resource
saturation, control plane status, active incidents, and pending remediations.

Queries the Kubernetes API directly. When the Dorgu Operator is installed,
shows richer data from IncidentMemory and RemediationAction CRDs.

Also names the Deployments that no ApplicationPersona covers: those are not
being watched, so nothing about them can appear as an incident. Without -n,
cluster add-on namespaces are left out.

Use --watch to stream real-time health updates via WebSocket. Requires
the Dorgu Operator to be running with --enable-websocket.

Exit codes:
  0  the cluster was read and no critical incident is active
  1  the check could not run (unreachable cluster, bad kubeconfig, no kubectl)
  2  active critical incidents (only with --exit-code)
  3  health could not be judged, e.g. the incident records are unreadable
     (only with --exit-code)

Without --exit-code the command exits 0 whenever it managed to read the
cluster, so it stays usable interactively. A cluster it cannot reach is
always a non-zero exit: reporting health from a failed API call would be
worse than reporting nothing.

Examples:
  dorgu health
  dorgu health --json
  dorgu health -n production
  dorgu health --exit-code
  dorgu health --watch
  dorgu health --watch --json`,
		RunE: runHealth,
	}

	cmd.Flags().StringP("namespace", "n", "", "filter incidents by namespace")
	cmd.Flags().String("kubeconfig", "", "path to kubeconfig (default: ~/.kube/config)")
	cmd.Flags().Bool("exit-code", false,
		"exit 2 when critical incidents are active, 3 when health cannot be judged (for monitoring scripts)")
	cmd.Flags().BoolP("watch", "w", false, "stream health updates in real-time via WebSocket")
	cmd.Flags().String("operator-url", "ws://localhost:9090/ws",
		"WebSocket URL of the Dorgu Operator (used with --watch)")

	return cmd
}

// healthSummary is the JSON output structure.
type healthSummary struct {
	Nodes               []healthNode        `json:"nodes"`
	ResourceSaturation  *resourceSaturation `json:"resourceSaturation,omitempty"`
	ControlPlane        *controlPlaneStatus `json:"controlPlane"`
	ActiveIncidents     *incidentsSummary   `json:"activeIncidents"`
	PendingRemediations *remediationSummary `json:"pendingRemediations"`
	// Unmonitored names the Deployments no ApplicationPersona covers. Reporting
	// health without it presents a blind spot as health (F-02b).
	Unmonitored *unmonitoredSummary `json:"unmonitored"`
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
	External   bool                    `json:"external,omitempty"` // true when control plane is not self-hosted
	Components []controlPlaneComponent `json:"components"`
}

type controlPlaneComponent struct {
	Name    string `json:"name"`
	Healthy bool   `json:"healthy"`
	Status  string `json:"status,omitempty"` // "external" when inferred healthy
}

type incidentsSummary struct {
	Count int             `json:"count"`
	Items []incidentBrief `json:"items"`

	// Unavailable is set when the incident records could not be read at all, so
	// a reader can tell "none" from "unknown". Rendering an unreadable API as
	// "Active Incidents: 0" is a blind spot dressed up as health (F-04).
	Unavailable bool `json:"unavailable,omitempty"`

	// Reason explains an Unavailable result in one line.
	Reason string `json:"reason,omitempty"`
}

// criticalCount returns the number of active critical incidents.
func (s *incidentsSummary) criticalCount() int {
	if s == nil {
		return 0
	}
	n := 0
	for _, inc := range s.Items {
		if strings.EqualFold(inc.Severity, "critical") {
			n++
		}
	}
	return n
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
	watchMode, _ := cmd.Flags().GetBool("watch")
	if watchMode {
		return runHealthWatch(cmd, args)
	}

	if _, err := exec.LookPath("kubectl"); err != nil {
		return fmt.Errorf("kubectl not found in PATH; required for health check")
	}

	namespace, _ := cmd.Flags().GetString("namespace")
	kubeconfigFlag, _ := cmd.Flags().GetString("kubeconfig")
	exitCode, _ := cmd.Flags().GetBool("exit-code")

	kubeconfig, err := validateKubeconfig(kubeconfigFlag)
	if err != nil {
		return withExitCode(ExitError, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), healthCmdTimeout)
	defer cancel()

	// Probe the API server before assembling anything. Against a cluster it could
	// not reach, this command used to print an empty node table, "Active
	// Incidents: 0" and exit 0: a blind spot presented as health (F-04).
	if err := checkClusterReachable(ctx, kubeconfig); err != nil {
		output.ErrorWithHint(err.Error(),
			"Check your current context: kubectl config current-context",
			"List available contexts: kubectl config get-contexts",
			"Point at a specific file with --kubeconfig <path>")
		return withExitCode(ExitError, nil)
	}

	summary := &healthSummary{}

	// Collect data — warn on per-section failures rather than failing silently.
	var fetchErr error

	summary.Nodes, fetchErr = fetchNodes(ctx, kubeconfig)
	if fetchErr != nil {
		output.Warn("Could not fetch nodes: " + fetchErr.Error())
	}

	summary.ResourceSaturation, fetchErr = fetchResourceSaturation(ctx, kubeconfig)
	if fetchErr != nil {
		output.Warn("Could not fetch resource saturation: " + fetchErr.Error())
	}

	summary.ControlPlane, fetchErr = fetchControlPlane(ctx, kubeconfig)
	if fetchErr != nil {
		output.Warn("Could not fetch control plane status: " + fetchErr.Error())
	}

	summary.ActiveIncidents = fetchIncidentsBrief(ctx, kubeconfig, namespace)
	summary.PendingRemediations = fetchPendingRemediations(ctx, kubeconfig, namespace)
	summary.Unmonitored = fetchUnmonitored(ctx, kubeconfig, namespace)

	if output.IsJSON() {
		if err := output.PrintJSON(summary); err != nil {
			return withExitCode(ExitError, err)
		}
		return healthExit(summary, exitCode)
	}

	printHealthSummary(os.Stdout, summary)
	return healthExit(summary, exitCode)
}

// healthExit turns the collected summary into the process exit code. Without
// --exit-code the command reports success whenever it managed to read the
// cluster; with it, "on fire" and "cannot judge" become distinguishable from
// "healthy", which is the whole point of running this in a monitoring script.
func healthExit(s *healthSummary, exitCode bool) error {
	if !exitCode {
		return nil
	}

	if s.ActiveIncidents != nil && s.ActiveIncidents.Unavailable {
		output.Warn("Health could not be judged: " + s.ActiveIncidents.Reason)
		return withExitCode(ExitUnknown, nil)
	}

	if criticals := s.ActiveIncidents.criticalCount(); criticals > 0 {
		output.Warn(fmt.Sprintf("%d critical incident(s) active", criticals))
		return withExitCode(ExitCritical, nil)
	}

	return nil
}

// checkClusterReachable confirms the API server answers before any health data is
// collected. It reads /version, which every authenticated user can see, so a
// namespace-scoped RBAC role is not mistaken for an unreachable cluster.
func checkClusterReachable(ctx context.Context, kubeconfig string) error {
	out, err := kubectlCmd(ctx, kubeconfig, "version", "-o", "json").Output()
	if err != nil {
		return fmt.Errorf("cannot reach cluster; check your kubeconfig/context: %s", kubectlFailure(err))
	}

	var version struct {
		ServerVersion *struct {
			GitVersion string `json:"gitVersion"`
		} `json:"serverVersion"`
	}
	if err := json.Unmarshal(out, &version); err != nil || version.ServerVersion == nil {
		return errors.New("cannot reach cluster; check your kubeconfig/context: " +
			"the API server did not report a version")
	}

	return nil
}

// kubectlFailure renders a failed kubectl invocation as its stderr, falling back
// to the process error when there is none.
func kubectlFailure(err error) string {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if stderr := strings.TrimSpace(string(exitErr.Stderr)); stderr != "" {
			return stderr
		}
	}
	return err.Error()
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
		if s.ControlPlane.External {
			fmt.Fprintf(w, "Control Plane: %s (external/managed)\n", healthStr)
		} else {
			fmt.Fprintf(w, "Control Plane: %s\n", healthStr)
		}

		var parts []string
		for _, c := range s.ControlPlane.Components {
			icon := output.Green("✓")
			if !c.Healthy {
				icon = output.Red("✗")
			}
			label := c.Name
			if c.Status == "external" {
				label = c.Name + " (inferred)"
			}
			parts = append(parts, fmt.Sprintf("%s %s", icon, label))
		}
		if len(parts) > 0 {
			fmt.Fprintf(w, "  %s\n", strings.Join(parts, "    "))
		}
		fmt.Fprintln(w)
	}

	// Active incidents
	if s.ActiveIncidents != nil {
		if s.ActiveIncidents.Unavailable {
			fmt.Fprintf(w, "Active Incidents: %s\n", output.Yellow("unknown"))
			output.DimPrint("  " + s.ActiveIncidents.Reason)
			fmt.Fprintln(w)
			printUnmonitored(w, s.Unmonitored)
			printPendingRemediations(w, s.PendingRemediations)
			return
		}

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

	// Unmonitored Deployments
	printUnmonitored(w, s.Unmonitored)

	printPendingRemediations(w, s.PendingRemediations)
}

// printPendingRemediations renders the pending-remediation tail of the summary.
func printPendingRemediations(w io.Writer, r *remediationSummary) {
	if r != nil {
		fmt.Fprintf(w, "Pending Remediations: %d\n", r.Count)
		if r.Count > 0 {
			output.DimPrint("  Run 'dorgu remediation list' to review pending fixes")
		}
	}
	fmt.Fprintln(w)
}

// validateKubeconfig validates and cleans a kubeconfig path.
// Returns empty string if no kubeconfig was specified.
func validateKubeconfig(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	clean := filepath.Clean(path)
	if _, err := os.Stat(clean); err != nil {
		return "", fmt.Errorf("kubeconfig file not found: %s", clean)
	}
	return clean, nil
}

// kubectlCmd builds a kubectl command with optional kubeconfig and context timeout.
func kubectlCmd(ctx context.Context, kubeconfig string, args ...string) *exec.Cmd {
	if kubeconfig != "" {
		args = append([]string{"--kubeconfig", kubeconfig}, args...)
	}
	return exec.CommandContext(ctx, "kubectl", args...)
}

// fetchNodes lists cluster nodes via kubectl.
func fetchNodes(ctx context.Context, kubeconfig string) ([]healthNode, error) {
	out, err := kubectlCmd(ctx, kubeconfig, "get", "nodes", "-o", "json").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%s", strings.TrimSpace(string(out)))
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
		return nil, fmt.Errorf("failed to parse node list: %w", err)
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
	return nodes, nil
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
	sort.Strings(roles)
	return strings.Join(roles, ",")
}

// fetchResourceSaturation gets cluster resource saturation from ClusterPersona if available.
func fetchResourceSaturation(ctx context.Context, kubeconfig string) (*resourceSaturation, error) {
	out, err := kubectlCmd(ctx, kubeconfig, "get", "clusterpersona", "-o", "json").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%s", strings.TrimSpace(string(out)))
	}
	return parseResourceSaturation(out)
}

// parseResourceSaturation parses ClusterPersona list JSON into a resourceSaturation.
// Extracted for unit testability without requiring kubectl.
func parseResourceSaturation(out []byte) (*resourceSaturation, error) {
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
		return nil, nil
	}

	rs := list.Items[0].Status.ResourceSummary
	if rs.AllocatableCPU == "" && rs.AllocatableMemory == "" {
		return nil, nil
	}

	sat := &resourceSaturation{}
	if rs.AllocatableCPU != "" {
		sat.CPU = newSaturationDetail(rs.CPUUtilization, rs.UsedCPU, rs.AllocatableCPU)
	}
	if rs.AllocatableMemory != "" {
		sat.Memory = newSaturationDetail(rs.MemoryUtilization, rs.UsedMemory, rs.AllocatableMemory)
	}
	return sat, nil
}

// newSaturationDetail builds one saturation line, substituting "n/a" for any
// figure the cluster did not report.
//
// The line renders as "<percentage> requests / allocatable (<used> / <allocatable>)",
// so an empty value printed as itself produced the nonsense
// "CPU: n/a requests / allocatable ( / 3860m)" (F-09). A missing value now reads
// "n/a" wherever it appears. Note that "0" is a real answer on an idle cluster
// and is left alone.
func newSaturationDetail(utilization, used, allocatable string) *saturationDetail {
	return &saturationDetail{
		Percentage:  orNotAvailable(utilization),
		Used:        orNotAvailable(used),
		Allocatable: orNotAvailable(allocatable),
	}
}

// orNotAvailable renders an unreported value as "n/a".
func orNotAvailable(value string) string {
	if strings.TrimSpace(value) == "" {
		return "n/a"
	}
	return value
}

// controlPlaneComponentNames is the canonical list of self-hosted control plane components.
var controlPlaneComponentNames = []string{
	"kube-apiserver", "kube-scheduler", "kube-controller-manager", "etcd",
}

// fetchControlPlane checks control plane component health.
func fetchControlPlane(ctx context.Context, kubeconfig string) (*controlPlaneStatus, error) {
	// Check control plane pods in kube-system.
	out, err := kubectlCmd(ctx, kubeconfig, "get", "pods", "-n", "kube-system",
		"-l", "tier=control-plane", "-o", "json").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%s", strings.TrimSpace(string(out)))
	}
	return parseControlPlanePods(out)
}

// parseControlPlanePods parses kubectl pod list output into a controlPlaneStatus.
// If no tier=control-plane pods are found, the control plane is inferred as external
// (vCluster, managed K8s like EKS/GKE/AKS, or k3s). Extracted for unit testability.
func parseControlPlanePods(out []byte) (*controlPlaneStatus, error) {
	var podList struct {
		Items []struct {
			Metadata struct {
				Labels map[string]string `json:"labels"`
			} `json:"metadata"`
			Status struct {
				Phase             string `json:"phase"`
				ContainerStatuses []struct {
					Ready bool `json:"ready"`
				} `json:"containerStatuses"`
			} `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal(out, &podList); err != nil {
		return nil, fmt.Errorf("failed to parse control plane pods: %w", err)
	}

	// If no control-plane pods found, the control plane is external
	// (vCluster, managed K8s like EKS/GKE/AKS, or k3s).
	if len(podList.Items) == 0 {
		cp := &controlPlaneStatus{Healthy: true, External: true}
		for _, name := range controlPlaneComponentNames {
			cp.Components = append(cp.Components, controlPlaneComponent{
				Name:    friendlyComponentName(name),
				Healthy: true,
				Status:  "external",
			})
		}
		return cp, nil
	}

	// Map component name → healthy (all containers ready).
	healthy := make(map[string]bool)
	for _, pod := range podList.Items {
		comp := pod.Metadata.Labels["component"]
		if pod.Status.Phase != "Running" {
			continue
		}
		allReady := true
		for _, cs := range pod.Status.ContainerStatuses {
			if !cs.Ready {
				allReady = false
				break
			}
		}
		if allReady {
			healthy[comp] = true
		}
	}

	cp := &controlPlaneStatus{Healthy: true}
	for _, name := range controlPlaneComponentNames {
		h := healthy[name]
		if !h {
			cp.Healthy = false
		}
		cp.Components = append(cp.Components, controlPlaneComponent{
			Name:    friendlyComponentName(name),
			Healthy: h,
		})
	}
	return cp, nil
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
func fetchIncidentsBrief(ctx context.Context, kubeconfig, namespace string) *incidentsSummary {
	args := []string{"get", "incidentmemory", "-o", "json"}
	if namespace != "" {
		args = append(args, "-n", namespace)
	} else {
		args = append(args, "--all-namespaces")
	}

	out, err := kubectlCmd(ctx, kubeconfig, args...).CombinedOutput()
	if err != nil {
		return unreadableIncidents(out)
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
		return &incidentsSummary{
			Unavailable: true,
			Reason:      "the incident list could not be parsed: " + err.Error(),
		}
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

// unreadableIncidents explains why the incident records could not be read. The
// operator not being installed is a different answer from "we could not look",
// and both are different from "there are none".
func unreadableIncidents(kubectlOutput []byte) *incidentsSummary {
	detail := strings.TrimSpace(string(kubectlOutput))

	reason := "the IncidentMemory records could not be read: " + detail
	if strings.Contains(detail, "the server doesn't have a resource type") {
		reason = "the dorgu operator is not installed, so there are no incident records to read " +
			"(install it with: dorgu cluster setup)"
	}

	return &incidentsSummary{Unavailable: true, Reason: reason}
}

// fetchPendingRemediations counts pending RemediationAction CRDs.
func fetchPendingRemediations(ctx context.Context, kubeconfig, namespace string) *remediationSummary {
	args := []string{"get", "remediationaction", "-o", "json"}
	if namespace != "" {
		args = append(args, "-n", namespace)
	} else {
		args = append(args, "--all-namespaces")
	}

	out, err := kubectlCmd(ctx, kubeconfig, args...).CombinedOutput()
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

// runHealthWatch connects to the operator WebSocket and streams health events.
func runHealthWatch(cmd *cobra.Command, _ []string) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle interrupt.
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		output.Info("Stopping health watch...")
		cancel()
	}()

	operatorURL, _ := cmd.Flags().GetString("operator-url")
	namespace, _ := cmd.Flags().GetString("namespace")

	client := ws.NewClient(operatorURL)
	if err := client.Connect(ctx); err != nil {
		return handleWSConnectError(err, operatorURL)
	}
	defer client.Close()

	output.Success("Connected to Dorgu Operator")
	output.Info("Watching health updates... (Ctrl+C to stop)")
	fmt.Println()

	// Subscribe to incidents topic.
	if err := client.Subscribe(ctx, ws.TopicIncidents, func(msg *ws.Message) {
		var event ws.IncidentEvent
		if err := json.Unmarshal(msg.Payload, &event); err != nil {
			return
		}

		if namespace != "" && event.Namespace != namespace {
			return
		}

		if output.IsJSON() {
			_ = output.PrintJSONLine(event)
			return
		}

		printIncidentEvent(msg.Timestamp, event)
	}); err != nil {
		return fmt.Errorf("failed to subscribe to incidents: %w", err)
	}

	// Subscribe to remediations topic.
	if err := client.Subscribe(ctx, ws.TopicRemediations, func(msg *ws.Message) {
		var event ws.RemediationEvent
		if err := json.Unmarshal(msg.Payload, &event); err != nil {
			return
		}

		if namespace != "" && event.Namespace != namespace {
			return
		}

		if output.IsJSON() {
			_ = output.PrintJSONLine(event)
			return
		}

		printRemediationEvent(msg.Timestamp, event)
	}); err != nil {
		return fmt.Errorf("failed to subscribe to remediations: %w", err)
	}

	// Subscribe to health updates.
	if err := client.Subscribe(ctx, ws.TopicHealth, func(msg *ws.Message) {
		var event ws.HealthUpdateEvent
		if err := json.Unmarshal(msg.Payload, &event); err != nil {
			return
		}

		if output.IsJSON() {
			_ = output.PrintJSONLine(event)
			return
		}

		printHealthUpdateEvent(msg.Timestamp, event)
	}); err != nil {
		return fmt.Errorf("failed to subscribe to health updates: %w", err)
	}

	// Emit initial health snapshot for JSON mode so the first output is immediate.
	if output.IsJSON() {
		if incidentsResp, err := client.ListIncidents(ctx, namespace); err == nil {
			for _, inc := range incidentsResp {
				_ = output.PrintJSONLine(map[string]any{
					"type": "incident", "eventType": "snapshot", "data": inc,
				})
			}
		}
		if remediationsResp, err := client.ListRemediations(ctx, namespace); err == nil {
			for _, rem := range remediationsResp {
				_ = output.PrintJSONLine(map[string]any{
					"type": "remediation", "eventType": "snapshot", "data": rem,
				})
			}
		}
	}

	// Block until context cancellation.
	<-ctx.Done()
	return nil
}

// printIncidentEvent prints a formatted incident event line.
func printIncidentEvent(ts time.Time, event ws.IncidentEvent) {
	timestamp := ts.Format("15:04:05")
	severity := output.SeverityColor(event.Severity)
	persona := event.PersonaName
	if event.Namespace != "" {
		persona = event.Namespace + "/" + persona
	}

	label := "INCIDENT"
	switch event.EventType {
	case "created":
		fmt.Printf("[%s] %-10s %-10s %-20s %-30s %s\n",
			timestamp, output.Red(label), severity, event.Signal, persona, output.Yellow("Detected"))
	case "updated":
		fmt.Printf("[%s] %-10s %-10s %-20s %-30s %s\n",
			timestamp, output.Yellow(label), severity, event.Signal, persona, output.Blue("Updated"))
	case "resolved":
		fmt.Printf("[%s] %-10s %-10s %-20s %-30s %s\n",
			timestamp, output.Green(label), output.Green("info"), event.Signal, persona, output.Green("Resolved"))
	default:
		fmt.Printf("[%s] %-10s %-10s %-20s %-30s %s\n",
			timestamp, label, severity, event.Signal, persona, event.EventType)
	}
}

// printRemediationEvent prints a formatted remediation event line.
func printRemediationEvent(ts time.Time, event ws.RemediationEvent) {
	timestamp := ts.Format("15:04:05")
	persona := event.PersonaName
	if event.Namespace != "" {
		persona = event.Namespace + "/" + persona
	}

	label := "REMEDY"
	switch event.EventType {
	case "created":
		fmt.Printf("[%s] %-10s %-10s %-20s %-30s %s\n",
			timestamp, output.Yellow(label), output.Yellow("warning"), event.ActionType, persona, output.Yellow("Pending"))
	case "approved":
		fmt.Printf("[%s] %-10s %-10s %-20s %-30s %s\n",
			timestamp, output.Blue(label), output.Blue("info"), event.ActionType, persona, output.Blue("Approved"))
	case "completed":
		fmt.Printf("[%s] %-10s %-10s %-20s %-30s %s\n",
			timestamp, output.Green(label), output.Green("info"), event.ActionType, persona, output.Green("Completed"))
	case "rolledback":
		fmt.Printf("[%s] %-10s %-10s %-20s %-30s %s\n",
			timestamp, output.Red(label), output.Red("warning"), event.ActionType, persona, output.Red("RolledBack"))
	case "rejected":
		fmt.Printf("[%s] %-10s %-10s %-20s %-30s %s\n",
			timestamp, output.Red(label), output.Red("warning"), event.ActionType, persona, output.Red("Rejected"))
	default:
		fmt.Printf("[%s] %-10s %-10s %-20s %-30s %s\n",
			timestamp, label, "", event.ActionType, persona, event.EventType)
	}
}

// printHealthUpdateEvent prints a formatted health update line.
func printHealthUpdateEvent(ts time.Time, event ws.HealthUpdateEvent) {
	timestamp := ts.Format("15:04:05")

	incidentStr := output.Green(fmt.Sprintf("%d", event.ActiveIncidents))
	if event.ActiveIncidents > 0 {
		incidentStr = output.Red(fmt.Sprintf("%d", event.ActiveIncidents))
	}

	remedyStr := fmt.Sprintf("%d", event.PendingRemedies)
	if event.PendingRemedies > 0 {
		remedyStr = output.Yellow(remedyStr)
	}

	parts := []string{
		fmt.Sprintf("incidents=%s", incidentStr),
		fmt.Sprintf("pending-remedies=%s", remedyStr),
	}
	if event.NodeCount > 0 {
		parts = append(parts, fmt.Sprintf("nodes=%d/%d", event.HealthyNodes, event.NodeCount))
	}
	if event.CPUUtilization != "" {
		parts = append(parts, fmt.Sprintf("cpu=%s", event.CPUUtilization))
	}
	if event.MemUtilization != "" {
		parts = append(parts, fmt.Sprintf("mem=%s", event.MemUtilization))
	}

	fmt.Printf("[%s] %-10s %s\n", timestamp, output.Blue("HEALTH"), strings.Join(parts, "  "))
}
