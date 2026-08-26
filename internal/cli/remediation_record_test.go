package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// CR-02 — `dorgu remediation heal` patched a workload 64Mi→128Mi and left the
// RemediationAction on Pending. These tests pin the two halves of the fix: the
// record is written only after kubectl accepted the workload patch, and it is
// always written when it did.

// statusPatches returns just the `patch remediationaction ... --subresource
// status` invocations, so an assertion about the record cannot accidentally pass
// on the Deployment patch.
func statusPatches(t *testing.T, patchLog string) string {
	t.Helper()
	var kept []string
	for _, line := range strings.Split(readPatchLog(t, patchLog), "\n") {
		if strings.Contains(line, "remediationaction") && strings.Contains(line, "--subresource status") {
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "\n")
}

// A Pending remediation healed directly is an implicit approval, and the command
// has always said so. Now it records it: without this the CRD said nobody had
// approved anything and nothing had happened, while the cluster carried the new
// limits.
func TestRunRemediationHealOnPendingRecordsApproval(t *testing.T) {
	patchLog := writeFakeKubectlDispatch(t, fakeKubectlResponses{
		context:    "kind-dorgu-spike",
		rem:        operatorRemediationFixture, // status.phase: Pending
		persona:    personaFixture,
		deployment: deploymentListFixture,
	})

	cmd := newRemediationHealCmd()
	cmd.SetArgs([]string{"fix-oom-api-server", "-n", "production", "--yes"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	require.NoError(t, cmd.Execute())

	log := readPatchLog(t, patchLog)
	assert.Contains(t, log, "patch deployment api-server", "the workload must still be patched")

	status := statusPatches(t, patchLog)
	require.NotEmpty(t, status, "a heal that changed the cluster must write the record")
	assert.Contains(t, status, `"phase":"Approved"`,
		"healing a Pending remediation records the implicit approval")
	assert.Contains(t, status, "WorkloadPatched", "the applied marker names what the CLI did")
	assert.Contains(t, status, "api-server", "the marker names the workload")

	// The record is written after the cluster changed, never before: a record
	// claiming a patch that then failed is the same divergence in the other
	// direction.
	assert.Less(t,
		strings.Index(log, "patch deployment api-server"),
		strings.Index(log, "--subresource status"),
		"the workload patch must land before the record claims it did")
}

// A re-heal of an already-approved remediation must not rewind or re-stamp the
// phase. The human decision is already recorded; what is missing is that the CLI
// wrote to the cluster.
func TestRunRemediationHealOnApprovedRecordsMarkerNotPhase(t *testing.T) {
	approved := strings.Replace(operatorRemediationFixture, `"phase": "Pending"`, `"phase": "Approved"`, 1)
	patchLog := writeFakeKubectlDispatch(t, fakeKubectlResponses{
		context:    "kind-dorgu-spike",
		rem:        approved,
		persona:    personaFixture,
		deployment: deploymentListFixture,
	})

	cmd := newRemediationHealCmd()
	cmd.SetArgs([]string{"fix-oom-api-server", "-n", "production", "--yes"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	require.NoError(t, cmd.Execute())

	status := statusPatches(t, patchLog)
	require.NotEmpty(t, status, "a heal that changed the cluster must write the record")
	assert.Contains(t, status, "WorkloadPatched")
	assert.NotContains(t, status, `phase`, "an approved remediation's phase is not the CLI's to move")
}

// Declining the confirmation prompt changes nothing, so it must record nothing.
func TestRunRemediationHealDeclinedRecordsNothing(t *testing.T) {
	patchLog := writeFakeKubectlDispatch(t, fakeKubectlResponses{
		context:    "kind-dorgu-spike",
		rem:        operatorRemediationFixture,
		persona:    personaFixture,
		deployment: deploymentListFixture,
	})

	cmd := newRemediationHealCmd()
	cmd.SetArgs([]string{"fix-oom-api-server", "-n", "production"})
	cmd.SetIn(strings.NewReader("n\n"))
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	require.NoError(t, cmd.Execute())

	assert.NotContains(t, readPatchLog(t, patchLog), "patch deployment")
	assert.Empty(t, statusPatches(t, patchLog), "nothing changed, so nothing may be recorded")
}

// A refused workload patch (RBAC, admission webhook) must leave the record
// untouched. Recording an approval for a change the cluster rejected is the F-01
// divergence with the two sides swapped.
func TestRunRemediationHealFailedPatchRecordsNothing(t *testing.T) {
	patchLog := writeFakeKubectlDispatch(t, fakeKubectlResponses{
		context:             "kind-dorgu-spike",
		rem:                 operatorRemediationFixture,
		persona:             personaFixture,
		deployment:          deploymentListFixture,
		failDeploymentPatch: true,
	})

	cmd := newRemediationHealCmd()
	cmd.SetArgs([]string{"fix-oom-api-server", "-n", "production", "--yes"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	assert.ErrorIs(t, cmd.Execute(), errSilent)
	assert.Empty(t, statusPatches(t, patchLog),
		"a heal the cluster refused must not be recorded as applied")
}

// The approve path heals too, and its heal must leave the same marker: otherwise
// the marker's absence would mean "not applied" on one path and "applied via
// approve" on the other.
func TestRunRemediationApproveHealStampsAppliedMarker(t *testing.T) {
	patchLog := writeFakeKubectlDispatch(t, fakeKubectlResponses{
		context:    "kind-dorgu-spike",
		rem:        operatorRemediationFixture,
		persona:    personaFixture,
		deployment: deploymentListFixture,
	})

	cmd := newRemediationApproveCmd()
	cmd.SetArgs([]string{"fix-oom-api-server", "-n", "production", "--yes"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	require.NoError(t, cmd.Execute())

	// Two separate status writes: the approval, then the heal's applied marker.
	// The fake API server keeps serving the pre-approval object, so the marker
	// patch re-asserts the phase here; against a real cluster the fresh read
	// returns Approved and only the marker is written.
	status := statusPatches(t, patchLog)
	assert.Contains(t, status, `"phase":"Approved"`, "approve still records the approval")
	assert.Contains(t, status, conditionWorkloadPatched,
		"and the heal that followed it is marked applied")
}

// --- pure: reading the record state ---

func TestParseRecordState(t *testing.T) {
	raw := []byte(`{
      "metadata": {"resourceVersion": "4711"},
      "status": {
        "phase": "Approved",
        "conditions": [
          {"type": "Applied", "status": "True", "reason": "PatchApplied", "message": "x",
           "lastTransitionTime": "2026-08-26T10:00:00Z", "unknownFutureField": 7}
        ]
      }
    }`)

	st, err := parseRecordState(raw)
	require.NoError(t, err)
	assert.Equal(t, "4711", st.ResourceVersion)
	assert.Equal(t, "Approved", st.Phase)
	require.Len(t, st.Conditions, 1)
	assert.Equal(t, "Applied", st.Conditions[0].Type)
	assert.Contains(t, string(st.Conditions[0].Raw), "unknownFutureField",
		"another writer's condition is preserved byte for byte")
}

func TestParseRecordStateNoConditions(t *testing.T) {
	st, err := parseRecordState([]byte(`{"status":{"phase":"Pending"}}`))
	require.NoError(t, err)
	assert.Equal(t, "Pending", st.Phase)
	assert.Empty(t, st.Conditions)
	assert.Empty(t, st.ResourceVersion)

	_, err = parseRecordState([]byte(`not json`))
	assert.Error(t, err)
}

// --- pure: building the patch ---

// healRecordFor builds the marker input the patch builder takes, so the tests
// below read as what the user did rather than as struct plumbing.
func healRecordFor() *healExecution {
	return &healExecution{
		Namespace:  "production",
		Deployment: "api-server",
		Container:  "api-server",
		Change:     &healResourceChange{Limits: map[string]string{"memory": "512Mi"}},
	}
}

func TestBuildHealRecordPatchPendingRecordsApproval(t *testing.T) {
	st := recordState{Phase: "Pending", ResourceVersion: "4711"}

	patch, err := buildHealRecordPatch(st, healRecordFor(), "2026-08-27T12:00:00Z")
	require.NoError(t, err)

	var got struct {
		Metadata struct {
			ResourceVersion string `json:"resourceVersion"`
		} `json:"metadata"`
		Status struct {
			Phase      string `json:"phase"`
			ApprovedBy string `json:"approvedBy"`
			ApprovedAt string `json:"approvedAt"`
			Conditions []struct {
				Type               string `json:"type"`
				Status             string `json:"status"`
				Reason             string `json:"reason"`
				Message            string `json:"message"`
				LastTransitionTime string `json:"lastTransitionTime"`
			} `json:"conditions"`
		} `json:"status"`
	}
	require.NoError(t, json.Unmarshal([]byte(patch), &got))

	assert.Equal(t, "4711", got.Metadata.ResourceVersion,
		"the write is a read-modify-write and must not clobber state it never saw")
	assert.Equal(t, "Approved", got.Status.Phase)
	assert.Equal(t, healApprovedBy, got.Status.ApprovedBy)
	assert.Equal(t, "2026-08-27T12:00:00Z", got.Status.ApprovedAt)

	require.Len(t, got.Status.Conditions, 1)
	c := got.Status.Conditions[0]
	assert.Equal(t, conditionWorkloadPatched, c.Type)
	assert.Equal(t, "True", c.Status)
	assert.Equal(t, reasonWorkloadPatched, c.Reason)
	assert.Equal(t, "2026-08-27T12:00:00Z", c.LastTransitionTime)
	assert.Contains(t, c.Message, "production/api-server")
	assert.Contains(t, c.Message, "api-server")
	assert.Contains(t, c.Message, "resources.limits.memory")
	assert.Contains(t, c.Message, "512Mi")
}

func TestBuildHealRecordPatchApprovedLeavesPhaseAlone(t *testing.T) {
	for _, phase := range []string{"Approved", "Applying", "Verifying", "Completed", "Acknowledged"} {
		patch, err := buildHealRecordPatch(recordState{Phase: phase}, healRecordFor(), "2026-08-27T12:00:00Z")
		require.NoError(t, err, phase)
		assert.NotContains(t, patch, `"phase"`, phase)
		assert.NotContains(t, patch, `"approvedBy"`, phase)
		assert.Contains(t, patch, conditionWorkloadPatched, phase)
	}
}

// An empty phase is the same case as Pending: the operator has not started, and
// nothing has recorded a decision.
func TestBuildHealRecordPatchEmptyPhaseRecordsApproval(t *testing.T) {
	patch, err := buildHealRecordPatch(recordState{}, healRecordFor(), "2026-08-27T12:00:00Z")
	require.NoError(t, err)
	assert.Contains(t, patch, `"phase":"Approved"`)
	assert.NotContains(t, patch, `"resourceVersion"`,
		"no resourceVersion was read, so none may be sent as a precondition")
}

// Every other writer's condition goes back exactly as it came in. Rewriting the
// operator's own record while fixing a record bug would be a poor trade.
func TestBuildHealRecordPatchPreservesForeignConditions(t *testing.T) {
	st := recordState{
		Phase: "Approved",
		Conditions: []recordCondition{
			{Type: "Applied", Raw: json.RawMessage(`{"type":"Applied","status":"True","futureField":1}`)},
			{Type: "Verified", Raw: json.RawMessage(`{"type":"Verified","status":"False"}`)},
		},
	}

	patch, err := buildHealRecordPatch(st, healRecordFor(), "2026-08-27T12:00:00Z")
	require.NoError(t, err)

	assert.Contains(t, patch, `"futureField":1`)
	assert.Contains(t, patch, `"type":"Verified"`)
	assert.Contains(t, patch, conditionWorkloadPatched)
}

// Re-healing replaces the previous marker rather than appending a second one:
// conditions are keyed by type, and two entries with the same type is an invalid
// object the API server would reject.
func TestBuildHealRecordPatchReplacesOwnPriorMarker(t *testing.T) {
	st := recordState{
		Phase: "Completed",
		Conditions: []recordCondition{
			{Type: conditionWorkloadPatched, Raw: json.RawMessage(
				`{"type":"WorkloadPatched","status":"True","message":"an earlier heal"}`)},
		},
	}

	patch, err := buildHealRecordPatch(st, healRecordFor(), "2026-08-27T12:00:00Z")
	require.NoError(t, err)

	assert.NotContains(t, patch, "an earlier heal")
	assert.Equal(t, 1, strings.Count(patch, conditionWorkloadPatched))
}

// The marker's message is the whole point: it has to say what changed, not that
// something did.
func TestHealRecordMessage(t *testing.T) {
	msg := healRecordMessage(&healExecution{
		Namespace:  "apps",
		Deployment: "report-worker",
		Container:  "worker",
		Change: &healResourceChange{
			Limits:   map[string]string{"memory": "128Mi"},
			Requests: map[string]string{"cpu": "100m"},
		},
	})

	assert.Contains(t, msg, "apps/report-worker")
	assert.Contains(t, msg, "container worker")
	assert.Contains(t, msg, "resources.limits.memory=128Mi")
	assert.Contains(t, msg, "resources.requests.cpu=100m")
}

// The path that matters most: the workload changed and the record could not be
// written. Exiting 0 with a green "✓ Healed" here would reproduce CR-02 exactly,
// so this exits non-zero and says which way round the disagreement runs.
func TestRunRemediationHealReportsAnUnrecordableHeal(t *testing.T) {
	patchLog := writeFakeKubectlDispatch(t, fakeKubectlResponses{
		context:    "kind-dorgu-spike",
		rem:        operatorRemediationFixture,
		persona:    personaFixture,
		deployment: deploymentListFixture,
		statusPatchError: "Error from server (Forbidden): " +
			"remediationactions.dorgu.io \"fix-oom-api-server\" is forbidden",
	})

	var out bytes.Buffer
	cmd := newRemediationHealCmd()
	cmd.SetArgs([]string{"fix-oom-api-server", "-n", "production", "--yes"})
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := cmd.Execute()
	assert.ErrorIs(t, err, errSilent)
	assert.Equal(t, ExitError, ExitCode(err), "a record that disagrees with the cluster is not a success")

	assert.Contains(t, readPatchLog(t, patchLog), "patch deployment api-server",
		"the workload was patched, so the message must not claim otherwise")

	text := out.String()
	assert.Contains(t, text, "could not record")
	assert.Contains(t, text, "does not say so")
	assert.Contains(t, text, "dorgu remediation heal fix-oom-api-server -n production",
		"the way out is re-running the heal, which is idempotent")
	assert.NotContains(t, text, "kubectl patch remediationaction",
		"the only patch that would work replaces the operator's conditions wholesale")
}

// A Conflict is retried against fresh state, and a write that keeps conflicting
// is reported rather than looped on.
func TestRecordHealGivesUpAndSaysSo(t *testing.T) {
	writeFakeKubectlDispatch(t, fakeKubectlResponses{
		context:          "kind-dorgu-spike",
		rem:              `{"metadata":{"name":"r","namespace":"production","resourceVersion":"1"},"status":{"phase":"Pending"}}`,
		statusPatchError: "Error from server (Conflict): the object has been modified",
	})

	err := recordHeal(t.Context(), "", "production", "r", healRecordFor())
	require.Error(t, err)

	var rec *recordError
	require.ErrorAs(t, err, &rec)
	assert.Equal(t, "production", rec.Namespace)
	assert.Contains(t, err.Error(), "remediationaction production/r")
	assert.Contains(t, err.Error(), "kept changing under the write (3 attempts)")
}
