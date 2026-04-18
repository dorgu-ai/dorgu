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

	"github.com/dorgu-ai/dorgu/internal/output"
)

// remediationCmdTimeout is the maximum time to wait for each kubectl call.
const remediationCmdTimeout = 30 * time.Second

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
	}
	cmd.AddCommand(
		newRemediationListCmd(),
		newRemediationDiffCmd(),
		newRemediationApproveCmd(),
		newRemediationRejectCmd(),
	)
	return cmd
}

// remediationFull is used for JSON parsing of RemediationAction resources.
type remediationFull struct {
	Metadata struct {
		Name              string `json:"name"`
		Namespace         string `json:"namespace"`
		CreationTimestamp string `json:"creationTimestamp"`
	} `json:"metadata"`
	Spec struct {
		ActionType  string `json:"actionType"`
		Severity    string `json:"severity"`
		Confidence  string `json:"confidence"`
		Explanation string `json:"explanation"`
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
			Patch         string `json:"patch"`
			PrePatchState string `json:"prePatchState"`
		} `json:"action"`
		Rollback *struct {
			Automatic      bool   `json:"automatic"`
			TimeoutMinutes int    `json:"timeoutMinutes"`
			Condition      string `json:"condition"`
		} `json:"rollback"`
	} `json:"spec"`
	Status struct {
		Phase      string `json:"phase"`
		ApprovedBy string `json:"approvedBy"`
		ApprovedAt string `json:"approvedAt"`
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

Examples:
  dorgu remediation list
  dorgu remediation list --phase Pending
  dorgu remediation list -n production --severity critical
  dorgu remediation list --all --limit 100`,
		RunE: runRemediationList,
	}

	cmd.Flags().StringP("namespace", "n", "", "filter by namespace (default: all)")
	cmd.Flags().String("phase", "", "filter by phase (Pending, Approved, Applying, Verifying, Completed, etc.)")
	cmd.Flags().String("severity", "", "filter by severity (info, warning, critical)")
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
	severity, _ := cmd.Flags().GetString("severity")
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
		if severity != "" && r.Spec.Severity != severity {
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
		outputStr := strings.TrimSpace(string(out))
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

	tbl := output.NewTable(w, "NAMESPACE", "NAME", "PHASE", "TYPE", "SEVERITY", "CONFIDENCE", "PERSONA", "AGE")
	for _, r := range remediations {
		tbl.AddRow(
			r.Metadata.Namespace,
			r.Metadata.Name,
			output.RemediationPhaseColor(r.Status.Phase),
			r.Spec.ActionType,
			output.SeverityColor(r.Spec.Severity),
			r.Spec.Confidence,
			r.Spec.PersonaRef.Name,
			formatAge(r.Metadata.CreationTimestamp),
		)
	}
	tbl.Render()
	fmt.Fprintln(w)
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
		outputStr := strings.TrimSpace(string(rawOutput))
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
	fmt.Fprintf(w, "Severity:   %s\n", output.SeverityColor(r.Spec.Severity))
	fmt.Fprintf(w, "Type:       %s\n", r.Spec.ActionType)
	fmt.Fprintf(w, "Confidence: %s\n", r.Spec.Confidence)
	fmt.Fprintf(w, "Phase:      %s\n", output.RemediationPhaseColor(r.Status.Phase))

	if r.Spec.IncidentRef.Name != "" {
		fmt.Fprintf(w, "Incident:   %s\n", r.Spec.IncidentRef.Name)
	}
	fmt.Fprintln(w)

	// Explanation
	if r.Spec.Explanation != "" {
		fmt.Fprintln(w, "Explanation:")
		for _, line := range strings.Split(r.Spec.Explanation, "\n") {
			fmt.Fprintf(w, "  %s\n", line)
		}
		fmt.Fprintln(w)
	}

	// Proposed change diff
	if r.Spec.Action.PrePatchState != "" || r.Spec.Action.Patch != "" {
		fmt.Fprintln(w, "Proposed change:")
		output.RenderDiff(w, r.Spec.Action.PrePatchState, r.Spec.Action.Patch, 3)
		fmt.Fprintln(w)
	}

	// Rollback info
	if r.Spec.Rollback != nil {
		rb := r.Spec.Rollback
		fmt.Fprintln(w, "Rollback:")
		if rb.Automatic {
			timeout := rb.TimeoutMinutes
			if timeout == 0 {
				timeout = 10
			}
			fmt.Fprintf(w, "  Automatic after %dm if health degrades\n", timeout)
		} else {
			fmt.Fprintln(w, "  Manual rollback required")
		}
		if rb.Condition != "" {
			fmt.Fprintf(w, "  Condition: %s\n", rb.Condition)
		}
		fmt.Fprintln(w)
	}

	// Action hints
	if r.Status.Phase == "Pending" {
		fmt.Fprintln(w, "Actions:")
		fmt.Fprintf(w, "  dorgu remediation approve %s -n %s\n",
			r.Metadata.Name, r.Metadata.Namespace)
		fmt.Fprintf(w, "  dorgu remediation reject %s -n %s --reason \"...\"\n",
			r.Metadata.Name, r.Metadata.Namespace)
		fmt.Fprintln(w)
	}
}

// --- approve ---

func newRemediationApproveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "approve [name]",
		Short: "Approve a pending remediation",
		Long: `Approve a RemediationAction, transitioning it from Pending to Approved.
The operator will then apply the proposed patch and monitor health.

Use --next to automatically approve the highest-severity pending remediation.

Examples:
  dorgu remediation approve fix-oom-api-server -n production
  dorgu remediation approve --next -n production
  dorgu remediation approve fix-oom-api-server -n production --reason "verified safe"`,
		RunE: runRemediationApprove,
	}

	cmd.Flags().StringP("namespace", "n", "", "namespace of the remediation")
	cmd.Flags().String("reason", "", "optional approval reason")
	cmd.Flags().Bool("next", false, "approve the highest-severity pending remediation")
	cmd.Flags().String("kubeconfig", "", "path to kubeconfig (default: ~/.kube/config)")

	return cmd
}

func runRemediationApprove(cmd *cobra.Command, args []string) error {
	if _, err := exec.LookPath("kubectl"); err != nil {
		return fmt.Errorf("kubectl not found in PATH; required for remediation approve")
	}

	next, _ := cmd.Flags().GetBool("next")
	namespace, _ := cmd.Flags().GetString("namespace")
	reason, _ := cmd.Flags().GetString("reason")
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

	// Patch status to Approved.
	now := time.Now().UTC().Format(time.RFC3339)
	patch := fmt.Sprintf(`{"status":{"phase":"Approved","approvedBy":"cli-user","approvedAt":"%s"}}`, now)

	patchArgs := []string{"patch", "remediationaction", name, "-n", namespace,
		"--type", "merge", "--subresource", "status", "-p", patch}
	patchOut, err := kubectlCmd(ctx, kubeconfig, patchArgs...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to approve remediation: %s", strings.TrimSpace(string(patchOut)))
	}

	msg := "Remediation approved. The operator will apply the patch and monitor health."
	if reason != "" {
		msg += fmt.Sprintf(" Reason: %s", reason)
	}
	output.Success(msg)
	return nil
}

// findNextPendingRemediation finds the highest-severity pending remediation.
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

	sort.Slice(pending, func(i, j int) bool {
		return severityRank(pending[i].Spec.Severity) > severityRank(pending[j].Spec.Severity)
	})

	chosen := pending[0]
	return chosen.Metadata.Name, chosen.Metadata.Namespace, nil
}

// severityRank returns a numeric rank for sorting severities (higher = more severe).
func severityRank(severity string) int {
	switch strings.ToLower(severity) {
	case "critical":
		return 3
	case "warning":
		return 2
	case "info":
		return 1
	default:
		return 0
	}
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
		return fmt.Errorf("failed to reject remediation: %s", strings.TrimSpace(string(patchOut)))
	}

	msg := "Remediation rejected."
	if reason != "" {
		msg += fmt.Sprintf(" Reason: %s", reason)
	}
	output.Success(msg)
	return nil
}
