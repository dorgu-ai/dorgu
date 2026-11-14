package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// B6 re-review 2, NEW-3. A standing Guardrails_Concurrent is the incident's own
// fix only while that fix is in flight (Pending to Verifying), or once the incident
// is Resolved. An own RemediationAction that FINISHED, however long ago, and an
// incident resolution record other than Resolved (after a rollback the incident
// stays open and can be re-proposed) do not make a real refusal a deferral.
// These are the reviewer's probes.

func TestNEW3_AnOwnActionThatFinishedDoesNotHideARealConcurrentRefusal(t *testing.T) {
	for _, phase := range []string{"RolledBack", "Acknowledged", "Completed"} {
		t.Run(phase, func(t *testing.T) {
			in := concurrentRefused(t, raOpts{name: "ra-old", phase: phase, created: 40 * 24 * time.Hour})

			q6 := buildReportForTest(in, reportNow, reportFixtureWindow).TierQ.Q6Refusals

			assert.Len(t, q6.Rows, 1, "the incident was re-proposed after %s and refused because another fix holds the persona", phase)
			assert.Equal(t, 0, deferredCount(q6))
		})
	}
}

func TestNEW3_AResolutionRecordThatIsNotResolvedDoesNotHideARealConcurrentRefusal(t *testing.T) {
	for _, outcome := range []string{"rollback", "partial", "acknowledged"} {
		t.Run(outcome, func(t *testing.T) {
			in := reportInputs{Incidents: []reportIncident{mustIncident(t,
				incidentJSON("inc-1", 3*time.Hour, 3*time.Hour, outcome, cond("ProposalRefused", "True", "Guardrails_Concurrent", time.Hour)))}}

			q6 := buildReportForTest(in, reportNow, reportFixtureWindow).TierQ.Q6Refusals

			assert.Len(t, q6.Rows, 1, "an incident left open with outcome %s is not resolved", outcome)
			assert.Equal(t, 0, deferredCount(q6))
		})
	}
}

// resolvedConcurrentIncident is an incident the operator marked Resolved (status.phase
// and resolution.outcome, as it writes them) that still carries a standing
// Guardrails_Concurrent refusal.
func resolvedConcurrentIncident(t *testing.T) reportInputs {
	t.Helper()
	raw := strings.Replace(
		incidentJSON("inc-1", 3*time.Hour, 3*time.Hour, "resolved", cond("ProposalRefused", "True", "Guardrails_Concurrent", time.Hour)),
		`"status":{"conditions"`, `"status":{"phase":"Resolved","conditions"`, 1)
	return reportInputs{Incidents: []reportIncident{mustIncident(t, raw)}}
}

func TestNEW3_AnInFlightOwnActionAndAResolvedIncidentStillDefer(t *testing.T) {
	inFlight := concurrentRefused(t, raOpts{name: "ra-own", phase: "Pending", created: 2 * time.Hour})
	q6 := buildReportForTest(inFlight, reportNow, reportFixtureWindow).TierQ.Q6Refusals
	assert.Empty(t, q6.Rows)
	assert.Equal(t, 1, deferredCount(q6))

	q6 = buildReportForTest(resolvedConcurrentIncident(t), reportNow, reportFixtureWindow).TierQ.Q6Refusals
	assert.Empty(t, q6.Rows)
	assert.Equal(t, 1, deferredCount(q6))
}
