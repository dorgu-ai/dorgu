package cli

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Fixtures for the operator's side of the ticket protocol: a RemediationAction
// that has been through the approval re-check, and Deployments that carry the
// identity (UID, resourceVersion) a ticket is bound to.

const (
	fixtureDeploymentUID = "uid-api-server"
	fixtureDeploymentRV  = "100"

	// fixtureGeneration is metadata.generation of the fixture Deployments, and the
	// observedGeneration of the fixture tickets: the spec has not moved.
	fixtureGeneration = 3
)

// fixtureApprovedAt is when the fixtures say the approval was recorded. Tickets
// are issued after it, as the operator's are.
func fixtureApprovedAt() time.Time {
	return time.Now().Add(-2 * time.Second).UTC().Truncate(time.Second)
}

// ticketPatchFor renders the persona-shaped patch a ticket would carry for the
// change the remediation plans: what the operator's gate would have cleared on an
// unchanged cluster.
func ticketPatchFor(t *testing.T, rem *remediationFull) map[string]any {
	t.Helper()
	plan, err := buildHealPlan(rem)
	require.NoError(t, err)
	resources := map[string]any{}
	if plan.Change != nil {
		if len(plan.Change.Limits) > 0 {
			resources["limits"] = plan.Change.Limits
		}
		if len(plan.Change.Requests) > 0 {
			resources["requests"] = plan.Change.Requests
		}
	}
	return map[string]any{"spec": map[string]any{"resources": resources}}
}

// withTicket returns the RemediationAction fixture as the operator leaves it
// after the approval re-check: in phase, holding a valid ticket bound to the
// fixture Deployment.
func withTicket(t *testing.T, fixture, phase string, mutate ...func(ticket map[string]any)) string {
	t.Helper()
	var rem remediationFull
	require.NoError(t, json.Unmarshal([]byte(fixture), &rem))

	var doc map[string]any
	require.NoError(t, json.Unmarshal([]byte(fixture), &doc))
	status, _ := doc["status"].(map[string]any)
	if status == nil {
		status = map[string]any{}
	}
	status["phase"] = phase
	if _, ok := status["approvedAt"]; !ok {
		status["approvedAt"] = fixtureApprovedAt().Format(time.RFC3339)
	}
	container := "api-server"
	if rem.Spec.WorkloadRef != nil && rem.Spec.WorkloadRef.Container != "" {
		container = rem.Spec.WorkloadRef.Container
	}
	ticket := map[string]any{
		"workloadUID":             fixtureDeploymentUID,
		"container":               container,
		"patch":                   ticketPatchFor(t, &rem),
		"observedResourceVersion": fixtureDeploymentRV,
		"observedGeneration":      fixtureGeneration,
		"issuedAt":                time.Now().Add(-time.Second).UTC().Format(time.RFC3339Nano),
		"expiresAt":               time.Now().Add(10 * time.Minute).UTC().Format(time.RFC3339),
		"verdicts":                []any{map[string]any{"stage": "Guardrails", "guardrail": "blast-radius", "verdict": "pass"}},
	}
	for _, m := range mutate {
		m(ticket)
	}
	status["applyTicket"] = ticket
	doc["status"] = status
	out, err := json.Marshal(doc)
	require.NoError(t, err)
	return string(out)
}

// refusedAtApprovalFixture is the RemediationAction after the operator's gate
// refused the approval on live state.
func refusedAtApprovalFixture(t *testing.T, fixture, guardrail, message string) string {
	t.Helper()
	var doc map[string]any
	require.NoError(t, json.Unmarshal([]byte(fixture), &doc))
	doc["status"] = map[string]any{
		"phase": "Failed",
		"conditions": []any{
			map[string]any{"type": "Applied", "status": "False", "reason": "GateRefusedAtApproval", "message": "refused at approval; nothing was applied"},
			map[string]any{"type": "GateRefused", "status": "True", "reason": guardrail, "message": message},
		},
	}
	out, err := json.Marshal(doc)
	require.NoError(t, err)
	return string(out)
}

// statusPhase reads status.phase out of a fixture.
func statusPhase(fixture string) string {
	var doc struct {
		Status struct {
			Phase       string          `json:"phase"`
			ApplyTicket json.RawMessage `json:"applyTicket"`
		} `json:"status"`
	}
	if json.Unmarshal([]byte(fixture), &doc) != nil {
		return ""
	}
	return doc.Status.Phase
}

func hasTicket(fixture string) bool {
	var doc struct {
		Status struct {
			ApplyTicket json.RawMessage `json:"applyTicket"`
		} `json:"status"`
	}
	return json.Unmarshal([]byte(fixture), &doc) == nil && len(doc.Status.ApplyTicket) > 0 &&
		string(doc.Status.ApplyTicket) != "null"
}

// withIdentity gives every Deployment in a list (or the single object) a UID and
// resourceVersion if it has none, the way the API server always does.
func withIdentity(t *testing.T, raw string) string {
	t.Helper()
	if raw == "" {
		return raw
	}
	var doc map[string]any
	if json.Unmarshal([]byte(raw), &doc) != nil {
		return raw
	}
	stamp := func(obj map[string]any) {
		meta, _ := obj["metadata"].(map[string]any)
		if meta == nil {
			return
		}
		if _, ok := meta["uid"]; !ok {
			meta["uid"] = fixtureDeploymentUID
		}
		if _, ok := meta["resourceVersion"]; !ok {
			meta["resourceVersion"] = fixtureDeploymentRV
		}
		if _, ok := meta["generation"]; !ok {
			meta["generation"] = fixtureGeneration
		}
	}
	if items, ok := doc["items"].([]any); ok {
		for _, item := range items {
			if obj, ok := item.(map[string]any); ok {
				stamp(obj)
			}
		}
	} else {
		stamp(doc)
	}
	out, err := json.Marshal(doc)
	require.NoError(t, err)
	return string(out)
}

func jsonUnmarshal(s string, v any) error { return json.Unmarshal([]byte(s), v) }
