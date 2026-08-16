package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/dorgu-ai/dorgu/internal/output"
)

// WS6 — heal-on-approve. The operator patches ApplicationPersona.spec (desired
// state) and never writes workloads (a non-negotiable invariant). So on approve,
// the persona's memory goes e.g. 64→128Mi but the running Deployment is unchanged
// and the pod keeps OOMing. This file closes the loop: the CLI (the user's own
// tool, with the user's creds) translates the approved persona-update into the
// equivalent Deployment change and applies it, so the pod recovers. v1 supports
// the resource limits/requests case (memory/CPU — the OOM/saturation path); other
// step types are surfaced as advisory ("manual step: …"), not executed.

// healOptions carries the caller-supplied heal parameters.
type healOptions struct {
	kubeconfig string
	workload   string // explicit Deployment name (overrides label discovery)
	container  string // explicit container name
	assumeYes  bool   // skip the confirmation prompt
}

// healResourceChange is the resource intent extracted from a persona-update patch.
// Limits/Requests hold only the fields the remediation actually changed
// (cpu/memory), so we patch nothing broader than the proposal.
type healResourceChange struct {
	Limits   map[string]string
	Requests map[string]string
}

func (h *healResourceChange) isEmpty() bool {
	return h == nil || (len(h.Limits) == 0 && len(h.Requests) == 0)
}

// advisoryStep is a plan step the CLI does not execute automatically (restart,
// scale, config-change, manual, or any non-resource persona-update). It is printed
// as an ordered manual instruction.
type advisoryStep struct {
	Order       int32
	Type        string
	Description string
	Reason      string
	// Command is the step's ready-to-run kubectl command, when the plan
	// supplied one that passes displayableStepCommand. Printed, never run.
	Command string
}

// healPlan is the CLI-side interpretation of a RemediationAction for healing:
// the auto-applicable resource change (if any) plus the advisory steps to print.
type healPlan struct {
	Change   *healResourceChange
	Advisory []advisoryStep
}

// deploymentSummary is the minimal Deployment shape the heal path needs.
// Labels and SelectorLabels feed the persona → Deployment fallback chain in
// workload_match.go.
type deploymentSummary struct {
	Name           string
	Containers     []string
	Labels         map[string]string
	SelectorLabels map[string]string
}

// healExecution is a fully resolved, ready-to-apply workload change: the
// Deployment, the container and the exact patch. Producing one is the preflight
// that approval runs before it flips a RemediationAction to Approved, so a heal
// that cannot be applied never reports success and never lets the operator
// advance the action to Applying/Verifying over a change that was never made.
type healExecution struct {
	Namespace  string
	Deployment string
	Container  string
	Patch      string
	Change     *healResourceChange
	Advisory   []advisoryStep
	Context    string
}

// hasWorkloadChange reports whether this plan actually patches a workload, as
// opposed to carrying advisory steps only.
func (e *healExecution) hasWorkloadChange() bool {
	return e != nil && !e.Change.isEmpty()
}

// --- plan interpretation (pure) ---

// buildHealPlan interprets a RemediationAction into a healPlan. The ordered
// Steps[] plan is the source of truth when present; otherwise the legacy single
// Action is used. Only persona-update steps carrying a resource change are
// auto-applied — everything else becomes an advisory step.
func buildHealPlan(r *remediationFull) (*healPlan, error) {
	plan := &healPlan{}

	if len(r.Spec.Steps) > 0 {
		for _, s := range sortedSteps(r.Spec.Steps) {
			if s.Type == "persona-update" {
				rc, err := extractResourceChange(s.Patch)
				if err != nil {
					return nil, fmt.Errorf("step %d (%s): %w", s.Order, s.ID, err)
				}
				if !rc.isEmpty() {
					plan.Change = mergeResourceChange(plan.Change, rc)
					continue
				}
			}
			plan.Advisory = append(plan.Advisory, advisoryStep{
				Order:       s.Order,
				Type:        s.Type,
				Description: s.Description,
				Reason:      advisoryReason(s.Type),
				Command:     displayableStepCommand(s.Command),
			})
		}
		return plan, nil
	}

	// Legacy single-Action object (no Steps[]).
	if r.Spec.Action.Type == "persona-update" {
		rc, err := extractResourceChange(r.Spec.Action.Patch)
		if err != nil {
			return nil, err
		}
		if !rc.isEmpty() {
			plan.Change = rc
			return plan, nil
		}
	}
	if r.Spec.Action.Type != "" {
		plan.Advisory = append(plan.Advisory, advisoryStep{
			Order:       1,
			Type:        r.Spec.Action.Type,
			Description: r.Spec.Explanation,
			Reason:      advisoryReason(r.Spec.Action.Type),
		})
	}
	return plan, nil
}

// advisoryReason explains why a step is surfaced rather than executed.
func advisoryReason(stepType string) string {
	switch stepType {
	case "restart":
		return "restart is handled by the workload controller after the resource patch"
	case "scale":
		return "scaling is a workload change; apply via your GitOps/deploy pipeline"
	case "config-change":
		return "config change requires manual review"
	case "manual":
		return "manual verification step"
	default:
		return "not an auto-applicable resource change"
	}
}

// extractResourceChange pulls resources.limits/requests.{cpu,memory} out of a
// persona-update patch. The operator emits spec-wrapped patches
// ({"spec":{"resources":…}}) but older/rule-based objects may be spec-relative
// ({"resources":…}); both are accepted. Returns nil when the patch carries no
// resource fields (e.g. a replicas/scale patch), which is treated as advisory.
func extractResourceChange(raw json.RawMessage) (*healResourceChange, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var m map[string]interface{}
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("parse patch: %w", err)
	}
	if spec, ok := m["spec"].(map[string]interface{}); ok {
		m = spec
	}
	res, ok := m["resources"].(map[string]interface{})
	if !ok {
		return nil, nil
	}
	rc := &healResourceChange{
		Limits:   stringMap(res["limits"]),
		Requests: stringMap(res["requests"]),
	}
	if rc.isEmpty() {
		return nil, nil
	}
	return rc, nil
}

// stringMap extracts a map[string]string from a decoded JSON value, keeping only
// string-valued entries. Returns nil for anything that is not such a map.
func stringMap(v interface{}) map[string]string {
	mm, ok := v.(map[string]interface{})
	if !ok {
		return nil
	}
	out := make(map[string]string)
	for k, val := range mm {
		if s, ok := val.(string); ok {
			out[k] = s
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// mergeResourceChange returns a new healResourceChange with b merged onto a
// (b wins on conflict). Neither input is mutated.
func mergeResourceChange(a, b *healResourceChange) *healResourceChange {
	out := &healResourceChange{Limits: map[string]string{}, Requests: map[string]string{}}
	for _, src := range []*healResourceChange{a, b} {
		if src == nil {
			continue
		}
		for k, v := range src.Limits {
			out.Limits[k] = v
		}
		for k, v := range src.Requests {
			out.Requests[k] = v
		}
	}
	if len(out.Limits) == 0 {
		out.Limits = nil
	}
	if len(out.Requests) == 0 {
		out.Requests = nil
	}
	return out
}

// --- translation (pure) ---

// buildDeploymentResourcePatch builds a strategic-merge patch that sets the given
// container's resources to exactly the fields the remediation changed. Map keys
// are marshalled in sorted order, so the output is deterministic.
func buildDeploymentResourcePatch(container string, rc *healResourceChange) (string, error) {
	if rc.isEmpty() {
		return "", fmt.Errorf("no resource change to apply")
	}
	if container == "" {
		return "", fmt.Errorf("container name is required to build the patch")
	}

	resources := map[string]interface{}{}
	if len(rc.Limits) > 0 {
		resources["limits"] = rc.Limits
	}
	if len(rc.Requests) > 0 {
		resources["requests"] = rc.Requests
	}

	patch := map[string]interface{}{
		"spec": map[string]interface{}{
			"template": map[string]interface{}{
				"spec": map[string]interface{}{
					"containers": []map[string]interface{}{
						{"name": container, "resources": resources},
					},
				},
			},
		},
	}
	b, err := json.Marshal(patch)
	if err != nil {
		return "", fmt.Errorf("marshal deployment patch: %w", err)
	}
	return string(b), nil
}

// resourceChangeSummary renders the changed fields as human-readable lines.
func resourceChangeSummary(rc *healResourceChange) []string {
	var lines []string
	appendKind := func(kind string, m map[string]string) {
		for _, k := range sortedKeys(m) {
			lines = append(lines, fmt.Sprintf("resources.%s.%s → %s", kind, k, m[k]))
		}
	}
	appendKind("limits", rc.Limits)
	appendKind("requests", rc.Requests)
	return lines
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// --- workload selection (pure) ---

// selectDeployment lives in workload_match.go: resolving a persona to its
// Deployment is the same ordered fallback chain the operator uses.

// selectContainer picks the container to patch: an explicit --container if valid,
// else the sole container, else the one whose name matches the app, else an error
// requiring --container.
func selectContainer(containers []string, appName, flag string) (string, error) {
	if flag != "" {
		for _, c := range containers {
			if c == flag {
				return flag, nil
			}
		}
		return "", fmt.Errorf("container %q not found in workload (have: %s)",
			flag, strings.Join(containers, ", "))
	}
	switch len(containers) {
	case 0:
		return "", fmt.Errorf("workload has no containers")
	case 1:
		return containers[0], nil
	default:
		for _, c := range containers {
			if c == appName {
				return c, nil
			}
		}
		return "", fmt.Errorf(
			"workload has multiple containers (%s); specify one with --container",
			strings.Join(containers, ", "))
	}
}

// --- safety (pure) ---

// guardKubeContext refuses to heal against a production-looking context, mirroring
// the aws-spike guard_context scripts (deny *prod* and the known prod context).
func guardKubeContext(ctxName string) error {
	name := strings.TrimSpace(ctxName)
	if name == "" {
		return fmt.Errorf("no active kube-context; point kubectl at your cluster first")
	}
	if strings.Contains(strings.ToLower(name), "prod") || name == "vox-prod-synthiolabs" {
		return fmt.Errorf(
			"refusing to heal: kube-context %q looks like PRODUCTION; switch to a non-production context",
			name)
	}
	return nil
}

// --- kubectl orchestration ---

// currentKubeContext returns the active kube-context name.
func currentKubeContext(ctx context.Context, kubeconfig string) (string, error) {
	out, err := kubectlCmd(ctx, kubeconfig, "config", "current-context").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to read current kube-context: %s", kubectlErrText(out))
	}
	return kubectlValue(out), nil
}

// personaNamespace returns the namespace the workload must live in: the persona's
// namespace (never anywhere else). Falls back to the remediation's namespace.
func personaNamespace(r *remediationFull) string {
	if r.Spec.PersonaRef.Namespace != "" {
		return r.Spec.PersonaRef.Namespace
	}
	return r.Metadata.Namespace
}

// resolveAppName determines the app name to resolve the workload by. The
// operator resolves Deployments from persona.spec.name, so we read spec.name
// from the ApplicationPersona (best-effort); if that lookup fails we fall back to
// the personaRef name.
func resolveAppName(ctx context.Context, kubeconfig string, r *remediationFull) string {
	name := r.Spec.PersonaRef.Name
	if name == "" {
		return ""
	}
	ns := personaNamespace(r)
	out, err := kubectlCmd(ctx, kubeconfig, "get", "applicationpersona", name, "-n", ns, "-o", "json").CombinedOutput()
	if err != nil {
		return name
	}
	var p struct {
		Spec struct {
			Name string `json:"name"`
		} `json:"spec"`
	}
	if json.Unmarshal(out, &p) == nil && p.Spec.Name != "" {
		return p.Spec.Name
	}
	return name
}

// discoverWorkload finds the target Deployment. An explicit --workload is fetched
// directly; otherwise every Deployment in the persona's namespace is a candidate
// and the fallback chain picks one. Listing without a label selector is the
// point: a selector can only find workloads labelled on the Deployment object,
// which most real manifests are not.
func discoverWorkload(ctx context.Context, kubeconfig, ns, appName, workloadFlag string) (deploymentSummary, error) {
	if workloadFlag != "" {
		return getDeployment(ctx, kubeconfig, ns, workloadFlag)
	}
	ds, err := listDeployments(ctx, kubeconfig, ns)
	if err != nil {
		return deploymentSummary{}, err
	}
	return selectDeployment(ds, appName)
}

// listDeployments returns every Deployment in the namespace.
func listDeployments(ctx context.Context, kubeconfig, ns string) ([]deploymentSummary, error) {
	out, err := kubectlCmd(ctx, kubeconfig, "get", "deployment", "-n", ns, "-o", "json").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to list deployments: %s", kubectlErrText(out))
	}
	return parseDeploymentList(out)
}

func getDeployment(ctx context.Context, kubeconfig, ns, name string) (deploymentSummary, error) {
	out, err := kubectlCmd(ctx, kubeconfig, "get", "deployment", name, "-n", ns, "-o", "json").CombinedOutput()
	if err != nil {
		return deploymentSummary{}, fmt.Errorf("failed to get deployment %s: %s", name, kubectlErrText(out))
	}
	return parseDeploymentObject(out)
}

// deploymentJSON is the minimal shape parsed from kubectl output.
type deploymentJSON struct {
	Metadata struct {
		Name   string            `json:"name"`
		Labels map[string]string `json:"labels"`
	} `json:"metadata"`
	Spec struct {
		Selector struct {
			MatchLabels map[string]string `json:"matchLabels"`
		} `json:"selector"`
		Template struct {
			Spec struct {
				Containers []struct {
					Name string `json:"name"`
				} `json:"containers"`
			} `json:"spec"`
		} `json:"template"`
	} `json:"spec"`
}

func (d deploymentJSON) toSummary() deploymentSummary {
	containers := make([]string, 0, len(d.Spec.Template.Spec.Containers))
	for _, c := range d.Spec.Template.Spec.Containers {
		containers = append(containers, c.Name)
	}
	return deploymentSummary{
		Name:           d.Metadata.Name,
		Containers:     containers,
		Labels:         d.Metadata.Labels,
		SelectorLabels: d.Spec.Selector.MatchLabels,
	}
}

func parseDeploymentList(raw []byte) ([]deploymentSummary, error) {
	var list struct {
		Items []deploymentJSON `json:"items"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, fmt.Errorf("parse deployments: %w", err)
	}
	out := make([]deploymentSummary, 0, len(list.Items))
	for _, it := range list.Items {
		out = append(out, it.toSummary())
	}
	return out, nil
}

func parseDeploymentObject(raw []byte) (deploymentSummary, error) {
	var d deploymentJSON
	if err := json.Unmarshal(raw, &d); err != nil {
		return deploymentSummary{}, fmt.Errorf("parse deployment: %w", err)
	}
	if d.Metadata.Name == "" {
		return deploymentSummary{}, fmt.Errorf("deployment not found")
	}
	return d.toSummary(), nil
}

func applyDeploymentPatch(ctx context.Context, kubeconfig, ns, name, patch string) error {
	out, err := kubectlCmd(ctx, kubeconfig,
		"patch", "deployment", name, "-n", ns, "--type", "strategic", "-p", patch).CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to patch deployment %s: %s", name, kubectlErrText(out))
	}
	return nil
}

// confirmHeal reads a y/N answer from in.
func confirmHeal(in io.Reader, out io.Writer) (bool, error) {
	fmt.Fprint(out, "\nApply this change to the workload? [y/N]: ")
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && err != io.EOF {
		return false, fmt.Errorf("failed to read confirmation: %w", err)
	}
	ans := strings.ToLower(strings.TrimSpace(line))
	return ans == "y" || ans == "yes", nil
}

// planHeal resolves everything the heal needs without changing anything: the
// kube-context guard, the remediation plan, the target Deployment, the container
// and the patch.
//
// This runs before approval writes anything. If it returns an error, no
// RemediationAction is approved, so the operator never patches the persona and
// the action never advances to Applying/Verifying over a workload change that
// could not be made.
func planHeal(ctx context.Context, r *remediationFull, opts healOptions) (*healExecution, error) {
	ctxName, err := currentKubeContext(ctx, opts.kubeconfig)
	if err != nil {
		return nil, err
	}
	if err := guardKubeContext(ctxName); err != nil {
		return nil, err
	}

	plan, err := buildHealPlan(r)
	if err != nil {
		return nil, fmt.Errorf("failed to read remediation plan: %w", err)
	}

	// Advisory-only plan: there is no workload change to resolve, so there is
	// nothing that can fail later either.
	if plan.Change.isEmpty() {
		return &healExecution{Advisory: plan.Advisory, Context: ctxName}, nil
	}

	ns := personaNamespace(r)
	if ns == "" {
		return nil, fmt.Errorf("remediation has no namespace; cannot locate the workload")
	}
	appName := resolveAppName(ctx, opts.kubeconfig, r)

	deploy, err := discoverWorkload(ctx, opts.kubeconfig, ns, appName, opts.workload)
	if err != nil {
		return nil, err
	}
	container, err := selectContainer(deploy.Containers, appName, opts.container)
	if err != nil {
		return nil, err
	}
	patch, err := buildDeploymentResourcePatch(container, plan.Change)
	if err != nil {
		return nil, err
	}

	return &healExecution{
		Namespace:  ns,
		Deployment: deploy.Name,
		Container:  container,
		Patch:      patch,
		Change:     plan.Change,
		Advisory:   plan.Advisory,
		Context:    ctxName,
	}, nil
}

// printHealPreamble prints the context header and any advisory steps. Advisory
// steps are always printed, never executed.
func printHealPreamble(w io.Writer, ctxName string, advisory []advisoryStep) {
	fmt.Fprintf(w, "\nHeal (context: %s)\n", ctxName)
	if len(advisory) == 0 {
		return
	}
	fmt.Fprintln(w, "\nManual steps (not applied automatically):")
	for _, a := range advisory {
		fmt.Fprintf(w, "  [%d] %s: %s\n", a.Order, a.Type, a.Description)
		if a.Reason != "" {
			fmt.Fprintf(w, "      %s\n", output.Dim(a.Reason))
		}
		if a.Command != "" {
			fmt.Fprintf(w, "      Run: %s\n", a.Command)
		}
	}
}

// executeHeal prints the resolved plan, confirms (unless assumeYes) and applies
// the patch. Success is only reported once kubectl has accepted the patch.
func executeHeal(ctx context.Context, exec *healExecution, opts healOptions, in io.Reader, w io.Writer) error {
	fmt.Fprintln(w, "\nWorkload heal plan:")
	fmt.Fprintf(w, "  Namespace:  %s\n", exec.Namespace)
	fmt.Fprintf(w, "  Deployment: %s\n", exec.Deployment)
	fmt.Fprintf(w, "  Container:  %s\n", exec.Container)
	for _, line := range resourceChangeSummary(exec.Change) {
		fmt.Fprintf(w, "  %s\n", line)
	}

	if !opts.assumeYes {
		ok, err := confirmHeal(in, w)
		if err != nil {
			return err
		}
		if !ok {
			output.Info("Heal skipped by user; the RemediationAction status is unchanged.")
			return nil
		}
	}

	if err := applyDeploymentPatch(ctx, opts.kubeconfig, exec.Namespace, exec.Deployment, exec.Patch); err != nil {
		return err
	}
	output.Success(fmt.Sprintf(
		"Healed %s/%s (container %s). The pod will restart with the updated resources.",
		exec.Namespace, exec.Deployment, exec.Container))
	return nil
}

// healWorkload plans and then applies the workload change. Used by
// `dorgu remediation heal`, which has nothing to preflight for: the
// RemediationAction has already been approved.
func healWorkload(ctx context.Context, r *remediationFull, opts healOptions, in io.Reader, w io.Writer) error {
	exec, err := planHeal(ctx, r, opts)
	if err != nil {
		return err
	}

	printHealPreamble(w, exec.Context, exec.Advisory)

	if !exec.hasWorkloadChange() {
		output.Warn("No auto-applicable resource change in this remediation; nothing to heal automatically.")
		return nil
	}
	return executeHeal(ctx, exec, opts, in, w)
}
