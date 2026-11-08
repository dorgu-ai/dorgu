package cli

import (
	"fmt"
	"os"
	"strings"
)

// protectedContextsEnv names the environment variable that lists the kube
// contexts heal refuses to touch: comma-separated, case-insensitive substrings.
const protectedContextsEnv = "DORGU_PROTECTED_CONTEXTS"

// defaultProtectedContexts applies when the variable is unset or lists nothing.
var defaultProtectedContexts = []string{"prod"}

// protectedContextPatterns parses the DORGU_PROTECTED_CONTEXTS value.
//
// An unset, empty or all-blank value falls back to the default rather than
// disabling the guard: an empty variable is far more often an accident than a
// decision to heal production. Narrow the list instead, e.g.
// DORGU_PROTECTED_CONTEXTS=production,live.
func protectedContextPatterns(raw string) []string {
	var patterns []string
	for _, part := range strings.Split(raw, ",") {
		if p := strings.ToLower(strings.TrimSpace(part)); p != "" {
			patterns = append(patterns, p)
		}
	}
	if len(patterns) == 0 {
		return defaultProtectedContexts
	}
	return patterns
}

// protectedAction is the verb a protected-context refusal opens with. It names
// what the user asked for, because the guard is shared by three command paths
// and "refusing to heal" is false for one of them (F-8).
type protectedAction string

const (
	// actionHeal is `remediation heal`.
	actionHeal protectedAction = "heal"
	// actionApprove is `remediation approve --no-heal`: a cluster write (the
	// operator patches the persona) but no workload change.
	actionApprove protectedAction = "approve"
	// actionApproveAndHeal is `remediation approve`, which heals by default.
	actionApproveAndHeal protectedAction = "approve and heal"
)

// guardKubeContext refuses to heal against a context whose name matches one of
// the protected patterns from DORGU_PROTECTED_CONTEXTS (default: "prod").
//
// This is a name check on the client and nothing more: renaming a context
// defeats it (adversarial scenario CLI-04). It stays as a seatbelt until the
// harness binds the write to an apply ticket and the approver's identity.
func guardKubeContext(ctxName string) error {
	return guardKubeContextFor(ctxName, actionHeal)
}

// guardKubeContextFor is guardKubeContext for the command being run, so the
// refusal says "approve" to a user who only asked to approve.
func guardKubeContextFor(ctxName string, action protectedAction) error {
	return guardKubeContextWith(ctxName, action, protectedContextPatterns(os.Getenv(protectedContextsEnv)))
}

// guardKubeContextWith is guardKubeContextFor with the patterns made explicit.
func guardKubeContextWith(ctxName string, action protectedAction, patterns []string) error {
	name := strings.TrimSpace(ctxName)
	if name == "" {
		return fmt.Errorf("no active kube-context; point kubectl at your cluster first")
	}
	lower := strings.ToLower(name)
	for _, pattern := range patterns {
		if strings.Contains(lower, pattern) {
			return &protectedContextError{context: name, message: fmt.Sprintf(
				"refusing to %s: kube-context %q matches protected pattern %q (%s); switch to a non-production context",
				action, name, pattern, protectedContextsEnv)}
		}
	}
	return nil
}
