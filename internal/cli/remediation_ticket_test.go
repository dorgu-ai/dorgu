package cli

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Task 1.4: approve sets Approved, waits for the operator's apply ticket, and
// patches exactly the ticket. It refuses without one, refuses an expired one, and
// refuses when the operator's gate refused the approval.

type approveRun struct {
	err      error
	out      string
	patchLog string
}

func runApprove(t *testing.T, r fakeKubectlResponses, extraArgs ...string) approveRun {
	t.Helper()
	patchLog := writeFakeKubectlDispatch(t, r)
	cmd := newRemediationApproveCmd()
	cmd.SetArgs(append([]string{"fix-oom-api-server", "-n", "production", "--yes"}, extraArgs...))
	var out strings.Builder
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	var err error
	var stderr string
	stdout := captureStdout(t, func() {
		stderr = captureStderr(t, func() { err = cmd.Execute() })
	})
	return approveRun{err: err, out: out.String() + stdout + stderr, patchLog: readPatchLog(t, patchLog)}
}

func baseResponses() fakeKubectlResponses {
	return fakeKubectlResponses{
		context:    "kind-dorgu-spike",
		rem:        operatorRemediationFixture,
		persona:    personaFixture,
		deployment: deploymentListFixture,
		noTicket:   true,
	}
}

func TestApprovePatchesExactlyTheTicketNotThePreview(t *testing.T) {
	r := baseResponses()
	// The gate re-checked on live state and cleared 384Mi, not the 512Mi the spec
	// and the preview say.
	r.remAfterApprove = withTicket(t, operatorRemediationFixture, "Applying", func(ticket map[string]any) {
		ticket["patch"] = map[string]any{"spec": map[string]any{"resources": map[string]any{
			"limits": map[string]string{"memory": "384Mi"}}}}
	})
	r.deployment = withIdentity(t, r.deployment)

	res := runApprove(t, r)

	require.NoError(t, res.err)
	assert.Contains(t, res.patchLog, `"memory":"384Mi"`, "the ticket is what is applied")
	assert.NotContains(t, res.patchLog, `"memory":"512Mi"`, "the spec's value must not reach the workload")
	assert.Contains(t, res.out, "different change than the preview")
	assert.Less(t, strings.Index(res.patchLog, "--subresource status"), strings.Index(res.patchLog, "patch deployment"),
		"approval is written before the ticket exists to apply")
}

func TestApproveRefusesAnOperatorThatIssuesNoTickets(t *testing.T) {
	cases := map[string]string{
		// An old operator moves straight to Applying with nothing.
		"moved to Applying without a ticket": strings.Replace(operatorRemediationFixture, `"phase": "Pending"`, `"phase": "Applying"`, 1),
		// A stuck or absent operator leaves it Approved.
		"never answered": strings.Replace(operatorRemediationFixture, `"phase": "Pending"`, `"phase": "Approved"`, 1),
	}
	for name, after := range cases {
		t.Run(name, func(t *testing.T) {
			r := baseResponses()
			r.deployment = withIdentity(t, r.deployment)
			r.remAfterApprove = after

			res := runApprove(t, r)

			require.Error(t, res.err)
			assert.NotContains(t, res.patchLog, "patch deployment", "the CLI must never fall back to the pre-ticket patch")
			assert.Contains(t, res.out, "apply ticket")
			assert.Contains(t, res.out, "v0.13.0", "the refusal carries an upgrade hint")
		})
	}
}

func TestApproveRefusesAnExpiredTicket(t *testing.T) {
	r := baseResponses()
	r.deployment = withIdentity(t, r.deployment)
	r.remAfterApprove = withTicket(t, operatorRemediationFixture, "Applying", func(ticket map[string]any) {
		ticket["expiresAt"] = time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
	})

	res := runApprove(t, r)

	require.Error(t, res.err)
	assert.NotContains(t, res.patchLog, "patch deployment")
	assert.Contains(t, res.out, "expired")
}

func TestApproveShowsTheGatesPersistedRefusal(t *testing.T) {
	r := baseResponses()
	r.deployment = withIdentity(t, r.deployment)
	r.remAfterApprove = refusedAtApprovalFixture(t, operatorRemediationFixture, "BlastRadius",
		"512Mi is 8x the live 64Mi, over the 2x cap")

	res := runApprove(t, r)

	require.Error(t, res.err)
	assert.Equal(t, ExitDeclined, ExitCode(res.err), "a refusal by the gate is a decision, not a breakage")
	assert.Contains(t, res.out, "BlastRadius", "the persisted reason is shown")
	assert.Contains(t, res.out, "8x the live 64Mi")
	assert.Contains(t, res.out, "Nothing was applied")
	assert.NotContains(t, res.patchLog, "patch deployment")
}

func TestApproveRefusesATicketForAnotherWorkloadOrAStaleOne(t *testing.T) {
	cases := map[string]struct {
		mutate func(ticket map[string]any)
		want   string
	}{
		"another workload": {func(ticket map[string]any) { ticket["workloadUID"] = "uid-someone-else" }, "not the workload the ticket was issued for"},
		"changed since":    {func(ticket map[string]any) { ticket["observedResourceVersion"] = "99" }, "changed since the ticket"},
		"unbound":          {func(ticket map[string]any) { ticket["workloadUID"] = "" }, "not bound to a workload"},
		"no container":     {func(ticket map[string]any) { ticket["container"] = "" }, "not bound to a workload"},
		"other container":  {func(ticket map[string]any) { ticket["container"] = "sidecar" }, "ticket names"},
		"no patch":         {func(ticket map[string]any) { ticket["patch"] = map[string]any{} }, "no resource change"},
		"no expiry":        {func(ticket map[string]any) { ticket["expiresAt"] = "" }, "no valid expiry"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			r := baseResponses()
			r.deployment = withIdentity(t, r.deployment)
			r.remAfterApprove = withTicket(t, operatorRemediationFixture, "Applying", tc.mutate)

			res := runApprove(t, r)

			require.Error(t, res.err)
			assert.NotContains(t, res.patchLog, "patch deployment")
			assert.Contains(t, res.out, tc.want)
		})
	}
}

func TestApproveStillRefusesAProtectedContextBeforeAnything(t *testing.T) {
	r := baseResponses()
	r.context = "acme-prod-cluster"

	res := runApprove(t, r)

	require.Error(t, res.err)
	assert.Empty(t, res.patchLog, "a protected context refuses before approving or patching")
}

func TestApproveNoHealDoesNotWaitForATicket(t *testing.T) {
	r := baseResponses()

	res := runApprove(t, r, "--no-heal")

	require.NoError(t, res.err)
	assert.NotContains(t, res.patchLog, "patch deployment")
	assert.NotContains(t, res.out, "Waiting for the operator's apply ticket")
}

func TestHealWaitsForTheTicketOfAnApprovedRemediationAndRefusesWithoutOne(t *testing.T) {
	r := baseResponses()
	r.rem = strings.Replace(operatorRemediationFixture, `"phase": "Pending"`, `"phase": "Approved"`, 1)
	r.deployment = withIdentity(t, r.deployment)
	patchLog := writeFakeKubectlDispatch(t, r)

	cmd := newRemediationHealCmd()
	cmd.SetArgs([]string{"fix-oom-api-server", "-n", "production", "--yes"})
	var out strings.Builder
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)

	var err error
	stdout := captureStdout(t, func() {
		_ = captureStderr(t, func() { err = cmd.Execute() })
	})
	require.Error(t, err)
	assert.NotContains(t, readPatchLog(t, patchLog), "patch deployment")
	assert.Contains(t, out.String()+stdout, "apply ticket")
}

func TestTicketOutcome(t *testing.T) {
	ticketed := func(phase string) *remediationFull {
		var r remediationFull
		require.NoError(t, jsonUnmarshal(withTicket(t, operatorRemediationFixture, phase), &r))
		return &r
	}
	phased := func(phase string) *remediationFull {
		var r remediationFull
		require.NoError(t, jsonUnmarshal(strings.Replace(operatorRemediationFixture, `"phase": "Pending"`, `"phase": "`+phase+`"`, 1), &r))
		return &r
	}

	ready, err := ticketOutcome(ticketed("Ticketed"))
	assert.True(t, ready)
	assert.NoError(t, err)

	ready, err = ticketOutcome(phased("Pending"))
	assert.False(t, ready, "keep waiting")
	assert.NoError(t, err)

	ready, err = ticketOutcome(phased("Approved"))
	assert.False(t, ready)
	assert.NoError(t, err)

	for _, phase := range []string{"Applying", "Verifying", "Completed"} {
		_, err = ticketOutcome(phased(phase))
		assert.ErrorContains(t, err, "predates tickets", phase)
	}

	for _, phase := range []string{"Failed", "Expired", "Rejected", "RolledBack"} {
		_, err = ticketOutcome(phased(phase))
		assert.ErrorContains(t, err, "ended in phase "+phase)
	}

	_, err = ticketOutcome(phased("Acknowledged"))
	assert.ErrorContains(t, err, "advisory")
}

func TestPlainOutputPrintsTheWaitOnce(t *testing.T) {
	var out strings.Builder
	spin := newSpinner(&out, "Waiting for the operator's apply ticket")
	assert.True(t, spin.wait(t.Context(), time.Millisecond))
	spin.stop()
	assert.Equal(t, 1, strings.Count(out.String(), "Waiting for the operator's apply ticket"))
	assert.NotContains(t, out.String(), "\r", "no animation when stdout is not a terminal")
}

func TestTerminalSpinnerAnimatesAndClearsItsLine(t *testing.T) {
	var out strings.Builder
	spin := &spinner{w: &out, label: "Waiting", tty: true}
	assert.True(t, spin.wait(t.Context(), 250*time.Millisecond))
	spin.stop()
	assert.Contains(t, out.String(), "\r| Waiting")
	assert.Contains(t, out.String(), "\r/ Waiting", "the frame advances")
	assert.True(t, strings.HasSuffix(out.String(), "\r"), "stop clears the line")

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	assert.False(t, (&spinner{w: io.Discard, label: "x", tty: true}).wait(ctx, time.Second), "a cancelled context ends the wait")
	assert.False(t, (&spinner{w: io.Discard, label: "x"}).wait(ctx, time.Second))
}

func TestRefusalErrorsRenderTheirReason(t *testing.T) {
	refused := &gateRefusedError{reason: "BlastRadius", message: "too far"}
	assert.Contains(t, refused.Error(), "BlastRadius")
	assert.Contains(t, refused.Error(), "too far")

	var out strings.Builder
	printTicketRefusal(&out, refused)
	assert.Contains(t, out.String(), "Reason:  BlastRadius")
	assert.Contains(t, out.String(), "Detail:  too far")
}

func TestTicketOutcomeNamesTheApplyFailureWhenTheApprovalEndsWithoutAGateRefusal(t *testing.T) {
	var r remediationFull
	require.NoError(t, jsonUnmarshal(`{"status":{"phase":"Failed","conditions":[
		{"type":"Applied","status":"False","reason":"PreconditionRejected","message":"the apply ticket expired"}]}}`, &r))
	_, err := ticketOutcome(&r)
	assert.ErrorContains(t, err, "the apply ticket expired")
	assert.NotContains(t, err.Error(), "gate refused")
}

func TestContainsAndBindWithoutAWorkload(t *testing.T) {
	assert.True(t, contains([]string{"a", "b"}, "b"))
	assert.False(t, contains(nil, "b"))

	_, err := bindTicket(t.Context(), "", &applyTicket{}, &healExecution{}, time.Now())
	assert.ErrorContains(t, err, "no workload was resolved")
	_, err = bindTicket(t.Context(), "", nil, &healExecution{Deployment: "x"}, time.Now())
	assert.ErrorContains(t, err, "no apply ticket")
}
