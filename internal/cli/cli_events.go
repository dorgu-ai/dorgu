package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/dorgu-ai/dorgu/internal/output"
)

// Task 1.10: a decline by the CLI is persisted as a Kubernetes Event on the
// RemediationAction.
//
// The operator persists every refusal the gate makes (a condition, an Event and a
// metric). What it cannot see is the CLI saying no: an owned workload (exit 4), a
// protected kube context, a missing ticket, an expired ticket. Those used to live
// only in a terminal, so nothing could count them. Each now leaves one Warning
// Event, which `dorgu report` reads back.
//
// The Event is the record and nothing else. Writing it is best effort: it never
// changes what the command did, what it printed, or its exit code, and a failure
// to write it is mentioned only with DORGU_DEBUG=1. The message is a sentence for
// a human; nothing reads it back, the reason is the enum.

// Event reasons the CLI writes. They are the contract with `dorgu report`.
const (
	// reasonCLIDeclined: the CLI exited 4. It understood the request and chose not
	// to write: the workload belongs to Helm, Argo CD, Flux or kustomize, or the
	// operator's gate refused the approval.
	reasonCLIDeclined = "DorguCLIDeclined"

	// reasonCLITicketMismatch: the ticket is not one the CLI may apply to what it
	// finds on the cluster: another workload (UID), a spec that moved, a ticket not
	// bound to a workload, a container the Deployment lacks, an empty or unreadable
	// patch, an unusable expiry, a remediation that moved on, or a policy that could
	// not be read. The message carries which; the reason is the one bounded value.
	reasonCLITicketMismatch = "DorguCLITicketMismatch"

	// reasonCLINoTicket: the operator issued no apply ticket the CLI could use.
	reasonCLINoTicket = "DorguCLINoTicket"

	// reasonCLITicketExpired: the apply ticket expired before the CLI could use it.
	reasonCLITicketExpired = "DorguCLITicketExpired"

	// reasonCLIPrefix marks every CLI decline Event; the report matches on it.
	reasonCLIPrefix = "DorguCLI"

	// eventSourceComponent is the Event's source.component.
	eventSourceComponent = "dorgu-cli"

	// maxEventMessage bounds an Event message (the API server allows 1024 bytes).
	maxEventMessage = 1024

	// declineEventTimeout bounds the Event write, so a slow API server cannot hold
	// up a command that has already finished its real work.
	declineEventTimeout = 10 * time.Second
)

// protectedContextError is the refusal to write to a kube context whose name
// matches a protected pattern.
type protectedContextError struct {
	context string
	message string
}

func (e *protectedContextError) Error() string { return e.message }

// ticketMismatchError is a ticket the CLI refuses to apply because it does not
// match the cluster as it is now, or is not well formed. It is a decline: the CLI
// understood the request and chose not to write. It wraps the cause so errors.Is
// and errors.As keep working through it.
type ticketMismatchError struct{ err error }

func (e *ticketMismatchError) Error() string { return e.err.Error() }
func (e *ticketMismatchError) Unwrap() error { return e.err }

// mismatch builds a ticketMismatchError from a format.
func mismatch(format string, args ...any) error {
	return &ticketMismatchError{err: fmt.Errorf(format, args...)}
}

// ticketExpiredError is an apply ticket that ran out before the CLI used it.
type ticketExpiredError struct {
	expiresAt    string
	whileWaiting bool
}

func (e *ticketExpiredError) Error() string {
	if e.whileWaiting {
		return fmt.Sprintf("the apply ticket expired at %s while waiting; nothing was applied to the workload", e.expiresAt)
	}
	return fmt.Sprintf("the apply ticket expired at %s; approve a fresh remediation, nothing was applied", e.expiresAt)
}

// declineFor classifies an error as one of the CLI's declines. It branches on the
// error's type and never on its text.
//
// A protected kube context is deliberately not here: the guard exists so the CLI
// writes nothing to a cluster the user marked as production, and an Event is a
// write. That decline is local only (stderr) and the report does not count it.
func declineFor(err error) (reason, message string, ok bool) {
	var (
		expired  *ticketExpiredError
		noTicket *noTicketError
		owned    *ownedWorkloadError
		refused  *gateRefusedError
		mism     *ticketMismatchError
	)
	switch {
	case errors.As(err, &expired):
		return reasonCLITicketExpired, expired.Error(), true
	case errors.As(err, &noTicket):
		return reasonCLINoTicket, "dorgu CLI applied nothing: " + noTicket.why, true
	case errors.As(err, &owned):
		return reasonCLIDeclined, fmt.Sprintf(
			"dorgu CLI declined to patch a workload that %s owns (exit %d): the change belongs to its owner",
			ownerName(owned.ref), ExitDeclined), true
	case errors.As(err, &refused):
		return reasonCLIDeclined, fmt.Sprintf(
			"dorgu CLI exited %d: the operator's gate refused this approval (%s); nothing was applied",
			ExitDeclined, refused.reason), true
	case errors.As(err, &mism):
		return reasonCLITicketMismatch, "dorgu CLI declined to apply the ticket: " + mism.Error(), true
	}
	return "", "", false
}

// declineEvent is a core/v1 Event, in the fields `kubectl create` needs.
type declineEvent struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Metadata   struct {
		GenerateName string `json:"generateName"`
		Namespace    string `json:"namespace"`
	} `json:"metadata"`
	InvolvedObject struct {
		APIVersion string `json:"apiVersion"`
		Kind       string `json:"kind"`
		Name       string `json:"name"`
		Namespace  string `json:"namespace"`
		UID        string `json:"uid,omitempty"`
	} `json:"involvedObject"`
	Reason  string `json:"reason"`
	Message string `json:"message"`
	Type    string `json:"type"`
	Source  struct {
		Component string `json:"component"`
	} `json:"source"`
	FirstTimestamp string `json:"firstTimestamp"`
	LastTimestamp  string `json:"lastTimestamp"`
	Count          int    `json:"count"`
}

// buildDeclineEvent renders one decline as a Warning Event on the
// RemediationAction. An empty uid is left out rather than invented.
func buildDeclineEvent(namespace, name, uid, reason, message string, at time.Time) declineEvent {
	var ev declineEvent
	ev.APIVersion = "v1"
	ev.Kind = "Event"
	prefix := name
	if len(prefix) > 200 {
		prefix = prefix[:200]
	}
	ev.Metadata.GenerateName = prefix + "."
	ev.Metadata.Namespace = namespace
	ev.InvolvedObject.APIVersion = "dorgu.io/v1"
	ev.InvolvedObject.Kind = "RemediationAction"
	ev.InvolvedObject.Name = name
	ev.InvolvedObject.Namespace = namespace
	ev.InvolvedObject.UID = uid
	ev.Reason = reason
	ev.Message = truncateRunes(message, maxEventMessage)
	ev.Type = "Warning"
	ev.Source.Component = eventSourceComponent
	stamp := at.UTC().Format(time.RFC3339)
	ev.FirstTimestamp, ev.LastTimestamp = stamp, stamp
	ev.Count = 1
	return ev
}

// truncateRunes cuts s to at most n bytes without splitting a character.
func truncateRunes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := s[:n]
	for len(cut) > 0 && !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	return cut
}

// recordDecline writes the Event for err if err is one of the CLI's declines. It
// is best effort and never returns an error: see the file comment.
func recordDecline(ctx context.Context, kubeconfig string, rem *remediationFull, err error) {
	if rem == nil || err == nil {
		return
	}
	var protected *protectedContextError
	if errors.As(err, &protected) {
		return // local only: nothing is written to the protected cluster
	}
	reason, message, ok := declineFor(err)
	if !ok {
		return
	}
	ev := buildDeclineEvent(rem.Metadata.Namespace, rem.Metadata.Name, rem.Metadata.UID, reason, message, time.Now())
	if werr := createEvent(ctx, kubeconfig, ev); werr != nil && os.Getenv("DORGU_DEBUG") == "1" {
		output.Warn(fmt.Sprintf("could not record the decline as an Event on %s/%s: %v",
			rem.Metadata.Namespace, rem.Metadata.Name, werr))
	}
}

// createEvent creates the Event with kubectl, from stdin.
func createEvent(ctx context.Context, kubeconfig string, ev declineEvent) error {
	body, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	// The command's own context may be spent by now; the Event gets its own clock.
	ectx, cancel := context.WithTimeout(context.WithoutCancel(ctx), declineEventTimeout)
	defer cancel()
	cmd := kubectlCmd(ectx, kubeconfig, "create", "-f", "-")
	cmd.Stdin = bytes.NewReader(body)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return errors.New(strings.TrimSpace(kubectlErrText(out)))
	}
	return nil
}
