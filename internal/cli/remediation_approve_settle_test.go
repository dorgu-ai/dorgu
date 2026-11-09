package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// R2-3 (B9 review): `approve` used to send a merge patch with no precondition and
// print "approved" straight away, before the operator had a chance to expire or
// supersede the action it had just been told to approve. These tests pin the
// replacement: the patch carries the resourceVersion the CLI read, a lost race is
// reported as such, and "approved" is printed only when a re-read after a short
// settle still shows the approval standing.

func withResourceVersion(doc, rv string) string {
	return strings.Replace(doc, `"namespace": "production",`, `"namespace": "production", "resourceVersion": "`+rv+`",`, 1)
}

func withApprovePhase(doc, phase string) string {
	return strings.Replace(doc, `"phase": "Pending"`, `"phase": "`+phase+`"`, 1)
}

func approveNoHeal(t *testing.T, r fakeKubectlResponses) approveRun {
	t.Helper()
	return runApprove(t, r, "--no-heal")
}

func TestApproveSendsTheResourceVersionAsAPrecondition(t *testing.T) {
	r := baseResponses()
	r.rem = withResourceVersion(operatorRemediationFixture, "4242")
	r.remAfterApprove = withApprovePhase(r.rem, "Approved")

	res := approveNoHeal(t, r)

	require.NoError(t, res.err)
	assert.Contains(t, res.patchLog, `"resourceVersion":"4242"`,
		"the approval must be an optimistic-lock patch on the version the CLI read")
	assert.Contains(t, res.out, "Remediation approved")
}

func TestApproveIsNotReportedWhenTheOperatorMovedTheActionOnAfterwards(t *testing.T) {
	for _, phase := range []string{"Superseded", "Expired"} {
		t.Run(phase, func(t *testing.T) {
			r := baseResponses()
			r.rem = withResourceVersion(operatorRemediationFixture, "4242")
			r.remAfterApprove = withApprovePhase(r.rem, phase)

			res := approveNoHeal(t, r)

			require.Error(t, res.err, "a withdrawn approval must exit non-zero")
			assert.NotContains(t, res.out, "Remediation approved")
			assert.NotContains(t, res.out, "Approval recorded")
			assert.Contains(t, res.out, phase, "the output says what the operator did")
			assert.Contains(t, res.out, "NOT approved")
		})
	}
}

func TestApproveWithHealAlsoDoesNotClaimApprovalWhenSuperseded(t *testing.T) {
	r := baseResponses()
	r.deployment = withIdentity(t, r.deployment)
	r.rem = withResourceVersion(operatorRemediationFixture, "4242")
	r.remAfterApprove = withApprovePhase(r.rem, "Superseded")

	res := runApprove(t, r)

	require.Error(t, res.err)
	assert.NotContains(t, res.out, "Remediation approved")
	assert.NotContains(t, res.patchLog, "patch deployment", "nothing is healed under a withdrawn approval")
}

func TestApproveLostRaceReportsWhatTheActionBecame(t *testing.T) {
	r := baseResponses()
	r.rem = withResourceVersion(operatorRemediationFixture, "4242")
	r.statusPatchError = `Error from server (Conflict): Operation cannot be fulfilled on remediationactions.dorgu.io "fix-oom-api-server": the object has been modified; please apply your changes to the latest version and try again`
	// Reads: 1 the preflight fetch, 2 the re-read after the conflict.
	r.remSequence = []string{r.rem, withResourceVersion(withApprovePhase(operatorRemediationFixture, "Superseded"), "4300")}

	res := approveNoHeal(t, r)

	require.Error(t, res.err)
	assert.NotContains(t, res.out, "Remediation approved")
	assert.Contains(t, res.out, "Superseded")
	assert.Contains(t, res.out, "NOT approved")
}

func TestApproveRetriesAConflictWhileTheActionIsStillPending(t *testing.T) {
	r := baseResponses()
	r.rem = withResourceVersion(operatorRemediationFixture, "4242")
	r.statusPatchError = `Error from server (Conflict): the object has been modified`
	r.remSequence = []string{r.rem} // every re-read: still Pending, and the write keeps conflicting

	res := approveNoHeal(t, r)

	require.Error(t, res.err, "a write that never lands is not an approval")
	assert.NotContains(t, res.out, "Remediation approved")
	assert.Greater(t, strings.Count(res.patchLog, "--subresource status"), 1, "a conflict on a still-Pending action is retried")
}

// B8 (1b): `heal` warns, as `approve` does, when no ticket arrived and the
// operator is older than v0.13.0.
func TestHealOnAPreTicketOperatorSaysWhy(t *testing.T) {
	operatorVersionReader = func(context.Context, string) *string { return strp("v0.12.1") }
	t.Cleanup(func() { operatorVersionReader = readOperatorVersion })
	r := baseResponses()
	r.rem = replacePhase(operatorRemediationFixture, "Approved")
	r.deployment = withIdentity(t, r.deployment)
	patchLog := writeFakeKubectlDispatch(t, r)

	cmd := newRemediationHealCmd()
	cmd.SetArgs([]string{"fix-oom-api-server", "-n", "production", "--yes"})
	var out strings.Builder
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	var err error
	stdout := captureStdout(t, func() { _ = captureStderr(t, func() { err = cmd.Execute() }) })

	require.Error(t, err)
	assert.NotContains(t, readPatchLog(t, patchLog), "patch deployment")
	assert.Contains(t, out.String()+stdout, "the operator on this cluster is v0.12.1")
	assert.Contains(t, out.String()+stdout, "does not issue apply tickets")
}
