package cli

import (
	"os"
	"regexp"
	"strings"
)

// F-14: every dorgu command shells out to kubectl and folds stdout+stderr into
// the message it shows the user. kubectl writes client-go's klog lines to
// stderr, so a single failed API call surfaced as:
//
//	⚠ Could not fetch nodes: E0809 23:43:37.498989 72374 memcache.go:265] "Unhandled Error" err="couldn't get current server API group list: ..."
//	E0809 23:43:37.499844 72374 memcache.go:265] "Unhandled Error" err="couldn't get current server API group list: ..."
//	(three more identical lines)
//	Error from server (NotFound): the server could not find the requested resource
//
// Five lines of client-go internals in front of the one line that says what
// went wrong. This file strips the internals so the real error is the message.

// debugEnvVar, when set to a non-empty value, keeps kubectl's raw output
// (klog lines and all) in error messages. It exists so the noise is routed
// somewhere rather than lost: filtering the default view is only safe if the
// full text is still reachable when someone is actually debugging kubectl.
const debugEnvVar = "DORGU_DEBUG"

// klogLine matches a klog-formatted log line, the format client-go and kubectl
// use for their internal logging:
//
//	E0809 23:43:37.498989   72374 memcache.go:265] "Unhandled Error" err="..."
//	│└─ MMDD  └─ HH:MM:SS.micros └─ pid  └─ source:line]
//
// The severity letter, the numeric date/time, the pid and the `file.go:NN]`
// prefix together are specific enough that no kubectl error message matches
// by accident.
var klogLine = regexp.MustCompile(`^[IWEF][0-9]{4} [0-9]{2}:[0-9]{2}:[0-9]{2}\.[0-9]+\s+[0-9]+ \S+\.go:[0-9]+\] `)

// kubectlErrText turns kubectl's combined output into the text to show a user:
// klog lines removed, whitespace trimmed.
//
// Two deliberate refusals to be clever:
//
//   - If filtering would leave nothing, the original text is returned. An empty
//     error message is worse than a noisy one, and a tool that hides the only
//     thing kubectl said is exactly the silent-failure habit this fix set exists
//     to remove.
//   - With DORGU_DEBUG set, nothing is filtered at all.
func kubectlErrText(out []byte) string {
	raw := strings.TrimSpace(string(out))
	if raw == "" {
		return ""
	}
	if os.Getenv(debugEnvVar) != "" {
		return raw
	}

	cleaned := stripKlogLines(raw)
	if cleaned == "" {
		return raw
	}
	return cleaned
}

// kubectlValue cleans kubectl output that is a value rather than a message,
// e.g. the current kube-context name.
//
// This is not cosmetic. currentKubeContext feeds the guard that refuses to heal
// against a production context; a klog line glued to the front of the context
// name would make that comparison meaningless.
func kubectlValue(out []byte) string {
	return stripKlogLines(string(out))
}

// stripKlogLines removes klog-formatted lines and trims the remainder.
func stripKlogLines(s string) string {
	lines := strings.Split(s, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if klogLine.MatchString(strings.TrimLeft(line, " \t")) {
			continue
		}
		kept = append(kept, line)
	}
	return strings.TrimSpace(strings.Join(kept, "\n"))
}
