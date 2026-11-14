package cli

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dorgu-ai/dorgu-platform-v1/pkg/report"
)

// The CLI writes the rejection enum and pkg/report reads it back. If a reason is
// added here and not there, the report would label it unrecognised.
func TestEveryRejectionReasonTheCLIWritesIsOneTheReportKnows(t *testing.T) {
	var ours []string
	for _, r := range rejectionReasons {
		ours = append(ours, r.flag)
	}
	assert.Equal(t, ours, report.RejectionFlags())
}

// The conditions and prefixes pkg/report reads are the ones the CLI writes.
func TestReportReadsTheEventPrefixTheCLIWrites(t *testing.T) {
	in := reportInputs{
		EventsRead: true,
		Events:     []reportEvent{mustEvent(t, `{"reason":"`+reasonCLIPrefix+`NoTicket","lastTimestamp":"`+ago(time.Hour)+`","involvedObject":{"kind":"RemediationAction"}}`)},
	}
	got := buildReportForTest(in, reportNow, reportFixtureWindow)
	assert.Equal(t, 1, got.CLIDeclines.ByReason[reasonCLIPrefix+"NoTicket"])
}

// A timestamp that does not parse is "no time" and never fails the report, as it
// always was. The operator's metav1.Time would have refused the whole list.
func TestAnUnparseableTimestampIsNoTimeNotAnError(t *testing.T) {
	ra := mustRA(t, raOpts{name: "x", phase: "Completed", created: time.Hour})
	ra.Status.ApprovedAt = "yesterday-ish"
	ra.Status.Conditions = append(ra.Status.Conditions, reportCondition{Type: "Verified", Status: "True", LastTransitionTime: "garbage"})

	converted := reportInputs{Remediations: []reportRA{ra}}.toReport()
	require.Len(t, converted.Remediations, 1)
	assert.Nil(t, converted.Remediations[0].Status.ApprovedAt)
	assert.True(t, converted.Remediations[0].Status.Conditions[0].LastTransitionTime.IsZero())

	got := buildReportForTest(reportInputs{Remediations: []reportRA{ra}}, reportNow, reportFixtureWindow)
	assert.Equal(t, 1, got.HealAccounting.Total)
	assert.Equal(t, 0, got.TierQ.Q1TimeToHeal.ProposalToDecision.N, "no approval time, so no sample")
}

func TestConversionKeepsEverythingTheReportReads(t *testing.T) {
	in := reportFixture(t)
	out := in.toReport()

	require.Len(t, out.Remediations, len(in.Remediations))
	require.Len(t, out.Incidents, len(in.Incidents))
	require.Len(t, out.Events, len(in.Events))
	assert.True(t, out.EventsRead)

	clean := out.Remediations[0]
	assert.Equal(t, "ra-clean", clean.Name)
	assert.Equal(t, "shop", clean.Namespace)
	assert.Equal(t, "inc-clean", clean.Spec.IncidentRef.Name)
	assert.Equal(t, "rule-based", clean.Spec.PlanSource)
	require.Len(t, clean.Spec.Steps, 1)
	require.Len(t, clean.Spec.Steps[0].Safety, 2)
	assert.Equal(t, "clamped", clean.Spec.Steps[0].Safety[0].Verdict)
	require.NotNil(t, clean.Status.Gate)
	assert.Equal(t, "ApprovalRecheck", clean.Status.Gate.Phase)
	assert.NotNil(t, clean.Status.ApprovedAt)
	assert.NotNil(t, clean.Status.AppliedAt)

	series := mustEvent(t, `{"reason":"DorguCLINoTicket","eventTime":"`+ago(time.Hour)+`","series":{"count":4},"involvedObject":{"kind":"RemediationAction"}}`).toOperator()
	assert.Equal(t, int32(4), series.Series.Count)
	assert.False(t, series.EventTime.IsZero())
}
