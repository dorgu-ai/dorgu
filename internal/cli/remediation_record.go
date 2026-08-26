package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/dorgu-ai/dorgu/internal/output"
)

// CF6-2 / CR-02 — a heal that changed the cluster must say so on the record.
//
// `dorgu remediation heal` patched a Deployment 64Mi→128Mi and left the
// RemediationAction on `Pending`. The command exited 0 with a green
// "✓ Healed …", so the two things Dorgu keeps disagreed: the cluster had the new
// limits, and the record said nobody had approved anything and nothing had
// happened. For a product whose pitch is organizational memory, memory that
// contradicts the cluster is worse than no memory. It is the same class as run
// #1's F-01 — a green result leaving the record wrong.
//
// The record gets two things written to it, both only after kubectl has accepted
// the workload patch:
//
//   - A `Pending` action becomes `Approved`. Healing a Pending remediation has
//     always been an implicit approval and the command has always said so in a
//     warning; it simply never recorded it. Recording it also closes the other
//     half of the same gap, because a Pending action is a no-op to the operator:
//     the persona never learned the new limits either, so the persona was
//     *also* out of step with the cluster.
//   - Every successful heal, whatever phase it started in, stamps a
//     `WorkloadPatched` condition naming the namespace, the Deployment, the
//     container and each field it set. This is the explicit applied marker.
//     It is a condition rather than `status.appliedAt` because `appliedAt`
//     belongs to the operator: it is set when the persona patch lands and read
//     in `Applying` to time the verification window, so writing to it would
//     move a clock the CLI does not own. And it is needed on top of the phase
//     transition because a re-heal after `--no-heal` starts from a phase that is
//     already past `Approved`, where a phase change would be both wrong and
//     invisible.
//
// What this does not do: move the phase of an action that is already Approved,
// Applying, Verifying or terminal. Those phases are the operator's lifecycle,
// and a CLI writing into the middle of a state machine it does not drive is a
// different bug from the one being fixed.

const (
	// conditionWorkloadPatched marks that the CLI applied the resource change to
	// the running workload. It is deliberately not the operator's `Applied`
	// condition: `Applied` means the persona patch landed, and the whole point
	// of this marker is that those two events are separate and used to be
	// confused for each other.
	conditionWorkloadPatched = "WorkloadPatched"

	// reasonWorkloadPatched is the condition reason. The CRD constrains reasons
	// to a CamelCase-ish token, so this is not free text.
	reasonWorkloadPatched = "CLIAppliedResourceChange"

	// healApprovedBy matches what `dorgu remediation approve` writes, so the two
	// routes to an approval are not distinguishable by accident. Which route it
	// was is recorded in the marker's message instead.
	healApprovedBy = "cli-user"
)

// recordAttempts is how many read-modify-write rounds the record write gets.
// Writing one condition means writing the whole list back, so a concurrent
// operator write has to be retried against fresh state rather than overwritten.
const recordAttempts = 3

// recordCondition is one entry of status.conditions: the type the CLI matches
// on, and the untouched JSON it arrived as.
//
// The raw JSON is the point. Conditions are keyed by type, so adding one means
// sending the whole list, and every entry the CLI is not touching has to go back
// byte for byte. Round-tripping the operator's conditions through a struct would
// silently drop any field this CLI does not know about, and quietly rewriting the
// operator's record while fixing a record bug is a poor trade.
type recordCondition struct {
	Type string
	Raw  json.RawMessage
}

// recordState is the RemediationAction status the record write reads before it
// writes: the phase it has to decide about, the conditions it has to preserve,
// and the resourceVersion it sends back as a precondition.
type recordState struct {
	ResourceVersion string
	Phase           string
	Conditions      []recordCondition
}

// needsImplicitApproval reports whether this phase means nobody has recorded a
// decision yet. An unset phase is the same case as Pending: on both, the
// operator does nothing and the record claims nothing.
func (s recordState) needsImplicitApproval() bool {
	return s.Phase == "" || s.Phase == "Pending"
}

// parseRecordState reads the status fields the record write needs out of
// `kubectl get remediationaction -o json` output.
func parseRecordState(raw []byte) (recordState, error) {
	var obj struct {
		Metadata struct {
			ResourceVersion string `json:"resourceVersion"`
		} `json:"metadata"`
		Status struct {
			Phase      string            `json:"phase"`
			Conditions []json.RawMessage `json:"conditions"`
		} `json:"status"`
	}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return recordState{}, fmt.Errorf("parse remediationaction: %w", err)
	}

	out := recordState{
		ResourceVersion: obj.Metadata.ResourceVersion,
		Phase:           obj.Status.Phase,
		Conditions:      make([]recordCondition, 0, len(obj.Status.Conditions)),
	}
	for _, c := range obj.Status.Conditions {
		var head struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(c, &head); err != nil {
			return recordState{}, fmt.Errorf("parse condition: %w", err)
		}
		out.Conditions = append(out.Conditions, recordCondition{Type: head.Type, Raw: c})
	}
	return out, nil
}

// healRecordMessage says what the heal actually did, in the one line a reader of
// the CRD will see. "The workload was patched" would be true and useless; the
// fields and values are what make the record checkable against the cluster.
func healRecordMessage(e *healExecution) string {
	fields := make([]string, 0, len(resourceKeyOrder))
	for _, k := range resourceKeyOrder {
		if v := proposedResourceValue(e.Change, k.kind, k.name); v != "" {
			fields = append(fields, fmt.Sprintf("resources.%s.%s=%s", k.kind, k.name, v))
		}
	}
	return fmt.Sprintf("dorgu CLI applied %s to Deployment %s/%s (container %s)",
		strings.Join(fields, ", "), e.Namespace, e.Deployment, e.Container)
}

// buildHealRecordPatch builds the status merge patch that records the heal.
//
// The resourceVersion goes in as an optimistic-concurrency precondition, for the
// same reason the footprint strip sends one: this is a read-modify-write over a
// list the CLI does not own. Note that the precondition is best-effort on a
// status subresource, so the conflict retry above it is what actually carries
// the correctness; the CLI only ever writes here on a phase the operator is not
// reconciling, or immediately after a phase it just set itself.
func buildHealRecordPatch(st recordState, e *healExecution, now string) (string, error) {
	marker := map[string]any{
		"type":               conditionWorkloadPatched,
		"status":             "True",
		"reason":             reasonWorkloadPatched,
		"message":            healRecordMessage(e),
		"lastTransitionTime": now,
	}
	markerJSON, err := json.Marshal(marker)
	if err != nil {
		return "", fmt.Errorf("marshal %s condition: %w", conditionWorkloadPatched, err)
	}

	// Every foreign condition back as it came in; our own prior marker replaced
	// rather than appended, because two entries of one type is an object the API
	// server rejects.
	conditions := make([]json.RawMessage, 0, len(st.Conditions)+1)
	for _, c := range st.Conditions {
		if c.Type == conditionWorkloadPatched {
			continue
		}
		conditions = append(conditions, c.Raw)
	}
	conditions = append(conditions, markerJSON)

	status := map[string]any{"conditions": conditions}
	if st.needsImplicitApproval() {
		status["phase"] = "Approved"
		status["approvedBy"] = healApprovedBy
		status["approvedAt"] = now
	}

	patch := map[string]any{"status": status}
	if st.ResourceVersion != "" {
		patch["metadata"] = map[string]any{"resourceVersion": st.ResourceVersion}
	}

	b, err := json.Marshal(patch)
	if err != nil {
		return "", fmt.Errorf("marshal status patch: %w", err)
	}
	return string(b), nil
}

// recordError says that Dorgu changed the cluster but could not write that fact
// onto the RemediationAction. The workload is patched, so this never un-does
// anything; it is reported in full and exits non-zero because a record that
// disagrees with the cluster is precisely the failure this fix exists to remove,
// and reporting it with a green exit code would reproduce it.
type recordError struct {
	Namespace string
	Name      string
	Cause     error
}

func (e *recordError) Error() string {
	return fmt.Sprintf("could not record the heal on remediationaction %s/%s: %v",
		e.Namespace, e.Name, e.Cause)
}

func (e *recordError) Unwrap() error { return e.Cause }

// recordHeal writes the heal onto the RemediationAction: the implicit approval
// when the action was still Pending, and the applied marker always.
//
// It is called only after kubectl has accepted the workload patch. Recording an
// approval for a change the cluster refused would be the same divergence with
// the two sides swapped, which is why nothing here runs on the declined,
// advisory or failed paths.
func recordHeal(ctx context.Context, kubeconfig, namespace, name string, e *healExecution) error {
	fail := func(cause error) error {
		return &recordError{Namespace: namespace, Name: name, Cause: cause}
	}
	now := time.Now().UTC().Format(time.RFC3339)

	for attempt := 0; attempt < recordAttempts; attempt++ {
		st, err := readRecordState(ctx, kubeconfig, namespace, name)
		if err != nil {
			return fail(err)
		}
		patch, err := buildHealRecordPatch(st, e, now)
		if err != nil {
			return fail(err)
		}
		if err := patchRemediationStatus(ctx, kubeconfig, namespace, name, patch); err != nil {
			if isConflictError(err) {
				continue
			}
			return fail(err)
		}
		return nil
	}

	return fail(fmt.Errorf(
		"the remediation kept changing under the write (%d attempts)", recordAttempts))
}

// readRecordState reads the RemediationAction status the record write builds on.
func readRecordState(ctx context.Context, kubeconfig, namespace, name string) (recordState, error) {
	out, err := kubectlCmd(ctx, kubeconfig,
		"get", "remediationaction", name, "-n", namespace, "-o", "json").CombinedOutput()
	if err != nil {
		return recordState{}, fmt.Errorf("read remediation status: %s", kubectlErrText(out))
	}
	return parseRecordState(out)
}

// patchRemediationStatus applies a merge patch to the status subresource.
func patchRemediationStatus(ctx context.Context, kubeconfig, namespace, name, patch string) error {
	out, err := kubectlCmd(ctx, kubeconfig,
		"patch", "remediationaction", name, "-n", namespace,
		"--type", "merge", "--subresource", "status", "-p", patch).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s", kubectlErrText(out))
	}
	return nil
}

// printRecordWarning tells the user that the cluster and the record now
// disagree, which way round, and what to do about it.
//
// It deliberately does not print a paste-ready `kubectl patch`. The only patch
// that would work replaces the whole conditions list, so handing one over would
// have the user overwrite the operator's own conditions to fix a record bug. The
// heal is idempotent, so re-running it is both the shorter instruction and the
// safe one.
func printRecordWarning(w io.Writer, namespace, name string, e *healExecution, err error) {
	output.Fwarn(w, fmt.Sprintf(
		"%s/%s was patched, but Dorgu could not record that on the remediation: %v",
		e.Namespace, e.Deployment, err))

	fmt.Fprintf(w, "  The cluster has %s and remediationaction %s/%s does not say so.\n",
		strings.Join(resourceChangeSummary(e.Change), ", "), namespace, name)
	fmt.Fprintln(w, "  Close the gap by re-running the heal, which will record it on the way through:")
	fmt.Fprintf(w, "    dorgu remediation heal %s -n %s\n", name, namespace)
	fmt.Fprintln(w, output.Dim(
		"  Until then, treat the remediation record as incomplete rather than as evidence."))
}
