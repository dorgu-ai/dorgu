package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	ExpiresAt               string          `json:"expiresAt"`
	Verdicts                []struct {
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
// when a usable (present) ticket exists; err is a terminal refusal; neither
// means keep waiting.
func ticketOutcome(rem *remediationFull) (ready bool, err error) {
	phase := rem.Status.Phase
	ticket := rem.Status.ApplyTicket

	switch phase {
	case "Failed", "Expired", "Rejected", "RolledBack":
		if refused := findCondition(rem.Status.Conditions, conditionGateRefused); refused != nil &&
			refused.Status == "True" && appliedReason(rem) == reasonGateRefusedAtApproval {
			return false, &gateRefusedError{reason: refused.Reason, message: refused.Message}
		}
		return false, fmt.Errorf("the approval ended in phase %s without applying anything%s", phase, conditionDetail(rem))
	case "Applying", "Verifying", "Completed":
		if ticket == nil {
			return false, &noTicketError{why: fmt.Sprintf(
				"The operator moved this remediation to %s without issuing an apply ticket, so it predates tickets.", phase)}
		}
		return true, nil
	case "Acknowledged":
		return false, errors.New("the operator acknowledged this plan as advisory: there is nothing to apply")
	}
	return ticket != nil, nil
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
				"The operator did not issue an apply ticket within %s (phase %q), so nothing was applied to the workload.",
				ticketWaitTimeout, rem.Status.Phase)}
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
		fmt.Fprint(s.w, "\r"+strings.Repeat(" ", len(s.label)+4)+"\r")
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
		return nil, errors.New("no workload was resolved for this remediation, so no ticket can be bound to one")
	}

	expires, err := time.Parse(time.RFC3339, ticket.ExpiresAt)
	if err != nil || expires.IsZero() {
		return nil, fmt.Errorf("the apply ticket has no valid expiry (%q); refusing it", ticket.ExpiresAt)
	}
	if !now.Before(expires) {
		return nil, fmt.Errorf("the apply ticket expired at %s; approve a fresh remediation, nothing was applied",
			expires.UTC().Format(time.RFC3339))
	}
	if ticket.WorkloadUID == "" || ticket.Container == "" || ticket.ObservedResourceVersion == "" {
		return nil, errors.New("the apply ticket is not bound to a workload (UID, container and resourceVersion are required); refusing it")
	}
	change, err := extractResourceChange(ticket.Patch)
	if err != nil {
		return nil, fmt.Errorf("the apply ticket's patch is unreadable: %w", err)
	}
	if change.isEmpty() {
		return nil, errors.New("the apply ticket carries no resource change")
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
	if live.UID != ticket.WorkloadUID {
		return nil, fmt.Errorf("Deployment %s/%s is not the workload the ticket was issued for (UID %q, ticket %q); nothing was applied",
			exec.Namespace, live.Name, live.UID, ticket.WorkloadUID)
	}
	if live.ResourceVersion != ticket.ObservedResourceVersion {
		return nil, fmt.Errorf("Deployment %s/%s changed since the ticket was issued (resourceVersion %s, ticket %s); nothing was applied",
			exec.Namespace, live.Name, live.ResourceVersion, ticket.ObservedResourceVersion)
	}
	if exec.Container != "" && exec.Container != ticket.Container {
		return nil, fmt.Errorf("the preview chose container %q but the ticket names %q; the ticket is what is applied, "+
			"so --container must agree with it", exec.Container, ticket.Container)
	}
	if !contains(live.Containers, ticket.Container) {
		return nil, fmt.Errorf("Deployment %s/%s has no container %q, which the ticket names", exec.Namespace, live.Name, ticket.Container)
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

func contains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
