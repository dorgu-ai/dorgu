package cli

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/dorgu-ai/dorgu-platform-v1/pkg/report"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// B8 fix round, from the CP1 EKS QA (2026-11-09).

// F-7: an action approved with --no-heal that the operator later "rolls back" never
// had anything to roll back. It is not a rolled-back heal.
func TestReport_ApprovedNotHealedIsNotARollback(t *testing.T) {
	never := mustRA(t, raOpts{name: "never-healed", phase: "RolledBack", created: time.Hour, approvedAt: time.Hour / 2,
		verification: "Degraded", conds: []string{
			cond("WorkloadDiverged", "False", "WorkloadRestored", 30*time.Minute),
			cond("RolledBack", "True", "HealthDegraded", 30*time.Minute),
		}})
	healed := mustRA(t, raOpts{name: "healed-then-rolled-back", phase: "RolledBack", created: time.Hour, approvedAt: time.Hour / 2,
		verification: "Degraded", conds: []string{
			cond("WorkloadPatched", "True", "CLIAppliedResourceChange", 40*time.Minute),
			cond("WorkloadDiverged", "True", "WorkloadDiverged", 30*time.Minute),
			cond("RolledBack", "True", "HealthDegraded", 30*time.Minute),
		}})

	got := buildReportForTest(reportInputs{Remediations: []reportRA{never, healed}}, reportNow, reportFixtureWindow).HealAccounting

	assert.Equal(t, 1, got.Excluded[report.ExcludedApprovedNotHealed], "approved, never patched, rolled back")
	assert.Equal(t, 1, got.Excluded[report.ExcludedRolledBack], "a heal that was applied and rolled back is a rollback")
}

func TestReport_ARollbackWithNoRecordOfTheWorkloadStaysARollback(t *testing.T) {
	// No WorkloadDiverged condition at all (an older operator): not provably unhealed.
	old := mustRA(t, raOpts{name: "old", phase: "RolledBack", created: time.Hour, verification: "Degraded"})

	got := buildReportForTest(reportInputs{Remediations: []reportRA{old}}, reportNow, reportFixtureWindow).HealAccounting

	assert.Equal(t, 1, got.Excluded[report.ExcludedRolledBack])
	assert.Equal(t, 0, got.Excluded[report.ExcludedApprovedNotHealed])
}

func TestReport_TheTextExplainsApprovedNotHealed(t *testing.T) {
	never := mustRA(t, raOpts{name: "n", phase: "RolledBack", created: time.Hour, conds: []string{
		cond("WorkloadDiverged", "False", "WorkloadRestored", 30*time.Minute)}})

	out := renderText(t, reportInputs{Remediations: []reportRA{never}})

	assert.Contains(t, out, "approved_not_healed")
}

// F-7: the operator tag is shown only when it is a version.
func TestReport_OperatorLabelIsAVersionOrAnUnreleasedBuild(t *testing.T) {
	for tag, want := range map[string]string{
		"v0.13.0": "v0.13.0", "0.13.0": "0.13.0", "0e8a180": "unreleased build 0e8a180",
		"latest": "unreleased build latest", "dev": "unreleased build dev",
	} {
		tag := tag
		assert.Equal(t, want, operatorLabel(&tag), tag)
	}
	assert.Equal(t, "unknown (not readable)", operatorLabel(nil))
}

func TestReport_TheHeaderUsesTheOperatorLabel(t *testing.T) {
	tag := "0e8a180"
	doc := buildReportForTest(reportInputs{}, reportNow, reportFixtureWindow)
	doc.Cluster.OperatorVersion = &tag
	var buf bytes.Buffer

	renderReportText(&buf, doc)

	assert.Contains(t, buf.String(), "Operator:  unreleased build 0e8a180")
}

// F-8: the health line counted only Pending actions under a label that says
// "Pending Remediations" while approved ones were being applied. It now says what
// it counts.
func TestHealth_TheCountIsLabelledAwaitingApproval(t *testing.T) {
	var buf bytes.Buffer
	printPendingRemediations(&buf, &remediationSummary{Count: 2})

	out := buf.String()
	assert.Contains(t, out, "Awaiting approval: 2")
	assert.NotContains(t, out, "Pending Remediations")
}

// An approval of a proposal that predates the operator's gate ends Expired with the
// reason PredatesGate. The CLI says so, as the gate's refusal (exit 4), instead of
// "ended in phase Expired".
func predatesGateFixture(t *testing.T) string {
	t.Helper()
	doc := refusedAtApprovalFixture(t, operatorRemediationFixture, "NoProposalRecord", "there is no record that the operator's gate cleared this plan")
	doc = strings.Replace(doc, `"phase":"Failed"`, `"phase":"Expired"`, 1)
	doc = strings.Replace(doc, `{"message":"refused at approval; nothing was applied","reason":"GateRefusedAtApproval","status":"False","type":"Applied"},`,
		`{"message":"This proposal predates the operator's gate (made by an older operator). Nothing was applied. The operator proposes again if the problem persists.","reason":"PredatesGate","status":"True","type":"Expired"},`, 1)
	return doc
}

func TestApproveOfAProposalThatPredatesTheGateIsTheGateRefusal(t *testing.T) {
	r := declineBase(t)
	r.remAfterApprove = predatesGateFixture(t)
	require.Contains(t, r.remAfterApprove, "PredatesGate")

	res := runApprove(t, r)

	require.Equal(t, ExitDeclined, ExitCode(res.err))
	assert.Contains(t, res.out, "NoProposalRecord")
	assert.Contains(t, res.out, "predates the operator's gate")
	assert.Contains(t, res.out, "proposes again")
	assert.NotContains(t, res.out, "Remediation approved")
	assert.NotContains(t, res.patchLog, "patch deployment")
}
