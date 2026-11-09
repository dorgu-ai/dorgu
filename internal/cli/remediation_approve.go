package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/dorgu-ai/dorgu/internal/output"
)

// approveSettle is how long `approve` waits after writing the approval before it
// re-reads the RemediationAction. The operator can answer an approval by
// expiring it (the Pending TTL ran out) or superseding it (a worse incident on
// the same workload replaced it) in the very next reconcile; reporting "approved"
// before that is a claim the operator may already have withdrawn (R2-3). A
// variable so tests do not wait for it.
var approveSettle = 1500 * time.Millisecond

// phasesWhereTheApprovalStands are the phases a RemediationAction is in when the
// operator has accepted an approval: still Approved, ticketed or being applied,
// or already past that. Acknowledged is the advisory plan's answer.
var phasesWhereTheApprovalStands = map[string]bool{
	"Approved": true, "Ticketed": true, "Applying": true, "Verifying": true,
	"Completed": true, "Acknowledged": true,
}

// buildApprovePatch is the status merge patch that records the approval. The
// resourceVersion goes in as a precondition, so the API server refuses it if
// anything wrote to the action since the CLI read it (an expiry, a supersede, a
// teammate's decision). An empty version sends no precondition; a live API server
// always has one.
func buildApprovePatch(resourceVersion, now string) (string, error) {
	patch := map[string]any{"status": map[string]any{
		"phase": "Approved", "approvedBy": "cli-user", "approvedAt": now,
	}}
	if resourceVersion != "" {
		patch["metadata"] = map[string]any{"resourceVersion": resourceVersion}
	}
	b, err := json.Marshal(patch)
	if err != nil {
		return "", fmt.Errorf("marshal approval patch: %w", err)
	}
	return string(b), nil
}

// approveAndConfirm writes the approval and returns nil only when the operator
// still has it standing after approveSettle. A lost race, an expired or
// superseded action, or a gate refusal is printed and returned as a non-zero exit;
// nothing here says "approved" until the re-read agrees. byAnother is true when
// somebody else's approval landed first and stands: it is said so, nothing is written
// by this command, and the caller reports no approval of its own.
func approveAndConfirm(
	ctx context.Context, kubeconfig, namespace, name string, rem *remediationFull, w io.Writer,
) (byAnother bool, err error) {
	current := rem
	written := false
	for attempt := 0; attempt < recordAttempts && !written; attempt++ {
		patch, err := buildApprovePatch(current.Metadata.ResourceVersion, time.Now().UTC().Format(time.RFC3339))
		if err != nil {
			return false, err
		}
		err = patchRemediationStatus(ctx, kubeconfig, namespace, name, patch)
		switch {
		case err == nil:
			written = true
		case !isConflictError(err):
			return false, fmt.Errorf("failed to approve remediation: %w", err)
		default:
			// Something wrote to the action since it was read.
			fresh, rerr := fetchRemediation(ctx, kubeconfig, name, namespace)
			if rerr != nil {
				return false, rerr
			}
			switch {
			case fresh.Status.Phase == "Pending":
				// Still Pending, but a spec edit since the user read the plan means the
				// approval would land on a plan they never saw (the operator's gate would
				// refuse it, but the user should be told, not surprised). A status-only
				// write is just a new version: retry on it.
				if fresh.Metadata.Generation != rem.Metadata.Generation {
					fmt.Fprintf(w, "\n")
					output.Error(fmt.Sprintf("%s changed since you read it (spec generation %d, now %d), so it is NOT approved.",
						name, rem.Metadata.Generation, fresh.Metadata.Generation))
					output.Info("Nothing was approved. Review the new plan with `dorgu remediation diff " + name +
						" -n " + namespace + "` and approve again.")
					return false, errSilent
				}
				current = fresh
			case approvalOfAnotherStands(fresh):
				output.Info(fmt.Sprintf("%s was already approved by another actor (phase %s); this command wrote nothing.",
					name, fresh.Status.Phase))
				return true, nil
			default:
				printApprovalWithdrawn(w, name, fresh)
				return false, errSilent
			}
		}
	}
	if !written {
		output.Error(fmt.Sprintf("NOT approved: %s kept changing under the write (%d attempts); nothing was approved.",
			name, recordAttempts))
		return false, errSilent
	}

	if !sleepCtx(ctx, approveSettle) {
		return false, ctx.Err()
	}
	settled, err := fetchRemediation(ctx, kubeconfig, name, namespace)
	if err != nil {
		output.Error(fmt.Sprintf("The approval was written but could not be confirmed (%v). "+
			"Check `dorgu remediation list` before relying on it.", err))
		return false, errSilent
	}
	if phasesWhereTheApprovalStands[settled.Status.Phase] {
		return false, nil
	}
	if refused := settledGateRefusal(settled); refused != nil {
		recordDecline(ctx, kubeconfig, rem, refused)
		printTicketRefusal(w, refused)
		return false, withExitCode(ExitDeclined, errSilent)
	}
	printApprovalWithdrawn(w, name, settled)
	return false, errSilent
}

// approvalOfAnotherStands reports whether an approval that is not ours is in place:
// Approved, or Ticketed, or Applying with the ticket the operator issued for it. An
// Applying action with no ticket is an operator that issues none, which this CLI
// cannot heal against; it is not called approved.
func approvalOfAnotherStands(rem *remediationFull) bool {
	switch rem.Status.Phase {
	case "Approved", "Ticketed":
		return true
	case "Applying":
		return rem.Status.ApplyTicket != nil
	}
	return false
}

// settledGateRefusal returns the gate's refusal when the operator answered the
// approval by failing the action at the gate.
func settledGateRefusal(rem *remediationFull) *gateRefusedError {
	_, err := ticketOutcome(rem)
	var refused *gateRefusedError
	if errors.As(err, &refused) {
		return refused
	}
	return nil
}

// sleepCtx sleeps for d and reports false if ctx ended first.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}

// printApprovalWithdrawn says plainly that the approval did not stand and why,
// in the operator's terms, so a script's non-zero exit and a human's reading
// agree.
func printApprovalWithdrawn(w io.Writer, name string, rem *remediationFull) {
	fmt.Fprintf(w, "\n")
	output.Error(fmt.Sprintf("%s is NOT approved (phase %s): %s", name, rem.Status.Phase, withdrawnReason(rem)))
	output.Info("Nothing was applied to the persona or the workload.")
}

// withdrawnReason says why the action is not approved. A proposal the operator
// superseded or expired is phase Expired; the reason is on the Expired condition,
// and each of its three values gets its own sentence.
func withdrawnReason(rem *remediationFull) string {
	switch rem.Status.Phase {
	case "Expired":
		c := findCondition(rem.Status.Conditions, "Expired")
		reason := ""
		if c != nil && c.Status == "True" {
			reason = c.Reason
		}
		switch reason {
		case "Superseded":
			return "the operator superseded it with a newer proposal for the same fault, which is worse than the one " +
				"it answered. Run `dorgu remediation list` for the current one."
		case "ApprovalTimedOut":
			return "the operator expired it: it waited in Pending longer than its time-to-live. " +
				"Run `dorgu remediation list`; the incident may have a fresh proposal."
		case reasonPredatesGate:
			return "it predates the operator's gate (it was made by an older operator), so it cannot be approved. " +
				"The operator proposes again; run `dorgu remediation list`."
		}
		return "the operator expired it. Run `dorgu remediation list`; the incident may have a fresh proposal."
	case "Rejected":
		return "it was rejected (by someone else or by the operator)."
	case "Pending":
		return "the operator still shows it Pending."
	case "Applying":
		if rem.Status.ApplyTicket == nil {
			return "the operator moved it to Applying without an apply ticket (an operator older than v0.13.0 issues none), " +
				"so this CLI cannot heal against it."
		}
	}
	return "the operator moved it to " + rem.Status.Phase + "."
}
