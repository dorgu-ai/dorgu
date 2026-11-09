package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// B6 fix round (CLI): the review's must-fix findings that are not covered by the
// adopted review tests in b6_review_test.go.

func renderText(t *testing.T, in reportInputs) string {
	t.Helper()
	var buf bytes.Buffer
	renderReportText(&buf, buildReport(in, reportNow, reportFixtureWindow))
	return buf.String()
}

// --- F-1: an outstanding proposal is not a refusal --------------------------

func TestFix_AStandingActiveRemediationConditionIsNotARefusal(t *testing.T) {
	// An operator before the fix wrote this on every incident one cycle after
	// proposing. The RemediationAction it is waiting on is counted once, as a proposal.
	standing := cond("ProposalRefused", "True", "DedupCooldown_ActiveRemediation", time.Hour)
	ra := mustRA(t, raOpts{name: "ra-1", phase: "Pending", created: 2 * time.Hour, incident: "inc-1"})
	in := reportInputs{
		Remediations: []reportRA{ra},
		Incidents:    []reportIncident{mustIncident(t, incidentJSON("inc-1", 3*time.Hour, 3*time.Hour, "", standing))},
	}

	q6 := buildReport(in, reportNow, reportFixtureWindow).TierQ.Q6Refusals

	assert.Equal(t, 1, q6.Proposals, "the proposal is counted once: the RemediationAction, not the incident condition too")
	assert.Empty(t, q6.Rows, "an outstanding proposal is not a refusal")
	assert.Equal(t, 1, deferredCount(q6))
	assert.Contains(t, renderText(t, in), "not refusals: 1 incident(s) with a proposal waiting for a decision")
}

func TestFix_ARealProposalRefusalIsStillCounted(t *testing.T) {
	refused := cond("ProposalRefused", "True", "ModePolicy_SelfHealingMode", time.Hour)
	in := reportInputs{Incidents: []reportIncident{mustIncident(t, incidentJSON("inc-1", 3*time.Hour, 3*time.Hour, "", refused))}}

	q6 := buildReport(in, reportNow, reportFixtureWindow).TierQ.Q6Refusals

	require.Len(t, q6.Rows, 1)
	assert.Equal(t, "ModePolicy", q6.Rows[0].Stage)
	assert.Equal(t, 0, deferredCount(q6))
}

// --- F-2: every mismatch decline is a bounded Event reason -----------------

func TestFix_EveryTicketMismatchErrorIsADeclineWithOneBoundedReason(t *testing.T) {
	for _, err := range []error{
		mismatch("the apply ticket carries no resource change"),
		&ticketMismatchError{err: errors.New("patch unreadable")},
		// reverifyBeforePatch and sameWorkloadAsTicket wrap what they return.
		errors.Join(errors.New("context"), mismatch("moved on")),
	} {
		reason, message, ok := declineFor(err)
		require.True(t, ok, err)
		assert.Equal(t, reasonCLITicketMismatch, reason)
		assert.NotEmpty(t, message)
	}
	cause := errors.New("root cause")
	assert.ErrorIs(t, &ticketMismatchError{err: cause}, cause, "the mismatch keeps its cause for errors.Is")
}

func TestFix_AnEventWriteFailureOnAMismatchChangesNothing(t *testing.T) {
	r := declineBase(t)
	r.remAfterApprove = withTicket(t, operatorRemediationFixture, "Applying", func(ticket map[string]any) {
		ticket["workloadUID"] = "uid-someone-else"
	})
	r.failEventCreate = true

	res := runApprove(t, r)

	require.Error(t, res.err)
	assert.NotContains(t, res.patchLog, "patch deployment")
}

// --- F-3: detect-to-proposal once per incident -------------------------------

func TestFix_AFirstProposalOlderThanTheWindowIsNotMeasuredFromALaterOne(t *testing.T) {
	h, m := time.Hour, time.Minute
	// The incident was detected 30 days ago; its first proposal is outside the
	// 7-day window; a retry two hours ago is inside it.
	first := raOpts{name: "ra-first", phase: "Failed", created: 30*24*h - m, incident: "inc-1"}
	retry := raOpts{name: "ra-retry", phase: "Pending", created: 2 * h, incident: "inc-1"}
	in := reportInputs{
		Remediations: []reportRA{mustRA(t, first), mustRA(t, retry)},
		Incidents:    []reportIncident{mustIncident(t, incidentJSON("inc-1", 30*24*h, 30*24*h, ""))},
	}

	got := buildReport(in, reportNow, reportFixtureWindow).TierQ.Q1TimeToHeal.DetectToProposal

	assert.Equal(t, 0, got.N, "the retry is not the incident's first proposal, and the first is outside the window")
	assert.True(t, got.NoData)
}

func TestFix_EachIncidentContributesItsOwnFirstProposal(t *testing.T) {
	h, m := time.Hour, time.Minute
	in := reportInputs{
		Remediations: []reportRA{
			mustRA(t, raOpts{name: "a", phase: "Pending", created: 5*h - m, incident: "i-a"}),
			mustRA(t, raOpts{name: "b", phase: "Pending", created: 4*h - 2*m, incident: "i-b"}),
		},
		Incidents: []reportIncident{
			mustIncident(t, incidentJSON("i-a", 5*h, 5*h, "")),
			mustIncident(t, incidentJSON("i-b", 4*h, 4*h, "")),
		},
	}

	got := buildReport(in, reportNow, reportFixtureWindow).TierQ.Q1TimeToHeal.DetectToProposal

	assert.Equal(t, 2, got.N)
	assert.InDelta(t, 120.0, *got.P90Seconds, 0.001)
}

// --- F-4: unreadable inputs are no data, with the error, in text and JSON ------

func credited(t *testing.T) reportRA {
	t.Helper()
	return mustRA(t, raOpts{name: "a", phase: "Completed", created: 4 * time.Hour, approvedAt: 3 * time.Hour,
		incident: "inc-a", gate: approvalGatePass,
		conds: []string{cond("HealCredited", "True", "WorkloadPatchedByTicket", time.Hour)}})
}

func TestFix_UnreadableIncidentsMakeEverythingThatNeedsThemNoData(t *testing.T) {
	in := reportInputs{
		Remediations: []reportRA{credited(t)},
		Errors:       []reportInputError{{Input: "incidents", Error: "forbidden"}},
	}

	doc := buildReport(in, reportNow, reportFixtureWindow)

	assert.True(t, doc.TierA.NorthStar.NoData)
	assert.Nil(t, doc.TierA.NorthStar.SafeHeals, "null, not 0")
	assert.Nil(t, doc.TierA.NorthStar.CleanHeal)
	assert.True(t, doc.HealAccounting.NoData)
	assert.Nil(t, doc.HealAccounting.Clean)
	assert.Empty(t, doc.HealAccounting.Excluded)
	assert.Equal(t, 1, doc.HealAccounting.Total, "the RemediationActions themselves were read")
	assert.True(t, doc.TierQ.Q6Refusals.NoData)
	assert.NotEmpty(t, doc.TierQ.Q6Refusals.Reason)
	assert.True(t, doc.TierQ.Q1TimeToHeal.DetectToProposal.NoData)
	assert.NotEmpty(t, doc.TierQ.Q1TimeToHeal.DetectToProposal.Reason)
	assert.False(t, doc.TierQ.Q1TimeToHeal.ProposalToDecision.NoData, "this one needs no incident")
	assert.Equal(t, []reportInputError{{Input: "incidents", Error: "forbidden"}}, doc.InputErrors)

	out := renderText(t, in)
	assert.Contains(t, out, "Safe heals: no data (")
	assert.Contains(t, out, "Unreadable: incidents (forbidden)")
	assert.NotContains(t, out, "Safe heals: 0")

	raw, err := json.Marshal(doc)
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"safe_heals":null`)
	assert.Contains(t, string(raw), `"input_errors":[{"input":"incidents","error":"forbidden"}]`)
}

func TestFix_UnreadableEventsMakeTheDeclinesNoDataWithTheReason(t *testing.T) {
	in := reportInputs{Errors: []reportInputError{{Input: "events", Error: "timeout"}}}

	doc := buildReport(in, reportNow, reportFixtureWindow)

	assert.True(t, doc.CLIDeclines.NoData)
	assert.Equal(t, "the Events could not be read", doc.CLIDeclines.Reason)
	assert.Contains(t, renderText(t, in), "no data: the Events could not be read")
}

func TestFix_AllReadableInputsCarryAnEmptyErrorList(t *testing.T) {
	doc := buildReport(reportInputs{}, reportNow, reportFixtureWindow)

	require.NotNil(t, doc.InputErrors, "an empty array in the JSON, not null")
	assert.Empty(t, doc.InputErrors)
}

func TestFix_ErrorClassesAreBounded(t *testing.T) {
	for msg, want := range map[string]string{
		`Error from server (Forbidden): incidentmemories is forbidden`: "forbidden",
		`the server doesn't have a resource type "incidentmemory"`:     "not_found",
		`context deadline exceeded`:                                    "timeout",
		`could not parse events: invalid character`:                    "unparseable",
		`connection refused`:                                           "error",
	} {
		assert.Equal(t, want, errorClass(msg), msg)
	}
}

func TestFix_ClusterPersonasThatCannotBeReadAreNamedNotSilentlyEmpty(t *testing.T) {
	installFakeReportKubectl(t, fakeClusterReads{
		remediations: list(recentRA("a", "Pending", 3, ``)),
		incidents:    list(), personas: list(), events: list(),
		// clusterpersonas empty file = the fake's Forbidden
	})

	out, _, err := runReportCmd(t, true)
	require.NoError(t, err)

	var doc reportDoc
	require.NoError(t, json.Unmarshal([]byte(out), &doc))
	assert.False(t, doc.Sources.ClusterPersonas.OK)
	inputs := []string{}
	for _, e := range doc.InputErrors {
		inputs = append(inputs, e.Input)
	}
	assert.Contains(t, inputs, "cluster_personas")
}

// --- F-5 (report side) ---------------------------------------------------------

func TestFix_TheReportSaysProtectedContextDeclinesAreLocalOnly(t *testing.T) {
	note := buildReport(reportInputs{EventsRead: true}, reportNow, reportFixtureWindow).CLIDeclines.Note

	assert.Contains(t, note, "local only")
	assert.Contains(t, note, "never counted")
}

// --- F-7: the final reject enum ------------------------------------------------

func TestFix_Q2CountsOnlyWrongDiagnosisAsWrong(t *testing.T) {
	var ras []reportRA
	for i, reason := range []string{"WrongDiagnosis", "TooRisky", "NotNow", "Duplicate", "NotNeeded", "Other"} {
		ras = append(ras, mustRA(t, raOpts{name: "r" + string(rune('a'+i)), phase: "Rejected", created: time.Hour,
			conds: []string{cond("RejectionReason", "True", reason, time.Minute)}}))
	}
	ras = append(ras, mustRA(t, raOpts{name: "ok", phase: "Pending", created: time.Hour, approvedAt: time.Minute}))

	q2 := buildReport(reportInputs{Remediations: ras}, reportNow, reportFixtureWindow).TierQ.Q2Precision

	assert.Equal(t, 7, q2.Decided, "the denominator is every decided proposal")
	assert.Equal(t, 1, q2.Wrong, "only wrong-diagnosis calls the proposal wrong")
	assert.Equal(t, map[string]int{"wrong-diagnosis": 1, "too-risky": 1, "not-now": 1, "duplicate": 1, "not-needed": 1, "other": 1,
		reportNoReasonGiven: 0}, q2.RejectionsByReason)
}

func TestFix_AStoredWrongActionReadsAsOtherNotAsWrong(t *testing.T) {
	ras := []reportRA{mustRA(t, raOpts{name: "old", phase: "Rejected", created: time.Hour,
		conds: []string{cond("RejectionReason", "True", "WrongAction", time.Minute)}})}

	q2 := buildReport(reportInputs{Remediations: ras}, reportNow, reportFixtureWindow).TierQ.Q2Precision

	assert.Equal(t, 0, q2.Wrong)
	assert.Equal(t, 1, q2.RejectionsByReason["other"])
}

// --- F-17: an operator before v0.13.0 -------------------------------------------

func strp(s string) *string { return &s }

func TestFix_OperatorVersionComparison(t *testing.T) {
	for tag, want := range map[string]bool{
		"v0.12.1": true, "0.12.1": true, "v0.9.0": true, "v0.13.0": false, "v0.13.1": false, "v0.14.0": false,
		"v1.0.0": false, "v0.13.0-rc1": false, "v0.12.9-rc1": true,
		"dev": false, "latest": false, "": false, "v0.13": false, "vx.y.z": false,
	} {
		assert.Equal(t, want, operatorPredatesTickets(strp(tag)), tag)
	}
	assert.False(t, operatorPredatesTickets(nil), "an unreadable version is never blamed")
}

func TestFix_AnOldOperatorMakesTheHealSectionsNoDataNotZero(t *testing.T) {
	in := reportInputs{Remediations: []reportRA{credited(t)}, OperatorVersion: strp("v0.12.1")}

	doc := buildReport(in, reportNow, reportFixtureWindow)

	assert.True(t, doc.HealAccounting.NoData)
	assert.Contains(t, doc.HealAccounting.Reason, "v0.12.1")
	assert.Contains(t, doc.HealAccounting.Reason, reportTicketOperator)
	assert.True(t, doc.TierA.NorthStar.NoData)
	assert.Nil(t, doc.TierA.NorthStar.SafeHeals)
	assert.False(t, doc.TierQ.Q6Refusals.NoData, "refusals do not depend on tickets")
	assert.Contains(t, renderText(t, in), "Safe heals: no data (the operator (v0.12.1) predates v0.13.0")
}

func TestFix_AnUnknownOperatorVersionIsNotMarked(t *testing.T) {
	for _, v := range []*string{nil, strp("dev"), strp("")} {
		doc := buildReport(reportInputs{Remediations: []reportRA{credited(t)}, OperatorVersion: v,
			Incidents: []reportIncident{mustIncident(t, incidentJSON("inc-a", 5*time.Hour, 5*time.Hour, "resolved"))}},
			reportNow, reportFixtureWindow)
		assert.False(t, doc.HealAccounting.NoData)
		require.NotNil(t, doc.HealAccounting.Clean)
		assert.Equal(t, 1, *doc.HealAccounting.Clean)
	}
}

func TestFix_NoTicketOnAnOldOperatorSaysWhy(t *testing.T) {
	operatorVersionReader = func(context.Context, string) *string { return strp("v0.12.1") }
	t.Cleanup(func() { operatorVersionReader = readOperatorVersion })
	r := declineBase(t)
	r.remAfterApprove = replacePhase(operatorRemediationFixture, "Approved")

	res := runApprove(t, r)

	require.Error(t, res.err)
	assert.Contains(t, res.out, "the operator on this cluster is v0.12.1")
	assert.Contains(t, res.out, "does not issue apply tickets")
}

// --- F-12 to F-14 ------------------------------------------------------------------

func TestFix_AFoldedEventSeriesThatBeganBeforeTheWindowCountsOnce(t *testing.T) {
	// 6 occurrences since before the window began, the last one an hour ago: only
	// the last occurrence is known to be inside it.
	folded := mustEvent(t, `{"reason":"DorguCLINoTicket","firstTimestamp":"`+ago(reportFixtureWindow+5*time.Hour)+`","lastTimestamp":"`+
		ago(time.Hour)+`","count":6,"involvedObject":{"kind":"RemediationAction"}}`)
	inside := mustEvent(t, `{"reason":"DorguCLINoTicket","firstTimestamp":"`+ago(2*time.Hour)+`","lastTimestamp":"`+
		ago(time.Hour)+`","count":3,"involvedObject":{"kind":"RemediationAction"}}`)

	got := buildReport(reportInputs{EventsRead: true, Events: []reportEvent{folded, inside}},
		reportNow, reportFixtureWindow).CLIDeclines

	assert.Equal(t, 1+3, got.ByReason["DorguCLINoTicket"])
}

func TestFix_TheTextStatesTheWindowBasis(t *testing.T) {
	out := renderText(t, reportInputs{})

	assert.Contains(t, out, "count by when they were created")
}

func TestFix_A2SaysInTheWindowNotThisWeek(t *testing.T) {
	out := renderText(t, reportInputs{})

	assert.Contains(t, out, "proposals decided in the window")
	assert.NotContains(t, out, "this week")
}

func replacePhase(doc, phase string) string {
	return string(bytes.Replace([]byte(doc), []byte(`"phase": "Pending"`), []byte(`"phase": "`+phase+`"`), 1))
}
