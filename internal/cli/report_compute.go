package cli

import (
	"sort"
	"strings"
	"time"
)

// buildReport computes the report from the cluster reads, a fixed clock and the
// window. It is a pure function: the same inputs give the same document.
//
// Time bases, so a number can be checked against the cluster: a RemediationAction
// and an IncidentMemory count when they were created inside the window; a
// proposal-time refusal counts when its ProposalRefused condition was recorded;
// an Event counts at its last timestamp.
func buildReport(in reportInputs, now time.Time, since time.Duration) reportDoc {
	start := now.Add(-since)
	w := reportWindowOf(now, start, since)

	ras := raInWindow(in.Remediations, start)
	incidents := incidentIndex(in.Incidents)

	doc := reportDoc{
		Schema:      reportSchema,
		GeneratedAt: now.UTC().Format(time.RFC3339),
		Window:      w,
	}
	doc.HealAccounting = accountForHeals(ras, incidents)

	decisions := countDecisions(ras)
	refusals := collectRefusals(ras, in.Incidents, start)

	doc.TierA = buildTierA(doc.HealAccounting, decisions, since)
	doc.TierQ = reportTierQ{
		Q1TimeToHeal: timeToHeal(ras, incidents),
		Q2Precision:  precision(decisions),
		Q3LLM:        llmEfficiency(ras),
		Q6Refusals:   refusalMix(len(ras)+refusals.proposalTime, refusals.rows),
	}
	doc.FirstRunClamps = firstRunClamps(ras)
	doc.CLIDeclines = cliDeclines(in, start)
	doc.NotMeasurable = notMeasurable()
	return doc
}

func reportWindowOf(now, start time.Time, since time.Duration) reportWindow {
	return reportWindow{
		Since: formatSince(since),
		Start: start.UTC().Format(time.RFC3339),
		End:   now.UTC().Format(time.RFC3339),
	}
}

// raInWindow keeps the RemediationActions created inside the window. One whose
// creation time is unreadable cannot be placed in it and is left out.
func raInWindow(all []reportRA, start time.Time) []*reportRA {
	var out []*reportRA
	for i := range all {
		if t, ok := all[i].created(); ok && !t.Before(start) {
			out = append(out, &all[i])
		}
	}
	return out
}

// incidentIndex finds an incident by namespace/name.
func incidentIndex(all []reportIncident) map[string]*reportIncident {
	idx := make(map[string]*reportIncident, len(all))
	for i := range all {
		idx[all[i].Metadata.Namespace+"/"+all[i].Metadata.Name] = &all[i]
	}
	return idx
}

// incidentOf is the incident a RemediationAction answers, if it can be read.
func incidentOf(ra *reportRA, idx map[string]*reportIncident) *reportIncident {
	ns := ra.Spec.IncidentRef.Namespace
	if ns == "" {
		ns = ra.Metadata.Namespace
	}
	return idx[ns+"/"+ra.Spec.IncidentRef.Name]
}

// decisions is what humans decided in the window.
type decisions struct {
	approved, rejected int
	rejections         map[string]int
}

// countDecisions counts approvals and rejections. A RemediationAction that was
// approved and then rejected is a rejection, counted once.
func countDecisions(ras []*reportRA) decisions {
	d := decisions{rejections: map[string]int{}}
	for _, r := range rejectionReasons {
		d.rejections[r.flag] = 0
	}
	d.rejections[reportNoReasonGiven] = 0

	for _, ra := range ras {
		switch {
		case ra.Status.Phase == "Rejected":
			d.rejected++
			d.rejections[rejectionReasonOf(ra)]++
		case strings.TrimSpace(ra.Status.ApprovedAt) != "":
			d.approved++
		}
	}
	return d
}

// rejectionReasonOf is the enum a rejection was recorded with, no-reason-given when
// the RemediationAction carries none, and the raw reason when it is one this CLI
// does not know (a newer CLI wrote it).
func rejectionReasonOf(ra *reportRA) string {
	c := ra.condition(conditionRejectionReason)
	if c == nil || c.Status != "True" || c.Reason == "" {
		return reportNoReasonGiven
	}
	if flag := rejectionFlagFor(c.Reason); flag != "" {
		return flag
	}
	return "unrecognised:" + c.Reason
}

// wrongRejection is a rejection that says the proposal itself was wrong: the
// numerator of Q2.
func wrongRejections(d decisions) int {
	return d.rejections["wrong-diagnosis"] + d.rejections["wrong-action"]
}

func buildTierA(h reportHealAccounting, d decisions, since time.Duration) reportTierA {
	decided := d.approved + d.rejected
	weeks := since.Hours() / (24 * 7)

	a := reportTierA{
		NorthStar:  reportNorthStar{SafeHeals: h.SafeHeals, CleanHeal: h.Clean},
		A2Active:   reportActive{Active: decided > 0, ProposalsDecided: decided},
		A5Depth:    reportDepth{ProposalsDecided: decided, WindowDays: since.Hours() / 24, NoData: decided == 0},
		A6Approval: reportApproval{Approved: d.approved, Rejected: d.rejected, Rate: ratio(d.approved, decided), NoData: decided == 0},
		A7External: reportNoData{NoData: true, Reason: "a RemediationAction records how its plan was written (planSource), " +
			"not who proposed it; there is no source field to count until external proposers exist (P2/P3)"},
		Reviewed: reportDecisions{Approved: d.approved, Rejected: d.rejected},
		ClusterNotes: []string{
			"A2 and A5 count decisions (approved or rejected). Reading a proposal with `dorgu remediation diff` leaves no record, so a decision is the measurable proxy for a review.",
			"A6 is a band, not a target: under 30% means noisy proposals, over 80% means rubber-stamping.",
		},
	}
	if decided > 0 {
		perWeek := float64(decided) / weeks
		a.A5Depth.PerWeek = &perWeek
	}
	if a.A6Approval.Rate != nil {
		switch rate := *a.A6Approval.Rate; {
		case rate < 0.30:
			a.A6Approval.Band = "below"
		case rate > 0.80:
			a.A6Approval.Band = "above"
		default:
			a.A6Approval.Band = "in"
		}
	}
	return a
}

func precision(d decisions) reportPrecision {
	decided := d.approved + d.rejected
	wrong := wrongRejections(d)
	return reportPrecision{
		Decided: decided, Wrong: wrong, Rate: ratio(wrong, decided), NoData: decided == 0,
		RejectionsByReason: d.rejections,
	}
}

func llmEfficiency(ras []*reportRA) reportLLM {
	sources := map[string]int{}
	for _, ra := range ras {
		src := ra.Spec.PlanSource
		if src == "" {
			src = "unspecified"
		}
		sources[src]++
	}
	return reportLLM{
		NoData: true,
		Reason: "LLM calls and cost per incident are operator metrics (dorgu_llm_requests_total, " +
			"dorgu_llm_tokens_total), not stored on any CRD",
		PlanSources: sources,
	}
}

func firstRunClamps(ras []*reportRA) reportClamps {
	c := reportClamps{
		NoData:       len(ras) == 0,
		StepVerdicts: map[string]int{},
		ClampsByRule: map[string]int{},
		GateClamps:   map[string]int{},
	}
	for _, ra := range ras {
		clamped := false
		for _, step := range ra.Spec.Steps {
			for _, s := range step.Safety {
				c.StepVerdicts[s.Verdict]++
				if s.Verdict == safetyVerdictClamped {
					c.ClampsByRule[s.Rule]++
					clamped = true
				}
			}
		}
		if clamped {
			c.ProposalsWithClamp++
		}
		if ra.Status.Gate != nil {
			for _, v := range ra.Status.Gate.Verdicts {
				if strings.EqualFold(v.Verdict, "Clamp") {
					c.GateClamps[v.Guardrail]++
				}
			}
		}
	}
	return c
}

// cliDeclines counts the CLI's own decline Events inside the window.
func cliDeclines(in reportInputs, start time.Time) reportCLIDeclines {
	out := reportCLIDeclines{
		ByReason: map[string]int{},
		Note: "The CLI records a decline only as a Kubernetes Event, and Kubernetes keeps Events for about an hour " +
			"unless the cluster is configured otherwise, so declines older than that are not counted here.",
	}
	if !in.EventsRead {
		out.NoData = true
		return out
	}
	for _, e := range in.Events {
		if !strings.HasPrefix(e.Reason, reasonCLIPrefix) || e.InvolvedObject.Kind != "RemediationAction" {
			continue
		}
		if at, ok := e.at(); !ok || at.Before(start) {
			continue
		}
		out.ByReason[e.Reason] += e.occurrences()
	}
	return out
}

func notMeasurable() []reportNotMeasurable {
	return []reportNotMeasurable{
		{"A1", "External installs", "a cluster cannot tell whether it is ours; read from partner reports"},
		{"A3", "Design partners", "comes from the conversation log"},
		{"A4", "Time to first value", "measured on a clean-room or screen-shared install, not from CRDs"},
		{"A8", "4-week retention", "needs reports from earlier weeks; one cluster's report cannot show it"},
		{"A9", "Would-pay signals", "comes from the conversation log"},
		{"A10", "Inbound", "comes from GitHub"},
		{"Q4", "Operator footprint", "CPU and memory come from /eks-qa and the operator's metrics"},
		{"Q5", "Clean-room", "comes from clean-room runs"},
	}
}

// sortedMapKeys returns a map's keys in order, for stable text output.
func sortedMapKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
