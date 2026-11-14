package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Task 2.7 (P2-D4): who approved is a fact the API server tells the CLI, not one
// the CLI chooses. `approvedBy` is the username of the SelfSubjectReview
// (authentication.k8s.io/v1), which is what `kubectl auth whoami` prints. The
// admission policy (brief C2) denies any approvedBy that is not the requester's own
// username, so a made-up name could never be accepted anyway; the CLI refuses to
// write one at all.
//
// When the cluster cannot say who the user is (kubectl or the API server without
// SelfSubjectReview, a denied review, an empty username) there is no fallback: the
// command stops before it writes anything.

// maxUsernameLength mirrors the operator's limit on status.approvedBy (253).
const maxUsernameLength = 253

// identityError is the refusal to act without an authenticated username.
type identityError struct{ why string }

func (e *identityError) Error() string {
	return "Cannot tell who you are: " + e.why + ". Nothing was approved, rejected or written. " +
		"Dorgu records the approver from the cluster's SelfSubjectReview (`kubectl auth whoami`) and never makes a name up. " +
		"This needs Kubernetes 1.28 or newer and kubectl 1.28 or newer."
}

// resolveIdentity asks the cluster, through the credentials kubectl uses for every
// other call, who the user is.
func resolveIdentity(ctx context.Context, kubeconfig string) (string, error) {
	out, err := kubectlCmd(ctx, kubeconfig, "auth", "whoami", "-o", "json").CombinedOutput()
	if err != nil {
		return "", &identityError{why: "SelfSubjectReview is not available (" + kubectlErrText(out) + ")"}
	}
	name, err := parseWhoami(out)
	if err != nil {
		return "", &identityError{why: "SelfSubjectReview answered, but " + err.Error()}
	}
	return name, nil
}

// parseWhoami reads the username out of a SelfSubjectReview as `kubectl auth whoami
// -o json` prints it. An empty or unprintable name is an error, never a default.
func parseWhoami(raw []byte) (string, error) {
	var review struct {
		Status struct {
			UserInfo struct {
				Username string `json:"username"`
			} `json:"userInfo"`
		} `json:"status"`
	}
	if err := json.Unmarshal(raw, &review); err != nil {
		return "", fmt.Errorf("could not read its answer: %w", err)
	}
	name := strings.TrimSpace(review.Status.UserInfo.Username)
	if name == "" {
		return "", fmt.Errorf("it returned no username")
	}
	if len(name) > maxUsernameLength || !utf8.ValidString(name) || strings.ContainsFunc(name, func(r rune) bool {
		return !unicode.IsPrint(r)
	}) {
		return "", fmt.Errorf("the username is too long or not printable text")
	}
	return name, nil
}
