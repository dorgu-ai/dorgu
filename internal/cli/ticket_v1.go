package cli

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	dorguv1 "github.com/dorgu-ai/dorgu-operator/api/v1"
)

// Ticket v1 (P2-D9, P2-D14, P2-D15). The operator's ticket now says what it
// cleared (specHash), who approved it (approvedBy) and until when (expiresAt). The
// CLI checks all three before it touches anything, because a ticket is only as
// good as what the CLI can verify about it:
//
//   - specHash is the SHA-256 of the RFC 8785 canonical JSON of the RemediationAction
//     .spec exactly as the API server serves it. The CLI hashes the raw `.spec` of
//     the object it read with kubectl, never a typed struct: a CLI built against
//     another version of the API would drop or add fields and compute another
//     hash (C3 review F-6). The function is the operator's own, so both sides
//     canonicalise the same way (golden vectors: testdata/spechash_vectors.json).
//   - approvedBy has to be the authenticated user. A ticket someone else approved
//     is not this user's to apply.
//   - expiresAt is honoured exactly. The CLI no longer caps a ticket's lifetime:
//     the operator's default is 15 minutes and the chart sets it (C3 review F-1).
//
// This is the CLI refusing to be the weak link. The operator's gate decides what is
// safe; admission (brief C2) decides who may write a ticket.

// specHashFromServedSpec is the specHash of a spec given as the raw JSON the API
// server served. See SpecHashAlgorithm in the operator's api/v1.
func specHashFromServedSpec(raw json.RawMessage) (string, error) {
	if trimmed := strings.TrimSpace(string(raw)); trimmed == "" || trimmed == "null" {
		return "", errors.New("the remediation carries no spec")
	}
	return dorguv1.SpecHashFromJSON(raw)
}

// checkTicketV1 refuses a ticket that is not a complete, unexpired, unforged ticket
// v1 for this user. Every refusal says nothing was applied. rem must be an object
// read from the API server (its raw spec is what is hashed).
func checkTicketV1(rem *remediationFull, user string, now time.Time) error {
	ticket := rem.Status.ApplyTicket
	if ticket == nil {
		return &noTicketError{why: "This remediation has no apply ticket."}
	}
	if strings.TrimSpace(user) == "" {
		return mismatch("there is no authenticated user to check the ticket's approver against; nothing was applied to the workload")
	}

	var missing []string
	for _, f := range []struct{ name, value string }{
		{"specHash", ticket.SpecHash}, {"approvedBy", ticket.ApprovedBy}, {"expiresAt", ticket.ExpiresAt},
	} {
		if strings.TrimSpace(f.value) == "" {
			missing = append(missing, f.name)
		}
	}
	if len(missing) > 0 {
		return mismatch("the apply ticket is missing %s: it predates ticket v1 or was not written by the operator; "+
			"nothing was applied to the workload", strings.Join(missing, ", "))
	}

	expires, err := time.Parse(time.RFC3339, ticket.ExpiresAt)
	if err != nil || expires.IsZero() {
		return mismatch("the apply ticket has no valid expiry (%q); refusing it, nothing was applied", ticket.ExpiresAt)
	}
	if !now.Before(expires) {
		return &ticketExpiredError{expiresAt: expires.UTC().Format(time.RFC3339)}
	}

	want, err := specHashFromServedSpec(rem.rawSpec)
	if err != nil {
		return mismatch("could not compute the hash of the remediation's spec (%v), so the ticket cannot be checked; "+
			"nothing was applied to the workload", err)
	}
	if want != ticket.SpecHash {
		return mismatch("the plan changed after the operator cleared it: the remediation's spec no longer matches the " +
			"ticket's specHash; nothing was applied to the workload")
	}

	if ticket.ApprovedBy != user {
		return mismatch("this ticket was approved by %q, but you are %q; only that approval can be applied by that user, "+
			"nothing was applied to the workload", ticket.ApprovedBy, user)
	}
	return nil
}

// UnmarshalJSON keeps the raw `.spec` beside the typed fields. The typed struct is
// what the CLI reads to show and plan; the raw bytes are what the ticket's specHash
// is checked against, because a hash of a typed round trip is a hash of something
// the API server never served.
func (r *remediationFull) UnmarshalJSON(b []byte) error {
	type plain remediationFull // no methods: no recursion
	var p plain
	if err := json.Unmarshal(b, &p); err != nil {
		return err
	}
	var envelope struct {
		Spec json.RawMessage `json:"spec"`
	}
	if err := json.Unmarshal(b, &envelope); err != nil {
		return err
	}
	*r = remediationFull(p)
	r.rawSpec = append(json.RawMessage(nil), envelope.Spec...)
	return nil
}
