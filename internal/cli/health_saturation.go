package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"time"

	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/dorgu-ai/dorgu/internal/output"
)

// CF6-2 / CR-03 — believable saturation, computed here.
//
// `dorgu health` reported **1689% CPU saturation** on a cluster where 25% was
// requested and 1% was in use. Two separate defects produced that.
//
// The number was not the CLI's. It was
// `ClusterPersona.status.resourceSummary.cpuUtilization`, printed verbatim, and
// the operator computes it as the requests of every non-terminal pod over node
// allocatable. A pod no node has accepted is non-terminal, so a backlog of
// unschedulable pods inflates the figure without limit. An unscheduled pod holds
// no allocation on any node: counting it against allocatable is a category
// error, not an approximation. That is fixed at the source in the operator
// (`internal/controller/clusterpersona_discovery.go`) as well, because the
// dashboard reads the same field.
//
// And the line conflated two different measurements. The CRD field is called
// `usedCPU` but holds *requests*; the CLI struct field was called `Used`; the
// label said "requests". Requests are what the scheduler has committed. Used is
// what the containers are actually burning. On the clean-room cluster those were
// 25% and 1%, which is the difference between "nearly full" and "nearly idle",
// and a single line could not say which one it meant.
//
// So saturation is computed here now, from the node list the node table already
// fetched plus a pod list, and reported as two figures instead of one. Computing
// it here is not duplication for its own sake: `dorgu health` documents itself as
// querying the Kubernetes API directly, this is the one section that did not, and
// a CLI that trusts a five-minute-stale field from whatever operator version
// happens to be installed cannot be right about it. It also means saturation
// appears with no operator installed at all, which it previously did not.
//
// Not fixed here: no saturation *incident* is raised at any level, because
// incident detection is the operator's. Flagged in the PR.

// resourceSaturation is what the cluster has committed and what it is using.
type resourceSaturation struct {
	CPU    *saturationDetail `json:"cpu,omitempty"`
	Memory *saturationDetail `json:"memory,omitempty"`

	// Nodes and ScheduledPods say what the figures cover, so a reader can tell
	// a quiet cluster from a partial read.
	Nodes         int `json:"nodes"`
	ScheduledPods int `json:"scheduledPods"`

	// UnscheduledPods counts the pods excluded from the requested figure because
	// no node has accepted them. Reporting the count keeps the exclusion visible
	// and surfaces the real problem the old percentage was burying: pods that
	// cannot be placed.
	UnscheduledPods int `json:"unscheduledPods"`

	// UsedUnavailable explains an absent used figure in one line, so "not
	// measured" is never mistaken for "zero".
	UsedUnavailable string `json:"usedUnavailable,omitempty"`
}

// saturationDetail reports one resource twice over, because requested and used
// answer different questions and used to share a line.
type saturationDetail struct {
	Allocatable      string `json:"allocatable"`
	Requested        string `json:"requested"`
	RequestedPercent string `json:"requestedPercent"`
	Used             string `json:"used,omitempty"`
	UsedPercent      string `json:"usedPercent,omitempty"`
}

// nodeCapacity is the allocatable pool: what the scheduler is allowed to hand
// out, summed over the nodes.
type nodeCapacity struct {
	CPU    resource.Quantity
	Memory resource.Quantity
	Nodes  int
}

// podClaims is what the scheduled pods have been granted from that pool.
type podClaims struct {
	CPU         resource.Quantity
	Memory      resource.Quantity
	Scheduled   int
	Unscheduled int
}

// nodeUsage is live consumption, from metrics-server.
type nodeUsage struct {
	CPU    resource.Quantity
	Memory resource.Quantity
}

// nodeMetricsPath is the metrics-server endpoint. It is read raw rather than via
// `kubectl top`, whose output is a formatted table meant for humans and has
// changed shape between versions.
const nodeMetricsPath = "/apis/metrics.k8s.io/v1beta1/nodes"

// saturationTimeout bounds the whole saturation section.
//
// Listing every pod in the cluster is the expensive call in `dorgu health`, and
// on a large cluster it is expensive enough to be worth a deadline of its own.
// Sharing the command's deadline would let one slow pod list take the node
// table, the incident list and the exit code down with it, which is a worse
// outcome than a cluster reporting no saturation figure and saying why.
const saturationTimeout = 15 * time.Second

// collectResourceSaturation computes saturation from the cluster.
//
// nodesJSON is the node list the node table was built from, reused so `dorgu
// health` does not list nodes twice. The pod list is required: without it there
// is no requested figure and saying so is better than inventing one. Metrics are
// optional, because metrics-server is not installed by default and a missing
// used figure is a known gap rather than a failure.
func collectResourceSaturation(parent context.Context, kubeconfig string, nodesJSON []byte) (*resourceSaturation, error) {
	capacity, err := parseNodeCapacity(nodesJSON)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(parent, saturationTimeout)
	defer cancel()

	podsJSON, err := fetchPodsForSaturation(ctx, kubeconfig)
	if err != nil {
		return nil, err
	}
	claims, err := parsePodClaims(podsJSON)
	if err != nil {
		return nil, err
	}

	usage, usageErr := fetchNodeUsage(ctx, kubeconfig)
	return buildSaturation(capacity, claims, usage, usageErr), nil
}

// fetchPodsForSaturation lists the pods that could be holding an allocation.
//
// Succeeded and Failed pods are excluded by the API server via the field
// selector: they have run to completion and their allocation is released, so
// they are neither requesting nor using anything. Pods that no node has accepted
// come back and are excluded here, in parsePodClaims, because there is no field
// selector for "unscheduled".
func fetchPodsForSaturation(ctx context.Context, kubeconfig string) ([]byte, error) {
	out, err := kubectlCmd(ctx, kubeconfig, "get", "pods", "--all-namespaces", "-o", "json",
		"--field-selector", "status.phase!=Succeeded,status.phase!=Failed").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("could not list pods: %s", kubectlReason(out, err))
	}
	return out, nil
}

// fetchNodeUsage reads live node usage from metrics-server.
func fetchNodeUsage(ctx context.Context, kubeconfig string) (*nodeUsage, error) {
	out, err := kubectlCmd(ctx, kubeconfig, "get", "--raw", nodeMetricsPath).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("metrics-server did not answer: %s", kubectlReason(out, err))
	}
	return parseNodeUsage(out)
}

// kubectlReason renders a failed call as its stderr, falling back to the process
// error when there is none. A call the deadline cancelled writes nothing to
// stderr, and "could not list pods: " with nothing after it is the empty-operand
// mistake in a different place.
func kubectlReason(out []byte, err error) string {
	if text := kubectlErrText(out); text != "" {
		return text
	}
	return err.Error()
}

// parseNodeCapacity sums allocatable CPU and memory over the node list.
//
// Allocatable rather than capacity: capacity includes what the kubelet and the
// OS reserve, which the scheduler will never hand to a pod, so requests over
// capacity understates how full the cluster is.
func parseNodeCapacity(raw []byte) (nodeCapacity, error) {
	var list struct {
		Items []struct {
			Status struct {
				Allocatable struct {
					CPU    string `json:"cpu"`
					Memory string `json:"memory"`
				} `json:"allocatable"`
			} `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		return nodeCapacity{}, fmt.Errorf("failed to parse node list: %w", err)
	}

	out := nodeCapacity{Nodes: len(list.Items)}
	for _, n := range list.Items {
		addQuantity(&out.CPU, n.Status.Allocatable.CPU)
		addQuantity(&out.Memory, n.Status.Allocatable.Memory)
	}
	return out, nil
}

// parsePodClaims sums what the scheduled pods have claimed, and counts what was
// left out.
//
// spec.nodeName is the scheduler's decision written onto the object, so an empty
// one means the pod is still in the queue: Pending, Unschedulable, or waiting on
// a node that does not exist yet. It holds nothing on any node. Summing those
// against allocatable is what produced 1689%.
func parsePodClaims(raw []byte) (podClaims, error) {
	var list struct {
		Items []struct {
			Spec struct {
				NodeName   string             `json:"nodeName"`
				Containers []containerRequest `json:"containers"`
				Init       []containerRequest `json:"initContainers"`
			} `json:"spec"`
			Status struct {
				Phase string `json:"phase"`
			} `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		return podClaims{}, fmt.Errorf("failed to parse pod list: %w", err)
	}

	var out podClaims
	for _, p := range list.Items {
		// The field selector should have removed these, but a cluster that
		// ignores it must not be allowed to skew the number.
		if p.Status.Phase == "Succeeded" || p.Status.Phase == "Failed" {
			continue
		}
		if p.Spec.NodeName == "" {
			out.Unscheduled++
			continue
		}
		out.Scheduled++
		cpu, memory := podRequests(p.Spec.Containers, p.Spec.Init)
		out.CPU.Add(cpu)
		out.Memory.Add(memory)
	}
	return out, nil
}

// containerRequest is one container's requests, the only part of a container
// spec this computation reads.
type containerRequest struct {
	Resources struct {
		Requests struct {
			CPU    string `json:"cpu"`
			Memory string `json:"memory"`
		} `json:"requests"`
	} `json:"resources"`
}

// podRequests returns what one pod claims from its node: the sum of its app
// containers, floored by the largest single init container.
//
// That is how the scheduler accounts for a pod. Init containers run before the
// app containers rather than alongside them, so a pod needs whichever is larger,
// not both. This mirrors the operator's own podRequests deliberately: two
// answers to the same question is worse than one answer in two places.
func podRequests(containers, initContainers []containerRequest) (cpu, memory resource.Quantity) {
	for _, c := range containers {
		addQuantity(&cpu, c.Resources.Requests.CPU)
		addQuantity(&memory, c.Resources.Requests.Memory)
	}
	for _, c := range initContainers {
		var icpu, imemory resource.Quantity
		addQuantity(&icpu, c.Resources.Requests.CPU)
		addQuantity(&imemory, c.Resources.Requests.Memory)
		if icpu.Cmp(cpu) > 0 {
			cpu = icpu
		}
		if imemory.Cmp(memory) > 0 {
			memory = imemory
		}
	}
	return cpu, memory
}

// parseNodeUsage sums live per-node usage from a metrics.k8s.io NodeMetricsList.
func parseNodeUsage(raw []byte) (*nodeUsage, error) {
	var list struct {
		Items []struct {
			Usage struct {
				CPU    string `json:"cpu"`
				Memory string `json:"memory"`
			} `json:"usage"`
		} `json:"items"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, fmt.Errorf("failed to parse node metrics: %w", err)
	}
	if len(list.Items) == 0 {
		return nil, fmt.Errorf("metrics-server reported no nodes")
	}

	out := &nodeUsage{}
	for _, n := range list.Items {
		addQuantity(&out.CPU, n.Usage.CPU)
		addQuantity(&out.Memory, n.Usage.Memory)
	}
	return out, nil
}

// addQuantity adds a quantity string to a running total, ignoring anything it
// cannot parse.
//
// An unparseable or absent value is treated as zero on purpose. A container with
// no CPU request genuinely requests no CPU, which is a real answer, and refusing
// to report saturation at all because one field on one pod is malformed would
// trade a slightly-off number for no number.
func addQuantity(total *resource.Quantity, value string) {
	if value == "" {
		return
	}
	q, err := resource.ParseQuantity(value)
	if err != nil {
		return
	}
	total.Add(q)
}

// buildSaturation assembles the reported figures. usageErr is the reason a used
// figure is absent, and it is carried through rather than dropped so the output
// can say why.
func buildSaturation(c nodeCapacity, p podClaims, u *nodeUsage, usageErr error) *resourceSaturation {
	out := &resourceSaturation{
		Nodes:           c.Nodes,
		ScheduledPods:   p.Scheduled,
		UnscheduledPods: p.Unscheduled,
	}
	if u == nil {
		out.UsedUnavailable = "no metrics-server"
		if usageErr != nil {
			out.UsedUnavailable = usageErr.Error()
		}
	}

	if c.CPU.MilliValue() > 0 {
		out.CPU = &saturationDetail{
			Allocatable:      formatCPU(c.CPU),
			Requested:        formatCPU(p.CPU),
			RequestedPercent: percentOf(p.CPU.MilliValue(), c.CPU.MilliValue()),
		}
		if u != nil {
			out.CPU.Used = formatCPU(u.CPU)
			out.CPU.UsedPercent = percentOf(u.CPU.MilliValue(), c.CPU.MilliValue())
		}
	}
	if c.Memory.Value() > 0 {
		out.Memory = &saturationDetail{
			Allocatable:      formatMemory(c.Memory),
			Requested:        formatMemory(p.Memory),
			RequestedPercent: percentOf(p.Memory.Value(), c.Memory.Value()),
		}
		if u != nil {
			out.Memory.Used = formatMemory(u.Memory)
			out.Memory.UsedPercent = percentOf(u.Memory.Value(), c.Memory.Value())
		}
	}
	return out
}

// percentOf renders part/whole as a whole-number percentage. An unknown or zero
// denominator yields "n/a" rather than a fabricated 0%.
func percentOf(part, whole int64) string {
	if whole <= 0 {
		return "n/a"
	}
	return fmt.Sprintf("%d%%", int64(math.Round(float64(part)/float64(whole)*100)))
}

// formatCPU renders CPU in millicores throughout, which is the unit both node
// allocatable and container requests are usually written in and the only one
// that stays readable for both a 50m sidecar and a 64-core node.
func formatCPU(q resource.Quantity) string {
	return fmt.Sprintf("%dm", q.MilliValue())
}

// formatMemory renders bytes in the largest binary unit that keeps the number
// above 1, so a sum reads "1.4Gi" rather than "1503238553".
func formatMemory(q resource.Quantity) string {
	const (
		ki = 1 << 10
		mi = 1 << 20
		gi = 1 << 30
	)
	b := q.Value()
	switch {
	case b >= gi:
		return fmt.Sprintf("%.1fGi", float64(b)/gi)
	case b >= mi:
		return fmt.Sprintf("%.0fMi", float64(b)/mi)
	case b >= ki:
		return fmt.Sprintf("%.0fKi", float64(b)/ki)
	default:
		return fmt.Sprintf("%d", b)
	}
}

// saturationWarnPercent is the requested share at which the output stops being
// neutral. Above it the cluster genuinely cannot take much more, and the reader
// should not have to do the division.
const saturationWarnPercent = 90

// printResourceSaturation renders the section: allocatable once, then requested
// and used side by side.
//
// The two columns are the fix for the conflation. A single "25% / 1689%" figure
// cannot say whether the cluster is booked out or busy, and those call for
// different actions: requests near allocatable means nothing more will schedule,
// used near allocatable means what is already running is struggling.
func printResourceSaturation(w io.Writer, s *resourceSaturation) {
	if s == nil || (s.CPU == nil && s.Memory == nil) {
		return
	}

	fmt.Fprintf(w, "Resource Saturation: %s\n", output.Dim(saturationScope(s)))

	tbl := output.NewTable(w, "", "REQUESTED", "USED")
	if s.CPU != nil {
		tbl.AddRow("  CPU", requestedCell(s.CPU), usedCell(s.CPU, s.UsedUnavailable))
	}
	if s.Memory != nil {
		tbl.AddRow("  Memory", requestedCell(s.Memory), usedCell(s.Memory, s.UsedUnavailable))
	}
	tbl.Render()

	fmt.Fprintln(w, output.Dim(
		"  Requested is what the scheduler has committed. Used is what the containers are consuming."))

	if s.UnscheduledPods > 0 {
		output.Fwarn(w, fmt.Sprintf(
			"%s not scheduled onto any node, so %s excluded from the figures above.",
			pluralPods(s.UnscheduledPods), isAre(s.UnscheduledPods)))
		fmt.Fprintln(w, output.Dim(
			"  A pod no node has accepted holds no allocation. Counting one would inflate saturation without limit."))
	}

	printSaturationPressure(w, s)
	fmt.Fprintln(w)
}

// saturationScope says what the numbers were computed over.
func saturationScope(s *resourceSaturation) string {
	return fmt.Sprintf("(%d node(s), %d scheduled pod(s))", s.Nodes, s.ScheduledPods)
}

// requestedCell renders "<requested> / <allocatable> (<percent>)".
func requestedCell(d *saturationDetail) string {
	return fmt.Sprintf("%s / %s (%s)", d.Requested, d.Allocatable, d.RequestedPercent)
}

// usedCell renders the used half, or names the reason there is none. It never
// prints an empty operand, which is the F-09 invariant this section already had.
func usedCell(d *saturationDetail, unavailable string) string {
	if d.Used == "" {
		reason := unavailable
		if reason == "" {
			reason = "not reported"
		}
		return fmt.Sprintf("n/a (%s)", reason)
	}
	return fmt.Sprintf("%s / %s (%s)", d.Used, d.Allocatable, d.UsedPercent)
}

// printSaturationPressure names a resource that is close to booked out. The old
// output left the reader to compare two numbers it had already conflated.
func printSaturationPressure(w io.Writer, s *resourceSaturation) {
	for _, r := range []struct {
		name   string
		detail *saturationDetail
	}{{"CPU", s.CPU}, {"Memory", s.Memory}} {
		if r.detail == nil {
			continue
		}
		if pct, ok := percentValue(r.detail.RequestedPercent); ok && pct >= saturationWarnPercent {
			output.Fwarn(w, fmt.Sprintf(
				"%s requests are at %s of allocatable; new pods may not schedule.", r.name, r.detail.RequestedPercent))
		}
	}
}

// percentValue reads back a percentage this package rendered. It reports false
// for "n/a", which is not a number and must not compare as one.
func percentValue(s string) (int, bool) {
	var pct int
	if _, err := fmt.Sscanf(s, "%d%%", &pct); err != nil {
		return 0, false
	}
	return pct, true
}

// pluralPods and isAre keep the two count-dependent sentences readable without a
// pluralisation library.
func pluralPods(n int) string {
	if n == 1 {
		return "1 pod is"
	}
	return fmt.Sprintf("%d pods are", n)
}

func isAre(n int) string {
	if n == 1 {
		return "it is"
	}
	return "they are"
}
