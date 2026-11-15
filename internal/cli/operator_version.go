package cli

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/dorgu-ai/dorgu-platform-v1/pkg/report"
	"github.com/dorgu-ai/dorgu/internal/output"
)

// ticketV1Operator is the first operator release that issues ticket v1 (specHash,
// approvedBy, expiresAt). This CLI heals only under ticket v1 (P2-D9, P2-D15), so an
// older operator can be approved against (the approval is recorded) but never
// healed against.
const ticketV1Operator = "v0.14.0"

// needsOperatorV014 is the one sentence every old-operator refusal carries.
const needsOperatorV014 = "this CLI needs operator v0.14 or later; upgrade the operator"

// operatorTooOldError is a ticket with none of the v1 fields: the operator on the
// cluster issues ticket v0, so it is older than v0.14.
type operatorTooOldError struct{ detail string }

func (e *operatorTooOldError) Error() string {
	return needsOperatorV014 + " (" + e.detail + ")"
}

// ticketIsV0 reports whether a ticket carries none of the three v1 fields.
func ticketIsV0(t *applyTicket) bool {
	return strings.TrimSpace(t.SpecHash) == "" && strings.TrimSpace(t.ApprovedBy) == "" &&
		strings.TrimSpace(t.ExpiresAt) == ""
}

func errV0Ticket() error {
	return &operatorTooOldError{detail: "the apply ticket has no specHash, approvedBy or expiresAt, so the operator issues ticket v0"}
}

// releaseVersion reads a plain release tag, vMAJOR.MINOR.PATCH (the v is optional).
// Anything else is not a version this CLI will compare: a pre-release (-rc.1), build
// metadata (+build5), a git-describe tag (v0.13.0-5-gSHA), a pseudo-version
// (v0.13.2-0.YYYYMMDDhhmmss-SHA), a dev tag, a digest or nothing. Such a tag may be
// built from code newer than its base release, so reading it as that base would
// refuse a working operator (C5b R-3).
func releaseVersion(tag *string) ([3]int, bool) {
	if tag == nil || strings.ContainsAny(*tag, "-+") {
		return [3]int{}, false
	}
	return report.ParseVersion(*tag)
}

// operatorVersionKnown reports whether the tag is a plain release version.
func operatorVersionKnown(tag *string) bool {
	_, ok := releaseVersion(tag)
	return ok
}

// operatorPredatesTicketV1 reports whether the operator's image tag is a release
// before v0.14.0. A tag that is not a plain release (a dev or pseudo-version build,
// a pre-release, a digest, nothing) is unknown, and unknown is not refused: the
// ticket itself settles it later.
func operatorPredatesTicketV1(tag *string) bool {
	have, ok := releaseVersion(tag)
	if !ok {
		return false
	}
	want, _ := report.ParseVersion(ticketV1Operator)
	for i := range have {
		if have[i] != want[i] {
			return have[i] < want[i]
		}
	}
	return false
}

// refuseOperatorBeforeV014 is asked before an approval that will be followed by a
// heal is written. When the cluster's operator is a release older than v0.14 the
// heal can never happen (every ticket it issues is v0), so approving would only
// record a decision the CLI then cannot act on. It says so, writes nothing, and
// returns true.
func refuseOperatorBeforeV014(ctx context.Context, kubeconfig string, w io.Writer) bool {
	version := operatorVersionReader(ctx, kubeconfig)
	if !operatorPredatesTicketV1(version) {
		return false
	}
	_, _ = fmt.Fprintln(w)
	output.Error(fmt.Sprintf("Cannot heal: %s (the operator on this cluster is %s).", needsOperatorV014, *version))
	output.Info("Nothing was approved and nothing was changed. Upgrade the operator (and apply its CRDs). " +
		"Or record the decision alone with `dorgu remediation approve --no-heal`; " +
		"once the operator is upgraded, `dorgu remediation heal` can apply it.")
	return true
}

// printOperatorTooOld is shown when a ticket v0 arrives: the approval stands, the
// CLI applied nothing.
func printOperatorTooOld(err error) {
	output.Error(err.Error())
	output.Info("The approval is recorded. Nothing was applied to the workload. " +
		"Upgrade the operator (and apply its CRDs), or apply the change yourself.")
}
