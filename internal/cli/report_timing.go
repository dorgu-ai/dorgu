package cli

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
func timeToHeal(ras []*reportRA, incidents map[string]*reportIncident) reportTimeToHeal {
	var d2p, p2d, d2a, a2v []float64
	add := func(dst *[]float64, secs float64, ok bool) {
		if ok {
			*dst = append(*dst, secs)
		}
	}

	for _, ra := range ras {
		created, okCreated := ra.created()

		if inc := incidentOf(ra, incidents); inc != nil {
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
	return reportTimeToHeal{
		DetectToProposal:   distribute(d2p),
		ProposalToDecision: distribute(p2d),
		DecisionToApplied:  distribute(d2a),
		AppliedToVerified:  distribute(a2v),
	}
}
