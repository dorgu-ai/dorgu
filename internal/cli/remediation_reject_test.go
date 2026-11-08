package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Task 1.13: `reject --reason <enum>` is persisted on the RemediationAction as a
// RejectionReason condition, so the report can count rejections by reason (Q2).

type rejectRun struct {
	err      error
	out      string
	patchLog string
}

func runReject(t *testing.T, rem string, args ...string) rejectRun {
	t.Helper()
	patchLog := writeFakeKubectlDispatch(t, fakeKubectlResponses{
		context: "kind-dorgu-spike", rem: rem, persona: personaFixture, deployment: deploymentListFixture, noTicket: true,
	})
	cmd := newRemediationRejectCmd()
	cmd.SetArgs(append([]string{"fix-oom-api-server", "-n", "production"}, args...))
	var sb strings.Builder
	cmd.SetOut(&sb)
	cmd.SetErr(&sb)
	var err error
	var stderr string
	stdout := captureStdout(t, func() { stderr = captureStderr(t, func() { err = cmd.Execute() }) })
	return rejectRun{err: err, out: sb.String() + stdout + stderr, patchLog: readPatchLog(t, patchLog)}
}

// statusPatchOf pulls the JSON of the one status patch out of the patch log.
func statusPatchOf(t *testing.T, patchLog string) map[string]any {
	t.Helper()
	i := strings.Index(patchLog, "-p ")
	require.GreaterOrEqual(t, i, 0, patchLog)
	var patch map[string]any
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(patchLog[i+3:])), &patch), patchLog)
	return patch
}

func conditionsOf(patch map[string]any) []map[string]any {
	status := patch["status"].(map[string]any)
	var out []map[string]any
	list, _ := status["conditions"].([]any)
	for _, c := range list {
		out = append(out, c.(map[string]any))
	}
	return out
}

func firstCond(t *testing.T, patchLog string) map[string]any {
	t.Helper()
	conds := conditionsOf(statusPatchOf(t, patchLog))
	require.NotEmpty(t, conds, "the patch carries no condition")
	return conds[0]
}

func TestRejectWithAReasonRecordsItAsACondition(t *testing.T) {
	res := runReject(t, operatorRemediationFixture, "--reason", "wrong-diagnosis", "--note", "it was a node issue")

	require.NoError(t, res.err)
	patch := statusPatchOf(t, res.patchLog)
	assert.Equal(t, "Rejected", patch["status"].(map[string]any)["phase"])
	conds := conditionsOf(patch)
	require.Len(t, conds, 1)
	assert.Equal(t, "RejectionReason", conds[0]["type"])
	assert.Equal(t, "True", conds[0]["status"])
	assert.Equal(t, "WrongDiagnosis", conds[0]["reason"])
	assert.Equal(t, "it was a node issue", conds[0]["message"])
	assert.NotEmpty(t, conds[0]["lastTransitionTime"])
	assert.Contains(t, res.out, "wrong-diagnosis")
}

func TestRejectKeepsEveryOtherCondition(t *testing.T) {
	rem := strings.Replace(operatorRemediationFixture, `"phase": "Pending"`, `"phase": "Pending",
    "conditions": [{"type":"Proposed","status":"True","reason":"GateCleared","message":"ok","observedGeneration":1,"lastTransitionTime":"2026-06-24T10:00:00Z","futureField":"kept"}]`, 1)
	res := runReject(t, rem, "--reason", "too-risky")

	require.NoError(t, res.err)
	conds := conditionsOf(statusPatchOf(t, res.patchLog))
	require.Len(t, conds, 2, "the operator's condition is sent back, not dropped")
	assert.Equal(t, "Proposed", conds[0]["type"])
	assert.Equal(t, "kept", conds[0]["futureField"], "byte for byte, including fields this CLI does not know")
	assert.Equal(t, "TooRisky", conds[1]["reason"])
}

func TestRejectReasonEnum(t *testing.T) {
	want := map[string]string{
		"wrong-diagnosis": "WrongDiagnosis", "too-risky": "TooRisky", "not-now": "NotNow",
		"duplicate": "Duplicate", "not-needed": "NotNeeded",
	}
	for flag, reason := range want {
		t.Run(flag, func(t *testing.T) {
			res := runReject(t, operatorRemediationFixture, "--reason", flag)
			require.NoError(t, res.err)
			assert.Equal(t, reason, firstCond(t, res.patchLog)["reason"])
		})
	}
	t.Run("other with a note", func(t *testing.T) {
		res := runReject(t, operatorRemediationFixture, "--reason", "other", "--note", "handled by the platform team")
		require.NoError(t, res.err)
		assert.Equal(t, "Other", firstCond(t, res.patchLog)["reason"])
	})
}

// wrong-action was dropped from the enum before release (F-7): it is no longer a
// value --reason accepts.
func TestRejectNoLongerAcceptsWrongAction(t *testing.T) {
	res := runReject(t, operatorRemediationFixture, "--reason", "wrong-action")

	require.Error(t, res.err)
	assert.Empty(t, res.patchLog, "nothing is written")
}

func TestRejectOtherNeedsANote(t *testing.T) {
	res := runReject(t, operatorRemediationFixture, "--reason", "other")

	require.Error(t, res.err)
	assert.Empty(t, res.patchLog, "nothing is written")
	assert.Contains(t, res.err.Error(), "--note")
}

func TestRejectRefusesAReasonOutsideTheEnum(t *testing.T) {
	// Before this change --reason was free text; "will handle manually" is now
	// what --note is for.
	res := runReject(t, operatorRemediationFixture, "--reason", "will handle manually")

	require.Error(t, res.err)
	assert.Empty(t, res.patchLog, "nothing is written")
	for _, valid := range []string{"wrong-diagnosis", "too-risky", "not-now", "duplicate", "not-needed", "other"} {
		assert.Contains(t, res.err.Error(), valid, "the error lists the valid values")
	}
}

func TestRejectWithoutAReasonStillRejects(t *testing.T) {
	res := runReject(t, operatorRemediationFixture)

	require.NoError(t, res.err)
	patch := statusPatchOf(t, res.patchLog)
	assert.Equal(t, "Rejected", patch["status"].(map[string]any)["phase"])
	_, hasConds := patch["status"].(map[string]any)["conditions"]
	assert.False(t, hasConds, "no reason given, none recorded")
	assert.Contains(t, res.out, "--reason", "the output points at the enum")
}

func TestRejectNoteIsTruncated(t *testing.T) {
	res := runReject(t, operatorRemediationFixture, "--reason", "duplicate", "--note", strings.Repeat("é", 400))

	require.NoError(t, res.err)
	msg := firstCond(t, res.patchLog)["message"].(string)
	assert.LessOrEqual(t, len([]rune(msg)), maxRejectionNote)
}

func TestRejectReplacesAPriorReasonInsteadOfAppendingASecond(t *testing.T) {
	rem := strings.Replace(operatorRemediationFixture, `"phase": "Pending"`, `"phase": "Approved",
    "conditions": [{"type":"RejectionReason","status":"True","reason":"Duplicate","message":"","lastTransitionTime":"2026-06-24T10:00:00Z"}]`, 1)
	res := runReject(t, rem, "--reason", "not-needed")

	require.NoError(t, res.err)
	conds := conditionsOf(statusPatchOf(t, res.patchLog))
	require.Len(t, conds, 1, "two conditions of one type is an object the API server rejects")
	assert.Equal(t, "NotNeeded", conds[0]["reason"])
}

func TestRejectStillRefusesAPhaseThatCannotBeRejected(t *testing.T) {
	rem := strings.Replace(operatorRemediationFixture, `"phase": "Pending"`, `"phase": "Completed"`, 1)
	res := runReject(t, rem, "--reason", "duplicate")

	require.Error(t, res.err)
	assert.Empty(t, res.patchLog)
}

func TestParseRejectionReason(t *testing.T) {
	for _, r := range rejectionReasons {
		got, err := parseRejectionReason(r.flag)
		require.NoError(t, err)
		assert.Equal(t, r, got)
	}
	_, err := parseRejectionReason("")
	assert.Error(t, err)
	assert.Equal(t, "WrongDiagnosis", conditionReasonFor("wrong-diagnosis"))
	assert.Equal(t, "", conditionReasonFor("nope"))
	assert.Equal(t, "wrong-diagnosis", rejectionFlagFor("WrongDiagnosis"))
	assert.Equal(t, "", rejectionFlagFor("Nope"))
}
