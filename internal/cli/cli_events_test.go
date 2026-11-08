package cli

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Task 1.10: a CLI decline is written as a Kubernetes Event on the
// RemediationAction, so it outlives the terminal and can be counted.

func declineBase(t *testing.T) fakeKubectlResponses {
	t.Helper()
	r := baseResponses()
	r.deployment = withIdentity(t, r.deployment)
	return r
}

func onlyEvent(t *testing.T, patchLog string) map[string]any {
	t.Helper()
	events := readEventLog(t, patchLog)
	require.Len(t, events, 1, "exactly one decline Event")
	return events[0]
}

func assertDeclineEvent(t *testing.T, ev map[string]any, reason string) {
	t.Helper()
	assert.Equal(t, "Event", ev["kind"])
	assert.Equal(t, "Warning", ev["type"])
	assert.Equal(t, reason, ev["reason"])
	assert.Equal(t, "production", ev["metadata"].(map[string]any)["namespace"])
	involved := ev["involvedObject"].(map[string]any)
	assert.Equal(t, "RemediationAction", involved["kind"])
	assert.Equal(t, "fix-oom-api-server", involved["name"])
	assert.Equal(t, "dorgu.io/v1", involved["apiVersion"])
	assert.Equal(t, "dorgu-cli", ev["source"].(map[string]any)["component"])
	assert.NotEmpty(t, ev["message"], "a human sentence, never parsed")
}

func TestOwnedWorkloadDeclineWritesAnEvent(t *testing.T) {
	r := declineBase(t)
	r.deployment = liveDeployments("api-server", helmLabels, helmAnnotations)

	res := runApprove(t, r)

	require.Error(t, res.err)
	require.Equal(t, ExitDeclined, ExitCode(res.err))
	assertDeclineEvent(t, onlyEvent(t, res.logPath), "DorguCLIDeclined")
}

func TestHealOfAnOwnedWorkloadWritesAnEvent(t *testing.T) {
	patchLog := writeFakeKubectlDispatch(t, fakeKubectlResponses{
		context:    "kind-dorgu-spike",
		rem:        strings.Replace(operatorRemediationFixture, `"phase": "Pending"`, `"phase": "Approved"`, 1),
		persona:    personaFixture,
		deployment: liveDeployments("api-server", helmLabels, helmAnnotations),
	})
	cmd := newRemediationHealCmd()
	cmd.SetArgs([]string{"fix-oom-api-server", "-n", "production", "--yes"})
	var err error
	captureStdout(t, func() { captureStderr(t, func() { err = cmd.Execute() }) })

	require.Equal(t, ExitDeclined, ExitCode(err))
	assertDeclineEvent(t, onlyEvent(t, patchLog), "DorguCLIDeclined")
}

func TestGateRefusalAtApprovalExitingFourWritesAnEvent(t *testing.T) {
	r := declineBase(t)
	r.remAfterApprove = refusedAtApprovalFixture(t, operatorRemediationFixture, "BlastRadius", "too far")

	res := runApprove(t, r)

	require.Equal(t, ExitDeclined, ExitCode(res.err))
	ev := onlyEvent(t, res.logPath)
	assertDeclineEvent(t, ev, "DorguCLIDeclined")
	assert.Contains(t, ev["message"], "BlastRadius", "the enum the operator persisted")
}

func TestProtectedContextRefusalWritesAnEvent(t *testing.T) {
	r := declineBase(t)
	r.context = "acme-prod-cluster"

	res := runApprove(t, r)

	require.Error(t, res.err)
	assert.NotContains(t, res.patchLog, "--subresource status", "still refuses before approving")
	assertDeclineEvent(t, onlyEvent(t, res.logPath), "DorguCLIProtectedContext")
}

func TestProtectedContextRefusalOfHealWritesAnEvent(t *testing.T) {
	patchLog := writeFakeKubectlDispatch(t, fakeKubectlResponses{
		context:    "acme-prod-cluster",
		rem:        strings.Replace(operatorRemediationFixture, `"phase": "Pending"`, `"phase": "Approved"`, 1),
		persona:    personaFixture,
		deployment: deploymentListFixture,
	})
	cmd := newRemediationHealCmd()
	cmd.SetArgs([]string{"fix-oom-api-server", "-n", "production", "--yes"})
	var err error
	captureStdout(t, func() { captureStderr(t, func() { err = cmd.Execute() }) })

	require.Error(t, err)
	assertDeclineEvent(t, onlyEvent(t, patchLog), "DorguCLIProtectedContext")
}

func TestNoTicketRefusalWritesAnEvent(t *testing.T) {
	r := declineBase(t)
	r.remAfterApprove = strings.Replace(operatorRemediationFixture, `"phase": "Pending"`, `"phase": "Approved"`, 1)

	res := runApprove(t, r)

	require.Error(t, res.err)
	assert.NotContains(t, res.patchLog, "patch deployment")
	assertDeclineEvent(t, onlyEvent(t, res.logPath), "DorguCLINoTicket")
}

func TestExpiredTicketRefusalWritesAnEvent(t *testing.T) {
	r := declineBase(t)
	r.remAfterApprove = withTicket(t, operatorRemediationFixture, "Applying", func(ticket map[string]any) {
		ticket["expiresAt"] = time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
	})

	res := runApprove(t, r)

	require.Error(t, res.err)
	assert.NotContains(t, res.patchLog, "patch deployment")
	assertDeclineEvent(t, onlyEvent(t, res.logPath), "DorguCLITicketExpired")
}

func TestEveryDeclineErrorMapsToItsEventReason(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{&ownedWorkloadError{ref: &workloadRef{Name: "api", ManagedBy: "helm"}}, reasonCLIDeclined},
		{&protectedContextError{context: "prod", message: "refusing to heal"}, reasonCLIProtectedContext},
		{&noTicketError{why: "none"}, reasonCLINoTicket},
		{&ticketExpiredError{expiresAt: "2026-11-08T00:00:00Z"}, reasonCLITicketExpired},
		{&gateRefusedError{reason: "BlastRadius"}, reasonCLIDeclined},
	}
	for _, tc := range cases {
		reason, message, ok := declineFor(fmt.Errorf("wrapped: %w", tc.err))
		require.True(t, ok, tc.err)
		assert.Equal(t, tc.want, reason)
		assert.NotEmpty(t, message)
	}
	_, _, ok := declineFor(errors.New("a plain failure is not a decline"))
	assert.False(t, ok)
}

func TestDeclineEventFailureNeverChangesTheOutcome(t *testing.T) {
	r := declineBase(t)
	r.deployment = liveDeployments("api-server", helmLabels, helmAnnotations)
	r.failEventCreate = true

	res := runApprove(t, r)

	assert.Equal(t, ExitDeclined, ExitCode(res.err), "the exit code is the decline's, whether or not the Event was written")
	assert.Empty(t, readEventLog(t, res.logPath))
	assert.NotContains(t, res.out, "events is forbidden", "silent unless DORGU_DEBUG=1")
}

func TestDeclineEventFailureIsShownWithDebug(t *testing.T) {
	t.Setenv("DORGU_DEBUG", "1")
	r := declineBase(t)
	r.deployment = liveDeployments("api-server", helmLabels, helmAnnotations)
	r.failEventCreate = true

	res := runApprove(t, r)

	assert.Equal(t, ExitDeclined, ExitCode(res.err))
	assert.Contains(t, res.out, "could not record the decline as an Event")
}

func TestSuccessfulApproveWritesNoDeclineEvent(t *testing.T) {
	r := declineBase(t)
	r.remAfterApprove = withTicket(t, operatorRemediationFixture, "Applying")

	res := runApprove(t, r)

	require.NoError(t, res.err)
	assert.Empty(t, readEventLog(t, res.logPath))
}

func TestBuildDeclineEvent(t *testing.T) {
	ev := buildDeclineEvent("production", "fix-oom", "uid-1", reasonCLIProtectedContext, "refusing to heal", time.Date(2026, 11, 8, 1, 2, 3, 0, time.UTC))

	assert.Equal(t, "fix-oom.", ev.Metadata.GenerateName)
	assert.Equal(t, "uid-1", ev.InvolvedObject.UID)
	assert.Equal(t, "2026-11-08T01:02:03Z", ev.FirstTimestamp)
	assert.Equal(t, 1, ev.Count)

	long := buildDeclineEvent("n", "x", "", reasonCLIDeclined, strings.Repeat("a", 5000), time.Now())
	assert.LessOrEqual(t, len(long.Message), maxEventMessage)
	assert.Empty(t, long.InvolvedObject.UID, "an empty UID is omitted rather than invented")
}
