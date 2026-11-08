package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/dorgu-ai/dorgu/internal/output"
)

// Task 1.13: why a proposal was declined is data. KPI Q2 (proposal precision) is
// the share of decided proposals a human called wrong, and it can only be read if
// the rejection says which kind it was.
//
// The reason is persisted as a RemediationAction status condition, not a CRD field:
// `RejectionReason`, status True, reason = the CamelCase of the enum, message = the
// optional note. Nothing reads the note back; the reason is the enum. It is written
// in the same patch that sets the phase, so a rejection never exists without it.

// conditionRejectionReason is the status condition the reason is recorded on.
const conditionRejectionReason = "RejectionReason"

// maxRejectionNote bounds the note stored on the condition, in characters.
const maxRejectionNote = 256

// rejectionReason is one value of `reject --reason` and the condition reason it is
// stored as (condition reasons are CamelCase tokens, so the flag's dashes become
// capitals).
type rejectionReason struct {
	flag      string
	condition string
	meaning   string
}

// rejectionReasons is the enum, in the order the help lists it.
var rejectionReasons = []rejectionReason{
	{"wrong-diagnosis", "WrongDiagnosis", "Dorgu misread what is wrong"},
	{"wrong-action", "WrongAction", "the diagnosis is right but the proposed fix is not"},
	{"too-risky", "TooRisky", "the change is right but not safe to make here"},
	{"duplicate", "Duplicate", "the same fix is already proposed or applied"},
	{"not-needed", "NotNeeded", "there is nothing to fix, or it fixed itself"},
	{"other", "Other", "none of the above (requires --note)"},
}

// rejectionReasonFlags lists the valid --reason values for messages.
func rejectionReasonFlags() string {
	flags := make([]string, len(rejectionReasons))
	for i, r := range rejectionReasons {
		flags[i] = r.flag
	}
	return strings.Join(flags, ", ")
}

// parseRejectionReason validates a --reason value against the enum.
func parseRejectionReason(s string) (rejectionReason, error) {
	for _, r := range rejectionReasons {
		if r.flag == s {
			return r, nil
		}
	}
	return rejectionReason{}, fmt.Errorf("invalid --reason %q: valid values are %s "+
		"(free text goes in --note)", s, rejectionReasonFlags())
}

// conditionReasonFor maps a flag value to the condition reason, or "".
func conditionReasonFor(flag string) string {
	if r, err := parseRejectionReason(flag); err == nil {
		return r.condition
	}
	return ""
}

// rejectionFlagFor maps a condition reason back to the flag value, or "".
func rejectionFlagFor(condition string) string {
	for _, r := range rejectionReasons {
		if r.condition == condition {
			return r.flag
		}
	}
	return ""
}

func newRemediationRejectCmd() *cobra.Command {
	var help strings.Builder
	for _, r := range rejectionReasons {
		fmt.Fprintf(&help, "  %-16s %s\n", r.flag, r.meaning)
	}
	cmd := &cobra.Command{
		Use:   "reject <name>",
		Short: "Reject a remediation proposal",
		Long: `Reject a RemediationAction, transitioning it to Rejected phase.
Can reject remediations in Pending or Approved phase.

--reason says why, from a fixed list, and is recorded on the RemediationAction as a
RejectionReason condition. ` + "`dorgu report`" + ` counts rejections by it (KPI Q2: the share
of decided proposals that were wrong). Add a sentence with --note; --reason other
requires one. Without --reason the proposal is still rejected, and the report counts it
as "no reason given".

Reasons:
` + help.String() + `
Examples:
  dorgu remediation reject fix-oom-api-server -n production --reason wrong-diagnosis
  dorgu remediation reject fix-oom-api-server -n production --reason too-risky --note "peak traffic"
  dorgu remediation reject fix-oom-api-server -n production --reason other --note "will handle manually"`,
		Args: cobra.ExactArgs(1),
		RunE: runRemediationReject,
	}

	cmd.Flags().StringP("namespace", "n", "", "namespace of the remediation (required)")
	_ = cmd.MarkFlagRequired("namespace")
	cmd.Flags().String("reason", "", "why it was rejected: "+rejectionReasonFlags()+" (recorded; recommended)")
	cmd.Flags().String("note", "", "a sentence of context, stored with the reason (required for --reason other)")
	cmd.Flags().String("kubeconfig", "", kubeconfigFlagUsage)

	return cmd
}

func runRemediationReject(cmd *cobra.Command, args []string) error {
	name := args[0]
	namespace, _ := cmd.Flags().GetString("namespace")
	reasonFlag, _ := cmd.Flags().GetString("reason")
	note, _ := cmd.Flags().GetString("note")
	kubeconfigFlag, _ := cmd.Flags().GetString("kubeconfig")

	// Usage errors first, before anything touches the cluster.
	var reason *rejectionReason
	if reasonFlag != "" {
		parsed, err := parseRejectionReason(reasonFlag)
		if err != nil {
			return err
		}
		if parsed.flag == "other" && strings.TrimSpace(note) == "" {
			return fmt.Errorf("--reason other needs a --note saying what the reason is")
		}
		reason = &parsed
	} else if strings.TrimSpace(note) != "" {
		return fmt.Errorf("--note goes with a --reason (%s)", rejectionReasonFlags())
	}

	if _, err := exec.LookPath("kubectl"); err != nil {
		return fmt.Errorf("kubectl not found in PATH; required for remediation reject")
	}
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

	if err := writeRejection(ctx, kubeconfig, namespace, name, reason, note); err != nil {
		return fmt.Errorf("failed to reject remediation: %w", err)
	}

	msg := "Remediation rejected."
	if reason != nil {
		msg += " Reason: " + reason.flag
		if n := strings.TrimSpace(note); n != "" {
			msg += fmt.Sprintf(" (%s)", truncateRunes(n, maxRejectionNote))
		}
	}
	output.Success(msg)
	if reason == nil {
		output.Info("No reason was recorded. Next time, --reason " + rejectionReasonFlags() +
			" lets `dorgu report` tell a wrong proposal from a mistimed one.")
	}
	return nil
}

// writeRejection sets the phase to Rejected and, when there is a reason, records
// it in the same patch.
//
// With no reason it is the single-field status patch it always was. With one it
// has to send the whole conditions list (a merge patch replaces a list), so it
// reads the status first, sends every condition it is not touching back untouched,
// and retries on a conflict, the same way the heal record does.
func writeRejection(
	ctx context.Context, kubeconfig, namespace, name string, reason *rejectionReason, note string,
) error {
	if reason == nil {
		return patchRemediationStatus(ctx, kubeconfig, namespace, name, `{"status":{"phase":"Rejected"}}`)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	for attempt := 0; attempt < recordAttempts; attempt++ {
		st, err := readRecordState(ctx, kubeconfig, namespace, name)
		if err != nil {
			return err
		}
		patch, err := buildRejectPatch(st, *reason, note, now)
		if err != nil {
			return err
		}
		if err := patchRemediationStatus(ctx, kubeconfig, namespace, name, patch); err != nil {
			if isConflictError(err) {
				continue
			}
			return err
		}
		return nil
	}
	return fmt.Errorf("the remediation kept changing under the write (%d attempts)", recordAttempts)
}

// buildRejectPatch builds the status merge patch: phase Rejected plus the
// RejectionReason condition, with every other condition returned byte for byte.
func buildRejectPatch(st recordState, reason rejectionReason, note, now string) (string, error) {
	marker, err := json.Marshal(map[string]any{
		"type":               conditionRejectionReason,
		"status":             "True",
		"reason":             reason.condition,
		"message":            truncateNote(note),
		"lastTransitionTime": now,
	})
	if err != nil {
		return "", fmt.Errorf("marshal %s condition: %w", conditionRejectionReason, err)
	}

	// A prior reason is replaced, not duplicated: two conditions of one type is an
	// object the API server rejects.
	conditions := make([]json.RawMessage, 0, len(st.Conditions)+1)
	for _, c := range st.Conditions {
		if c.Type != conditionRejectionReason {
			conditions = append(conditions, c.Raw)
		}
	}
	conditions = append(conditions, marker)

	patch := map[string]any{"status": map[string]any{"phase": "Rejected", "conditions": conditions}}
	if st.ResourceVersion != "" {
		patch["metadata"] = map[string]any{"resourceVersion": st.ResourceVersion}
	}
	b, err := json.Marshal(patch)
	if err != nil {
		return "", fmt.Errorf("marshal status patch: %w", err)
	}
	return string(b), nil
}

// truncateNote trims a note to the condition's bound, on a character boundary.
func truncateNote(note string) string {
	note = strings.TrimSpace(note)
	if utf8.RuneCountInString(note) <= maxRejectionNote {
		return note
	}
	return string([]rune(note)[:maxRejectionNote])
}
