package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// B8 planner additions (from the docs pass, docs#13).

// The diff footer printed `reject ... --reason "..."`, which the enum rejects.
func TestDiffFooterPrintsTheRejectEnumNotFreeText(t *testing.T) {
	applicable := makeTestRemediation("ra-oom", "apps", "Pending", "persona-update", "88%", "worker")
	applicable.Spec.Action.PrePatchState = json.RawMessage(`{"spec":{"resources":{"limits":{"memory":"48Mi"}}}}`)
	applicable.Spec.Action.Patch = json.RawMessage(`{"spec":{"resources":{"limits":{"memory":"96Mi"}}}}`)

	applicable.Spec.WorkloadRef = &workloadRef{Kind: "Deployment", Name: "worker", ManagedBy: managedByUnmanaged}

	owned := makeTestRemediation("ra-owned", "apps", "Pending", "persona-update", "88%", "worker")
	owned.Spec.Action.PrePatchState = applicable.Spec.Action.PrePatchState
	owned.Spec.Action.Patch = applicable.Spec.Action.Patch
	owned.Spec.WorkloadRef = &workloadRef{Kind: "Deployment", Name: "worker", ManagedBy: managedByHelm,
		ManagedByDetail: `Helm release "worker" in namespace apps`}

	advisory := makeTestRemediation("ra-adv", "apps", "Pending", "notification", "91%", "web")
	advisory.Spec.Steps = []remediationStep{{Order: 1, ID: "s1", Type: "manual", Description: "Fix the tag.", AutoExecutable: false}}

	for name, r := range map[string]remediationFull{"applicable": applicable, "owned": owned, "advisory": advisory} {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			printRemediationDiff(&buf, &r)
			out := buf.String()

			assert.NotContains(t, out, `--reason "..."`)
			assert.Contains(t, out, "--reason <wrong-diagnosis|too-risky|not-now|duplicate|not-needed|other>")
			assert.Contains(t, out, `[--note "..."]`)
		})
	}
}

func TestRemediationHelpExampleUsesAValidRejectReason(t *testing.T) {
	long := newRemediationCmd().Long
	assert.NotContains(t, long, `--reason "not needed"`)
	assert.Contains(t, long, "--reason not-needed")
}

// `remediation list` hid Ticketed: a ticketed action is waiting to be applied.
func TestEveryNonTerminalPhaseIsListedByDefault(t *testing.T) {
	active := []string{"Pending", "Approved", "Ticketed", "Applying", "Verifying", "Failed"}
	hidden := []string{"Completed", "Acknowledged", "RolledBack", "Rejected", "Expired"}
	for _, p := range active {
		assert.True(t, activeRemediationPhases[p], "%s should be listed without --all", p)
	}
	for _, p := range hidden {
		assert.False(t, activeRemediationPhases[p], "%s should need --all", p)
	}
}

// The report is read by users: no internal phase labels or tooling names.
func TestReportTextCarriesNoInternalLabels(t *testing.T) {
	out := renderText(t, reportInputs{})
	for _, internal := range []string{"P2/P3", "/eks-qa", "eks-qa"} {
		assert.False(t, strings.Contains(out, internal), "report text mentions %q", internal)
	}
	var buf bytes.Buffer
	doc := buildReport(reportInputs{}, reportNow, reportFixtureWindow)
	b, err := json.Marshal(doc)
	require.NoError(t, err)
	buf.Write(b)
	for _, internal := range []string{"P2/P3", "/eks-qa"} {
		assert.NotContains(t, buf.String(), internal)
	}
}
