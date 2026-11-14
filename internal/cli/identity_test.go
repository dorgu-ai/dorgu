package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Task 2.7 (P2-D4): approvedBy is the authenticated username from a
// SelfSubjectReview, never a name the CLI made up. If the cluster cannot say who
// the user is, the CLI refuses.

// recordTestUser is the authenticated user the record tests write as.
const recordTestUser = "frank@example.com"

func TestParseWhoami(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    string
		wantErr string
	}{
		{"a user", whoamiJSON("alice@example.com"), "alice@example.com", ""},
		{"an IAM-style name", whoamiJSON("arn:aws:iam::123456789012:user/alice"), "arn:aws:iam::123456789012:user/alice", ""},
		{"surrounding space is not part of the name", whoamiJSON("  alice  "), "alice", ""},
		{"empty username", whoamiJSON(""), "", "no username"},
		{"blank username", whoamiJSON("   "), "", "no username"},
		{"no status", `{"kind":"SelfSubjectReview"}`, "", "no username"},
		{"not json", `error: unknown command "whoami"`, "", "could not read"},
		{"empty output", ``, "", "could not read"},
		{"a control character in the name", `{"status":{"userInfo":{"username":"al\u0007ice"}}}`, "", "not printable"},
		{"a line break in the name", `{"status":{"userInfo":{"username":"alice\nbob"}}}`, "", "not printable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseWhoami([]byte(tt.raw))
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				assert.Empty(t, got)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestResolveIdentityAsksTheClusterThroughKubectl(t *testing.T) {
	patchLog := writeFakeKubectlDispatch(t, fakeKubectlResponses{context: "kind-dorgu-spike", whoami: whoamiJSON("carol@example.com")})

	got, err := resolveIdentity(t.Context(), "")

	require.NoError(t, err)
	assert.Equal(t, "carol@example.com", got)
	assert.Contains(t, readCallLog(t, patchLog), "auth whoami -o json")
}

func TestResolveIdentityRefusesWhenSelfSubjectReviewIsUnavailable(t *testing.T) {
	for name, stderr := range map[string]string{
		"old kubectl": `error: unknown command "whoami" for "kubectl auth"`,
		"old cluster": `Error from server (NotFound): the server could not find the requested resource (post selfsubjectreviews.authentication.k8s.io)`,
		"forbidden":   `Error from server (Forbidden): selfsubjectreviews.authentication.k8s.io is forbidden`,
	} {
		t.Run(name, func(t *testing.T) {
			writeFakeKubectlDispatch(t, fakeKubectlResponses{context: "kind-dorgu-spike", whoamiError: stderr})

			got, err := resolveIdentity(t.Context(), "")

			require.Error(t, err)
			assert.Empty(t, got, "no name is made up when the cluster cannot say who you are")
			assert.Contains(t, err.Error(), "SelfSubjectReview")
			assert.Contains(t, err.Error(), "Nothing was")
			assert.NotContains(t, err.Error(), "cli-user")
		})
	}
}

func TestResolveIdentityRefusesAnEmptyUsername(t *testing.T) {
	writeFakeKubectlDispatch(t, fakeKubectlResponses{context: "kind-dorgu-spike", whoami: whoamiJSON("")})

	got, err := resolveIdentity(t.Context(), "")

	require.Error(t, err)
	assert.Empty(t, got)
	assert.Contains(t, err.Error(), "no username")
}

// --- approve ---

func TestApproveRecordsTheAuthenticatedUserAndSaysSoBeforeTheWrite(t *testing.T) {
	r := baseResponses()
	r.whoami = whoamiJSON("dana@example.com")
	r.rem = withResourceVersion(operatorRemediationFixture, "4242")
	r.remAfterApprove = withApprovePhase(r.rem, "Approved")

	res := approveNoHeal(t, r)

	require.NoError(t, res.err)
	assert.Contains(t, res.patchLog, `"approvedBy":"dana@example.com"`)
	assert.NotContains(t, res.patchLog, "cli-user")
	assert.Contains(t, res.out, "Approving as dana@example.com")

	calls := readCallLog(t, res.logPath)
	who, write := strings.Index(calls, "auth whoami"), strings.Index(calls, "--subresource status")
	require.GreaterOrEqual(t, who, 0, calls)
	require.GreaterOrEqual(t, write, 0, calls)
	assert.Less(t, who, write, "the identity is read before anything is written")
}

func TestApproveWritesNothingWhenTheIdentityCannotBeResolved(t *testing.T) {
	for name, mutate := range map[string]func(*fakeKubectlResponses){
		"unavailable":    func(r *fakeKubectlResponses) { r.whoamiError = `error: unknown command "whoami"` },
		"empty username": func(r *fakeKubectlResponses) { r.whoami = whoamiJSON("") },
	} {
		t.Run(name, func(t *testing.T) {
			r := baseResponses()
			r.deployment = withIdentity(t, r.deployment)
			r.rem = withResourceVersion(operatorRemediationFixture, "4242")
			mutate(&r)

			res := runApprove(t, r)

			require.Error(t, res.err)
			assert.Empty(t, res.patchLog, "nothing is approved, patched or recorded")
			assert.Contains(t, res.out, "SelfSubjectReview")
			assert.NotContains(t, res.out, "Remediation approved")
		})
	}
}

// --- reject ---

func TestRejectRecordsTheAuthenticatedUser(t *testing.T) {
	res := runRejectAs(t, "erin@example.com", operatorRemediationFixture, "--reason", "too-risky")

	require.NoError(t, res.err)
	status := statusPatchOf(t, res.patchLog)["status"].(map[string]any)
	assert.Equal(t, "Rejected", status["phase"])
	assert.Equal(t, "erin@example.com", status["approvedBy"])
	assert.Contains(t, res.out, "Rejecting as erin@example.com")
}

func TestRejectWithoutAReasonRecordsTheAuthenticatedUserToo(t *testing.T) {
	res := runRejectAs(t, "erin@example.com", operatorRemediationFixture)

	require.NoError(t, res.err)
	status := statusPatchOf(t, res.patchLog)["status"].(map[string]any)
	assert.Equal(t, "erin@example.com", status["approvedBy"])
}

func TestRejectWritesNothingWhenTheIdentityCannotBeResolved(t *testing.T) {
	res := runRejectAs(t, "", operatorRemediationFixture, "--reason", "too-risky")

	require.Error(t, res.err)
	assert.Empty(t, res.patchLog)
	assert.Contains(t, res.out, "SelfSubjectReview")
}

// --- heal record ---

func TestHealRecordImplicitApprovalUsesTheGivenUser(t *testing.T) {
	st := recordState{Phase: "Pending", ResourceVersion: "4711"}

	patch, err := buildHealRecordPatch(st, healRecordFor(), "2026-08-27T12:00:00Z", recordTestUser)
	require.NoError(t, err)

	assert.Contains(t, patch, `"approvedBy":"`+recordTestUser+`"`)
	assert.NotContains(t, patch, "cli-user")
}

func TestHealRecordNeverInventsAnApprover(t *testing.T) {
	st := recordState{Phase: "Pending", ResourceVersion: "4711"}

	_, err := buildHealRecordPatch(st, healRecordFor(), "2026-08-27T12:00:00Z", "")

	require.Error(t, err, "an implicit approval with no authenticated user is not written")
}

func TestHealRecordOfAnApprovedActionWritesNoApprover(t *testing.T) {
	st := recordState{Phase: "Applying", ResourceVersion: "4711"}

	patch, err := buildHealRecordPatch(st, healRecordFor(), "2026-08-27T12:00:00Z", recordTestUser)
	require.NoError(t, err)

	assert.NotContains(t, patch, "approvedBy", "somebody else's approval is not overwritten")
}

// runRejectAs runs `reject` as the given user; an empty user makes the
// SelfSubjectReview fail.
func runRejectAs(t *testing.T, user, rem string, args ...string) rejectRun {
	t.Helper()
	r := fakeKubectlResponses{
		context: "kind-dorgu-spike", rem: rem, persona: personaFixture, deployment: deploymentListFixture, noTicket: true,
	}
	if user == "" {
		r.whoamiError = `error: unknown command "whoami"`
	} else {
		r.whoami = whoamiJSON(user)
	}
	return runRejectWith(t, r, args...)
}

func TestBuildApprovePatchNeverInventsAnApprover(t *testing.T) {
	for _, user := range []string{"", "   "} {
		_, err := buildApprovePatch("42", "2026-08-27T12:00:00Z", user)
		require.Error(t, err)
	}

	patch, err := buildApprovePatch("42", "2026-08-27T12:00:00Z", "gina@example.com")
	require.NoError(t, err)
	assert.Contains(t, patch, `"approvedBy":"gina@example.com"`)
}

func TestWriteRejectionNeverInventsAnApprover(t *testing.T) {
	patchLog := writeFakeKubectlDispatch(t, fakeKubectlResponses{context: "kind-dorgu-spike", rem: operatorRemediationFixture, noTicket: true})

	err := writeRejection(t.Context(), "", "production", "r", "", nil, "")

	require.Error(t, err)
	assert.Empty(t, readPatchLog(t, patchLog))
}

func TestIdentityErrorDoesNotSuggestAFallback(t *testing.T) {
	err := &identityError{why: "SelfSubjectReview is not available (x)"}

	assert.Contains(t, err.Error(), "never makes a name up")
	assert.NotContains(t, err.Error(), "\u2014")
}

func TestAdmissionErrorShowsTheRawMessageUnderDebug(t *testing.T) {
	t.Setenv(debugEnvVar, "1")
	err := classifyWriteError(vapDenied("dorgu: ApproverIsProposer: x"), "shop")

	assert.Contains(t, err.Error(), "You proposed this, so someone else has to approve it.")
	assert.Contains(t, err.Error(), "(raw: ")
}

func TestRemediationFullKeepsTheServedSpecAndRejectsGarbage(t *testing.T) {
	var rem remediationFull
	require.NoError(t, json.Unmarshal([]byte(`{"spec":{"b":1,"a":[]},"status":{"phase":"Pending"}}`), &rem))
	assert.JSONEq(t, `{"b":1,"a":[]}`, string(rem.rawSpec))
	assert.Equal(t, "Pending", rem.Status.Phase)

	require.Error(t, json.Unmarshal([]byte(`{"spec":`), &rem))
	require.Error(t, json.Unmarshal([]byte(`{"spec":"not an object"}`), &rem))
}
