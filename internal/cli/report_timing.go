package cli

import "time"

// timeToHeal is KPI Q1: how long each stage of a heal took, from timestamps the
// cluster already holds. A stage counts only RemediationActions that have both of
// its timestamps; n says how many.
//
//	detect_to_proposal    incident detection.firstSeen -> RemediationAction created
//	proposal_to_decision  created -> approvedAt (approved), or the Rejected condition (rejected)
//	decision_to_applied   approvedAt -> appliedAt
//	applied_to_verified   appliedAt -> the Verified condition turning True
//
// proposal_to_decision is a human's time and is tracked, not targeted.
//
// detect_to_proposal is a machine latency (target p90 <= 90 s), so it is measured
// once per incident: from first detection to its FIRST RemediationAction. A second
// proposal for the same incident (after a failure, a rollback or a rejection)
// would be dated from the original detection and add hours to it, so it adds no
// sample. all is every RemediationAction on the cluster, so an incident whose first
// proposal is older than the window is not measured from a later one.
// detectGone, when not empty, is why the incidents cannot be read.
func timeToHeal(ras []*reportRA, all []reportRA, incidents map[string]*reportIncident, detectGone string) reportTimeToHeal {
	var d2p, p2d, d2a, a2v []float64
	add := func(dst *[]float64, secs float64, ok bool) {
		if ok {
			*dst = append(*dst, secs)
		}
	}

	firstFor := firstProposals(all)

	for _, ra := range ras {
		created, okCreated := ra.created()

		if inc := incidentOf(ra, incidents); inc != nil && firstFor[incidentKeyOf(ra)] == ra.key() {
			first, okFirst := parseReportTime(inc.Spec.Detection.FirstSeen)
			secs, ok := secondsBetween(first, created, okFirst, okCreated)
			add(&d2p, secs, ok)
		}

		approved, okApproved := parseReportTime(ra.Status.ApprovedAt)
		if ra.Status.Phase == "Rejected" {
			// A rejection has no approvedAt; the operator dates it with the
			// Rejected condition the first time it sees it.
			decided, okDecided := condTime(ra.condition(conditionRejected))
			secs, ok := secondsBetween(created, decided, okCreated, okDecided)
			add(&p2d, secs, ok)
		} else {
			secs, ok := secondsBetween(created, approved, okCreated, okApproved)
			add(&p2d, secs, ok)
		}

		applied, okApplied := parseReportTime(ra.Status.AppliedAt)
		secs, ok := secondsBetween(approved, applied, okApproved, okApplied)
		add(&d2a, secs, ok)

		if v := ra.condition(conditionVerified); v != nil && v.Status == "True" {
			verified, okVerified := parseReportTime(v.LastTransitionTime)
			secs, ok := secondsBetween(applied, verified, okApplied, okVerified)
			add(&a2v, secs, ok)
		}
	}
	detect := distribute(d2p)
	if detectGone != "" {
		detect = reportDistribution{NoData: true, Reason: detectGone}
	}
	return reportTimeToHeal{
		DetectToProposal:   detect,
		ProposalToDecision: distribute(p2d),
		DecisionToApplied:  distribute(d2a),
		AppliedToVerified:  distribute(a2v),
	}
}

// incidentKeyOf is namespace/name of the incident a RemediationAction answers.
func incidentKeyOf(ra *reportRA) string {
	ns := ra.Spec.IncidentRef.Namespace
	if ns == "" {
		ns = ra.Metadata.Namespace
	}
	return ns + "/" + ra.Spec.IncidentRef.Name
}

// key identifies a RemediationAction.
func (r *reportRA) key() string { return r.Metadata.Namespace + "/" + r.Metadata.Name }

// firstProposals maps each incident to its first RemediationAction: the earliest
// created, the lower name winning a tie. One whose creation time is unreadable
// cannot be placed and is not a candidate.
func firstProposals(all []reportRA) map[string]string {
	firstAt := map[string]time.Time{}
	first := map[string]string{}
	for i := range all {
		ra := &all[i]
		created, ok := ra.created()
		if !ok {
			continue
		}
		inc := incidentKeyOf(ra)
		at, seen := firstAt[inc]
		if !seen || created.Before(at) || (created.Equal(at) && ra.key() < first[inc]) {
			firstAt[inc], first[inc] = created, ra.key()
		}
	}
	return first
}
