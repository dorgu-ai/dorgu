package cli

import (
	"errors"
	"os"
	"regexp"
	"strings"
)

// Admission denials (P2 execution plan §3). Every ValidatingAdmissionPolicy message
// the operator's chart ships starts with `dorgu: <Reason>: ` where <Reason> is one
// CamelCase token. The CLI maps the token to one plain sentence that says what
// happened and what to do. It never parses anything after the token: the rest of
// the message is the policy author's, and may carry text that is not ours to
// interpret. A token this CLI does not know falls back to the raw message, so a
// newer operator's new reason is shown rather than hidden.

// namespacePlaceholder is replaced with the namespace the command acted in.
const namespacePlaceholder = "<ns>"

// admissionReasons is the §3 table, one sentence per reason. No em dashes.
var admissionReasons = map[string]string{
	"ProposerMismatch": "The proposal names a proposer other than the account that sent it. " +
		"A proposal must name the account that creates it.",
	"OperatorOnly": "Only the Dorgu operator may create that object or write that field. " +
		"Use `dorgu remediation approve` or `reject` to record a decision.",
	"SpecImmutable": "The spec of a remediation or proposal cannot be edited after it is created. " +
		"Reject it and wait for a new proposal.",
	"ServiceAccountCannotApprove": "Service accounts and other system users cannot approve or reject remediations. " +
		"Run this as a person, with your own credentials.",
	"ApprovedByMismatch": "The approver this command recorded does not match your authenticated username. " +
		"Upgrade the CLI to v0.15.0 or later; if you already have, something is writing for you.",
	"ApproverIsProposer": "You proposed this, so someone else has to approve it.",
	"MissingApproveVerb": "Your account lacks the approve permission on remediations in " + namespacePlaceholder +
		"; ask an admin for the dorgu-approver role.",
	"InvalidTransition": "That is not a change a person may make. " +
		"A remediation can only go from Pending to Approved or Rejected.",
	"TargetNamespaceMismatch": "The proposal's namespace must be the namespace of the workload it targets.",
}

// admissionTokenRe finds the contract's prefix. The leftmost match wins: the text
// before it is the API server's own.
var admissionTokenRe = regexp.MustCompile(`dorgu: ([A-Z][A-Za-z0-9]*): `)

// friendlyAdmission returns the sentence for a denial whose message carries a known
// reason token, and true. For an unknown token, or a message with none, it returns
// the raw message and false.
func friendlyAdmission(raw, namespace string) (string, bool) {
	m := admissionTokenRe.FindStringSubmatch(raw)
	if m == nil {
		return raw, false
	}
	sentence, ok := admissionReasons[m[1]]
	if !ok {
		return raw, false
	}
	return strings.ReplaceAll(sentence, namespacePlaceholder, namespace), true
}

// admissionDeniedError is a write the API server refused with a dorgu: <Reason>
// message. Error is the friendly sentence for a known reason and the raw message
// for an unknown one; Raw always keeps what the API server said.
type admissionDeniedError struct {
	Token     string
	Raw       string
	Namespace string
	known     bool
}

func (e *admissionDeniedError) Error() string {
	if !e.known {
		return e.Raw
	}
	sentence, _ := friendlyAdmission(e.Raw, e.Namespace)
	if os.Getenv(debugEnvVar) != "" {
		return sentence + " (raw: " + e.Raw + ")"
	}
	return sentence
}

// classifyWriteError turns the text kubectl printed for a refused write into an
// error. A message carrying the contract's prefix becomes an admissionDeniedError
// (whose token is set even when this CLI does not know it); anything else stays a
// plain error with the text unchanged.
func classifyWriteError(raw, namespace string) error {
	m := admissionTokenRe.FindStringSubmatch(raw)
	if m == nil {
		return errors.New(raw)
	}
	_, known := friendlyAdmission(raw, namespace)
	return &admissionDeniedError{Token: m[1], Raw: raw, Namespace: namespace, known: known}
}
