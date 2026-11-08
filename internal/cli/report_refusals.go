package cli

import (
	"sort"
	"strings"
	"time"
)

// refusalKey identifies one row of the refusal mix.
type refusalKey struct{ phase, stage, reason string }

// collected is every refusal found, and how many of them happened before a
// RemediationAction existed.
type collected struct {
	rows         map[refusalKey]int
	proposalTime int

	// deferred counts incidents whose proposal is outstanding: not refusals.
	deferred int
}

// isDeferredProposal reports whether a ProposalRefused reason is really an
// outstanding proposal. The operator no longer writes it, but an operator before
// the fix wrote DedupCooldown_ActiveRemediation on every open incident one cycle
// after proposing, and those conditions stand until the incident goes. An incident
// waiting for a human to decide is the normal state, not a refusal, and it already
// has the RemediationAction that is waiting.
func isDeferredProposal(stage, guardrail string) bool {
	return stage == "DedupCooldown" && guardrail == "ActiveRemediation"
}

// collectRefusals gathers the refusals the operator persisted, from CRD state
// (Events expire, conditions do not):
//
//   - approval: a RemediationAction Failed with Applied=GateRefusedAtApproval. The
//     stage is the refusing verdict in status.gate; the reason is GateRefused's
//     reason, which is the guardrail enum.
//   - apply: Applied=PreconditionRejected. The ticket check is not a pipeline stage,
//     so the stage is "Ticket".
//   - proposal: an IncidentMemory ProposalRefused condition that is True, recorded as
//     <Stage>_<Reason>. Those created no RemediationAction.
func collectRefusals(ras []*reportRA, incidents []reportIncident, start time.Time) collected {
	c := collected{rows: map[refusalKey]int{}}

	for _, ra := range ras {
		refused := ra.condition(conditionGateRefused)
		if refused == nil || refused.Status != "True" {
			continue
		}
		phase, stage := reportPhaseApproval, refusingStage(ra)
		if appliedReasonOf(ra) == reasonPreconditionRejected {
			phase, stage = reportPhaseApply, reportStageTicket
		}
		c.rows[refusalKey{phase, stage, refused.Reason}]++
	}

	for i := range incidents {
		cond := incidents[i].condition(conditionProposalRefused)
		if cond == nil || cond.Status != "True" {
			continue
		}
		at, ok := parseReportTime(cond.LastTransitionTime)
		if !ok {
			at, ok = parseReportTime(incidents[i].Metadata.CreationTimestamp)
		}
		if !ok || at.Before(start) {
			continue
		}
		stage, reason := splitProposalRefusal(cond.Reason)
		if isDeferredProposal(stage, reason) {
			c.deferred++
			continue
		}
		c.rows[refusalKey{reportPhaseProposal, stage, reason}]++
		c.proposalTime++
	}
	return c
}

func appliedReasonOf(ra *reportRA) string {
	if c := ra.condition(conditionApplied); c != nil {
		return c.Reason
	}
	return ""
}

// refusingStage is the stage of the first Refuse verdict in status.gate.
func refusingStage(ra *reportRA) string {
	if g := ra.Status.Gate; g != nil {
		for _, v := range g.Verdicts {
			if strings.EqualFold(v.Verdict, "Refuse") && v.Stage != "" {
				return v.Stage
			}
		}
	}
	return reportStageUnknown
}

// splitProposalRefusal splits the IncidentMemory ProposalRefused reason, an enum
// compound of the stage and the guardrail (ModePolicy_SelfHealingMode), on its
// first underscore. A reason with no stage in it is kept whole under Unknown.
func splitProposalRefusal(reason string) (stage, guardrail string) {
	if stage, guardrail, ok := strings.Cut(reason, "_"); ok && stage != "" && guardrail != "" {
		return stage, guardrail
	}
	return reportStageUnknown, reason
}

// refusalMix turns the collected refusals into Q6's rows, largest first, and flags
// any reason that is a large share of proposals.
func refusalMix(proposals int, rows map[refusalKey]int, deferred int) reportRefusals {
	out := reportRefusals{Proposals: proposals, NoData: proposals == 0, Rows: []reportRefusal{}, Deferred: deferred}
	for k, n := range rows {
		share := ratio(n, proposals)
		out.Rows = append(out.Rows, reportRefusal{
			Phase: k.phase, Stage: k.stage, Reason: k.reason, Count: n, ShareOfProposals: share,
			Flagged: share != nil && *share > reportRefusalFlagShare && proposals >= reportMinSampleForFlag,
		})
	}
	sort.Slice(out.Rows, func(i, j int) bool {
		a, b := out.Rows[i], out.Rows[j]
		if a.Count != b.Count {
			return a.Count > b.Count
		}
		if a.Phase != b.Phase {
			return a.Phase < b.Phase
		}
		if a.Stage != b.Stage {
			return a.Stage < b.Stage
		}
		return a.Reason < b.Reason
	})
	return out
}
