package cli

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dorgu-ai/dorgu-platform-v1/pkg/report"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var updateGolden = flag.Bool("update", false, "rewrite the report golden files")

func fixtureReport(t *testing.T) report.Doc {
	t.Helper()
	doc := buildReportForTest(reportFixture(t), reportNow, reportFixtureWindow)
	version := "v0.13.0"
	personas := 3
	doc.Cluster = report.Cluster{Context: "kind-demo", Namespace: "all", OperatorVersion: &version,
		Personas: &personas, SelfHealing: []string{"propose"}}
	doc.Sources = report.Sources{
		RemediationActions: report.Source{OK: true, Count: 13},
		Incidents:          report.Source{OK: true, Count: 10},
		Personas:           report.Source{OK: true, Count: 3},
		ClusterPersonas:    report.Source{OK: true, Count: 1},
		Events:             report.Source{OK: true, Count: 7},
	}
	return doc
}

func TestReportCountsOnlyTheWindow(t *testing.T) {
	doc := fixtureReport(t)

	assert.Equal(t, report.Schema, doc.Schema)
	assert.Equal(t, ago(reportFixtureWindow), doc.Window.Start)
	assert.Equal(t, ago(0), doc.Window.End)
	assert.Equal(t, 12, doc.HealAccounting.Total, "ra-old is 10 days old and must not be counted")
}

// KPIS.md: a clean heal excludes partial, self-resolved and HealCredited=False.
func TestReportCleanHealAccountingFollowsKPIs(t *testing.T) {
	h := doc(t).HealAccounting

	assert.Equal(t, 4, h.Completed)
	assert.Equal(t, 1, *h.Clean, "only ra-clean is HealCredited=True with incident outcome resolved")
	assert.Equal(t, 1, *h.SafeHeals)
	assert.Equal(t, map[string]int{
		report.ExcludedPartial:           1,
		report.ExcludedSelfResolved:      2,
		report.ExcludedRolledBack:        1,
		report.ExcludedApprovedNotHealed: 0,
		report.ExcludedFailed:            1,
		report.ExcludedRefusedAtGate:     2,
		report.ExcludedRejected:          3,
		report.ExcludedInFlight:          1,
		report.ExcludedHealNotCredited:   0,
		report.ExcludedNoCreditRecord:    0,
		report.ExcludedOutcomeUnknown:    0,
		report.ExcludedExpired:           0,
		report.ExcludedAcknowledged:      0,
	}, h.Excluded)
	assert.Equal(t, map[string]int{"NothingPatched": 1, "ResourcesNotTicket": 1}, h.HealNotCredited)

	total := *h.Clean
	for _, n := range h.Excluded {
		total += n
	}
	assert.Equal(t, h.Total, total, "every RemediationAction is either a clean heal or excluded for exactly one reason")
}

func doc(t *testing.T) report.Doc { return fixtureReport(t) }

func TestReportHealClassification(t *testing.T) {
	healthy := func(o raOpts, outcome string) (reportRA, []reportIncident) {
		o.incident = "i"
		o.created = 5 * time.Hour
		o.phase = "Completed"
		in := []reportIncident{mustIncident(t, incidentJSON("i", 6*time.Hour, 6*time.Hour, outcome))}
		return mustRA(t, o), in
	}
	credited := cond("HealCredited", "True", "WorkloadPatchedByTicket", time.Hour)
	notCredited := cond("HealCredited", "False", "NothingPatched", time.Hour)

	cases := []struct {
		name     string
		ra       raOpts
		outcome  string
		want     string // "" is a clean heal
		wantSafe bool
	}{
		{"credited and resolved is clean", raOpts{gate: approvalGatePass, conds: []string{credited}}, "resolved", "", true},
		{"clean heal whose gate record is missing is not a SAFE heal", raOpts{conds: []string{credited}}, "resolved", "", false},
		{"partial incident", raOpts{conds: []string{credited}}, "partial", report.ExcludedPartial, false},
		{"diverged workload", raOpts{conds: []string{cond("WorkloadDiverged", "True", "WorkloadDiverged", time.Hour)}}, "resolved", report.ExcludedPartial, false},
		{"self-resolved incident", raOpts{conds: []string{notCredited}}, "self-resolved", report.ExcludedSelfResolved, false},
		{"HealCredited false, incident resolved", raOpts{conds: []string{notCredited}}, "resolved", report.ExcludedHealNotCredited, false},
		{"no credit record at all (pre-ticket heal)", raOpts{}, "resolved", report.ExcludedNoCreditRecord, false},
		{"credited but the incident outcome is not resolved", raOpts{conds: []string{credited}}, "acknowledged", report.ExcludedOutcomeUnknown, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ra, incidents := healthy(tc.ra, tc.outcome)
			got := buildReportForTest(reportInputs{Remediations: []reportRA{ra}, Incidents: incidents, OperatorVersion: strp("v0.13.0")}, reportNow, reportFixtureWindow)
			h := got.HealAccounting
			if tc.want == "" {
				assert.Equal(t, 1, *h.Clean)
				if tc.wantSafe {
					assert.Equal(t, 1, *h.SafeHeals)
				} else {
					assert.Equal(t, 0, *h.SafeHeals)
				}
				return
			}
			assert.Equal(t, 0, *h.Clean)
			assert.Equal(t, 1, h.Excluded[tc.want], "%+v", h.Excluded)
		})
	}

	t.Run("a credited heal whose incident is missing cannot be confirmed", func(t *testing.T) {
		ra := mustRA(t, raOpts{name: "x", phase: "Completed", created: time.Hour, incident: "gone", gate: approvalGatePass,
			conds: []string{credited}})
		got := buildReportForTest(reportInputs{Remediations: []reportRA{ra}}, reportNow, reportFixtureWindow)
		assert.Equal(t, 0, *got.HealAccounting.Clean)
		assert.Equal(t, 1, got.HealAccounting.Excluded[report.ExcludedOutcomeUnknown])
	})
}

func TestReportApprovalAndPrecision(t *testing.T) {
	d := doc(t)

	assert.Equal(t, 8, d.TierA.A6Approval.Approved)
	assert.Equal(t, 3, d.TierA.A6Approval.Rejected)
	require.NotNil(t, d.TierA.A6Approval.Rate)
	assert.InDelta(t, 8.0/11.0, *d.TierA.A6Approval.Rate, 1e-9)
	assert.Equal(t, "in", d.TierA.A6Approval.Band)

	assert.True(t, d.TierA.A2Active.Active)
	assert.Equal(t, 11, d.TierA.A2Active.ProposalsDecided)
	require.NotNil(t, d.TierA.A5Depth.PerWeek)
	assert.InDelta(t, 11.0, *d.TierA.A5Depth.PerWeek, 1e-9)

	q2 := d.TierQ.Q2Precision
	assert.Equal(t, 11, q2.Decided)
	assert.Equal(t, 1, q2.Wrong)
	require.NotNil(t, q2.Rate)
	assert.InDelta(t, 1.0/11.0, *q2.Rate, 1e-9)
	assert.Equal(t, map[string]int{
		"wrong-diagnosis": 1, "not-now": 0, "too-risky": 1, "duplicate": 0,
		"not-needed": 0, "other": 0, report.NoReasonGiven: 1,
	}, q2.RejectionsByReason)
}

func TestReportTimeToHeal(t *testing.T) {
	q1 := doc(t).TierQ.Q1TimeToHeal

	assert.Equal(t, 7, q1.DetectToProposal.N, "one per incident, from detection to its first proposal; inc-pending adds the seventh")
	assert.Equal(t, 11, q1.ProposalToDecision.N, "approved and rejected both count; a rejection is dated by its Rejected condition")
	assert.Equal(t, 5, q1.DecisionToApplied.N)
	require.NotNil(t, q1.DecisionToApplied.P50Seconds)
	assert.Equal(t, 60.0, *q1.DecisionToApplied.P50Seconds)
	assert.Equal(t, 4, q1.AppliedToVerified.N)
	require.NotNil(t, q1.AppliedToVerified.P90Seconds)
	assert.Equal(t, 3540.0, *q1.AppliedToVerified.P90Seconds)
}

func TestReportRefusalMixByStageAndReason(t *testing.T) {
	q6 := doc(t).TierQ.Q6Refusals

	assert.Equal(t, 14, q6.Proposals, "12 RemediationActions plus 2 proposals refused before one was written")
	got := map[string]int{}
	for _, row := range q6.Rows {
		got[row.Phase+"/"+row.Stage+"/"+row.Reason] += row.Count
		assert.False(t, row.Flagged)
		require.NotNil(t, row.ShareOfProposals)
	}
	assert.Equal(t, map[string]int{
		"approval/Guardrails/BlastRadius":     1,
		"apply/Ticket/TicketExpired":          1,
		"proposal/ModePolicy/SelfHealingMode": 1,
		"proposal/Ground/WorkloadGone":        1,
	}, got, "a ProposalRefused condition that a later proposal cleared is not a refusal")
}

func TestReportFlagsAReasonAbove30PercentOfProposals(t *testing.T) {
	var in reportInputs
	for i := 0; i < 10; i++ {
		in.Incidents = append(in.Incidents, mustIncident(t, incidentJSON("i", time.Hour, time.Hour, "",
			cond("ProposalRefused", "True", "ModePolicy_SelfHealingMode", time.Hour))))
	}
	got := buildReportForTest(in, reportNow, reportFixtureWindow).TierQ.Q6Refusals

	require.Len(t, got.Rows, 1)
	assert.True(t, got.Rows[0].Flagged)
	assert.InDelta(t, 1.0, *got.Rows[0].ShareOfProposals, 1e-9)

	small := buildReportForTest(reportInputs{Incidents: in.Incidents[:2]}, reportNow, reportFixtureWindow).TierQ.Q6Refusals
	assert.False(t, small.Rows[0].Flagged, "two proposals are too few to call a reason dominant")
}

func TestReportFirstRunClampsComeFromStepSafety(t *testing.T) {
	c := doc(t).FirstRunClamps

	assert.Equal(t, 1, c.ProposalsWithClamp)
	assert.Equal(t, map[string]int{"clamped": 1, "rejected": 1}, c.StepVerdicts)
	assert.Equal(t, map[string]int{"blast-radius": 1}, c.ClampsByRule)
	assert.Empty(t, c.GateClamps, "status.gate records the seal, which is a clean pass")
}

func TestReportCountsCLIDeclinesFromEventsInTheWindow(t *testing.T) {
	d := doc(t).CLIDeclines

	assert.False(t, d.NoData)
	assert.Equal(t, map[string]int{
		"DorguCLIDeclined": 2, "DorguCLITicketMismatch": 1, "DorguCLINoTicket": 3, "DorguCLITicketExpired": 1,
	}, d.ByReason, "events outside the window and the operator's own events are not CLI declines")
	assert.NotEmpty(t, d.Note)
}

// An empty cluster has no data. Every ratio is null and says so; nothing is a
// measured zero.
func TestReportSaysNoDataRatherThanZero(t *testing.T) {
	d := buildReportForTest(reportInputs{}, reportNow, reportFixtureWindow)

	assert.Equal(t, 0, d.HealAccounting.Total)
	assert.Nil(t, d.TierA.A6Approval.Rate)
	assert.True(t, d.TierA.A6Approval.NoData)
	assert.Empty(t, d.TierA.A6Approval.Band)
	assert.Nil(t, d.TierA.A5Depth.PerWeek)
	assert.True(t, d.TierA.A5Depth.NoData)
	assert.False(t, d.TierA.A2Active.Active)
	assert.Nil(t, d.TierQ.Q2Precision.Rate)
	assert.True(t, d.TierQ.Q2Precision.NoData)
	for _, dist := range []report.Distribution{
		d.TierQ.Q1TimeToHeal.DetectToProposal, d.TierQ.Q1TimeToHeal.ProposalToDecision,
		d.TierQ.Q1TimeToHeal.DecisionToApplied, d.TierQ.Q1TimeToHeal.AppliedToVerified,
	} {
		assert.True(t, dist.NoData)
		assert.Nil(t, dist.P50Seconds)
		assert.Nil(t, dist.P90Seconds)
		assert.Equal(t, 0, dist.N)
	}
	assert.True(t, d.TierQ.Q6Refusals.NoData)
	assert.True(t, d.TierQ.Q3LLM.NoData)
	assert.True(t, d.TierA.A7External.NoData)
	assert.True(t, d.CLIDeclines.NoData, "no events were read, so nothing was measured")
}

func TestReportListsWhatAClusterCannotMeasure(t *testing.T) {
	ids := map[string]bool{}
	for _, n := range doc(t).NotMeasurable {
		ids[n.ID] = true
		assert.NotEmpty(t, n.Reason)
	}
	for _, id := range []string{"A1", "A3", "A4", "A8", "A9", "A10"} {
		assert.True(t, ids[id], id)
	}
}

// --- golden files ---

func goldenPath(name string) string { return filepath.Join("testdata", name) }

func checkGolden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := goldenPath(name)
	if *updateGolden {
		require.NoError(t, os.MkdirAll("testdata", 0o755))
		require.NoError(t, os.WriteFile(path, got, 0o644))
	}
	want, err := os.ReadFile(path)
	require.NoError(t, err, "run `go test ./internal/cli -run Golden -update` to create it")
	assert.Equal(t, string(want), string(got))
}

func TestReportJSONGolden(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, renderReportJSON(&buf, fixtureReport(t)))
	checkGolden(t, "report_golden.json", buf.Bytes())
}

func TestReportTextGolden(t *testing.T) {
	var buf bytes.Buffer
	renderReportText(&buf, fixtureReport(t))
	checkGolden(t, "report_golden.txt", buf.Bytes())
}

func TestFormatSince(t *testing.T) {
	assert.Equal(t, "7d", report.FormatWindow(7*24*time.Hour))
	assert.Equal(t, "14d", report.FormatWindow(14*24*time.Hour))
	assert.Equal(t, "36h0m0s", report.FormatWindow(36*time.Hour))
}

func TestParseSince(t *testing.T) {
	cases := map[string]time.Duration{
		"7d": 7 * 24 * time.Hour, "1d": 24 * time.Hour, "2w": 14 * 24 * time.Hour,
		"36h": 36 * time.Hour, "90m": 90 * time.Minute, "1h30m": 90 * time.Minute, "0.5d": 12 * time.Hour,
	}
	for in, want := range cases {
		got, err := parseSince(in)
		require.NoError(t, err, in)
		assert.Equal(t, want, got, in)
	}
	for _, bad := range []string{"", "d", "-1d", "0d", "abc", "7x", "0h", "1y"} {
		_, err := parseSince(bad)
		assert.Error(t, err, bad)
	}
}
