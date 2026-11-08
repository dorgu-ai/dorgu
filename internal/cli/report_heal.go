package cli

import (
	"strings"
)

// Condition types and reasons the operator writes that the report reads.
const (
	conditionHealCredited      = "HealCredited"
	conditionWorkloadDiverged  = "WorkloadDiverged"
	conditionVerified          = "Verified"
	conditionRejected          = "Rejected"
	conditionProposalRefused   = "ProposalRefused"
	incidentOutcomeResolved    = "resolved"
	incidentOutcomePartial     = "partial"
	incidentOutcomeSelfResolve = "self-resolved"
)

// accountForHeals sorts every RemediationAction created in the window into a clean
// heal or the one reason it is not, following KPIS.md: a heal is clean when the
// RemediationAction Completed, HealCredited is True and the incident's outcome is
// resolved. Partial outcomes, self-resolved recoveries and HealCredited=False are
// never counted, however healthy the workload looks afterwards.
//
// unavailable, when not empty, says why the accounting cannot be done at all (the
// incidents were unreadable, or the operator predates the records it needs): the
// RemediationActions are still counted, but how they ended is no data, never zero.
func accountForHeals(ras []*reportRA, incidents map[string]*reportIncident, unavailable string) reportHealAccounting {
	h := reportHealAccounting{
		Total:           len(ras),
		Excluded:        map[string]int{},
		HealNotCredited: map[string]int{},
	}
	if unavailable != "" {
		for _, ra := range ras {
			if ra.Status.Phase == "Completed" {
				h.Completed++
			}
		}
		h.NoData, h.Reason = true, unavailable
		return h
	}
	for _, name := range allExclusions {
		h.Excluded[name] = 0
	}
	clean, safe := 0, 0

	for _, ra := range ras {
		if ra.Status.Phase == "Completed" {
			h.Completed++
			if c := ra.condition(conditionHealCredited); c != nil && c.Status == "False" {
				h.HealNotCredited[c.Reason]++
			}
		}
		category := classifyOutcome(ra, incidentOf(ra, incidents))
		if category != "" {
			h.Excluded[category]++
			continue
		}
		clean++
		if approvalGatePassed(ra) {
			safe++
		}
	}
	h.Clean, h.SafeHeals = &clean, &safe
	return h
}

// classifyOutcome returns the exclusion category of a RemediationAction, or ""
// for a clean heal.
func classifyOutcome(ra *reportRA, incident *reportIncident) string {
	switch ra.Status.Phase {
	case "Completed":
		return classifyCompleted(ra, incident)
	case "RolledBack":
		return excludedRolledBack
	case "Failed":
		if c := ra.condition(conditionGateRefused); c != nil && c.Status == "True" {
			return excludedRefusedAtGate
		}
		return excludedFailed
	case "Rejected":
		return excludedRejected
	case "Expired":
		return excludedExpired
	case "Acknowledged":
		return excludedAcknowledged
	default:
		return excludedInFlight
	}
}

// classifyCompleted decides a Completed RemediationAction. The order matters: a
// diverged or partial outcome is named first because it is the most specific
// reason, a self-resolved recovery next (HealCredited=False carries it too, and is
// broken down by reason separately), and only then the credit record itself.
func classifyCompleted(ra *reportRA, incident *reportIncident) string {
	outcome := ""
	if incident != nil && incident.Spec.Resolution != nil {
		outcome = incident.Spec.Resolution.Outcome
	}
	if outcome == incidentOutcomePartial {
		return excludedPartial
	}
	if c := ra.condition(conditionWorkloadDiverged); c != nil && c.Status == "True" {
		return excludedPartial
	}
	if outcome == incidentOutcomeSelfResolve {
		return excludedSelfResolved
	}
	credit := ra.condition(conditionHealCredited)
	switch {
	case credit == nil:
		return excludedNoCreditRecord
	case credit.Status != "True":
		return excludedHealNotCredited
	case outcome != incidentOutcomeResolved:
		return excludedOutcomeUnknown
	}
	return ""
}

// approvalGatePassed reports whether the gate's approval-time run cleared this
// action: status.gate is an ApprovalRecheck that passed or clamped.
func approvalGatePassed(ra *reportRA) bool {
	g := ra.Status.Gate
	if g == nil || g.Phase != "ApprovalRecheck" {
		return false
	}
	return strings.EqualFold(g.Result, "Pass") || strings.EqualFold(g.Result, "Clamp")
}
