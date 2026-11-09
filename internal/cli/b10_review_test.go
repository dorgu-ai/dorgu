package cli

// Independent review test for CLI PR #66 (B10), head 879baf1. Fails on that head.
// Drop in internal/cli/ and run: go test ./internal/cli -run TestReviewB10 -count=1

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The operator (PR #62) adds a fourth Expired reason, IncidentResolved. The CLI explains the
// other three by name in withdrawnReason and gives every other reason a generic sentence. A
// person whose approval lost the race with the new sweep is told "the operator expired it"
// and not that the incident had resolved, the one fact that tells them the advice was stale.
func TestReviewB10_ApprovalLostToTheResolvedSweepIsExplained(t *testing.T) {
	r := baseResponses()
	r.rem = withResourceVersion(operatorRemediationFixture, "4242")
	r.remAfterApprove = expiredFor(t, r.rem, "IncidentResolved")

	res := approveNoHeal(t, r)

	require.Error(t, res.err)
	assert.Contains(t, res.out, "NOT approved")
	assert.Contains(t, res.out, "resolved",
		"the output should say the incident resolved before the approval landed; got the generic sentence")
}

// A fault that recurs on a reopened incident (the recurrence cap folds it into one incident)
// and is refused for the same reason again leaves the operator's record at its FIRST time
// (setCondition keeps the transition time while status and reason are unchanged), while the
// live ProposalRefused condition is True again with a fresh time. The report prefers the
// record, so the refusal that is standing today is dated 10 days ago and falls outside a
// 7 day window: a refusal that is on the incident now is not in Q6.
func TestReviewB10_ARefusalThatRecurredIsDatedByTheFirstOne(t *testing.T) {
	h := time.Hour
	in := reportInputs{Incidents: []reportIncident{
		mustIncident(t, incidentJSON("inc-capped", 10*24*h, 2*h, "",
			cond("ProposalRefused", "True", "Validate_StepCommand", 2*h),
			cond(refusalRecorded, "True", "Validate_StepCommand", 10*24*h))),
	}}

	q6 := buildReport(in, reportNow, reportFixtureWindow).TierQ.Q6Refusals

	require.Len(t, q6.Rows, 1, "the refusal standing on the incident 2 hours ago is inside the window")
}
