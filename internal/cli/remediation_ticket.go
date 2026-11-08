package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"time"

	"github.com/dorgu-ai/dorgu/internal/output"
)

// The operator's gate re-checks an approval on live state and, when it clears,
// writes status.applyTicket. The CLI applies exactly that ticket and nothing
// else: not the spec's patch, not the plan it previewed before approving.
//
// What the CLI checks here is not the gate (the operator's gate decides what is
// safe). It is the CLI refusing to be the weak link: a missing, expired or stale
// ticket, a different workload than the ticket names, or a Deployment that
// turned out to be owned. There is no path that patches without a ticket, and
// none that falls back to the pre-ticket behaviour against an operator that
// does not issue one.

// applyTicket mirrors status.applyTicket of the RemediationAction CRD.
type applyTicket struct {
	WorkloadUID             string          `json:"workloadUID"`
	Container               string          `json:"container"`
	Patch                   json.RawMessage `json:"patch"`
	ObservedResourceVersion string          `json:"observedResourceVersion"`
	// ObservedGeneration is the Deployment's metadata.generation when the ticket
	// was written. The CLI compares generation, not resourceVersion: a status or
	// scale write moves the resourceVersion and says nothing about the spec.
	ObservedGeneration int64 `json:"observedGeneration"`
	// IssuedAt is when the operator wrote the ticket. A ticket that predates the
	// approval being recorded is not the operator's answer to it.
	IssuedAt  string `json:"issuedAt"`
	ExpiresAt string `json:"expiresAt"`
	Verdicts  []struct {
		Stage     string `json:"stage"`
		Guardrail string `json:"guardrail"`
		Verdict   string `json:"verdict"`
		Reason    string `json:"reason"`
	} `json:"verdicts"`
}

// statusCondition is the part of a metav1.Condition the CLI reads.
type statusCondition struct {
	Type    string `json:"type"`
	Status  string `json:"status"`
	Reason  string `json:"reason"`
	Message string `json:"message"`
}

// Condition types and reasons the operator writes (see the operator's
// api/v1/remediationaction_steps.go).
const (
	conditionGateRefused        = "GateRefused"
	conditionApplied            = "Applied"
	reasonGateRefusedAtApproval = "GateRefusedAtApproval"
	reasonPreconditionRejected  = "PreconditionRejected"
)

// maxTicketLifetime mirrors the operator's gate.TicketTTL: no ticket it issues
// lives longer. A ticket claiming more was not issued by it. ticketTimeSlack
// covers the whole-second resolution both timestamps are stored at, and
// ticketFutureSkew how far ahead of this machine's clock an issue time may be
// (the operator's clock is not this one's).
const (
	maxTicketLifetime = 10 * time.Minute
	ticketTimeSlack   = 2 * time.Second
	ticketFutureSkew  = 2 * time.Minute
)

// ticketWaitTimeout is how long approve waits for the operator's ticket, and
// ticketPollInterval how often it looks. Variables so tests do not wait 30s.
var (
	ticketWaitTimeout  = 30 * time.Second
	ticketPollInterval = time.Second
)

// upgradeHint is appended to every refusal that means "the operator is not
// issuing tickets": the two sides have to be upgraded together.
const upgradeHint = "This CLI needs an operator that issues apply tickets (dorgu-operator v0.13.0 or newer; " +
	"upgrade it with `helm upgrade` and apply the CRDs). It will not patch a workload without a ticket."

// gateRefusedError is the operator's gate refusing an approval on live state.
// Nothing was applied by the operator, and the CLI patches nothing either.
type gateRefusedError struct {
	reason  string // the CamelCase guardrail enum on the GateRefused condition
	message string
}

func (e *gateRefusedError) Error() string {
	return fmt.Sprintf("the operator's gate refused this approval (%s): %s", e.reason, e.message)
}

// noTicketError means the operator never produced a ticket: it is too old, or it
// is not reconciling.
type noTicketError struct{ why string }

func (e *noTicketError) Error() string { return e.why + " " + upgradeHint }

// ticketOutcome reads what the operator has done with an approval. ready is true
// when a ticket that answers THIS approval exists; err is a terminal refusal;
// neither means keep waiting.
//
// A ticket answers the approval only if it was issued at or after the approval
// was recorded. The comparison is between two clocks: issuedAt is the operator's,
// and approvedAt is this CLI's own clock, written into status by `approve` (the
// API server stores it, it does not stamp it). If this machine's clock runs ahead
// of the operator's by more than the time the operator takes to answer, a fresh
// ticket reads as issued before the approval and is ignored until the wait times
// out. The action must also still be in the phase the operator leaves a fresh
// ticket in: Ticketed, or the Applying it moves to in the same pass. A ticket sitting on a Pending action, one issued before
// the approval, and one on an action that has moved on to Verifying or Completed
// are not answers; they are ignored (F2).
func ticketOutcome(rem *remediationFull) (ready bool, err error) {
	phase := rem.Status.Phase
	ticket := rem.Status.ApplyTicket

	switch phase {
	case "Failed", "Expired", "Rejected", "RolledBack":
		if refused := findCondition(rem.Status.Conditions, conditionGateRefused); refused != nil &&
			refused.Status == "True" && refusedAtGate(rem) {
			return false, &gateRefusedError{reason: refused.Reason, message: refused.Message}
		}
		return false, mismatch("the approval ended in phase %s without applying anything%s", phase, conditionDetail(rem))
	case "Verifying", "Completed":
		if ticket == nil {
			return false, &noTicketError{why: fmt.Sprintf(
				"The operator moved this remediation to %s without issuing an apply ticket, so it predates tickets.", phase)}
		}
		return false, mismatch("the remediation is already in phase %s: a ticket seen now belongs to an earlier apply, "+
			"not to this approval, so nothing was applied to the workload", phase)
	case "Applying":
		if ticket == nil {
			return false, &noTicketError{why: fmt.Sprintf(
				"The operator moved this remediation to %s without issuing an apply ticket, so it predates tickets.", phase)}
		}
	case "Ticketed":
	case "Acknowledged":
		return false, errors.New("the operator acknowledged this plan as advisory: there is nothing to apply")
	default:
		return false, nil
	}
	if ticket == nil {
		return false, nil
	}
	return ticketAnswersApproval(rem, ticket, time.Now())
}

// refusedAtGate reports whether the Applied condition says the gate refused the
// approval or the apply (both persist the guardrail on GateRefused).
func refusedAtGate(rem *remediationFull) bool {
	switch appliedReason(rem) {
	case reasonGateRefusedAtApproval, reasonPreconditionRejected:
		return true
	}
	return false
}

// ticketAnswersApproval decides whether a ticket in a fresh-ticket phase is the
// operator's answer to the approval on record. A ticket that predates the
// approval is not an error (the operator may still answer): ready is false and the
// caller keeps waiting. One that cannot have come from the operator is refused.
func ticketAnswersApproval(rem *remediationFull, ticket *applyTicket, now time.Time) (bool, error) {
	if strings.TrimSpace(rem.Status.ApprovedAt) == "" {
		return false, mismatch("the remediation records no approval (status.approvedAt), so no ticket on it was issued " +
			"for one; nothing was applied to the workload")
	}
	approved, err := time.Parse(time.RFC3339, rem.Status.ApprovedAt)
	if err != nil {
		return false, mismatch("the recorded approval time %q is unreadable; nothing was applied to the workload", rem.Status.ApprovedAt)
	}
	issued, err := time.Parse(time.RFC3339, ticket.IssuedAt)
	if err != nil || issued.IsZero() {
		return false, &noTicketError{why: "The apply ticket carries no issue time, so it cannot be tied to this approval."}
	}
	expires, err := time.Parse(time.RFC3339, ticket.ExpiresAt)
	if err != nil || expires.IsZero() {
		return false, mismatch("the apply ticket has no valid expiry (%q); refusing it", ticket.ExpiresAt)
	}
	if issued.After(now.Add(ticketFutureSkew)) {
		return false, mismatch("the apply ticket claims to be issued at %s, in the future; refusing it",
			issued.UTC().Format(time.RFC3339))
	}
	if expires.Sub(issued) > maxTicketLifetime+ticketTimeSlack {
		return false, mismatch("the apply ticket's lifetime (%s) is longer than the operator ever issues; refusing it",
			expires.Sub(issued).Round(time.Second))
	}
	if issued.Before(approved) {
		return false, nil
	}
	return true, nil
}

// predatingTicketNote says, for the timeout message, that a ticket was there and
// was set aside because it was issued before this approval.
func predatingTicketNote(rem *remediationFull) string {
	ticket := rem.Status.ApplyTicket
	if ticket == nil {
		return ""
	}
	issued, err1 := time.Parse(time.RFC3339, ticket.IssuedAt)
	approved, err2 := time.Parse(time.RFC3339, rem.Status.ApprovedAt)
	if err1 != nil || err2 != nil || !issued.Before(approved) {
		return ""
	}
	return fmt.Sprintf(" An apply ticket issued at %s, before this approval was recorded at %s, was ignored.",
		issued.UTC().Format(time.RFC3339), approved.UTC().Format(time.RFC3339))
}

func findCondition(conds []statusCondition, condType string) *statusCondition {
	for i := range conds {
		if conds[i].Type == condType {
			return &conds[i]
		}
	}
	return nil
}

func appliedReason(rem *remediationFull) string {
	if c := findCondition(rem.Status.Conditions, conditionApplied); c != nil {
		return c.Reason
	}
	return ""
}

func conditionDetail(rem *remediationFull) string {
	if c := findCondition(rem.Status.Conditions, conditionApplied); c != nil && c.Message != "" {
		return ": " + c.Message
	}
	return ""
}

// waitForTicket polls the RemediationAction until the operator has issued a
// ticket or refused, for at most ticketWaitTimeout. It draws a spinner on a
// terminal and a single line otherwise.
func waitForTicket(ctx context.Context, kubeconfig, name, namespace string, w io.Writer) (*remediationFull, error) {
	deadline := time.Now().Add(ticketWaitTimeout)
	spin := newSpinner(w, "Waiting for the operator's apply ticket")
	defer spin.stop()

	for {
		rem, err := fetchRemediation(ctx, kubeconfig, name, namespace)
		if err != nil {
			return nil, err
		}
		ready, outcomeErr := ticketOutcome(rem)
		if outcomeErr != nil {
			return nil, outcomeErr
		}
		if ready {
			return rem, nil
		}
		if !time.Now().Before(deadline) {
			return nil, &noTicketError{why: fmt.Sprintf(
				"The operator did not issue an apply ticket within %s (phase %q), so nothing was applied to the workload.%s",
				ticketWaitTimeout, rem.Status.Phase, predatingTicketNote(rem))}
		}
		if !spin.wait(ctx, ticketPollInterval) {
			return nil, ctx.Err()
		}
	}
}

// spinner is the 30-second wait indicator. On a terminal it animates one line;
// piped, it prints the line once, so logs stay readable.
type spinner struct {
	w      io.Writer
	label  string
	tty    bool
	frame  int
	active bool
}

var spinnerFrames = []string{"|", "/", "-", "\\"}

func newSpinner(w io.Writer, label string) *spinner {
	s := &spinner{w: w, label: label, tty: output.IsTTY()}
	if !s.tty {
		fmt.Fprintf(w, "%s (up to %s)...\n", label, ticketWaitTimeout)
	}
	return s
}

// wait sleeps for d, animating while it does. It reports false if ctx ended.
func (s *spinner) wait(ctx context.Context, d time.Duration) bool {
	if !s.tty {
		select {
		case <-ctx.Done():
			return false
		case <-time.After(d):
			return true
		}
	}
	s.active = true
	end := time.Now().Add(d)
	for time.Now().Before(end) {
		fmt.Fprintf(s.w, "\r%s %s", spinnerFrames[s.frame%len(spinnerFrames)], s.label)
		s.frame++
		select {
		case <-ctx.Done():
			return false
		case <-time.After(100 * time.Millisecond):
		}
	}
	return true
}

func (s *spinner) stop() {
	if s.active {
		_, _ = fmt.Fprint(s.w, "\r"+strings.Repeat(" ", len(s.label)+4)+"\r")
	}
}

// printTicketRefusal says, in the operator's own terms, why nothing was applied.
func printTicketRefusal(w io.Writer, err error) {
	var refused *gateRefusedError
	var noTicket *noTicketError
	switch {
	case errors.As(err, &refused):
		fmt.Fprintln(w)
		fmt.Fprintln(w, "The operator's gate refused this approval on the cluster as it is now.")
		fmt.Fprintf(w, "  Reason:  %s\n", refused.reason)
		if refused.message != "" {
			fmt.Fprintf(w, "  Detail:  %s\n", refused.message)
		}
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Nothing was applied to the persona or the workload.")
	case errors.As(err, &noTicket):
		output.Error(noTicket.Error())
		output.Info("Nothing was applied to the workload.")
	default:
		output.Error(err.Error())
	}
}

// bindTicket turns the preflight plan and the operator's ticket into the one
// change the CLI will make. The ticket is the authority: its patch is what is
// applied, its container is the container, and the live Deployment has to still
// be the one it names.
//
// It re-reads the Deployment immediately before the patch. Everything the
// preflight read (ownership, UID) is a fact about a moment that is now gone, and
// the ticket binds the write to a UID and resourceVersion.
func bindTicket(ctx context.Context, kubeconfig string, ticket *applyTicket, exec *healExecution, now time.Time) (*healExecution, error) {
	if ticket == nil {
		return nil, &noTicketError{why: "This remediation has no apply ticket."}
	}
	if exec == nil || exec.Deployment == "" {
		return nil, mismatch("no workload was resolved for this remediation, so no ticket can be bound to one")
	}

	expires, err := time.Parse(time.RFC3339, ticket.ExpiresAt)
	if err != nil || expires.IsZero() {
		return nil, mismatch("the apply ticket has no valid expiry (%q); refusing it", ticket.ExpiresAt)
	}
	if !now.Before(expires) {
		return nil, &ticketExpiredError{expiresAt: expires.UTC().Format(time.RFC3339)}
	}
	if ticket.WorkloadUID == "" || ticket.Container == "" || ticket.ObservedGeneration <= 0 {
		return nil, mismatch("the apply ticket is not bound to a workload (UID, container and observed generation are required); refusing it")
	}
	change, err := extractResourceChange(ticket.Patch)
	if err != nil {
		return nil, &ticketMismatchError{err: fmt.Errorf("the apply ticket's patch is unreadable: %w", err)}
	}
	if change.isEmpty() {
		return nil, mismatch("the apply ticket carries no resource change")
	}

	live, err := getDeployment(ctx, kubeconfig, exec.Namespace, exec.Deployment)
	if err != nil {
		return nil, err
	}
	if owner := liveOwnerOf(live); owner != nil {
		return nil, &ownedWorkloadError{
			ref:  owner.workloadRef(live.Name, exec.Namespace),
			plan: &healPlan{Change: change, Advisory: exec.Advisory, Safety: exec.Safety},
		}
	}
	if err := sameWorkloadAsTicket(live, ticket, exec); err != nil {
		return nil, err
	}
	if exec.Container != "" && exec.Container != ticket.Container {
		return nil, mismatch("the preview chose container %q but the ticket names %q; the ticket is what is applied, "+
			"so --container must agree with it", exec.Container, ticket.Container)
	}
	if !contains(live.Containers, ticket.Container) {
		return nil, mismatch("Deployment %s/%s has no container %q, which the ticket names", exec.Namespace, live.Name, ticket.Container)
	}

	patch, err := buildDeploymentResourcePatch(ticket.Container, change)
	if err != nil {
		return nil, err
	}
	bound := *exec
	bound.Deployment = live.Name
	bound.Container = ticket.Container
	bound.Change = change
	bound.Patch = patch
	return &bound, nil
}

// sameWorkloadAsTicket checks the live Deployment is the one the ticket was issued
// for and that its spec has not moved since: same UID, and metadata.generation
// equal to the ticket's observed generation. resourceVersion is deliberately not
// compared: the Deployment controller and the status subresource move it all the
// time and none of that changes what the ticket would patch (F6).
func sameWorkloadAsTicket(live deploymentSummary, ticket *applyTicket, exec *healExecution) error {
	if live.UID != ticket.WorkloadUID {
		return mismatch("Deployment %s/%s is not the workload the ticket was issued for (UID %q, ticket %q); nothing was applied",
			exec.Namespace, live.Name, live.UID, ticket.WorkloadUID)
	}
	if live.Generation != ticket.ObservedGeneration {
		return mismatch("Deployment %s/%s spec changed since the ticket was issued (generation %d, ticket %d); nothing was applied",
			exec.Namespace, live.Name, live.Generation, ticket.ObservedGeneration)
	}
	return nil
}

// reverifyBeforePatch is the last thing the CLI does before it writes. The
// ticket was fetched and bound some time ago, and a confirmation prompt may have
// sat open since: it re-reads the RemediationAction (still in a phase that
// carries a fresh ticket, the same ticket, unexpired), the Deployment (same UID,
// same generation) and the ClusterPersona (not in observe mode).
//
// The ClusterPersona and RemediationAction reads are a CLI-side stopgap. The
// operator's gate already refuses an approval and an apply under observe mode;
// the CLI cannot yet rely on the operator being the only writer of the ticket
// (admission policy for that is P2 task 2.5), so it checks the policy itself.
func reverifyBeforePatch(ctx context.Context, kubeconfig string, ticketed *remediationFull, bound *healExecution) error {
	name, namespace := ticketed.Metadata.Name, ticketed.Metadata.Namespace
	fresh, err := fetchRemediation(ctx, kubeconfig, name, namespace)
	if err != nil {
		return fmt.Errorf("could not re-read the remediation before patching: %w", err)
	}
	if fresh.Status.Phase != "Ticketed" && fresh.Status.Phase != "Applying" {
		return mismatch("the remediation is now in phase %q, no longer Ticketed; nothing was applied to the workload", fresh.Status.Phase)
	}
	now := fresh.Status.ApplyTicket
	was := ticketed.Status.ApplyTicket
	if now == nil || !sameTicket(now, was) {
		return mismatch("the apply ticket changed since it was read; nothing was applied to the workload")
	}
	expires, err := time.Parse(time.RFC3339, now.ExpiresAt)
	if err != nil || !time.Now().Before(expires) {
		return &ticketExpiredError{expiresAt: now.ExpiresAt, whileWaiting: true}
	}

	live, err := getDeployment(ctx, kubeconfig, bound.Namespace, bound.Deployment)
	if err != nil {
		return err
	}
	if err := sameWorkloadAsTicket(live, now, bound); err != nil {
		return err
	}
	return refuseObserveMode(ctx, kubeconfig)
}

// sameTicket reports whether two tickets are the same clearance: the same issue
// time, workload, container, generation and patch.
func sameTicket(a, b *applyTicket) bool {
	if a.IssuedAt != b.IssuedAt || a.ExpiresAt != b.ExpiresAt || a.WorkloadUID != b.WorkloadUID ||
		a.Container != b.Container || a.ObservedGeneration != b.ObservedGeneration {
		return false
	}
	var pa, pb any
	if json.Unmarshal(a.Patch, &pa) != nil || json.Unmarshal(b.Patch, &pb) != nil {
		return false
	}
	return reflect.DeepEqual(pa, pb)
}

// refuseObserveMode refuses the heal when the cluster's self-healing policy says
// Dorgu only observes. It reads every ClusterPersona and takes the most
// restrictive mode, as the operator does; an unrecognised mode, or a read that
// fails, refuses. A cluster with no ClusterPersona is the default policy (propose).
func refuseObserveMode(ctx context.Context, kubeconfig string) error {
	out, err := kubectlCmd(ctx, kubeconfig, "get", "clusterpersona", "-o", "json").CombinedOutput()
	if err != nil {
		return mismatch("could not read the ClusterPersona self-healing mode (%s); refusing to patch without knowing it",
			kubectlErrText(out))
	}
	var list struct {
		Items []struct {
			Spec struct {
				Policies struct {
					SelfHealing struct {
						Mode string `json:"mode"`
					} `json:"selfHealing"`
				} `json:"policies"`
			} `json:"spec"`
		} `json:"items"`
	}
	if err := json.Unmarshal(out, &list); err != nil {
		return mismatch("the ClusterPersona self-healing mode is unreadable (%v); refusing to patch without knowing it", err)
	}
	for _, item := range list.Items {
		switch mode := item.Spec.Policies.SelfHealing.Mode; mode {
		case "", "propose", "auto-approve":
		case "observe":
			return mismatch("the ClusterPersona self-healing mode is observe: Dorgu only observes on this cluster, " +
				"so a ticket does not license a patch; nothing was applied to the workload")
		default:
			return mismatch("the ClusterPersona self-healing mode %q is not one Dorgu recognises; nothing was applied to the workload", mode)
		}
	}
	return nil
}

func contains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

// operatorVersionReader reads the operator's image tag. A variable so a test can
// stand in for a cluster that runs an old operator.
var operatorVersionReader = readOperatorVersion

// warnIfOperatorPredatesTickets tells the user, when the CLI found no apply
// ticket, that the likely reason is the operator: one before v0.13.0 does not
// issue them. It fails closed: a version that cannot be determined (an unreadable
// Deployment, an image pinned by digest, a tag that is not a version) is said so,
// never silently dropped, because this CLI cannot tell a slow operator from an old
// one. Only a release at v0.13.0 or later says nothing extra.
func warnIfOperatorPredatesTickets(ctx context.Context, kubeconfig string, w io.Writer) {
	version := operatorVersionReader(ctx, kubeconfig)
	switch {
	case operatorPredatesTickets(version):
		_, _ = fmt.Fprintf(w, "\nWarning: the operator on this cluster is %s, which is older than %s and does not issue apply tickets.\n"+
			"This CLI cannot heal against it, and that is why no ticket arrived. Upgrade the operator to %s or newer; the CLI and the operator are a pair.\n",
			*version, reportTicketOperator, reportTicketOperator)
	case !operatorVersionKnown(version):
		_, _ = fmt.Fprintf(w, "\nWarning: could not determine the operator version; an operator older than %s issues no tickets.\n"+
			"If no ticket arrived, check the operator is %s or newer; the CLI and the operator are a pair.\n",
			reportTicketOperator, reportTicketOperator)
	}
}
