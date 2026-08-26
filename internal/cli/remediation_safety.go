package cli

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/dorgu-ai/dorgu/internal/output"
)

// Step safety: the guardrail verdict as data, printed as Dorgu's own.
//
// A guardrail's verdict used to reach the reader as a "[safety:blast-radius] …"
// prefix on the model's rationale. In clean-room run #4 that put Dorgu's
// arithmetic one line below the model's claim that the same 16x change was
// "well within a 2x ceiling", with no way to tell which sentence was measured
// and which was written. The operator records the verdict as structured data
// now (dorgu-operator #47, spec.steps[].safety), and this file is the half that
// renders it: under its own heading, with the numbers, away from any sentence a
// model may have authored.
//
// The field is optional. Absent means no guardrail ruled on the step, which is
// the ordinary case and must render exactly as it did before the field existed.

// stepSafety mirrors the operator's StepSafety (api/v1). Every string in it is
// written by Dorgu from its own arithmetic against the workload it observed. No
// part of it comes from a model.
//
// The rules the operator emits are blast-radius, plan-validation and
// absent-field; the verdicts are clamped, rejected and derived. Neither set is
// treated as closed here: an operator newer than this CLI may add one, and an
// entry this CLI cannot classify is still Dorgu's verdict, so it is printed
// verbatim rather than dropped.
type stepSafety struct {
	Rule      string `json:"rule"`
	Verdict   string `json:"verdict"`
	Field     string `json:"field"`
	Baseline  string `json:"baseline,omitempty"`
	Requested string `json:"requested,omitempty"`
	Permitted string `json:"permitted,omitempty"`
	Ratio     string `json:"ratio,omitempty"`
	MaxRatio  string `json:"maxRatio,omitempty"`
	Message   string `json:"message"`
}

// Verdicts this CLI recognises, mirroring dorgu-operator api/v1. They are used
// to rank a plan's verdicts and to colour them; an unrecognised verdict is
// still rendered, just unranked and uncoloured.
const (
	safetyVerdictClamped  = "clamped"
	safetyVerdictRejected = "rejected"
	safetyVerdictDerived  = "derived"
)

// safetyHeading labels the block, and the wording is load-bearing. The reader
// has just been shown a description and a rationale that may be a model's, and
// what follows is neither.
const safetyHeading = "Dorgu guardrails (Dorgu's measurement, not the plan's):"

// printStepSafety renders every verdict a guardrail reached, one block, indented
// under whatever printed it. Nothing is printed when no guardrail ruled.
func printStepSafety(w io.Writer, indent string, entries []stepSafety) {
	if len(entries) == 0 {
		return
	}

	fmt.Fprintf(w, "%s%s\n", indent, safetyHeading)
	for _, e := range entries {
		fmt.Fprintf(w, "%s  %s\n", indent, safetyEntryHeading(e))
		if facts := safetyFacts(e); facts != "" {
			fmt.Fprintf(w, "%s    %s\n", indent, facts)
		}
		if msg := strings.TrimSpace(e.Message); msg != "" {
			fmt.Fprintf(w, "%s    %s\n", indent, msg)
		}
	}
}

// safetyEntryHeading names the verdict, the field it fell on and the guardrail
// that reached it. Each part is dropped when the entry does not carry it, so a
// partial record reads as a shorter line rather than as blanks.
func safetyEntryHeading(e stepSafety) string {
	parts := make([]string, 0, 3)
	if v := strings.TrimSpace(e.Verdict); v != "" {
		parts = append(parts, output.SafetyVerdictColor(v))
	}
	if f := strings.TrimSpace(e.Field); f != "" {
		parts = append(parts, f)
	}
	if r := strings.TrimSpace(e.Rule); r != "" {
		parts = append(parts, "("+r+")")
	}
	if len(parts) == 0 {
		return "guardrail verdict"
	}
	return strings.Join(parts, "  ")
}

// safetyFacts is the one line that answers what was asked for, what Dorgu
// measured it against, and what will actually be applied.
//
// Every value is optional in the CRD, so each is included only when the entry
// carries it. The outcome is the exception: an empty Permitted means nothing
// will be applied for this field, which is a fact worth stating rather than one
// to leave the reader to infer from a missing word.
func safetyFacts(e stepSafety) string {
	parts := make([]string, 0, 4)
	if v := strings.TrimSpace(e.Requested); v != "" {
		parts = append(parts, "requested "+v)
	}
	if v := strings.TrimSpace(e.Baseline); v != "" {
		parts = append(parts, "baseline "+v)
	}
	ratio, maxRatio := strings.TrimSpace(e.Ratio), strings.TrimSpace(e.MaxRatio)
	switch {
	case ratio != "" && maxRatio != "":
		parts = append(parts, fmt.Sprintf("%s against a %s ceiling", ratio, maxRatio))
	case ratio != "":
		parts = append(parts, "ratio "+ratio)
	case maxRatio != "":
		parts = append(parts, "ceiling "+maxRatio)
	}

	permitted := strings.TrimSpace(e.Permitted)
	if len(parts) == 0 && permitted == "" {
		return ""
	}
	if permitted != "" {
		parts = append(parts, "applying "+permitted)
	} else {
		parts = append(parts, "applying nothing")
	}
	return strings.Join(parts, ", ")
}

// descriptionWithoutSafetyMessages returns a step's description with the
// guardrail messages taken back out of it.
//
// The operator writes a guarded step's description as the step's own sentence
// followed by every safety message verbatim, so a client that renders the
// description and the verdicts prints the same forty words twice, three lines
// apart. Saying it once, under a heading, with the numbers beside it, is the
// whole point of the field.
//
// A description that turns out to be nothing but the message is left alone:
// printing it twice is worse than printing it, but printing an empty step is
// worse than both.
func descriptionWithoutSafetyMessages(description string, entries []stepSafety) string {
	if len(entries) == 0 {
		return description
	}

	stripped := description
	for _, e := range entries {
		if msg := strings.TrimSpace(e.Message); msg != "" {
			stripped = strings.ReplaceAll(stripped, msg, " ")
		}
	}

	stripped = strings.Join(strings.Fields(stripped), " ")
	if stripped == "" {
		return description
	}
	return stripped
}

// verdictRank orders verdicts by how much a reader needs to know about them:
// a field Dorgu refused outright, then one it substituted a value for, then one
// it sized itself because the plan carried nothing appliable.
var verdictRank = []string{safetyVerdictRejected, safetyVerdictClamped, safetyVerdictDerived}

// guardrailVerdictSummary is the list column: the strongest verdict any
// guardrail reached on any step of this remediation, or "-" when none did.
//
// It is a pointer, not the record. The field-by-field account lives in
// `dorgu remediation diff`, which the list's help says so.
func guardrailVerdictSummary(r remediationFull) string {
	seen := make(map[string]bool)
	for _, s := range r.Spec.Steps {
		for _, e := range s.Safety {
			if v := strings.TrimSpace(e.Verdict); v != "" {
				seen[v] = true
			}
		}
	}
	if len(seen) == 0 {
		return "-"
	}

	for _, v := range verdictRank {
		if seen[v] {
			return output.SafetyVerdictColor(v)
		}
	}

	// A verdict from an operator newer than this CLI. Sorted so repeated runs
	// of the same list read the same way.
	unranked := make([]string, 0, len(seen))
	for v := range seen {
		unranked = append(unranked, v)
	}
	sort.Strings(unranked)
	return output.SafetyVerdictColor(unranked[0])
}

// anyGuardrailVerdict reports whether any remediation in the list carries a
// verdict, which is what decides whether the list grows a column for it.
//
// The column is conditional on purpose: on a cluster where no guardrail has
// ruled on anything, `dorgu remediation list` prints exactly what it printed
// before this field existed.
func anyGuardrailVerdict(remediations []remediationFull) bool {
	for _, r := range remediations {
		for _, s := range r.Spec.Steps {
			if len(s.Safety) > 0 {
				return true
			}
		}
	}
	return false
}
