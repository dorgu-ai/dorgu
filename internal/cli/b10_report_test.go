package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// B10 (f), D1.1 finding 4: `dorgu report` showed deferred_proposals: 0 and an empty refusal
// mix while the operator's counters held 25 deferrals and 3 Validate/step-command refusals.
//
// Deferrals are deliberately not stored on cluster objects (B6), so 0 was a statement the
// report could not make: it now says they are not recorded, and where to read them.
//
// The 3 refusals were stored and then erased: a ProposalRefused condition is rewritten to
// False when the incident resolves, or when a later proposal answers it (the fallback that
// proposes the rest of a refused plan does that at once), and the report reads only a True
// one. The operator (v0.13.1) now also keeps ProposalRefusalRecorded, which is never
// cleared, and the report counts a refusal from it.

const refusalRecorded = "ProposalRefusalRecorded"

func TestReportCountsARefusalThatResolvedAfterwards(t *testing.T) {
	h := time.Hour
	in := reportInputs{Incidents: []reportIncident{
		// The incident resolved: the live condition was cleared, the record stands.
		mustIncident(t, incidentJSON("inc-resolved", 5*h, 5*h, "self-resolved",
			cond("ProposalRefused", "False", "IncidentResolved", h),
			cond(refusalRecorded, "True", "Validate_StepCommand", 3*h))),
	}}

	q6 := buildReport(in, reportNow, reportFixtureWindow).TierQ.Q6Refusals

	require.Len(t, q6.Rows, 1)
	assert.Equal(t, "proposal", q6.Rows[0].Phase)
	assert.Equal(t, "Validate", q6.Rows[0].Stage)
	assert.Equal(t, "StepCommand", q6.Rows[0].Reason)
	assert.Equal(t, 1, q6.Rows[0].Count)
	assert.Equal(t, 1, q6.Proposals, "a proposal that was refused and never written is one proposal")
}

// A refusal followed by a proposal for the same incident (the fallback that proposes the rest
// of a refused plan): the refusal is counted, the proposal is not counted twice.
func TestReportCountsARefusalAnAnswerClearedWithoutDoublingTheProposal(t *testing.T) {
	h := time.Hour
	in := reportInputs{
		Incidents: []reportIncident{mustIncident(t, incidentJSON("inc-fallback", 5*h, 5*h, "",
			cond("ProposalRefused", "False", "ProposalMade", 3*h),
			cond(refusalRecorded, "True", "Validate_StepCommand", 3*h)))},
		Remediations: []reportRA{mustRA(t, raOpts{name: "ra-fallback", phase: "Pending", created: 3 * h, incident: "inc-fallback"})},
	}

	q6 := buildReport(in, reportNow, reportFixtureWindow).TierQ.Q6Refusals

	require.Len(t, q6.Rows, 1)
	assert.Equal(t, 1, q6.Rows[0].Count)
	assert.Equal(t, 1, q6.Proposals, "the RemediationAction is the one proposal")
}

// A refusal that is still standing is on the incident twice (the live condition and the
// record) and is one refusal.
func TestReportCountsAStandingRefusalOnce(t *testing.T) {
	h := time.Hour
	in := reportInputs{Incidents: []reportIncident{
		mustIncident(t, incidentJSON("inc-standing", 5*h, 5*h, "",
			cond("ProposalRefused", "True", "ModePolicy_SelfHealingMode", 3*h),
			cond(refusalRecorded, "True", "ModePolicy_SelfHealingMode", 3*h))),
	}}

	q6 := buildReport(in, reportNow, reportFixtureWindow).TierQ.Q6Refusals

	require.Len(t, q6.Rows, 1)
	assert.Equal(t, 1, q6.Rows[0].Count)
	assert.Equal(t, 1, q6.Proposals)
}

// A refusal recorded before the window is not in it.
func TestReportLeavesOutARecordedRefusalFromBeforeTheWindow(t *testing.T) {
	in := reportInputs{Incidents: []reportIncident{
		mustIncident(t, incidentJSON("inc-old", 20*24*time.Hour, 20*24*time.Hour, "resolved",
			cond(refusalRecorded, "True", "Validate_StepCommand", 10*24*time.Hour))),
	}}

	q6 := buildReport(in, reportNow, reportFixtureWindow).TierQ.Q6Refusals

	assert.Empty(t, q6.Rows)
}

// An operator before v0.13.1 writes no record, and a refusal it cleared is gone: that is
// as before, and the live condition is still read.
func TestReportStillReadsTheLiveConditionOfAnOlderOperator(t *testing.T) {
	h := time.Hour
	in := reportInputs{Incidents: []reportIncident{
		mustIncident(t, incidentJSON("inc-live", 5*h, 5*h, "", cond("ProposalRefused", "True", "Ground_WorkloadGone", 2*h))),
		mustIncident(t, incidentJSON("inc-cleared", 5*h, 5*h, "", cond("ProposalRefused", "False", "ProposalMade", 2*h))),
	}}

	q6 := buildReport(in, reportNow, reportFixtureWindow).TierQ.Q6Refusals

	require.Len(t, q6.Rows, 1)
	assert.Equal(t, "WorkloadGone", q6.Rows[0].Reason)
}

// --- deferrals are not recorded ---------------------------------------------------------

const deferralsNote = "not recorded on cluster objects (see the dorgu_gate_decisions_total metric, result=deferred)"

func TestReportDoesNotClaimZeroDeferrals(t *testing.T) {
	d := buildReport(reportInputs{Incidents: []reportIncident{
		mustIncident(t, incidentJSON("i", 5*time.Hour, 5*time.Hour, "", cond("ProposalRefused", "True", "ModePolicy_SelfHealingMode", time.Hour))),
	}}, reportNow, reportFixtureWindow)

	q6 := d.TierQ.Q6Refusals
	assert.Nil(t, q6.Deferred, "no number: the operator does not record deferrals on objects")
	assert.Equal(t, deferralsNote, q6.DeferredNote)

	raw, err := json.Marshal(d)
	require.NoError(t, err)
	var generic map[string]any
	require.NoError(t, json.Unmarshal(raw, &generic))
	refusals := generic["tier_q"].(map[string]any)["q6_refusal_mix"].(map[string]any)
	assert.Contains(t, refusals, "deferred_proposals")
	assert.Nil(t, refusals["deferred_proposals"], "null, not 0")
	assert.Equal(t, deferralsNote, refusals["deferred_note"])

	var out bytes.Buffer
	renderReportText(&out, d)
	assert.Contains(t, out.String(), "deferred proposals: "+deferralsNote)
	assert.NotContains(t, out.String(), "deferred proposals: 0")
}

// Deferrals an operator before the fix wrote onto incidents are still counted, and the note says
// the count is partial.
func TestReportCountsTheDeferralsAnOlderOperatorWrote(t *testing.T) {
	d := buildReport(reportInputs{Incidents: []reportIncident{
		mustIncident(t, incidentJSON("i", 3*time.Hour, 3*time.Hour, "", cond("ProposalRefused", "True", "DedupCooldown_ActiveRemediation", 2*time.Hour))),
	}}, reportNow, reportFixtureWindow)

	require.NotNil(t, d.TierQ.Q6Refusals.Deferred)
	assert.Equal(t, 1, *d.TierQ.Q6Refusals.Deferred)
	assert.Equal(t, deferralsNote, d.TierQ.Q6Refusals.DeferredNote)

	var out bytes.Buffer
	renderReportText(&out, d)
	assert.Contains(t, out.String(), "1 incident(s)")
	assert.Contains(t, out.String(), deferralsNote)
}

// deferredCount is the number of deferrals read off incidents, 0 when none were (the field is
// null then: see TestReportDoesNotClaimZeroDeferrals).
func deferredCount(q reportRefusals) int {
	if q.Deferred == nil {
		return 0
	}
	return *q.Deferred
}

// --- B10 (g): `dorgu health` counts Pending proposals only -------------------------------

func fakeRemediationList(t *testing.T, phases ...string) {
	t.Helper()
	items := make([]string, len(phases))
	for i, p := range phases {
		items[i] = `{"status":{"phase":"` + p + `"}}`
	}
	dir := t.TempDir()
	list := filepath.Join(dir, "ras.json")
	require.NoError(t, os.WriteFile(list, []byte(`{"items":[`+strings.Join(items, ",")+`]}`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "kubectl"), []byte("#!/bin/sh\ncat "+list+"\n"), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// The v0.14.0 CHANGELOG says "Awaiting approval" counts proposals in Pending. The code counted
// Pending and Approved: an approved one is not awaiting approval.
func TestHealth_AwaitingApprovalCountsPendingOnly(t *testing.T) {
	fakeRemediationList(t, "Pending", "Pending", "Approved", "Ticketed", "Applying", "Completed", "Expired")

	got := fetchPendingRemediations(t.Context(), "", "")

	assert.Equal(t, 2, got.Count, "only Pending is awaiting approval")
	assert.Equal(t, 1, got.Approved, "an approved proposal that has not been applied yet is counted on its own")
}

func TestHealth_TheApprovedCountIsLabelledSeparately(t *testing.T) {
	var buf bytes.Buffer
	printPendingRemediations(&buf, &remediationSummary{Count: 2, Approved: 1})
	out := buf.String()
	assert.Contains(t, out, "Awaiting approval: 2")
	assert.Contains(t, out, "Approved, not yet applied: 1")

	buf.Reset()
	printPendingRemediations(&buf, &remediationSummary{Count: 0})
	assert.NotContains(t, buf.String(), "Approved, not yet applied", "nothing to say when none is approved")
}
