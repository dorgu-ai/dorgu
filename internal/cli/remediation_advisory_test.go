package cli

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// F-03: Dorgu proposed a notification-type remediation for an ImagePullBackOff
// and printed `dorgu remediation approve ...` as its own suggested action.
// Following that instruction produced a Failed remediation and a 30-minute
// cooldown on the app. The CLI already knows it cannot apply anything here.
func TestRemediationDiffDoesNotOfferApproveForAdvisoryPlans(t *testing.T) {
	r := makeTestRemediation("ra-web-imagepull", "apps", "Pending", "notification", "91%", "web")
	r.Spec.Explanation = "The image tag nginx:1.27-alpineX does not exist. The correct tag is nginx:1.27-alpine."
	r.Spec.PlanSource = "ai-anthropic"
	r.Spec.PlanSummary = "web cannot pull its image because the tag is a typo."
	r.Spec.Steps = []remediationStep{
		{
			Order:          1,
			ID:             "step-1",
			Type:           "manual",
			Description:    "Correct the image tag to nginx:1.27-alpine.",
			Risk:           "low",
			AutoExecutable: false,
		},
	}

	var buf bytes.Buffer
	printRemediationDiff(&buf, &r)
	out := buf.String()

	assert.NotContains(t, out, "dorgu remediation approve",
		"an advisory plan must not suggest the command that fails it")
	assert.Contains(t, out, "This plan is advisory")
	assert.Contains(t, out, "Carry out the steps above yourself")
	assert.Contains(t, out, "dorgu remediation reject ra-web-imagepull -n apps")
	// The manual step itself is still shown: it is the whole value of the plan.
	assert.Contains(t, out, "Correct the image tag to nginx:1.27-alpine.")
}

// A plan that does carry an applicable change keeps the approve suggestion.
func TestRemediationDiffOffersApproveForApplicablePlans(t *testing.T) {
	r := makeTestRemediation("ra-oom-worker", "apps", "Pending", "persona-update", "88%", "report-worker")
	r.Spec.Action.PrePatchState = json.RawMessage(`{"spec":{"resources":{"limits":{"memory":"48Mi"}}}}`)
	r.Spec.Action.Patch = json.RawMessage(`{"spec":{"resources":{"limits":{"memory":"96Mi"}}}}`)

	var buf bytes.Buffer
	printRemediationDiff(&buf, &r)
	out := buf.String()

	assert.Contains(t, out, "dorgu remediation approve ra-oom-worker -n apps")
	assert.Contains(t, out, "dorgu remediation reject ra-oom-worker -n apps")
	assert.NotContains(t, out, "This plan is advisory")
}

// Nothing is suggested for a plan that is no longer pending.
func TestRemediationDiffSuggestsNothingWhenNotPending(t *testing.T) {
	r := makeTestRemediation("ra-done", "apps", "Completed", "persona-update", "88%", "report-worker")
	r.Spec.Action.PrePatchState = json.RawMessage(`{"spec":{"resources":{"limits":{"memory":"48Mi"}}}}`)
	r.Spec.Action.Patch = json.RawMessage(`{"spec":{"resources":{"limits":{"memory":"96Mi"}}}}`)

	var buf bytes.Buffer
	printRemediationDiff(&buf, &r)
	out := buf.String()

	assert.NotContains(t, out, "Actions:")
	assert.NotContains(t, out, "This plan is advisory")
}

func TestHasAutoApplicableChange(t *testing.T) {
	tests := []struct {
		name       string
		actionType string
		patch      string
		steps      []remediationStep
		want       bool
	}{
		{name: "notification", actionType: "notification", want: false},
		{
			name:       "persona-update with a resource change",
			actionType: "persona-update",
			patch:      `{"spec":{"resources":{"limits":{"memory":"96Mi"}}}}`,
			want:       true,
		},
		{
			name:       "persona-update with a non-resource patch",
			actionType: "persona-update",
			patch:      `{"spec":{"scaling":{"minReplicas":3}}}`,
			want:       false,
		},
		{
			name:       "unparseable patch counts as not applicable",
			actionType: "persona-update",
			patch:      `{not json`,
			want:       false,
		},
		{
			name:       "advisory steps only",
			actionType: "notification",
			steps: []remediationStep{
				{Order: 1, ID: "step-1", Type: "manual", Description: "Fix the tag."},
			},
			want: false,
		},
		{
			name:       "a persona-update step carries the change",
			actionType: "persona-update",
			steps: []remediationStep{
				{Order: 1, ID: "step-1", Type: "manual", Description: "Watch the pod."},
				{
					Order: 2, ID: "step-2", Type: "persona-update", AutoExecutable: true,
					Patch: json.RawMessage(`{"spec":{"resources":{"limits":{"memory":"96Mi"}}}}`),
				},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := makeTestRemediation("ra", "apps", "Pending", tt.actionType, "88%", "app")
			if tt.patch != "" {
				r.Spec.Action.Patch = json.RawMessage(tt.patch)
			}
			r.Spec.Steps = tt.steps

			assert.Equal(t, tt.want, hasAutoApplicableChange(&r))
		})
	}
}

// House style: no em dashes in anything the user reads.
func TestAdvisoryCopyHasNoEmDashes(t *testing.T) {
	r := makeTestRemediation("ra-web", "apps", "Pending", "notification", "91%", "web")

	var buf bytes.Buffer
	printRemediationDiff(&buf, &r)

	require.NotContains(t, buf.String(), "—")
}
