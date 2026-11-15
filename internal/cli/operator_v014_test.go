package cli

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// C5 review F-3: this CLI heals only under ticket v1, which operator v0.14
// introduced. Against an older operator it refuses before it records an approval,
// and says to upgrade the operator.

func withOperatorVersion(t *testing.T, version *string) {
	t.Helper()
	operatorVersionReader = func(context.Context, string) *string { return version }
	t.Cleanup(func() { operatorVersionReader = readOperatorVersion })
}

func TestApproveOnAnOperatorOlderThanV014RefusesBeforeRecordingTheApproval(t *testing.T) {
	for _, version := range []string{"v0.13.2", "v0.13.0", "v0.12.1", "0.13.9"} {
		t.Run(version, func(t *testing.T) {
			withOperatorVersion(t, strp(version))
			r := baseResponses()
			r.deployment = withIdentity(t, r.deployment)

			res := runApprove(t, r)

			require.Error(t, res.err)
			assert.Empty(t, res.patchLog, "no approval and no patch is written")
			assert.Contains(t, res.out, needsOperatorV014)
			assert.Contains(t, res.out, version, "it says which operator it found")
			assert.Contains(t, res.out, "Nothing was approved")
			assert.Contains(t, res.out, "--no-heal", "the way to record the decision alone is named")
			assert.NotContains(t, res.out, "—")
			assert.Contains(t, res.out, "approve --no-heal", "the command that records the decision alone is named")
			assert.Contains(t, res.out, "dorgu remediation heal", "heal can apply it once the operator is upgraded")
			assert.Contains(t, res.out, "upgraded")
		})
	}
}

// C5b R-3: only a plain release tag is a version. A dev build, a git-describe or
// pseudo-version tag and any pre-release is unknown (never "older"), so the ticket
// itself settles it later.
func TestOperatorVersionDetectionTable(t *testing.T) {
	for _, tc := range []struct {
		tag     *string
		predate bool
		known   bool
	}{
		{strp("v0.12.1"), true, true},
		{strp("v0.13.0"), true, true},
		{strp("v0.13.9"), true, true},
		{strp("0.13.9"), true, true},
		{strp("v0.9.0"), true, true},
		{strp("v0.14.0"), false, true},
		{strp("v0.14.1"), false, true},
		{strp("v0.15.0"), false, true},
		{strp("v0.100.0"), false, true},
		{strp("v1.0.0"), false, true},
		// unknown: not a plain release
		{strp("v0.13.0-5-gabcdef"), false, false},
		{strp("v0.13.0-5-gabcdef0"), false, false},
		{strp("v0.13.2-0.20261114233415-ee08af450eaa"), false, false},
		{strp("v0.13.0+build5"), false, false},
		{strp("v0.13.0-rc.1"), false, false},
		{strp("v0.13.0-alpha"), false, false},
		{strp("v0.12.1-beta.2"), false, false},
		{strp("v0.14.0-rc.1"), false, false},
		{strp("v0.14.0-dirty"), false, false},
		{strp("dev"), false, false},
		{strp("dev-9fd7079"), false, false},
		{strp("main-ee08af4"), false, false},
		{strp("latest"), false, false},
		{strp("v0.13"), false, false},
		{strp("V0.13.0"), false, false},
		{strp(""), false, false},
		{nil, false, false},
	} {
		name := "nil"
		if tc.tag != nil {
			name = *tc.tag
		}
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.predate, operatorPredatesTicketV1(tc.tag), "predates v0.14.0")
			assert.Equal(t, tc.known, operatorVersionKnown(tc.tag), "is a release version")
		})
	}
}

func TestApproveOnADevOrPrereleaseOperatorIsNotRefusedAsOld(t *testing.T) {
	for _, version := range []string{
		"v0.13.0-5-gabcdef", "v0.13.2-0.20261114233415-ee08af450eaa", "v0.13.0+build5", "v0.13.0-rc.1", "dev-9fd7079",
	} {
		t.Run(version, func(t *testing.T) {
			withOperatorVersion(t, strp(version))
			r := baseResponses()
			r.deployment = withIdentity(t, r.deployment)

			res := runApprove(t, r)

			assert.NotContains(t, res.out, "Nothing was approved", "an unknown tag is settled by the ticket, not refused up front")
			assert.NotContains(t, res.out, "Cannot heal: "+needsOperatorV014)
		})
	}
}

func TestApproveNoHealOnAnOperatorOlderThanV014StillRecordsTheDecision(t *testing.T) {
	withOperatorVersion(t, strp("v0.13.2"))
	r := baseResponses()
	r.rem = withResourceVersion(operatorRemediationFixture, "4242")
	r.remAfterApprove = withApprovePhase(r.rem, "Approved")

	res := approveNoHeal(t, r)

	require.NoError(t, res.err)
	assert.Contains(t, res.patchLog, "--subresource status")
}

func TestApproveProceedsOnV014AndOnAnUnknownOperatorVersion(t *testing.T) {
	for name, version := range map[string]*string{
		"v0.14.0": strp("v0.14.0"), "a later one": strp("v0.15.1"), "a release candidate": strp("v0.14.0-rc.1"),
		"unreadable": nil, "latest": strp("latest"), "a dev build": strp("dev-9fd7079"),
	} {
		t.Run(name, func(t *testing.T) {
			withOperatorVersion(t, version)
			r := baseResponses()
			r.deployment = withIdentity(t, r.deployment)
			r.remAfterApprove = withTicket(t, operatorRemediationFixture, "Applying")

			res := runApprove(t, r)

			require.NoError(t, res.err)
			assert.Contains(t, res.patchLog, "patch deployment")
		})
	}
}

// A ticket with none of the v1 fields is a v0 ticket: the operator is old even if
// its version could not be read.
func TestATicketWithNoV1FieldsMeansTheOperatorMustBeUpgraded(t *testing.T) {
	v0 := func(ticket map[string]any) {
		delete(ticket, "specHash")
		delete(ticket, "approvedBy")
		delete(ticket, "expiresAt")
	}
	t.Run("through approve", func(t *testing.T) {
		r := baseResponses()
		r.deployment = withIdentity(t, r.deployment)
		r.remAfterApprove = withTicket(t, operatorRemediationFixture, "Applying", v0)

		res := runApprove(t, r)

		require.Error(t, res.err)
		assert.NotContains(t, res.patchLog, "patch deployment")
		assert.Contains(t, res.out, needsOperatorV014)
		assert.Contains(t, res.out, "approval is recorded")
		assert.NotContains(t, res.out, "was not written by the operator")
	})
	t.Run("through heal", func(t *testing.T) {
		r := baseResponses()
		r.deployment = withIdentity(t, r.deployment)
		r.rem = withTicket(t, replacePhase(operatorRemediationFixture, "Approved"), "Ticketed", v0)
		patchLog := writeFakeKubectlDispatch(t, r)

		cmd := newRemediationHealCmd()
		cmd.SetArgs([]string{"fix-oom-api-server", "-n", "production", "--yes"})
		var out strings.Builder
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		var err error
		var stderr string
		stdout := captureStdout(t, func() { stderr = captureStderr(t, func() { err = cmd.Execute() }) })

		require.Error(t, err)
		assert.NotContains(t, readPatchLog(t, patchLog), "patch deployment")
		assert.Contains(t, out.String()+stdout+stderr, needsOperatorV014)
	})
}

func TestACheckTicketV1WithOnlySomeV1FieldsKeepsTheSpecificMessage(t *testing.T) {
	rem := ticketedFixture(t, func(m map[string]any) { delete(m, "specHash") })

	err := checkTicketV1(rem, fixtureUser, time.Now())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "specHash")
	assert.NotContains(t, err.Error(), needsOperatorV014)
}

func TestApproveHelpNamesTheOperatorVersionAndExitCode4(t *testing.T) {
	long := newRemediationApproveCmd().Long

	assert.Contains(t, long, "v0.14.0 or newer")
	assert.NotContains(t, long, "v0.13.0 or newer")
	assert.Contains(t, long, "operator older than v0.14")
	assert.Contains(t, long, "Exit code 4 means declined")
	assert.NotContains(t, long, "—")
}

func TestRejectHelpNamesExitCode4(t *testing.T) {
	long := newRemediationRejectCmd().Long

	assert.Contains(t, long, "Exit code 4 means declined")
}
