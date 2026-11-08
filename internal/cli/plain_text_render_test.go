package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The text the AI wrote for incident ec-demo-memhog in the 2026-11-06 EKS QA
// run: a heading, paragraphs, bold, a numbered list with inline code, and a
// block quote. It was printed verbatim after "Summary:".
const aiMarkdownRootCause = "## Root Cause\n\n" +
	"The container `memhog` is being **OOMKilled** because it allocates more memory than its limit.\n\n" +
	"1. The limit is 64Mi.\n" +
	"2. The process allocates 128Mi at startup.\n\n" +
	"> **Note**: raising the limit is the smallest safe fix."

// F-7: `incidents describe` printed the model's markdown raw, so `## Root Cause`
// and `**bold**` sat in the middle of an aligned block and the paragraphs below
// it started in column 0, under no label.
func TestIncidentDescribeRendersAIMarkdownForATerminal(t *testing.T) {
	inc := makeTestIncident("im-memhog", "demo", "critical", "resource", "OOMKilled", "memhog", "Detected")
	inc.Spec.RootCause = &struct {
		Summary      string `json:"summary"`
		Confidence   string `json:"confidence"`
		Provider     string `json:"provider"`
		Contributing []struct {
			Signal string `json:"signal"`
			Detail string `json:"detail"`
		} `json:"contributing"`
	}{
		Summary:    aiMarkdownRootCause,
		Confidence: "0.9",
		Provider:   "ai-enhanced",
		Contributing: []struct {
			Signal string `json:"signal"`
			Detail string `json:"detail"`
		}{
			{Signal: "OOMKilled", Detail: "**Exit code 137** after `128Mi` of allocation"},
		},
	}

	var buf bytes.Buffer
	printIncidentDescribe(&buf, &inc)
	out := buf.String()

	for _, raw := range []string{"##", "**", "`", "> "} {
		assert.NotContains(t, out, raw, "markdown syntax must not reach the terminal")
	}
	// The words survive.
	assert.Contains(t, out, "The container memhog is being OOMKilled because it allocates more memory than its limit.")
	assert.Contains(t, out, "Exit code 137 after 128Mi of allocation")

	// Every line of the summary is indented under the label, so the Root Cause
	// block still reads as one block.
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if strings.Contains(line, "The limit is 64Mi") || strings.Contains(line, "raising the limit") {
			assert.True(t, strings.HasPrefix(line, "              ") || strings.HasPrefix(line, "  "),
				"continuation line must be indented: %q", line)
		}
	}
	assert.NotContains(t, out, "Summary:    Root Cause", "a heading that repeats the label is dropped")
	assert.Contains(t, out, "1. The limit is 64Mi.")
	assert.Contains(t, out, "2. The process allocates 128Mi at startup.")
	assert.Contains(t, out, "Note: raising the limit is the smallest safe fix.")
}

// --json keeps the model's text exactly as the operator stored it: a script
// reading it wants the original, and the CLI must not rewrite evidence.
func TestIncidentJSONKeepsAIMarkdownVerbatim(t *testing.T) {
	inc := makeTestIncident("im-memhog", "demo", "critical", "resource", "OOMKilled", "memhog", "Detected")
	inc.Spec.RootCause = &struct {
		Summary      string `json:"summary"`
		Confidence   string `json:"confidence"`
		Provider     string `json:"provider"`
		Contributing []struct {
			Signal string `json:"signal"`
			Detail string `json:"detail"`
		} `json:"contributing"`
	}{Summary: aiMarkdownRootCause}

	raw, err := json.Marshal(inc)
	require.NoError(t, err)
	var back incidentFull
	require.NoError(t, json.Unmarshal(raw, &back))
	assert.Equal(t, aiMarkdownRootCause, back.Spec.RootCause.Summary)
}

// The same model text lands in a remediation's explanation, plan summary and
// step rationale; `remediation diff` has the same problem.
func TestRemediationDiffRendersAIMarkdownForATerminal(t *testing.T) {
	r := makeTestRemediation("fix-memhog", "demo", "Pending", "persona-update", "85%", "memhog")
	r.Spec.Explanation = aiMarkdownRootCause
	r.Spec.PlanSummary = "Raise **memory** to `128Mi`."
	r.Spec.Steps = []remediationStep{{
		Order: 1, ID: "step-1", Type: "manual", Description: "Check the **limit**.",
		Rationale: "Because `memhog` allocates 128Mi.", AutoExecutable: false,
	}}

	var buf bytes.Buffer
	printRemediationDiff(&buf, &r)
	out := buf.String()

	for _, raw := range []string{"##", "**", "`", "> "} {
		assert.NotContains(t, out, raw, "markdown syntax must not reach the terminal")
	}
	assert.Contains(t, out, "Raise memory to 128Mi.")
	assert.Contains(t, out, "Because memhog allocates 128Mi.")
}
