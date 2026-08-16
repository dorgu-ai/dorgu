package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/dorgu-ai/dorgu/internal/output"
)

// remediationCmdTimeout is the maximum time to wait for each kubectl call.
const remediationCmdTimeout = 30 * time.Second

// maxStepCommandLength and stepCommandForbidden mirror the operator's
// SanitizeStepCommand (dorgu-operator api/v1). They are duplicated rather than
// imported because the CLI does not depend on the operator module, and because
// this guard has to hold even when the object was written by something other
// than the operator. Keep the two in step.
const (
	maxStepCommandLength = 1024
	stepCommandForbidden = ";&|<>`$\n\r"
)

// activeRemediationPhases are phases considered "active" (shown by default without --all).
var activeRemediationPhases = map[string]bool{
	"Pending":   true,
	"Approved":  true,
	"Applying":  true,
	"Verifying": true,
	"Failed":    true,
}

func newRemediationCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remediation",
		Short: "Manage remediation proposals",
		Long: `List, review, approve, and reject remediation proposals for detected incidents.

Examples:
  dorgu remediation list
  dorgu remediation diff fix-oom-api-server -n production
  dorgu remediation approve fix-oom-api-server -n production
  dorgu remediation reject fix-oom-api-server -n production --reason "not needed"`,
		Aliases: []string{"rem"},
		// Reject a stray subcommand instead of printing help and exiting 0 (F-12).
		Args: noUnknownSubcommand,
		RunE: runSubcommandGroup,
	}
	cmd.AddCommand(
		newRemediationListCmd(),
		newRemediationDiffCmd(),
		newRemediationApproveCmd(),
		newRemediationRejectCmd(),
		newRemediationHealCmd(),
	)
	return cmd
}

// remediationStep mirrors the operator's RemediationStep (api/v1). It is a single
// ordered action within a multi-step remediation plan. Patch/PrePatchState are the
// operator's apiextensionsv1.JSON objects, so they are parsed as raw JSON.
type remediationStep struct {
	Order          int32           `json:"order"`
	ID             string          `json:"id"`
	Type           string          `json:"type"`
	Description    string          `json:"description"`
	Rationale      string          `json:"rationale"`
	Risk           string          `json:"risk"`
	AutoExecutable bool            `json:"autoExecutable"`
	Command        string          `json:"command"`
	Patch          json.RawMessage `json:"patch"`
	PrePatchState  json.RawMessage `json:"prePatchState"`
}

// remediationStepStatus mirrors the operator's StepStatus (api/v1).
type remediationStepStatus struct {
	Order              int32  `json:"order"`
	Phase              string `json:"phase"`
	AppliedAt          string `json:"appliedAt"`
	VerificationResult string `json:"verificationResult"`
}

// remediationFull is used for JSON parsing of RemediationAction resources. It is
// aligned to the operator CRD (dorgu-operator/api/v1/remediationaction_types.go):
// the action patch/prePatchState are JSON objects (apiextensionsv1.JSON), the
// action type is nested under spec.action.type, and the ordered Steps[] plan +
// PlanSource/PlanSummary are the plan of record when present.
type remediationFull struct {
	Metadata struct {
		Name              string `json:"name"`
		Namespace         string `json:"namespace"`
		CreationTimestamp string `json:"creationTimestamp"`
	} `json:"metadata"`
	Spec struct {
		Confidence  string `json:"confidence"`
		Explanation string `json:"explanation"`
		PlanSource  string `json:"planSource"`
		PlanSummary string `json:"planSummary"`
		PersonaRef  struct {
			Kind      string `json:"kind"`
			Name      string `json:"name"`
			Namespace string `json:"namespace"`
		} `json:"personaRef"`
		IncidentRef struct {
			Name      string `json:"name"`
			Namespace string `json:"namespace"`
		} `json:"incidentRef"`
		Action struct {
			Type          string          `json:"type"`
			Patch         json.RawMessage `json:"patch"`
			PrePatchState json.RawMessage `json:"prePatchState"`
		} `json:"action"`
		Steps    []remediationStep `json:"steps"`
		Rollback *struct {
			Enabled          bool   `json:"enabled"`
			HealthCheckAfter string `json:"healthCheckAfter"`
			MaxRetries       int32  `json:"maxRetries"`
		} `json:"rollback"`
	} `json:"spec"`
	Status struct {
		Phase        string                  `json:"phase"`
		ApprovedBy   string                  `json:"approvedBy"`
		ApprovedAt   string                  `json:"approvedAt"`
		CurrentStep  int32                   `json:"currentStep"`
		StepStatuses []remediationStepStatus `json:"stepStatuses"`
	} `json:"status"`
}

// --- list ---

func newRemediationListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List remediation proposals",
		Long: `List RemediationAction resources from the cluster. By default shows only
active remediations (Pending, Approved, Applying, Verifying, Failed).
Use --all to include completed, rejected, and expired.

Severity lives on the linked IncidentMemory, not on the RemediationAction — read it
with 'dorgu incidents describe <incident>'.

Examples:
  dorgu remediation list
  dorgu remediation list --phase Pending
  dorgu remediation list --all --limit 100`,
		RunE: runRemediationList,
	}

	cmd.Flags().StringP("namespace", "n", "", "filter by namespace (default: all)")
	cmd.Flags().String("phase", "", "filter by phase (Pending, Approved, Applying, Verifying, Completed, etc.)")
	cmd.Flags().Bool("all", false, "include completed/rejected/expired remediations")
	cmd.Flags().Int("limit", 50, "maximum number of remediations to show")
	cmd.Flags().String("kubeconfig", "", "path to kubeconfig (default: ~/.kube/config)")

	return cmd
}

func runRemediationList(cmd *cobra.Command, args []string) error {
	if _, err := exec.LookPath("kubectl"); err != nil {
		return fmt.Errorf("kubectl not found in PATH; required for remediation list")
	}

	namespace, _ := cmd.Flags().GetString("namespace")
	phase, _ := cmd.Flags().GetString("phase")
	showAll, _ := cmd.Flags().GetBool("all")
	limit, _ := cmd.Flags().GetInt("limit")
	kubeconfigFlag, _ := cmd.Flags().GetString("kubeconfig")

	kubeconfig, err := validateKubeconfig(kubeconfigFlag)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), remediationCmdTimeout)
	defer cancel()

	remediations, err := fetchRemediations(ctx, kubeconfig, namespace)
	if err != nil {
		return err
	}

	filtered := make([]remediationFull, 0)
	for _, r := range remediations {
		if phase != "" && r.Status.Phase != phase {
			continue
		}
		if !showAll && !activeRemediationPhases[r.Status.Phase] {
			continue
		}
		filtered = append(filtered, r)
		if len(filtered) >= limit {
			break
		}
	}

	if output.IsJSON() {
		return output.PrintJSON(filtered)
	}

	printRemediationList(os.Stdout, filtered, showAll)
	return nil
}

func fetchRemediations(ctx context.Context, kubeconfig, namespace string) ([]remediationFull, error) {
	args := []string{"get", "remediationaction", "-o", "json"}
	if namespace != "" {
		args = append(args, "-n", namespace)
	} else {
		args = append(args, "--all-namespaces")
	}

	out, err := kubectlCmd(ctx, kubeconfig, args...).CombinedOutput()
	if err != nil {
		outputStr := kubectlErrText(out)
		if strings.Contains(outputStr, "the server doesn't have a resource type") {
			output.ErrorWithHint("RemediationAction CRD not found. Is the dorgu operator installed?",
				"To install the operator: dorgu cluster setup")
			return nil, errSilent
		}
		return nil, fmt.Errorf("failed to list remediations: %s", outputStr)
	}

	var list struct {
		Items []remediationFull `json:"items"`
	}
	if err := json.Unmarshal(out, &list); err != nil {
		return nil, fmt.Errorf("failed to parse remediations: %w", err)
	}
	return list.Items, nil
}

func printRemediationList(w io.Writer, remediations []remediationFull, showAll bool) {
	label := "Active"
	if showAll {
		label = "All"
	}

	fmt.Fprintln(w)
	fmt.Fprintf(w, "%s Remediations (%d)\n", label, len(remediations))
	fmt.Fprintln(w)

	if len(remediations) == 0 {
		fmt.Fprintln(w, output.Dim("  No remediations found."))
		fmt.Fprintln(w)
		return
	}

	tbl := output.NewTable(w, "NAMESPACE", "NAME", "PHASE", "TYPE", "PLAN", "STEPS", "CONFIDENCE", "PERSONA", "AGE")
	for _, r := range remediations {
		tbl.AddRow(
			r.Metadata.Namespace,
			r.Metadata.Name,
			output.RemediationPhaseColor(r.Status.Phase),
			r.Spec.Action.Type,
			planSourceDisplay(r.Spec.PlanSource),
			stepCountDisplay(r),
			r.Spec.Confidence,
			r.Spec.PersonaRef.Name,
			formatAge(r.Metadata.CreationTimestamp),
		)
	}
	tbl.Render()
	fmt.Fprintln(w)
}

// planSourceDisplay renders the plan source column, using a placeholder for legacy
// objects that predate the ordered-plan schema.
func planSourceDisplay(planSource string) string {
	if planSource == "" {
		return "-"
	}
	return planSource
}

// stepCountDisplay renders the number of ordered steps, falling back to "-" for
// legacy single-Action objects that carry no Steps[] plan.
func stepCountDisplay(r remediationFull) string {
	if len(r.Spec.Steps) == 0 {
		return "-"
	}
	return fmt.Sprintf("%d", len(r.Spec.Steps))
}

// --- diff ---

func newRemediationDiffCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "diff <name>",
		Short: "Show remediation proposal diff",
		Long: `Display detailed information about a remediation proposal including the
proposed YAML change as a colored diff, explanation, and rollback config.

Examples:
  dorgu remediation diff fix-oom-api-server -n production
  dorgu remediation diff fix-oom-api-server -n production --json`,
		Args: cobra.ExactArgs(1),
		RunE: runRemediationDiff,
	}

	cmd.Flags().StringP("namespace", "n", "", "namespace of the remediation (required)")
	_ = cmd.MarkFlagRequired("namespace")
	cmd.Flags().String("kubeconfig", "", "path to kubeconfig (default: ~/.kube/config)")

	return cmd
}

func runRemediationDiff(cmd *cobra.Command, args []string) error {
	if _, err := exec.LookPath("kubectl"); err != nil {
		return fmt.Errorf("kubectl not found in PATH; required for remediation diff")
	}

	name := args[0]
	namespace, _ := cmd.Flags().GetString("namespace")
	kubeconfigFlag, _ := cmd.Flags().GetString("kubeconfig")

	kubeconfig, err := validateKubeconfig(kubeconfigFlag)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), remediationCmdTimeout)
	defer cancel()

	rem, err := fetchRemediation(ctx, kubeconfig, name, namespace)
	if err != nil {
		return err
	}

	if output.IsJSON() {
		return output.PrintJSON(rem)
	}

	printRemediationDiff(os.Stdout, rem)
	return nil
}

func fetchRemediation(ctx context.Context, kubeconfig, name, namespace string) (*remediationFull, error) {
	kcmd := kubectlCmd(ctx, kubeconfig, "get", "remediationaction", name,
		"-n", namespace, "-o", "json")
	rawOutput, err := kcmd.CombinedOutput()
	if err != nil {
		outputStr := kubectlErrText(rawOutput)
		if strings.Contains(outputStr, "the server doesn't have a resource type") {
			output.ErrorWithHint("RemediationAction CRD not found. Is the dorgu operator installed?",
				"To install the operator: dorgu cluster setup")
			return nil, errSilent
		}
		if strings.Contains(outputStr, "not found") {
			output.ErrorWithHint("RemediationAction not found: "+name,
				"List available remediations: dorgu remediation list -n "+namespace)
			return nil, errSilent
		}
		return nil, fmt.Errorf("failed to get remediation: %s", outputStr)
	}

	var rem remediationFull
	if err := json.Unmarshal(rawOutput, &rem); err != nil {
		return nil, fmt.Errorf("failed to parse remediation: %w", err)
	}
	return &rem, nil
}

func printRemediationDiff(w io.Writer, r *remediationFull) {
	fmt.Fprintln(w)

	// Header box
	personaDisplay := r.Spec.PersonaRef.Name
	if r.Spec.PersonaRef.Kind != "" {
		personaDisplay = r.Spec.PersonaRef.Kind + "/" + r.Spec.PersonaRef.Name
	}
	if r.Spec.PersonaRef.Namespace != "" {
		personaDisplay += " (" + r.Spec.PersonaRef.Namespace + ")"
	}

	fmt.Fprintf(w, "Remediation: %s\n", r.Metadata.Name)
	fmt.Fprintln(w, strings.Repeat("═", len([]rune("Remediation: "+r.Metadata.Name))))
	fmt.Fprintln(w)
	fmt.Fprintf(w, "Target:     %s\n", personaDisplay)
	fmt.Fprintf(w, "Type:       %s\n", r.Spec.Action.Type)
	fmt.Fprintf(w, "Confidence: %s\n", r.Spec.Confidence)
	if r.Spec.PlanSource != "" {
		fmt.Fprintf(w, "Plan:       %s\n", r.Spec.PlanSource)
	}
	fmt.Fprintf(w, "Phase:      %s\n", output.RemediationPhaseColor(r.Status.Phase))

	if r.Spec.IncidentRef.Name != "" {
		fmt.Fprintf(w, "Incident:   %s\n", r.Spec.IncidentRef.Name)
	}
	fmt.Fprintln(w)

	// Explanation. printRemediationPlan prints the plan summary below, so this
	// skips the explanation when the two say the same thing (F-15).
	if explanation := nonRedundantExplanation(r); explanation != "" {
		fmt.Fprintln(w, "Explanation:")
		for _, line := range strings.Split(explanation, "\n") {
			fmt.Fprintf(w, "  %s\n", line)
		}
		fmt.Fprintln(w)
	}

	// Proposed change: the ordered plan when present, else the legacy single Action.
	printRemediationPlan(w, r)

	// Rollback info
	if r.Spec.Rollback != nil {
		rb := r.Spec.Rollback
		fmt.Fprintln(w, "Rollback:")
		if rb.Enabled {
			if rb.HealthCheckAfter != "" {
				fmt.Fprintf(w, "  Automatic rollback if health degrades (verified after %s)\n", rb.HealthCheckAfter)
			} else {
				fmt.Fprintln(w, "  Automatic rollback if health degrades")
			}
			if rb.MaxRetries > 0 {
				fmt.Fprintf(w, "  Max retries: %d\n", rb.MaxRetries)
			}
		} else {
			fmt.Fprintln(w, "  Manual rollback required")
		}
		fmt.Fprintln(w)
	}

	printRemediationActions(w, r)
}

// printRemediationActions prints what the reader can do next.
//
// A plan with no auto-applicable step is not something to approve and heal. The
// CLI already knows it cannot apply anything (it prints "No auto-applicable
// resource change" a moment later), yet it used to offer the approve command
// anyway, which is what walked the clean-room tester into a Failed remediation
// and a 30-minute cooldown on the app (F-03).
func printRemediationActions(w io.Writer, r *remediationFull) {
	if r.Status.Phase != "Pending" {
		return
	}

	if !hasAutoApplicableChange(r) {
		fmt.Fprintln(w, "This plan is advisory: nothing in it can be applied for you.")
		fmt.Fprintln(w, "Carry out the steps above yourself. Nothing changes in the cluster until you do.")
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Actions:")
		fmt.Fprintf(w, "  dorgu remediation reject %s -n %s --reason \"...\"\n",
			r.Metadata.Name, r.Metadata.Namespace)
		fmt.Fprintln(w)
		return
	}

	fmt.Fprintln(w, "Actions:")
	fmt.Fprintf(w, "  dorgu remediation approve %s -n %s\n",
		r.Metadata.Name, r.Metadata.Namespace)
	fmt.Fprintf(w, "  dorgu remediation reject %s -n %s --reason \"...\"\n",
		r.Metadata.Name, r.Metadata.Namespace)
	fmt.Fprintln(w)
}

// hasAutoApplicableChange reports whether this remediation carries a resource
// change the CLI can apply to the workload. An unreadable patch counts as not
// applicable: if it cannot be interpreted, it cannot be applied.
func hasAutoApplicableChange(r *remediationFull) bool {
	plan, err := buildHealPlan(r)
	if err != nil || plan == nil {
		return false
	}
	return !plan.Change.isEmpty()
}

// printRemediationPlan renders the proposed change. When the ordered Steps[] plan
// is present it is the plan of record: print the plan summary then each step in
// order with a per-step colored diff. For legacy objects with only a single Action,
// fall back to a single proposed-change diff.
func printRemediationPlan(w io.Writer, r *remediationFull) {
	if len(r.Spec.Steps) == 0 {
		// Legacy single-Action object.
		pre := rawJSONToString(r.Spec.Action.PrePatchState)
		patch := rawJSONToString(r.Spec.Action.Patch)
		if pre != "" || patch != "" {
			fmt.Fprintln(w, "Proposed change:")
			output.RenderDiff(w, pre, patch, 3)
			fmt.Fprintln(w)
		}
		return
	}

	if planSummaryIsPrinted(r) {
		fmt.Fprintln(w, "Plan summary:")
		for _, line := range strings.Split(r.Spec.PlanSummary, "\n") {
			fmt.Fprintf(w, "  %s\n", line)
		}
		fmt.Fprintln(w)
	}

	steps := sortedSteps(r.Spec.Steps)
	fmt.Fprintf(w, "Plan (%d steps):\n", len(steps))
	fmt.Fprintln(w)
	for _, s := range steps {
		mode := "advisory"
		if s.AutoExecutable {
			mode = "auto"
		}
		risk := s.Risk
		if risk == "" {
			risk = "unknown"
		}
		fmt.Fprintf(w, "  [%d] %s (%s; %s): %s\n", s.Order, s.Type, risk, mode, s.Description)
		if s.Rationale != "" {
			for _, line := range strings.Split(s.Rationale, "\n") {
				fmt.Fprintf(w, "      %s\n", line)
			}
		}
		if cmd := displayableStepCommand(s.Command); cmd != "" {
			fmt.Fprintf(w, "      Run: %s\n", cmd)
		}
		pre := rawJSONToString(s.PrePatchState)
		patch := rawJSONToString(s.Patch)
		if pre != "" || patch != "" {
			output.RenderDiff(w, pre, patch, 3)
		}
		fmt.Fprintln(w)
	}
}

// planSummaryIsPrinted reports whether printRemediationPlan will render a
// "Plan summary:" block, so the explanation above it can avoid repeating it.
func planSummaryIsPrinted(r *remediationFull) bool {
	return len(r.Spec.Steps) > 0 && strings.TrimSpace(r.Spec.PlanSummary) != ""
}

// nonRedundantExplanation returns the explanation to print, or "" when the plan
// summary below already says the same thing.
//
// F-15: the operator used to write the root cause into PlanSummary and the same
// sentence with a prefix into Explanation, so `remediation diff` printed one
// paragraph twice under two headings. The operator no longer does that, but
// RemediationActions created by older operators are still in clusters, so the
// renderer has to cope with them.
func nonRedundantExplanation(r *remediationFull) string {
	explanation := strings.TrimSpace(r.Spec.Explanation)
	if explanation == "" || !planSummaryIsPrinted(r) {
		return explanation
	}

	summary := normalizeProse(r.Spec.PlanSummary)
	body := normalizeProse(explanation)
	if strings.Contains(body, summary) || strings.Contains(summary, body) {
		return ""
	}
	return explanation
}

// normalizeProse collapses case and whitespace so two renderings of the same
// sentence compare equal.
func normalizeProse(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

// displayableStepCommand returns a step's suggested command if it is safe to
// print as something a reader will paste into a shell, and "" otherwise.
//
// The operator sanitizes the command before persisting it, but this CLI reads
// RemediationActions straight out of the cluster: an older operator, a
// hand-written object, or anything with permission to create the CRD could put
// arbitrary text here. A guard the user's shell depends on belongs on the side
// that does the printing, so the same rules are enforced again.
//
// Nothing here executes the command. It is printed for a human to read and run.
func displayableStepCommand(cmd string) string {
	trimmed := strings.TrimSpace(cmd)
	if trimmed == "" || len(trimmed) > maxStepCommandLength {
		return ""
	}
	if !strings.HasPrefix(trimmed, "kubectl ") {
		return ""
	}
	if strings.ContainsAny(trimmed, stepCommandForbidden) {
		return ""
	}
	return trimmed
}

// sortedSteps returns a new slice of steps ordered by Order (ascending), without
// mutating the input.
func sortedSteps(steps []remediationStep) []remediationStep {
	out := make([]remediationStep, len(steps))
	copy(out, steps)
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Order < out[j].Order
	})
	return out
}

// rawJSONToString converts a raw JSON value (the operator's apiextensionsv1.JSON)
// into a stable, human-readable YAML string for diffing. Empty input yields an
// empty string; unparseable input falls back to the raw bytes.
func rawJSONToString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var v interface{}
	if err := json.Unmarshal(raw, &v); err != nil {
		return string(raw)
	}
	out, err := yaml.Marshal(v)
	if err != nil {
		return string(raw)
	}
	return string(out)
}

// --- approve ---

func newRemediationApproveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "approve [name]",
		Short: "Approve a pending remediation",
		Long: `Approve a RemediationAction, transitioning it from Pending to Approved.
The operator will then apply the proposed patch and monitor health.

Use --next to approve the oldest pending remediation without naming it.

After approval the CLI heals the running workload: it translates the approved
persona-update resource change (memory/CPU limits/requests) into an equivalent
strategic-merge patch on the matching Deployment and applies it with your
credentials, so the pod recovers. The operator stays advisory and never writes
workloads. Use --no-heal to only patch the RemediationAction status.

Examples:
  dorgu remediation approve fix-oom-api-server -n production
  dorgu remediation approve --next -n production
  dorgu remediation approve fix-oom-api-server -n production --no-heal
  dorgu remediation approve fix-oom-api-server -n production --yes
  dorgu remediation approve fix-oom-api-server -n production --workload api --container app`,
		RunE: runRemediationApprove,
	}

	cmd.Flags().StringP("namespace", "n", "", "namespace of the remediation")
	cmd.Flags().String("reason", "", "optional approval reason")
	cmd.Flags().Bool("next", false, "approve the oldest pending remediation")
	cmd.Flags().Bool("heal", true, "after approval, apply the resource change to the workload")
	cmd.Flags().Bool("no-heal", false, "skip the workload heal; only patch the RemediationAction status")
	cmd.Flags().String("workload", "", "explicit Deployment name (overrides label discovery)")
	cmd.Flags().String("container", "", "explicit container name to patch")
	cmd.Flags().Bool("yes", false, "skip the heal confirmation prompt")
	cmd.Flags().String("kubeconfig", "", "path to kubeconfig (default: ~/.kube/config)")
	cmd.MarkFlagsMutuallyExclusive("heal", "no-heal")

	return cmd
}

func runRemediationApprove(cmd *cobra.Command, args []string) error {
	if _, err := exec.LookPath("kubectl"); err != nil {
		return fmt.Errorf("kubectl not found in PATH; required for remediation approve")
	}

	next, _ := cmd.Flags().GetBool("next")
	namespace, _ := cmd.Flags().GetString("namespace")
	reason, _ := cmd.Flags().GetString("reason")
	healFlag, _ := cmd.Flags().GetBool("heal")
	noHealFlag, _ := cmd.Flags().GetBool("no-heal")
	workloadFlag, _ := cmd.Flags().GetString("workload")
	containerFlag, _ := cmd.Flags().GetString("container")
	assumeYes, _ := cmd.Flags().GetBool("yes")
	kubeconfigFlag, _ := cmd.Flags().GetString("kubeconfig")

	kubeconfig, err := validateKubeconfig(kubeconfigFlag)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), remediationCmdTimeout)
	defer cancel()

	var name string
	if next {
		name, namespace, err = findNextPendingRemediation(ctx, kubeconfig, namespace)
		if err != nil {
			return err
		}
		output.Info(fmt.Sprintf("Selected: %s/%s", namespace, name))
	} else {
		if len(args) == 0 {
			return fmt.Errorf("remediation name required (or use --next)")
		}
		name = args[0]
		if namespace == "" {
			return fmt.Errorf("--namespace is required")
		}
	}

	// Verify it's in Pending phase.
	rem, err := fetchRemediation(ctx, kubeconfig, name, namespace)
	if err != nil {
		return err
	}
	if rem.Status.Phase != "Pending" {
		output.Error(fmt.Sprintf("Remediation is in %s phase, can only approve Pending remediations",
			rem.Status.Phase))
		return errSilent
	}

	heal := healFlag && !noHealFlag
	opts := healOptions{
		kubeconfig: kubeconfig,
		workload:   workloadFlag,
		container:  containerFlag,
		assumeYes:  assumeYes,
	}

	// Preflight the workload change BEFORE approving anything. Approval is what
	// tells the operator to patch the persona and start the verification clock,
	// so approving first and discovering afterwards that the Deployment cannot be
	// found leaves the persona at the new limits, the workload at the old ones,
	// and a 10-minute Applying/Verifying window over a change that never landed.
	// Nothing is written until we know the change can be applied.
	var exec *healExecution
	if heal {
		exec, err = planHeal(ctx, rem, opts)
		if err != nil {
			output.Error(fmt.Sprintf("Cannot apply this remediation to the workload: %v", err))
			output.Info("Nothing was approved and nothing was changed.")
			output.Info("Name the workload with --workload <deployment>, or approve without healing " +
				"using --no-heal if you will apply the change yourself.")
			return errSilent
		}
	}

	// Patch status to Approved.
	now := time.Now().UTC().Format(time.RFC3339)
	patch := fmt.Sprintf(`{"status":{"phase":"Approved","approvedBy":"cli-user","approvedAt":"%s"}}`, now)

	patchArgs := []string{"patch", "remediationaction", name, "-n", namespace,
		"--type", "merge", "--subresource", "status", "-p", patch}
	patchOut, err := kubectlCmd(ctx, kubeconfig, patchArgs...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to approve remediation: %s", kubectlErrText(patchOut))
	}

	// Say what approval actually does for this plan. An advisory plan changes
	// nothing: the operator records the decision and marks it Acknowledged.
	msg := "Remediation approved. The operator will update the persona and monitor health."
	if !hasAutoApplicableChange(rem) {
		msg = "Approval recorded. This plan is advisory, so nothing was applied; " +
			"the operator will mark it Acknowledged."
	}
	if reason != "" {
		msg += fmt.Sprintf(" Reason: %s", reason)
	}
	output.Success(msg)

	// Heal the workload (default on). The operator only patches the persona spec;
	// the CLI applies the equivalent workload change so the pod actually recovers.
	if !heal {
		output.Info("Skipping workload heal (--no-heal). Apply the resource change yourself to recover the pod.")
		output.Warn("Until you do, the persona and the running workload disagree.")
		return nil
	}

	out := cmd.OutOrStdout()
	printHealPreamble(out, exec.Context, exec.Advisory)

	if !exec.hasWorkloadChange() {
		output.Info("No resource change to apply: the steps above are for you to carry out.")
		return nil
	}

	if err := executeHeal(ctx, exec, opts, cmd.InOrStdin(), out); err != nil {
		output.Error(fmt.Sprintf("Workload heal failed: %v", err))
		output.Warn(fmt.Sprintf(
			"The remediation is Approved but %s/%s was NOT patched; the persona and the workload now disagree.",
			exec.Namespace, exec.Deployment))
		output.Info("Re-run the heal with: dorgu remediation heal " + name + " -n " + namespace)
		return errSilent
	}
	return nil
}

// findNextPendingRemediation returns the oldest pending remediation.
//
// It used to rank by severity, but RemediationAction carries no severity field —
// every candidate tied at rank 0 and the "highest-severity" pick was really just
// whichever object the API server listed first. Oldest-first is the honest
// ordering: the longest-waiting incident goes next, and ties break on
// namespace/name so repeated runs choose the same action.
func findNextPendingRemediation(ctx context.Context, kubeconfig, namespace string) (string, string, error) {
	remediations, err := fetchRemediations(ctx, kubeconfig, namespace)
	if err != nil {
		return "", "", err
	}

	var pending []remediationFull
	for _, r := range remediations {
		if r.Status.Phase == "Pending" {
			pending = append(pending, r)
		}
	}

	if len(pending) == 0 {
		output.Info("No pending remediations found.")
		return "", "", errSilent
	}

	sort.SliceStable(pending, func(i, j int) bool {
		return pendingOrderKey(pending[i]) < pendingOrderKey(pending[j])
	})

	chosen := pending[0]
	return chosen.Metadata.Name, chosen.Metadata.Namespace, nil
}

// pendingOrderKey builds the sort key for --next: creation time first, then
// namespace/name as a stable tie-break. Unparseable or missing timestamps sort
// last so a malformed object never hijacks the pick.
func pendingOrderKey(r remediationFull) string {
	created := "9999-12-31T23:59:59Z"
	if t, err := time.Parse(time.RFC3339, r.Metadata.CreationTimestamp); err == nil {
		created = t.UTC().Format(time.RFC3339)
	}
	return created + "/" + r.Metadata.Namespace + "/" + r.Metadata.Name
}

// --- reject ---

func newRemediationRejectCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reject <name>",
		Short: "Reject a remediation proposal",
		Long: `Reject a RemediationAction, transitioning it to Rejected phase.
Can reject remediations in Pending or Approved phase.

Examples:
  dorgu remediation reject fix-oom-api-server -n production
  dorgu remediation reject fix-oom-api-server -n production --reason "will handle manually"`,
		Args: cobra.ExactArgs(1),
		RunE: runRemediationReject,
	}

	cmd.Flags().StringP("namespace", "n", "", "namespace of the remediation (required)")
	_ = cmd.MarkFlagRequired("namespace")
	cmd.Flags().String("reason", "", "rejection reason (optional but recommended)")
	cmd.Flags().String("kubeconfig", "", "path to kubeconfig (default: ~/.kube/config)")

	return cmd
}

func runRemediationReject(cmd *cobra.Command, args []string) error {
	if _, err := exec.LookPath("kubectl"); err != nil {
		return fmt.Errorf("kubectl not found in PATH; required for remediation reject")
	}

	name := args[0]
	namespace, _ := cmd.Flags().GetString("namespace")
	reason, _ := cmd.Flags().GetString("reason")
	kubeconfigFlag, _ := cmd.Flags().GetString("kubeconfig")

	kubeconfig, err := validateKubeconfig(kubeconfigFlag)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), remediationCmdTimeout)
	defer cancel()

	// Verify phase allows rejection.
	rem, err := fetchRemediation(ctx, kubeconfig, name, namespace)
	if err != nil {
		return err
	}
	if rem.Status.Phase != "Pending" && rem.Status.Phase != "Approved" {
		output.Error(fmt.Sprintf("Remediation is in %s phase, can only reject Pending or Approved remediations",
			rem.Status.Phase))
		return errSilent
	}

	// Patch status to Rejected.
	patch := `{"status":{"phase":"Rejected"}}`
	patchArgs := []string{"patch", "remediationaction", name, "-n", namespace,
		"--type", "merge", "--subresource", "status", "-p", patch}
	patchOut, err := kubectlCmd(ctx, kubeconfig, patchArgs...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to reject remediation: %s", kubectlErrText(patchOut))
	}

	msg := "Remediation rejected."
	if reason != "" {
		msg += fmt.Sprintf(" Reason: %s", reason)
	}
	output.Success(msg)
	return nil
}

// --- heal ---

func newRemediationHealCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "heal <name>",
		Short: "Apply an approved remediation's resource change to the workload",
		Long: `Translate a RemediationAction's approved persona-update resource change
(memory/CPU limits/requests) into an equivalent strategic-merge patch on the
matching Deployment and apply it with your credentials, so the pod recovers.

This is run automatically by 'approve' (unless --no-heal); use it to re-run the
workload sync on its own (e.g. after --no-heal, or if a previous heal failed).
The operator never writes workloads — this is the CLI (your tool) closing the
loop. Non-resource steps (restart/scale/manual) are printed as advisory, not
executed.

Examples:
  dorgu remediation heal fix-oom-api-server -n production
  dorgu remediation heal fix-oom-api-server -n production --yes
  dorgu remediation heal fix-oom-api-server -n production --workload api --container app`,
		Args: cobra.ExactArgs(1),
		RunE: runRemediationHeal,
	}

	cmd.Flags().StringP("namespace", "n", "", "namespace of the remediation (required)")
	_ = cmd.MarkFlagRequired("namespace")
	cmd.Flags().String("workload", "", "explicit Deployment name (overrides label discovery)")
	cmd.Flags().String("container", "", "explicit container name to patch")
	cmd.Flags().Bool("yes", false, "skip the heal confirmation prompt")
	cmd.Flags().String("kubeconfig", "", "path to kubeconfig (default: ~/.kube/config)")

	return cmd
}

func runRemediationHeal(cmd *cobra.Command, args []string) error {
	if _, err := exec.LookPath("kubectl"); err != nil {
		return fmt.Errorf("kubectl not found in PATH; required for remediation heal")
	}

	name := args[0]
	namespace, _ := cmd.Flags().GetString("namespace")
	workloadFlag, _ := cmd.Flags().GetString("workload")
	containerFlag, _ := cmd.Flags().GetString("container")
	assumeYes, _ := cmd.Flags().GetBool("yes")
	kubeconfigFlag, _ := cmd.Flags().GetString("kubeconfig")

	kubeconfig, err := validateKubeconfig(kubeconfigFlag)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), remediationCmdTimeout)
	defer cancel()

	rem, err := fetchRemediation(ctx, kubeconfig, name, namespace)
	if err != nil {
		return err
	}
	// Preserve the approve/reject safety gate: never heal a remediation a human has
	// explicitly rejected or that the operator has failed/expired/rolled back.
	switch rem.Status.Phase {
	case "Rejected", "Failed", "Expired", "RolledBack":
		output.Error(fmt.Sprintf("Remediation is in %s phase; refusing to heal a %s remediation.",
			rem.Status.Phase, rem.Status.Phase))
		return errSilent
	case "Approved", "Applying", "Verifying", "Completed":
		// Expected: heal (or re-heal) an approved remediation.
	default:
		// Pending/unset: allow but warn — running heal directly is an implicit approval.
		output.Warn(fmt.Sprintf("Remediation is in %s phase; healing anyway. Approve it first to record intent.",
			rem.Status.Phase))
	}

	opts := healOptions{
		kubeconfig: kubeconfig,
		workload:   workloadFlag,
		container:  containerFlag,
		assumeYes:  assumeYes,
	}
	if err := healWorkload(ctx, rem, opts, cmd.InOrStdin(), cmd.OutOrStdout()); err != nil {
		output.Error(fmt.Sprintf("Workload heal failed: %v", err))
		return errSilent
	}
	return nil
}
