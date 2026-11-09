package cli

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// B8 review (pr65). The operator never sets a phase "Superseded": a superseded
// proposal is phase Expired with the Expired condition reason Superseded
// (dorgu-operator internal/controller/incident_supersede.go:110-111, and the CRD phase
// enum has no Superseded). The R2-3 tests use a phase the operator does not have, so
// the `case "Superseded"` in printApprovalWithdrawn is dead code and, on a real cluster,
// a superseded approval is explained as a Pending time-to-live expiry.

func asRealSupersede(t *testing.T, doc string) string {
	t.Helper()
	var m map[string]any
	require.NoError(t, json.Unmarshal([]byte(doc), &m))
	status, _ := m["status"].(map[string]any)
	if status == nil {
		status = map[string]any{}
		m["status"] = status
	}
	status["phase"] = "Expired"
	conds, _ := status["conditions"].([]any)
	status["conditions"] = append(conds, map[string]any{
		"type": "Expired", "status": "True", "reason": "Superseded",
		"message":            "Superseded: the incident this proposal answered has become a worse one, so this advice no longer fits.",
		"lastTransitionTime": "2026-11-09T10:00:00Z",
	})
	out, err := json.Marshal(m)
	require.NoError(t, err)
	return string(out)
}

func TestReview_ARealSupersededApprovalIsNotExplainedAsATimeToLiveExpiry(t *testing.T) {
	r := baseResponses()
	r.rem = withResourceVersion(operatorRemediationFixture, "4242")
	r.remAfterApprove = asRealSupersede(t, r.rem)

	res := approveNoHeal(t, r)

	require.Error(t, res.err)
	assert.NotContains(t, res.out, "Remediation approved")
	assert.NotContains(t, res.out, "time-to-live",
		"the operator superseded this with a newer proposal; it did not time out")
	assert.Contains(t, res.out, "superseded")
}

// A teammate approving at the same moment is a conflict, but the action IS approved.
// The CLI prints "is NOT approved (phase Approved)", a sentence that contradicts itself.
func TestReview_AConflictWithAnotherApprovalDoesNotSayNotApproved(t *testing.T) {
	r := baseResponses()
	r.rem = withResourceVersion(operatorRemediationFixture, "4242")
	r.statusPatchError = `Error from server (Conflict): the object has been modified`
	r.remSequence = []string{r.rem, withResourceVersion(withApprovePhase(operatorRemediationFixture, "Approved"), "4300")}

	res := approveNoHeal(t, r)

	assert.NotContains(t, res.out, "is NOT approved (phase Approved)")
}

// expiredFor is what the operator writes: phase Expired and the Expired condition
// with one of its three reasons.
func expiredFor(t *testing.T, doc, reason string) string {
	t.Helper()
	out := asRealSupersede(t, doc)
	if reason == "Superseded" {
		return out
	}
	var m map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &m))
	conds := m["status"].(map[string]any)["conditions"].([]any)
	conds[len(conds)-1].(map[string]any)["reason"] = reason
	conds[len(conds)-1].(map[string]any)["message"] = "operator message for " + reason
	res, err := json.Marshal(m)
	require.NoError(t, err)
	return string(res)
}

// L-3: another actor approved at the same moment. The action is approved: say so, exit 0.
func TestConcurrentApprovalByAnotherActorIsSaidAndExitsZero(t *testing.T) {
	for name, phase := range map[string]string{"Approved": "Approved", "Ticketed": "Ticketed", "Applying": "Applying"} {
		t.Run(name, func(t *testing.T) {
			r := baseResponses()
			r.rem = withResourceVersion(operatorRemediationFixture, "4242")
			r.statusPatchError = `Error from server (Conflict): the object has been modified`
			after := withResourceVersion(withApprovePhase(operatorRemediationFixture, phase), "4300")
			if phase != "Approved" {
				after = withTicket(t, after, phase)
			}
			r.remSequence = []string{r.rem, after}

			res := approveNoHeal(t, r)

			require.NoError(t, res.err, "the action is approved")
			assert.Contains(t, res.out, "already approved by another actor")
			assert.NotContains(t, res.out, "NOT approved")
			assert.NotContains(t, res.out, "Remediation approved", "this command did not approve it")
		})
	}
}

// Applying with no ticket is an operator that issues none: not "approved" for our purposes.
func TestConcurrentApplyingWithoutATicketIsNotCalledApproved(t *testing.T) {
	r := baseResponses()
	r.rem = withResourceVersion(operatorRemediationFixture, "4242")
	r.statusPatchError = `Error from server (Conflict): the object has been modified`
	r.remSequence = []string{r.rem, withResourceVersion(withApprovePhase(operatorRemediationFixture, "Applying"), "4300")}

	res := approveNoHeal(t, r)

	require.Error(t, res.err)
	assert.NotContains(t, res.out, "already approved by another actor")
	assert.Contains(t, res.out, "without an apply ticket")
}

func TestConcurrentRejectionByAnotherActorIsSaid(t *testing.T) {
	r := baseResponses()
	r.rem = withResourceVersion(operatorRemediationFixture, "4242")
	r.statusPatchError = `Error from server (Conflict): the object has been modified`
	r.remSequence = []string{r.rem, withResourceVersion(withApprovePhase(operatorRemediationFixture, "Rejected"), "4300")}

	res := approveNoHeal(t, r)

	require.Error(t, res.err)
	assert.Contains(t, res.out, "rejected")
	assert.NotContains(t, res.out, "the operator moved it to Rejected")
}

// L-4: on a conflict the action is still Pending, but someone changed its spec since
// the user read it. The approval would land on a plan they never saw: refuse.
func TestConflictRetryRefusesWhenTheSpecChangedSinceTheUserReadIt(t *testing.T) {
	r := baseResponses()
	r.rem = withGeneration(withResourceVersion(operatorRemediationFixture, "4242"), 1)
	r.statusPatchError = `Error from server (Conflict): the object has been modified`
	r.remSequence = []string{r.rem, withGeneration(withResourceVersion(operatorRemediationFixture, "4300"), 2)}

	res := approveNoHeal(t, r)

	require.Error(t, res.err)
	assert.Contains(t, res.out, "changed since you read it")
	assert.Contains(t, res.out, "dorgu remediation diff")
	assert.Equal(t, 1, strings.Count(res.patchLog, "--subresource status"), "no second attempt on a plan the user did not see")
}

func TestConflictRetryStillRetriesWhenOnlyStatusMoved(t *testing.T) {
	r := baseResponses()
	r.rem = withGeneration(withResourceVersion(operatorRemediationFixture, "4242"), 1)
	r.statusPatchError = `Error from server (Conflict): the object has been modified`
	r.remSequence = []string{r.rem, withGeneration(withResourceVersion(operatorRemediationFixture, "4300"), 1)}

	res := approveNoHeal(t, r)

	require.Error(t, res.err, "the fake keeps conflicting")
	assert.Greater(t, strings.Count(res.patchLog, "--subresource status"), 1)
}

func withGeneration(doc string, gen int) string {
	return strings.Replace(doc, `"namespace": "production",`, fmt.Sprintf(`"namespace": "production", "generation": %d,`, gen), 1)
}
