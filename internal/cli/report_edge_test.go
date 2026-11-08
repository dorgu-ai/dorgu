package cli

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReportClassifiesEveryTerminalPhase(t *testing.T) {
	cases := map[string]string{
		"Expired": excludedExpired, "Acknowledged": excludedAcknowledged, "RolledBack": excludedRolledBack,
		"Failed": excludedFailed, "Applying": excludedInFlight, "Verifying": excludedInFlight,
		"Ticketed": excludedInFlight, "": excludedInFlight,
	}
	for phase, want := range cases {
		ra := mustRA(t, raOpts{name: "x", phase: phase, created: time.Hour})
		got := buildReport(reportInputs{Remediations: []reportRA{ra}}, reportNow, reportFixtureWindow)
		assert.Equal(t, 1, got.HealAccounting.Excluded[want], "phase %q", phase)
		assert.Equal(t, 0, *got.HealAccounting.Clean)
	}
}

func TestReportTellsApprovalRefusalsFromApplyRefusals(t *testing.T) {
	approval := mustRA(t, raOpts{name: "a", phase: "Failed", created: time.Hour, gate: approvalGateRefused, conds: []string{
		cond("Applied", "False", "GateRefusedAtApproval", time.Hour), cond("GateRefused", "True", "BlastRadius", time.Hour)}})
	apply := mustRA(t, raOpts{name: "b", phase: "Failed", created: time.Hour, gate: approvalGatePass, conds: []string{
		cond("Applied", "False", "PreconditionRejected", time.Hour), cond("GateRefused", "True", "ObserveMode", time.Hour)}})
	noVerdict := mustRA(t, raOpts{name: "c", phase: "Failed", created: time.Hour, conds: []string{
		cond("Applied", "False", "GateRefusedAtApproval", time.Hour), cond("GateRefused", "True", "NoPlan", time.Hour)}})

	rows := buildReport(reportInputs{Remediations: []reportRA{approval, apply, noVerdict}}, reportNow, reportFixtureWindow).
		TierQ.Q6Refusals.Rows

	got := map[string]bool{}
	for _, r := range rows {
		got[r.Phase+"/"+r.Stage+"/"+r.Reason] = true
	}
	assert.Equal(t, map[string]bool{
		"approval/Guardrails/BlastRadius": true,
		"apply/Ticket/ObserveMode":        true,
		"approval/Unknown/NoPlan":         true,
	}, got, "the stage is the refusing verdict's; with none recorded it is Unknown, never invented")
}

func TestSplitProposalRefusal(t *testing.T) {
	stage, reason := splitProposalRefusal("DedupCooldown_RejectionCooldown")
	assert.Equal(t, "DedupCooldown", stage)
	assert.Equal(t, "RejectionCooldown", reason)

	stage, reason = splitProposalRefusal("Ground_Workload_Gone")
	assert.Equal(t, "Ground", stage, "split on the first underscore only")
	assert.Equal(t, "Workload_Gone", reason)

	for _, bad := range []string{"NoStage", "_Reason", "Stage_", ""} {
		stage, reason = splitProposalRefusal(bad)
		assert.Equal(t, reportStageUnknown, stage, bad)
		assert.Equal(t, bad, reason, bad)
	}
}

func TestReportKeepsARejectionReasonItDoesNotKnow(t *testing.T) {
	ra := mustRA(t, raOpts{name: "x", phase: "Rejected", created: time.Hour, conds: []string{
		cond("RejectionReason", "True", "FromANewerCLI", time.Hour)}})
	got := buildReport(reportInputs{Remediations: []reportRA{ra}}, reportNow, reportFixtureWindow)

	assert.Equal(t, 1, got.TierQ.Q2Precision.RejectionsByReason["unrecognised:FromANewerCLI"])
	assert.Equal(t, 0, got.TierQ.Q2Precision.Wrong, "an unknown reason is not assumed to mean wrong")
}

// An approved-then-rejected RemediationAction is one rejection, not an approval too.
func TestReportCountsARejectionAfterApprovalOnce(t *testing.T) {
	ra := mustRA(t, raOpts{name: "x", phase: "Rejected", created: 2 * time.Hour, approvedAt: time.Hour})
	got := buildReport(reportInputs{Remediations: []reportRA{ra}}, reportNow, reportFixtureWindow).TierA.A6Approval

	assert.Equal(t, 0, got.Approved)
	assert.Equal(t, 1, got.Rejected)
	assert.True(t, got.TooFewDecisions, "one decision is a count, not a band")
	assert.Empty(t, got.Band)
	assert.Nil(t, got.Rate)
}

func TestReportApprovalBands(t *testing.T) {
	mk := func(approved, rejected int) string {
		var ras []reportRA
		for i := 0; i < approved; i++ {
			ras = append(ras, mustRA(t, raOpts{name: "a", phase: "Pending", created: time.Hour, approvedAt: time.Minute}))
		}
		for i := 0; i < rejected; i++ {
			ras = append(ras, mustRA(t, raOpts{name: "r", phase: "Rejected", created: time.Hour}))
		}
		return buildReport(reportInputs{Remediations: ras}, reportNow, reportFixtureWindow).TierA.A6Approval.Band
	}
	assert.Equal(t, "below", mk(2, 8))
	assert.Equal(t, "in", mk(5, 5))
	assert.Equal(t, "above", mk(9, 1))
	assert.Equal(t, "in", mk(8, 2), "80% is the edge of the band, not over it")
	assert.Equal(t, "", mk(4, 0), "four decisions are too few to call a band")
	assert.Equal(t, "above", mk(5, 0), "five is the floor")
}

// F-12: below five decisions the report prints the counts and says it cannot judge,
// instead of "100.0% ... rubber-stamping" or "0.0% called wrong".
func TestReportTooFewDecisionsPrintsCountsNotVerdicts(t *testing.T) {
	ra := mustRA(t, raOpts{name: "a", phase: "Pending", created: time.Hour, approvedAt: time.Minute})
	var buf bytes.Buffer
	renderReportText(&buf, buildReport(reportInputs{Remediations: []reportRA{ra}}, reportNow, reportFixtureWindow))
	out := buf.String()

	assert.Contains(t, out, "1 approved, 0 rejected; too few decisions (n=1) to judge")
	assert.Contains(t, out, "0 of 1 decided proposals called wrong-diagnosis; too few decisions (n=1) to judge")
	assert.NotContains(t, out, "check for rubber-stamping")
	assert.NotContains(t, out, "100.0%")
	assert.NotContains(t, out, "0.0% of decided")
}

func TestReportEventOccurrencesAndTimes(t *testing.T) {
	series := mustEvent(t, `{"reason":"DorguCLINoTicket","eventTime":"`+ago(time.Hour)+`","series":{"count":4},"involvedObject":{"kind":"RemediationAction"}}`)
	counted := mustEvent(t, `{"reason":"DorguCLINoTicket","firstTimestamp":"`+ago(time.Hour)+`","count":2,"involvedObject":{"kind":"RemediationAction"}}`)
	bare := mustEvent(t, `{"reason":"DorguCLINoTicket","lastTimestamp":"`+ago(time.Hour)+`","involvedObject":{"kind":"RemediationAction"}}`)
	undated := mustEvent(t, `{"reason":"DorguCLINoTicket","involvedObject":{"kind":"RemediationAction"}}`)
	otherKind := mustEvent(t, `{"reason":"DorguCLINoTicket","lastTimestamp":"`+ago(time.Hour)+`","involvedObject":{"kind":"Pod"}}`)

	got := buildReport(reportInputs{EventsRead: true, Events: []reportEvent{series, counted, bare, undated, otherKind}},
		reportNow, reportFixtureWindow).CLIDeclines

	assert.Equal(t, 4+2+1, got.ByReason["DorguCLINoTicket"], "series count, event count, a lone event; an undated event and another kind are not counted")
}

func TestReportFlagsRefusalsOnlyWithEnoughProposals(t *testing.T) {
	rows := map[refusalKey]int{{reportPhaseProposal, "ModePolicy", "SelfHealingMode"}: 4}
	assert.False(t, refusalMix(9, rows, 0).Rows[0].Flagged, "9 proposals is under the sample floor")
	assert.True(t, refusalMix(10, rows, 0).Rows[0].Flagged, "4 of 10 is over 30%")
	assert.False(t, refusalMix(20, rows, 0).Rows[0].Flagged, "4 of 20 is 20%")
}

func TestReportGateClampsAreReadFromStatusGate(t *testing.T) {
	gate := `{"phase":"ApprovalRecheck","result":"Clamp","verdicts":[{"stage":"Guardrails","guardrail":"blast-radius","verdict":"Clamp"}]}`
	ra := mustRA(t, raOpts{name: "x", phase: "Completed", created: time.Hour, gate: gate})
	got := buildReport(reportInputs{Remediations: []reportRA{ra}}, reportNow, reportFixtureWindow)

	assert.Equal(t, map[string]int{"blast-radius": 1}, got.FirstRunClamps.GateClamps)
	assert.Equal(t, 0, got.FirstRunClamps.ProposalsWithClamp, "no step recorded a clamp at proposal")

	var buf bytes.Buffer
	renderReportText(&buf, got)
	assert.Contains(t, buf.String(), "clamps recorded by the approval-time gate: blast-radius 1")
}

func TestReportSkipsARemediationWithNoCreationTime(t *testing.T) {
	ra := mustRA(t, raOpts{name: "x", phase: "Completed", created: time.Hour})
	ra.Metadata.CreationTimestamp = ""
	got := buildReport(reportInputs{Remediations: []reportRA{ra}}, reportNow, reportFixtureWindow)
	assert.Equal(t, 0, got.HealAccounting.Total, "it cannot be placed in the window")
}

func TestSecondsText(t *testing.T) {
	assert.Equal(t, "45s", secondsText(45))
	assert.Equal(t, "12m05s", secondsText(12*60+5))
	assert.Equal(t, "3h20m", secondsText(3*3600+20*60))
	assert.Equal(t, "3d4h", secondsText(3*86400+4*3600))
}

func TestTextReportBandSentences(t *testing.T) {
	assert.Contains(t, bandSentence("below"), "noisy")
	assert.Contains(t, bandSentence("above"), "rubber-stamping")
	assert.Contains(t, bandSentence("in"), "inside")
}

func TestTextReportOfAnEmptyWindowSaysNoDataEverywhere(t *testing.T) {
	var buf bytes.Buffer
	renderReportText(&buf, buildReport(reportInputs{}, reportNow, reportFixtureWindow))
	out := buf.String()

	for _, want := range []string{
		"no data: no RemediationActions were created in the window",
		"A5 review depth:      no data",
		"A6 approval rate:     no data",
		"Q2 proposal precision: no data",
		"Q6 refusal mix:        no data",
		"no data (n=0)",
		"no data: the Events could not be read",
	} {
		assert.Contains(t, out, want)
	}
	assert.NotContains(t, out, "0.0%", "an empty window has no percentages")
}

func TestParseReportListRejectsGarbage(t *testing.T) {
	_, err := parseReportList[reportRA]([]byte("nope"))
	require.Error(t, err)
}
