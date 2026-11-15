package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
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
// other call, who the user is. Only stdout is the SelfSubjectReview: a warning or a
// login prompt kubectl writes to stderr on a successful call must not make the JSON
// unreadable. Stderr is kept for the error text.
func resolveIdentity(ctx context.Context, kubeconfig string) (string, error) {
	cmd := kubectlCmd(ctx, kubeconfig, "auth", "whoami", "-o", "json")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return "", &identityError{why: "SelfSubjectReview is not available (" + kubectlErrText(joinOutput(stdout.Bytes(), stderr.Bytes())) + ")"}
	}
	name, err := parseWhoami(stdout.Bytes())
	if err != nil {
		why := "SelfSubjectReview answered, but " + err.Error()
		if text := kubectlErrText(stderr.Bytes()); text != "" {
			why += " (kubectl said: " + text + ")"
		}
		return "", &identityError{why: why}
	}
	return name, nil
}

// joinOutput is what a failed kubectl printed: its stderr, then any stdout.
func joinOutput(stdout, stderr []byte) []byte {
	return append(append([]byte(nil), stderr...), stdout...)
}

// displayUser is the username as shown on a terminal. The name itself is never
// altered (it is what is written and what the API server compares); only its
// display is escaped when it is not plain printable text without spaces, so padding
// or an invisible character cannot hide in a line that says who is acting.
func displayUser(name string) string {
	plain := name != ""
	for _, r := range name {
		if r == ' ' || !strconv.IsPrint(r) {
			plain = false
			break
		}
	}
	if plain {
		return name
	}
	return strconv.Quote(name)
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
	// The name is used byte for byte: the API server compares approvedBy with
	// request.userInfo.username exactly, so trimming it would turn a valid write into
	// an ApprovedByMismatch denial.
	name := review.Status.UserInfo.Username
	if strings.TrimSpace(name) == "" {
		return "", fmt.Errorf("it returned no username")
	}
	if len(name) > maxUsernameLength || !utf8.ValidString(name) || strings.ContainsFunc(name, func(r rune) bool {
		return !unicode.IsPrint(r)
	}) {
		return "", fmt.Errorf("the username is too long or not printable text")
	}
	return name, nil
}
