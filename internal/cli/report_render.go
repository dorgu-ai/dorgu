package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strings"
	"time"
)

// renderReportJSON prints the report as indented JSON, the schema the golden test
// pins.
func renderReportJSON(w io.Writer, d reportDoc) error {
	b, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal report: %w", err)
	}
	_, err = fmt.Fprintln(w, string(b))
	return err
}

// renderReportText prints the report for a person. Anything with no sample behind
// it reads "no data", never 0.
func renderReportText(w io.Writer, d reportDoc) {
	p := func(format string, args ...any) { fmt.Fprintf(w, format+"\n", args...) }

	p("Dorgu report (%s)", d.Schema)
	p("  Window:    %s to %s (last %s)", d.Window.Start, d.Window.End, d.Window.Since)
	p("  Cluster:   %s, namespace scope: %s", orUnknown(d.Cluster.Context), d.Cluster.Namespace)
	p("  Operator:  %s", orUnknownPtr(d.Cluster.OperatorVersion))
	p("  Read:      %s", sourcesLine(d.Sources))
	p("")

	p("North star")
	p("  Safe heals: %d  (clean heals whose approval-time gate run passed; clean heals in all: %d)",
		d.TierA.NorthStar.SafeHeals, d.TierA.NorthStar.CleanHeal)
	p("")

	renderHealAccounting(w, d.HealAccounting)
	renderTierA(w, d.TierA)
	renderTierQ(w, d.TierQ)
	renderClamps(w, d.FirstRunClamps)
	renderDeclines(w, d.CLIDeclines)

	p("Not measurable from a cluster")
	for _, n := range d.NotMeasurable {
		p("  %-4s %s: %s", n.ID, n.Name, n.Reason)
	}
}

func orUnknown(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}

func orUnknownPtr(s *string) string {
	if s == nil || *s == "" {
		return "unknown (not readable)"
	}
	return *s
}

func sourcesLine(s reportSources) string {
	part := func(name string, src reportSource) string {
		if !src.OK {
			return fmt.Sprintf("%s UNREADABLE (%s)", name, src.Error)
		}
		return fmt.Sprintf("%s %d", name, src.Count)
	}
	return strings.Join([]string{
		part("RemediationActions", s.RemediationActions), part("IncidentMemories", s.Incidents),
		part("personas", s.Personas), part("Events", s.Events),
	}, ", ")
}

func renderHealAccounting(w io.Writer, h reportHealAccounting) {
	fmt.Fprintf(w, "Heal accounting (%d RemediationActions created in the window)\n", h.Total)
	if h.Total == 0 {
		fmt.Fprintln(w, "  no data: no RemediationActions were created in the window")
		fmt.Fprintln(w)
		return
	}
	fmt.Fprintf(w, "  completed: %d\n", h.Completed)
	fmt.Fprintf(w, "  clean heals: %d (HealCredited=True and the incident outcome is resolved)\n", h.Clean)
	fmt.Fprintln(w, "  everything else, by why it is not a clean heal:")
	for _, k := range allExclusions {
		if n := h.Excluded[k]; n > 0 {
			fmt.Fprintf(w, "    %-20s %d\n", k, n)
		}
	}
	if len(h.HealNotCredited) > 0 {
		parts := make([]string, 0, len(h.HealNotCredited))
		for _, k := range sortedMapKeys(h.HealNotCredited) {
			parts = append(parts, fmt.Sprintf("%s %d", k, h.HealNotCredited[k]))
		}
		fmt.Fprintf(w, "  HealCredited=False by reason: %s\n", strings.Join(parts, ", "))
	}
	fmt.Fprintln(w)
}

func renderTierA(w io.Writer, a reportTierA) {
	fmt.Fprintln(w, "Tier A: value and adoption")
	fmt.Fprintf(w, "  A2 active cluster:    %s (%d proposals decided)\n", yesNo(a.A2Active.Active), a.A2Active.ProposalsDecided)
	if a.A5Depth.NoData {
		fmt.Fprintln(w, "  A5 review depth:      no data (no proposal was decided)")
	} else {
		fmt.Fprintf(w, "  A5 review depth:      %.1f decisions per week (%d over %.0f days)\n",
			*a.A5Depth.PerWeek, a.A5Depth.ProposalsDecided, a.A5Depth.WindowDays)
	}
	if a.A6Approval.NoData {
		fmt.Fprintln(w, "  A6 approval rate:     no data (no proposal was approved or rejected)")
	} else {
		fmt.Fprintf(w, "  A6 approval rate:     %s (%d approved, %d rejected; %s)\n",
			percent(a.A6Approval.Rate), a.A6Approval.Approved, a.A6Approval.Rejected, bandSentence(a.A6Approval.Band))
	}
	fmt.Fprintf(w, "  A7 external agents:   no data (%s)\n", a.A7External.Reason)
	for _, n := range a.ClusterNotes {
		fmt.Fprintf(w, "  note: %s\n", n)
	}
	fmt.Fprintln(w)
}

func bandSentence(band string) string {
	switch band {
	case "below":
		return "below the 30-80% band: proposals may be noisy"
	case "above":
		return "above the 30-80% band: check for rubber-stamping"
	default:
		return "inside the 30-80% band"
	}
}

func renderTierQ(w io.Writer, q reportTierQ) {
	fmt.Fprintln(w, "Tier Q: operational quality")
	fmt.Fprintln(w, "  Q1 time to heal (p50 / p90, n):")
	stages := []struct {
		label string
		d     reportDistribution
	}{
		{"detect -> proposal", q.Q1TimeToHeal.DetectToProposal},
		{"proposal -> decision (human)", q.Q1TimeToHeal.ProposalToDecision},
		{"decision -> applied", q.Q1TimeToHeal.DecisionToApplied},
		{"applied -> verified", q.Q1TimeToHeal.AppliedToVerified},
	}
	for _, s := range stages {
		fmt.Fprintf(w, "    %-30s %s\n", s.label, distributionText(s.d))
	}

	p := q.Q2Precision
	if p.NoData {
		fmt.Fprintln(w, "  Q2 proposal precision: no data (no proposal was decided)")
	} else {
		fmt.Fprintf(w, "  Q2 proposal precision: %s of decided proposals were called wrong (%d of %d)\n",
			percent(p.Rate), p.Wrong, p.Decided)
	}
	if rejected := sumValues(p.RejectionsByReason); rejected > 0 {
		parts := []string{}
		for _, k := range sortedMapKeys(p.RejectionsByReason) {
			if n := p.RejectionsByReason[k]; n > 0 {
				parts = append(parts, fmt.Sprintf("%s %d", k, n))
			}
		}
		fmt.Fprintf(w, "    rejections by reason: %s\n", strings.Join(parts, ", "))
	}

	fmt.Fprintf(w, "  Q3 LLM efficiency:     no data (%s)\n", q.Q3LLM.Reason)
	if len(q.Q3LLM.PlanSources) > 0 {
		parts := []string{}
		for _, k := range sortedMapKeys(q.Q3LLM.PlanSources) {
			parts = append(parts, fmt.Sprintf("%s %d", k, q.Q3LLM.PlanSources[k]))
		}
		fmt.Fprintf(w, "    plan sources: %s\n", strings.Join(parts, ", "))
	}

	r := q.Q6Refusals
	switch {
	case r.NoData:
		fmt.Fprintln(w, "  Q6 refusal mix:        no data (no proposals in the window)")
	case len(r.Rows) == 0:
		fmt.Fprintf(w, "  Q6 refusal mix:        no refusals across %d proposals\n", r.Proposals)
	default:
		fmt.Fprintf(w, "  Q6 refusal mix (%d proposals; phase / stage / reason):\n", r.Proposals)
		for _, row := range r.Rows {
			flag := ""
			if row.Flagged {
				flag = "  <-- over 30% of proposals: investigate"
			}
			fmt.Fprintf(w, "    %-9s %-14s %-24s %3d  (%s)%s\n",
				row.Phase, row.Stage, row.Reason, row.Count, percent(row.ShareOfProposals), flag)
		}
	}
	fmt.Fprintln(w)
}

func renderClamps(w io.Writer, c reportClamps) {
	fmt.Fprintln(w, "First-run clamps (spec.steps[].safety and status.gate)")
	if c.NoData {
		fmt.Fprintln(w, "  no data: no RemediationActions in the window")
		fmt.Fprintln(w)
		return
	}
	fmt.Fprintf(w, "  proposals with a clamped value: %d\n", c.ProposalsWithClamp)
	if len(c.ClampsByRule) > 0 {
		parts := []string{}
		for _, k := range sortedMapKeys(c.ClampsByRule) {
			parts = append(parts, fmt.Sprintf("%s %d", k, c.ClampsByRule[k]))
		}
		fmt.Fprintf(w, "  clamps by guardrail: %s\n", strings.Join(parts, ", "))
	}
	if len(c.StepVerdicts) > 0 {
		parts := []string{}
		for _, k := range sortedMapKeys(c.StepVerdicts) {
			parts = append(parts, fmt.Sprintf("%s %d", k, c.StepVerdicts[k]))
		}
		fmt.Fprintf(w, "  step verdicts: %s\n", strings.Join(parts, ", "))
	}
	if len(c.GateClamps) > 0 {
		parts := []string{}
		for _, k := range sortedMapKeys(c.GateClamps) {
			parts = append(parts, fmt.Sprintf("%s %d", k, c.GateClamps[k]))
		}
		fmt.Fprintf(w, "  clamps recorded by the approval-time gate: %s\n", strings.Join(parts, ", "))
	}
	fmt.Fprintln(w)
}

func renderDeclines(w io.Writer, d reportCLIDeclines) {
	fmt.Fprintln(w, "CLI declines (Events on RemediationActions)")
	if d.NoData {
		fmt.Fprintln(w, "  no data: the Events could not be read")
	} else if len(d.ByReason) == 0 {
		fmt.Fprintln(w, "  none recorded in the window")
	} else {
		for _, k := range sortedMapKeys(d.ByReason) {
			fmt.Fprintf(w, "  %-26s %d\n", k, d.ByReason[k])
		}
	}
	fmt.Fprintf(w, "  note: %s\n\n", d.Note)
}

func distributionText(d reportDistribution) string {
	if d.NoData {
		return "no data (n=0)"
	}
	return fmt.Sprintf("%s / %s (n=%d)", secondsText(*d.P50Seconds), secondsText(*d.P90Seconds), d.N)
}

// secondsText renders a length of time: 45s, 12m05s, 3h20m, 2d4h.
func secondsText(s float64) string {
	d := time.Duration(math.Round(s)) * time.Second
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	default:
		return fmt.Sprintf("%dd%dh", int(d.Hours())/24, int(d.Hours())%24)
	}
}

func percent(v *float64) string {
	if v == nil {
		return "no data"
	}
	return fmt.Sprintf("%.1f%%", *v*100)
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func sumValues(m map[string]int) int {
	total := 0
	for _, n := range m {
		total += n
	}
	return total
}
