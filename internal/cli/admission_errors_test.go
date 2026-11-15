package cli

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// P2 §3: every admission denial message starts `dorgu: <Reason>: `. The CLI maps
// the token to one plain sentence and never parses the rest. The policies do not
// exist yet (brief C2), so these tests use synthetic API errors carrying the
// messages the contract describes.

// vapDenied wraps a policy message the way the API server reports a
// ValidatingAdmissionPolicy denial.
func vapDenied(message string) string {
	return `The remediationactions "fix-oom-api-server" is invalid: status: ` +
		`ValidatingAdmissionPolicy 'dorgu-remediation-identity' with binding 'dorgu-remediation-identity' denied request: ` + message
}

func TestFriendlyAdmissionKnownReasons(t *testing.T) {
	tests := []struct {
		token string
		want  []string
	}{
		{"ProposerMismatch", []string{"proposer", "account that sent"}},
		{"OperatorOnly", []string{"Only the Dorgu operator"}},
		{"SpecImmutable", []string{"cannot be edited"}},
		{"ServiceAccountCannotApprove", []string{"Service accounts", "your own"}},
		{"ApprovedByMismatch", []string{"does not match your authenticated username", "Upgrade"}},
		{"ApproverIsProposer", []string{"You proposed this, so someone else has to approve it."}},
		{"MissingApproveVerb", []string{"Your account lacks the approve permission on remediations in shop; ask an admin for the dorgu-approver role."}},
		{"InvalidTransition", []string{"Pending", "Approved or Rejected"}},
		{"TargetNamespaceMismatch", []string{"namespace", "workload"}},
	}
	for _, tt := range tests {
		t.Run(tt.token, func(t *testing.T) {
			raw := vapDenied("dorgu: " + tt.token + ": details that the CLI must not parse")

			sentence, known := friendlyAdmission(raw, "shop")

			require.True(t, known)
			for _, w := range tt.want {
				assert.Contains(t, sentence, w)
			}
			assert.NotContains(t, sentence, "details that the CLI must not parse", "nothing beyond the token is read")
			assert.NotContains(t, sentence, "ValidatingAdmissionPolicy")
			assert.NotContains(t, sentence, "—", "no em dashes in user-facing text")
			assert.NotContains(t, sentence, "–")
		})
	}
}

func TestFriendlyAdmissionEveryContractReasonIsMapped(t *testing.T) {
	// The nine reasons of the contract (P2-execution §3). A new reason has to be
	// added here and to the map deliberately.
	contract := []string{
		"ProposerMismatch", "OperatorOnly", "SpecImmutable", "ServiceAccountCannotApprove", "ApprovedByMismatch",
		"ApproverIsProposer", "MissingApproveVerb", "InvalidTransition", "TargetNamespaceMismatch",
	}
	assert.Len(t, admissionReasons, len(contract))
	for _, token := range contract {
		_, ok := admissionReasons[token]
		assert.True(t, ok, token)
	}
}

func TestFriendlyAdmissionUnknownTokenFallsBackToTheRawMessage(t *testing.T) {
	raw := vapDenied("dorgu: SomethingNew: the new policy says no")

	sentence, known := friendlyAdmission(raw, "shop")

	assert.False(t, known)
	assert.Equal(t, raw, sentence)
}

func TestFriendlyAdmissionIgnoresMessagesWithoutAToken(t *testing.T) {
	for _, raw := range []string{
		`Error from server (Forbidden): remediationactions.dorgu.io is forbidden`,
		vapDenied("some other policy: ApproverIsProposer: not ours"),
		vapDenied("dorgu:ApproverIsProposer: no space"),
		vapDenied("dorgu: lowercase: not a CamelCase token"),
		"",
	} {
		sentence, known := friendlyAdmission(raw, "shop")
		assert.False(t, known, raw)
		assert.Equal(t, raw, sentence, raw)
	}
}

func TestFriendlyAdmissionTakesTheFirstTokenOnly(t *testing.T) {
	raw := vapDenied("dorgu: ApproverIsProposer: x dorgu: MissingApproveVerb: y")

	sentence, known := friendlyAdmission(raw, "shop")

	require.True(t, known)
	assert.Contains(t, sentence, "You proposed this")
	assert.NotContains(t, sentence, "approve permission")
}

func TestAdmissionDeniedErrorShowsTheFriendlySentenceAndKeepsTheRaw(t *testing.T) {
	err := classifyWriteError(vapDenied("dorgu: ApproverIsProposer: you are the proposer"), "shop")

	var denied *admissionDeniedError
	require.True(t, errors.As(err, &denied))
	assert.Equal(t, "ApproverIsProposer", denied.Token)
	assert.Equal(t, "You proposed this, so someone else has to approve it.", err.Error())
	assert.Contains(t, denied.Raw, "denied request")
	assert.False(t, isConflictError(err), "a denial is never retried as a conflict")
}

func TestClassifyWriteErrorLeavesOtherErrorsAlone(t *testing.T) {
	err := classifyWriteError("Error from server (Conflict): the object has been modified", "shop")

	var denied *admissionDeniedError
	assert.False(t, errors.As(err, &denied))
	assert.True(t, isConflictError(err))
	assert.Equal(t, "Error from server (Conflict): the object has been modified", err.Error())
}

func TestClassifyWriteErrorUnknownTokenKeepsTheRawText(t *testing.T) {
	raw := vapDenied("dorgu: SomethingNew: the new policy says no")

	err := classifyWriteError(raw, "shop")

	assert.Equal(t, raw, err.Error())
}

// --- through the commands ---

func TestApproveShowsAFriendlyDenialAndSaysNothingWasApproved(t *testing.T) {
	r := baseResponses()
	r.rem = withResourceVersion(operatorRemediationFixture, "4242")
	r.statusPatchError = vapDenied("dorgu: ApproverIsProposer: dorgu: the approver proposed this action")

	res := approveNoHeal(t, r)

	require.Error(t, res.err)
	assert.Equal(t, ExitDeclined, ExitCode(res.err))
	assert.Contains(t, res.out, "You proposed this, so someone else has to approve it.")
	assert.Contains(t, res.out, "Nothing was approved")
	assert.NotContains(t, res.out, "Remediation approved")
	assert.NotContains(t, res.out, "ValidatingAdmissionPolicy")
	assert.Equal(t, 1, strings.Count(res.patchLog, "--subresource status"), "a denial is not retried")
}

func TestApproveMissingVerbNamesTheNamespaceAndTheRole(t *testing.T) {
	r := baseResponses()
	r.rem = withResourceVersion(operatorRemediationFixture, "4242")
	r.statusPatchError = vapDenied("dorgu: MissingApproveVerb: no approve verb")

	res := approveNoHeal(t, r)

	require.Error(t, res.err)
	assert.Contains(t, res.out, "Your account lacks the approve permission on remediations in production; ask an admin for the dorgu-approver role.")
}

func TestApproveUnknownDenialShowsTheRawMessage(t *testing.T) {
	r := baseResponses()
	r.rem = withResourceVersion(operatorRemediationFixture, "4242")
	raw := vapDenied("dorgu: SomethingNew: the new policy says no")
	r.statusPatchError = raw

	res := approveNoHeal(t, r)

	require.Error(t, res.err)
	assert.Contains(t, res.out, "dorgu: SomethingNew: the new policy says no")
}

func TestRejectShowsAFriendlyDenial(t *testing.T) {
	r := fakeKubectlResponses{
		context: "kind-dorgu-spike", rem: operatorRemediationFixture, persona: personaFixture,
		deployment: deploymentListFixture, noTicket: true,
		statusPatchError: vapDenied("dorgu: ServiceAccountCannotApprove: system users cannot reject"),
	}

	res := runRejectWith(t, r, "--reason", "too-risky")

	require.Error(t, res.err)
	assert.Contains(t, res.out, "Service accounts and other system users cannot approve or reject")
	assert.Contains(t, res.out, "Nothing was rejected")
}

// --- C5 review F-6: the token counts only in the policy denial position ---

func TestFriendlyAdmissionAcceptsTheTokenOnlyWhereThePolicyPutsIt(t *testing.T) {
	for name, raw := range map[string]string{
		"validating admission policy": vapDenied("dorgu: ApproverIsProposer: x"),
		"webhook wording":             `Error from server (Forbidden): admission webhook "dorgu.io" denied the request: dorgu: ApproverIsProposer: x`,
		"the API status message":      "dorgu: ApproverIsProposer: x",
		"message with a leading line": "\n dorgu: ApproverIsProposer: x",
	} {
		t.Run(name, func(t *testing.T) {
			sentence, known := friendlyAdmission(raw, "shop")
			require.True(t, known)
			assert.Contains(t, sentence, "You proposed this")
		})
	}
}

func TestFriendlyAdmissionIsNotSpoofedByEchoedUserText(t *testing.T) {
	for name, raw := range map[string]string{
		"token in a rejected field value": `The remediationactions "x" is invalid: status.approvedBy: Invalid value: ` +
			`"dorgu: ApproverIsProposer: x": must not ...`,
		"token in the username of an RBAC refusal": `Error from server (Forbidden): remediationactions.dorgu.io "x" is forbidden: ` +
			`User "dorgu: MissingApproveVerb: x" cannot patch resource "remediationactions/status" in API group "dorgu.io"`,
		"whole denial phrase inside a quoted username": `Error from server (Forbidden): remediationactions.dorgu.io "x" is forbidden: ` +
			`User "eve denied request: dorgu: ApproverIsProposer: x" cannot patch resource`,
		"token after other free text": "something else happened: dorgu: OperatorOnly: x",
		// C5b R-2: the API server prints a username with %q, so a quote inside it is
		// escaped; the escaped quote must not close the value.
		"username with an escaped quote, as %q prints it": `Error from server (Forbidden): remediationactions.dorgu.io "x" is forbidden: ` +
			fmt.Sprintf("User %q cannot patch resource", `a" denied request: dorgu: MissingApproveVerb: z`),
		"webhook phrase after an escaped quote": `Invalid value: ` +
			fmt.Sprintf("%q", `q" denied the request: dorgu: ApproverIsProposer: `) + `: must not ...`,
	} {
		t.Run(name, func(t *testing.T) {
			sentence, known := friendlyAdmission(raw, "shop")
			assert.False(t, known)
			assert.Equal(t, raw, sentence)

			err := classifyWriteError(raw, "shop")
			assert.False(t, isAdmissionDenial(err), "not a Dorgu denial: it must not exit 4 with a wrong cause")
			assert.Equal(t, raw, err.Error())
		})
	}
}

func TestApproveDoesNotMistakeAnEchoedUsernameForADenial(t *testing.T) {
	r := baseResponses()
	r.rem = withResourceVersion(operatorRemediationFixture, "4242")
	r.statusPatchError = `Error from server (Forbidden): remediationactions.dorgu.io "x" is forbidden: ` +
		`User "dorgu: MissingApproveVerb: x" cannot patch resource`

	res := approveNoHeal(t, r)

	require.Error(t, res.err)
	assert.NotEqual(t, ExitDeclined, ExitCode(res.err))
	assert.NotContains(t, res.out, "lacks the approve permission")
}
