package cli

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// C5 review F-2: the Deployment patch carries a uid and resourceVersion
// precondition, the RemediationAction ticket check is the last read before it, and
// a Conflict is retried a bounded number of times with everything re-read.

func deploymentPatchLines(patchLog string) []string {
	var out []string
	for _, line := range strings.Split(patchLog, "\n") {
		if strings.HasPrefix(line, "patch deployment") {
			out = append(out, line)
		}
	}
	return out
}

// patchBody extracts the -p argument (a JSON document) from a logged patch line.
func patchBody(t *testing.T, line string) map[string]any {
	t.Helper()
	i := strings.Index(line, " -p ")
	require.GreaterOrEqual(t, i, 0, line)
	var doc map[string]any
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(line[i+4:])), &doc), line)
	return doc
}

func ticketedApprove(t *testing.T, mutate func(*fakeKubectlResponses)) approveRun {
	t.Helper()
	r := baseResponses()
	r.deployment = withIdentity(t, r.deployment)
	r.remAfterApprove = withTicket(t, operatorRemediationFixture, "Applying")
	if mutate != nil {
		mutate(&r)
	}
	return runApprove(t, r)
}

func TestDeploymentPatchCarriesUIDAndResourceVersionPrecondition(t *testing.T) {
	res := ticketedApprove(t, nil)

	require.NoError(t, res.err)
	lines := deploymentPatchLines(res.patchLog)
	require.Len(t, lines, 1)
	meta, _ := patchBody(t, lines[0])["metadata"].(map[string]any)
	require.NotNil(t, meta, "the patch has no metadata precondition: %s", lines[0])
	assert.Equal(t, fixtureDeploymentUID, meta["uid"])
	assert.Equal(t, fixtureDeploymentRV, meta["resourceVersion"])
	assert.Contains(t, lines[0], `"memory":"512Mi"`, "the ticket's change is still what is applied")
}

func TestTheTicketCheckIsTheLastReadBeforeTheDeploymentPatch(t *testing.T) {
	res := ticketedApprove(t, nil)
	require.NoError(t, res.err)

	calls := strings.Split(strings.TrimSpace(readCallLog(t, res.logPath)), "\n")
	patchAt := -1
	for i, c := range calls {
		if strings.HasPrefix(c, "patch deployment") {
			patchAt = i
			break
		}
	}
	require.Greater(t, patchAt, 0)
	assert.True(t, strings.HasPrefix(calls[patchAt-1], "get remediationaction"),
		"the last read before the patch must be the RemediationAction, got %q", calls[patchAt-1])
}

func TestAConflictOnTheDeploymentPatchRereadsAndChecksAgainBeforeRetrying(t *testing.T) {
	res := ticketedApprove(t, func(r *fakeKubectlResponses) { r.deploymentPatchConflicts = 1 })

	require.NoError(t, res.err)
	require.Len(t, deploymentPatchLines(res.patchLog), 2, "one lost race, one retry")
	calls := readCallLog(t, res.logPath)
	first := strings.Index(calls, "patch deployment")
	second := strings.Index(calls[first+1:], "patch deployment") + first + 1
	between := calls[first:second]
	assert.Contains(t, between, "get remediationaction", "the ticket is checked again before the retry")
	assert.Contains(t, between, "get deployment", "the Deployment is read again before the retry")
}

func TestARetryNeverPatchesUnderATicketThatNoLongerHolds(t *testing.T) {
	good := withTicket(t, operatorRemediationFixture, "Applying")
	edited := strings.Replace(good, `"explanation":"`, `"explanation":"edited after the ticket: `, 1)
	require.NotEqual(t, good, edited)

	res := ticketedApprove(t, func(r *fakeKubectlResponses) {
		r.deploymentPatchConflicts = 1
		// reads: 1 preflight, 2 settle after the approval, 3 ticket poll, 4 check
		// before the first patch (good), 5 check before the retry (edited meanwhile)
		r.remSequence = []string{operatorRemediationFixture, good, good, good, edited}
	})

	require.Error(t, res.err)
	assert.Len(t, deploymentPatchLines(res.patchLog), 1, "the retry must not be written")
}

func TestTheConflictRetryIsBounded(t *testing.T) {
	res := ticketedApprove(t, func(r *fakeKubectlResponses) { r.deploymentPatchConflicts = 100 })

	require.Error(t, res.err)
	assert.Len(t, deploymentPatchLines(res.patchLog), patchAttempts)
	assert.Contains(t, res.err.Error()+res.out, "nothing was applied")
}

func TestARefusalThatIsNotAConflictIsNotRetried(t *testing.T) {
	res := ticketedApprove(t, func(r *fakeKubectlResponses) {
		r.deploymentPatchError = `Error from server (Invalid): metadata.uid: Invalid value: "uid-other": field is immutable`
	})

	require.Error(t, res.err)
	assert.Len(t, deploymentPatchLines(res.patchLog), 1)
}

func TestPreconditionRefusesWithoutAUIDOrResourceVersion(t *testing.T) {
	base := `{"spec":{"template":{"spec":{"containers":[{"name":"c"}]}}}}`
	for name, pre := range map[string]patchPrecondition{
		"no uid": {ResourceVersion: "1"}, "no resourceVersion": {UID: "u"}, "neither": {},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := withPrecondition(base, pre)
			require.Error(t, err)
		})
	}
	got, err := withPrecondition(base, patchPrecondition{UID: "u", ResourceVersion: "7"})
	require.NoError(t, err)
	assert.JSONEq(t, `{"metadata":{"uid":"u","resourceVersion":"7"},"spec":{"template":{"spec":{"containers":[{"name":"c"}]}}}}`, got)
}

// --- branches of the guard and the write ---

func TestPreconditionRefusesAnUnreadablePatch(t *testing.T) {
	_, err := withPrecondition("not json", patchPrecondition{UID: "u", ResourceVersion: "1"})

	require.Error(t, err)
}

func TestExecuteHealRefusesToRunWithoutAGuard(t *testing.T) {
	err := executeHeal(t.Context(), &remediationFull{}, &healExecution{}, healOptions{}, strings.NewReader(""), new(strings.Builder), nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "without a guard")
}

func TestTheGuardRefusesADeploymentWithNoResourceVersion(t *testing.T) {
	// A Deployment the API server served without a resourceVersion cannot be bound
	// to: the patch is refused rather than sent unbound.
	noRV := `{"items":[{"metadata":{"name":"api-server","uid":"` + fixtureDeploymentUID + `","generation":3},` +
		`"spec":{"template":{"spec":{"containers":[{"name":"api-server"}]}}}}]}`
	r := baseResponses()
	r.deployment = noRV
	r.remAfterApprove = withTicket(t, operatorRemediationFixture, "Applying")

	res := runApprove(t, r)

	require.Error(t, res.err)
	assert.Empty(t, deploymentPatchLines(res.patchLog))
	assert.Contains(t, res.out, "no resourceVersion")
}

func TestTheGuardRefusesARemediationWithoutATicket(t *testing.T) {
	_, err := reverifyBeforePatch(t.Context(), "", &remediationFull{}, &healExecution{}, fixtureUser)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no apply ticket")
}

func TestPhaseOrUnsetNamesAnEmptyPhase(t *testing.T) {
	assert.Equal(t, "(unset)", phaseOrUnset(""))
	assert.Equal(t, "Pending", phaseOrUnset("Pending"))
}

func TestCheckTicketV1RefusesAMissingTicketAndAMissingUser(t *testing.T) {
	var none remediationFull
	err := checkTicketV1(&none, fixtureUser, time.Now())
	var noTicket *noTicketError
	require.ErrorAs(t, err, &noTicket)

	rem := ticketedFixture(t)
	err = checkTicketV1(rem, "  ", time.Now())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no authenticated user")
}
