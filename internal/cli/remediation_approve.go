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
// nothing here says "approved" until the re-read agrees.
func approveAndConfirm(ctx context.Context, kubeconfig, namespace, name string, rem *remediationFull, w io.Writer) error {
	current := rem
	written := false
	for attempt := 0; attempt < recordAttempts && !written; attempt++ {
		patch, err := buildApprovePatch(current.Metadata.ResourceVersion, time.Now().UTC().Format(time.RFC3339))
		if err != nil {
			return err
		}
		err = patchRemediationStatus(ctx, kubeconfig, namespace, name, patch)
		switch {
		case err == nil:
			written = true
		case !isConflictError(err):
			return fmt.Errorf("failed to approve remediation: %w", err)
		default:
			// Something wrote to the action since it was read. If it is still
			// Pending (an unrelated status write), try again on the new version; if
			// it moved on, say what it became and stop.
			fresh, rerr := fetchRemediation(ctx, kubeconfig, name, namespace)
			if rerr != nil {
				return rerr
			}
			if fresh.Status.Phase != "Pending" {
				printApprovalWithdrawn(w, name, fresh)
				return errSilent
			}
			current = fresh
		}
	}
	if !written {
		output.Error(fmt.Sprintf("NOT approved: %s kept changing under the write (%d attempts); nothing was approved.",
			name, recordAttempts))
		return errSilent
	}

	if !sleepCtx(ctx, approveSettle) {
		return ctx.Err()
	}
	settled, err := fetchRemediation(ctx, kubeconfig, name, namespace)
	if err != nil {
		output.Error(fmt.Sprintf("The approval was written but could not be confirmed (%v). "+
			"Check `dorgu remediation list` before relying on it.", err))
		return errSilent
	}
	if phasesWhereTheApprovalStands[settled.Status.Phase] {
		return nil
	}
	if refused := settledGateRefusal(settled); refused != nil {
		recordDecline(ctx, kubeconfig, rem, refused)
		printTicketRefusal(w, refused)
		return withExitCode(ExitDeclined, errSilent)
	}
	printApprovalWithdrawn(w, name, settled)
	return errSilent
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
	phase := rem.Status.Phase
	why := "the operator moved it to " + phase + "."
	switch phase {
	case "Superseded":
		why = "the operator superseded it with a newer proposal for the same workload. " +
			"Run `dorgu remediation list` for the current one."
	case "Expired":
		why = "the operator expired it: it waited in Pending longer than its time-to-live. " +
			"Run `dorgu remediation list`; the incident may have a fresh proposal."
	case "Pending":
		why = "the operator still shows it Pending."
	}
	fmt.Fprintf(w, "\n")
	output.Error(fmt.Sprintf("%s is NOT approved (phase %s): %s", name, phase, why))
	output.Info("Nothing was applied to the persona or the workload.")
}
