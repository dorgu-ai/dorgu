package cli

import (
	"context"
	"testing"
	"time"

	"github.com/dorgu-ai/dorgu-platform-v1/pkg/report"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// B6 fix round 2: N-1 (own in-flight RA is not a refusal), N-6 (version skew fails
// closed by evidence), N-7 (header wording).

func concurrentRefused(t *testing.T, ras ...raOpts) reportInputs {
	t.Helper()
	in := reportInputs{Incidents: []reportIncident{mustIncident(t,
		incidentJSON("inc-1", 3*time.Hour, 3*time.Hour, "", cond("ProposalRefused", "True", "Guardrails_Concurrent", time.Hour)))}}
	for _, o := range ras {
		o.incident = "inc-1"
		in.Remediations = append(in.Remediations, mustRA(t, o))
	}
	return in
}

func TestN1_AConcurrentRefusalWhileTheIncidentsOwnActionIsInFlightIsDeferred(t *testing.T) {
	for _, phase := range []string{"Pending", "Approved", "Ticketed", "Applying", "Verifying"} {
		t.Run(phase, func(t *testing.T) {
			in := concurrentRefused(t, raOpts{name: "ra-own", phase: phase, created: 2 * time.Hour})

			q6 := buildReportForTest(in, reportNow, reportFixtureWindow).TierQ.Q6Refusals

			assert.Empty(t, q6.Rows, "the incident's own fix is being worked: nothing was refused")
			assert.Equal(t, 1, deferredCount(q6))
			assert.Equal(t, 1, q6.Proposals, "the RemediationAction is counted once, the condition not again")
		})
	}
}

func TestN1_AConcurrentRefusalOnAResolvedIncidentIsDeferred(t *testing.T) {
	q6 := buildReportForTest(resolvedConcurrentIncident(t), reportNow, reportFixtureWindow).TierQ.Q6Refusals

	assert.Empty(t, q6.Rows)
	assert.Equal(t, 1, deferredCount(q6))
}

func TestN1_AConcurrentRefusalWithNoActionOfItsOwnIsStillARefusal(t *testing.T) {
	in := concurrentRefused(t) // another incident's fix holds the persona

	q6 := buildReportForTest(in, reportNow, reportFixtureWindow).TierQ.Q6Refusals

	require.Len(t, q6.Rows, 1)
	assert.Equal(t, "Guardrails", q6.Rows[0].Stage)
	assert.Equal(t, "Concurrent", q6.Rows[0].Reason)
	assert.Equal(t, 0, deferredCount(q6))
}

func TestN1_AFailedOrRejectedActionDoesNotExemptAConcurrentRefusal(t *testing.T) {
	for _, phase := range []string{"Failed", "Rejected", "Expired"} {
		in := concurrentRefused(t, raOpts{name: "ra-old", phase: phase, created: 2 * time.Hour})

		q6 := buildReportForTest(in, reportNow, reportFixtureWindow).TierQ.Q6Refusals

		assert.Len(t, q6.Rows, 1, "%s: the fix is not in flight, so the refusal is real", phase)
	}
}

// --- N-6: fail closed by evidence, not by tag --------------------------------------

func completedNoCredit(t *testing.T) reportRA {
	t.Helper()
	return mustRA(t, raOpts{name: "old-heal", phase: "Completed", created: 4 * time.Hour, approvedAt: 3 * time.Hour, incident: "inc-a"})
}

func TestN6_AnUnknownVersionWithCompletedActionsAndNoHealCreditedIsNoData(t *testing.T) {
	for name, version := range map[string]*string{
		"unreadable (nil: forbidden list, no label, digest-pinned)": nil,
		"latest":     strp("latest"),
		"a branch":   strp("main"),
		"empty":      strp(""),
		"not semver": strp("v0.13"),
	} {
		t.Run(name, func(t *testing.T) {
			in := reportInputs{Remediations: []reportRA{completedNoCredit(t)}, OperatorVersion: version}

			doc := buildReportForTest(in, reportNow, reportFixtureWindow)

			assert.True(t, doc.HealAccounting.NoData)
			assert.Contains(t, doc.HealAccounting.Reason, "could not be determined")
			assert.True(t, doc.TierA.NorthStar.NoData)
			assert.Nil(t, doc.TierA.NorthStar.SafeHeals, "not a measured zero")
			assert.Contains(t, renderText(t, in), "Safe heals: no data (the operator version could not be determined")
		})
	}
}

func TestN6_OneHealCreditedRecordAmongCompletedActionsMakesTheDataTrusted(t *testing.T) {
	for _, version := range []*string{nil, strp("latest")} {
		in := reportInputs{
			Remediations:    []reportRA{completedNoCredit(t), credited(t)},
			OperatorVersion: version,
			Incidents:       []reportIncident{mustIncident(t, incidentJSON("inc-a", 5*time.Hour, 5*time.Hour, "resolved"))},
		}

		doc := buildReportForTest(in, reportNow, reportFixtureWindow)

		assert.False(t, doc.HealAccounting.NoData, "an operator that writes HealCredited on one writes it on all")
		require.NotNil(t, doc.HealAccounting.Clean)
		assert.Equal(t, 1, *doc.HealAccounting.Clean)
	}
}

func TestN6_ANotCreditedRecordIsEvidenceToo(t *testing.T) {
	notCredited := mustRA(t, raOpts{name: "x", phase: "Completed", created: 4 * time.Hour,
		conds: []string{cond("HealCredited", "False", "NothingPatched", time.Hour)}})
	doc := buildReportForTest(reportInputs{Remediations: []reportRA{notCredited}}, reportNow, reportFixtureWindow)

	assert.False(t, doc.HealAccounting.NoData, "HealCredited=False is still a HealCredited record")
}

func TestN6_NoCompletedActionsMeansNothingToBlank(t *testing.T) {
	pending := mustRA(t, raOpts{name: "p", phase: "Pending", created: time.Hour})
	doc := buildReportForTest(reportInputs{Remediations: []reportRA{pending}}, reportNow, reportFixtureWindow)

	assert.False(t, doc.HealAccounting.NoData, "zero heals is true when nothing Completed")
	require.NotNil(t, doc.HealAccounting.Clean)
	assert.Equal(t, 0, *doc.HealAccounting.Clean)
}

func TestN6_AKnownNewVersionIsTrustedWithoutHealCredited(t *testing.T) {
	in := reportInputs{Remediations: []reportRA{completedNoCredit(t)}, OperatorVersion: strp("v0.13.0")}

	doc := buildReportForTest(in, reportNow, reportFixtureWindow)

	assert.False(t, doc.HealAccounting.NoData, "the tag is a fast path and it says 0.13+")
}

func TestN6_AKnownOldVersionIsNoDataWhateverTheRecordsSay(t *testing.T) {
	in := reportInputs{Remediations: []reportRA{credited(t)}, OperatorVersion: strp("v0.12.1")}

	assert.True(t, buildReportForTest(in, reportNow, reportFixtureWindow).HealAccounting.NoData)
}

func TestN6_TheNoTicketWarningNeverStaysSilentOnAnUnknownVersion(t *testing.T) {
	for name, tc := range map[string]struct {
		version *string
		want    string
		silent  bool
	}{
		"unreadable": {nil, "could not determine the operator version; an operator older than v0.13.0 issues no tickets", false},
		"latest":     {strp("latest"), "could not determine the operator version", false},
		"old":        {strp("v0.12.1"), "the operator on this cluster is v0.12.1", false},
		"new":        {strp("v0.13.0"), "", true},
	} {
		t.Run(name, func(t *testing.T) {
			operatorVersionReader = func(context.Context, string) *string { return tc.version }
			t.Cleanup(func() { operatorVersionReader = readOperatorVersion })
			r := declineBase(t)
			r.remAfterApprove = replacePhase(operatorRemediationFixture, "Approved")

			res := runApprove(t, r)

			require.Error(t, res.err)
			if tc.silent {
				assert.NotContains(t, res.out, "operator version")
				assert.NotContains(t, res.out, "the operator on this cluster")
				return
			}
			assert.Contains(t, res.out, tc.want)
		})
	}
}

// --- N-7 ------------------------------------------------------------------------------

func TestN7_UnreadablePersonasDoNotClaimASectionDependsOnThem(t *testing.T) {
	in := reportInputs{Errors: []report.InputError{{Input: "personas", Error: "forbidden"}, {Input: "cluster_personas", Error: "forbidden"}}}

	text := renderText(t, in)

	assert.NotContains(t, text, "every section computed from it")
	assert.Contains(t, text, "Unreadable: personas (forbidden): no number in this report depends on it")
	assert.Contains(t, text, "Unreadable: cluster_personas (forbidden): no number in this report depends on it")
}

func TestN7_UnreadableIncidentsStillSayWhichSectionsAreNoData(t *testing.T) {
	text := renderText(t, reportInputs{Errors: []report.InputError{{Input: "incidents", Error: "forbidden"}}})

	assert.Contains(t, text, "Unreadable: incidents (forbidden): the sections that need it say no data below, never zero")
}
