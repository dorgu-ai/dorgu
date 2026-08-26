package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// guardedRemediationFixture is a RemediationAction from an operator carrying
// dorgu-operator #47: the step's spec.steps[].safety records what Dorgu's
// guardrails decided, field by field.
//
// It is the clean-room run #4 shape. The plan asked for 4Gi against a live
// 256Mi (16x, ceiling 2x) and also tried to introduce a CPU limit the container
// does not set. Dorgu clamped the first and refused the second, so the patch
// carries 512Mi and no cpu key at all.
//
// The step description is the operator's own: its sentence followed by each
// safety message verbatim, which is exactly why the renderer must not print
// both.
const guardedRemediationFixture = `{
  "apiVersion": "dorgu.io/v1",
  "kind": "RemediationAction",
  "metadata": {
    "name": "fix-oom-checkout",
    "namespace": "apps",
    "creationTimestamp": "2026-08-26T10:00:00Z"
  },
  "spec": {
    "incidentRef": {"name": "im-oom-checkout", "namespace": "apps"},
    "personaRef": {"kind": "ApplicationPersona", "name": "checkout", "namespace": "apps"},
    "workloadRef": {
      "kind": "Deployment",
      "name": "checkout",
      "namespace": "apps",
      "container": "checkout",
      "managedBy": "unmanaged",
      "managedByDetail": "",
      "observedResources": {"limits": {"memory": "256Mi"}},
      "observedImage": "ghcr.io/acme/checkout:2.1.0",
      "observedAt": "2026-08-26T09:59:00Z"
    },
    "action": {"type": "persona-update"},
    "steps": [
      {
        "order": 1,
        "id": "step-1",
        "type": "persona-update",
        "description": "Set spec.resources.limits.memory to 512Mi on the ApplicationPersona. Blast-radius guardrail: the plan asked to set spec.resources.limits.memory to 4Gi, which is 16.0x the current 256Mi. The ceiling is 2.0x, so Dorgu will apply 512Mi instead, which may not be enough on its own. Left out: container \"checkout\" on checkout does not set cpu today, so Dorgu will not introduce it as a side effect of another fix. Ask for it as its own change.",
        "rationale": "Dorgu decides what its guardrails permit, so the outcome above is Dorgu's own arithmetic against the workload it observed, not the plan's account of it.",
        "risk": "low",
        "autoExecutable": true,
        "patch": {"spec": {"resources": {"limits": {"memory": "512Mi"}}}},
        "prePatchState": {"spec": {"resources": {"limits": {"memory": "256Mi"}}}},
        "safety": [
          {
            "rule": "blast-radius",
            "verdict": "clamped",
            "field": "spec.resources.limits.memory",
            "baseline": "256Mi",
            "requested": "4Gi",
            "permitted": "512Mi",
            "ratio": "16.0x",
            "maxRatio": "2.0x",
            "message": "Blast-radius guardrail: the plan asked to set spec.resources.limits.memory to 4Gi, which is 16.0x the current 256Mi. The ceiling is 2.0x, so Dorgu will apply 512Mi instead, which may not be enough on its own."
          },
          {
            "rule": "absent-field",
            "verdict": "rejected",
            "field": "spec.resources.limits.cpu",
            "requested": "500m",
            "message": "Left out: container \"checkout\" on checkout does not set cpu today, so Dorgu will not introduce it as a side effect of another fix. Ask for it as its own change."
          }
        ]
      },
      {
        "order": 2,
        "id": "step-2",
        "type": "restart",
        "description": "Restart the deployment so the new limit takes effect",
        "rationale": "New limits only apply to new pods",
        "risk": "low",
        "autoExecutable": false
      }
    ],
    "planSource": "ai-anthropic",
    "planSummary": "Container OOMKilled at a 256Mi memory limit.",
    "explanation": "OOM remediation for checkout",
    "confidence": "0.85",
    "approval": {"required": true}
  },
  "status": {"phase": "Pending"}
}`

// clampMessage and rejectMessage are the two Dorgu-authored sentences in the
// fixture, named so a test can assert each appears exactly once.
const (
	clampMessage = "Blast-radius guardrail: the plan asked to set spec.resources.limits.memory to 4Gi, " +
		"which is 16.0x the current 256Mi. The ceiling is 2.0x, so Dorgu will apply 512Mi instead, " +
		"which may not be enough on its own."
	rejectMessage = "Left out: container \"checkout\" on checkout does not set cpu today, so Dorgu will not " +
		"introduce it as a side effect of another fix. Ask for it as its own change."
)

func guardedRemediation(t *testing.T) *remediationFull {
	t.Helper()
	var r remediationFull
	require.NoError(t, json.Unmarshal([]byte(guardedRemediationFixture), &r))
	return &r
}

// TestUnmarshalStepSafety is the parse half: the CLI has to read the field at
// all before it can render it.
func TestUnmarshalStepSafety(t *testing.T) {
	r := guardedRemediation(t)

	require.Len(t, r.Spec.Steps, 2)
	require.Len(t, r.Spec.Steps[0].Safety, 2)
	assert.Empty(t, r.Spec.Steps[1].Safety)

	clamp := r.Spec.Steps[0].Safety[0]
	assert.Equal(t, "blast-radius", clamp.Rule)
	assert.Equal(t, "clamped", clamp.Verdict)
	assert.Equal(t, "spec.resources.limits.memory", clamp.Field)
	assert.Equal(t, "256Mi", clamp.Baseline)
	assert.Equal(t, "4Gi", clamp.Requested)
	assert.Equal(t, "512Mi", clamp.Permitted)
	assert.Equal(t, "16.0x", clamp.Ratio)
	assert.Equal(t, "2.0x", clamp.MaxRatio)
	assert.Equal(t, clampMessage, clamp.Message)

	reject := r.Spec.Steps[0].Safety[1]
	assert.Equal(t, "absent-field", reject.Rule)
	assert.Equal(t, "rejected", reject.Verdict)
	assert.Equal(t, "spec.resources.limits.cpu", reject.Field)
	assert.Equal(t, "500m", reject.Requested)
	assert.Empty(t, reject.Permitted)
}

// TestUnmarshalStepSafetyAbsent covers the optional half: an object from an
// older operator carries no safety at all, and that is not an error.
func TestUnmarshalStepSafetyAbsent(t *testing.T) {
	var r remediationFull
	require.NoError(t, json.Unmarshal([]byte(operatorRemediationFixture), &r))

	require.Len(t, r.Spec.Steps, 3)
	for _, s := range r.Spec.Steps {
		assert.Empty(t, s.Safety)
	}
}

// TestPrintRemediationDiffRendersGuardrailVerdict is the finding: the reader
// had to take the guardrail's word out of a sentence a model may have written.
// The verdict renders as its own block with the numbers Dorgu computed.
func TestPrintRemediationDiffRendersGuardrailVerdict(t *testing.T) {
	var buf bytes.Buffer
	printRemediationDiff(&buf, guardedRemediation(t))
	out := buf.String()

	assert.Contains(t, out, "Dorgu guardrails")

	// The clamp: what was asked for, what Dorgu measured, what will be applied.
	assert.Contains(t, out, "clamped")
	assert.Contains(t, out, "spec.resources.limits.memory")
	assert.Contains(t, out, "(blast-radius)")
	assert.Contains(t, out, "requested 4Gi, baseline 256Mi, 16.0x against a 2.0x ceiling, applying 512Mi")

	// The refusal: a key the workload does not set is not introduced, and the
	// line says so in the same shape rather than by omission.
	assert.Contains(t, out, "rejected")
	assert.Contains(t, out, "spec.resources.limits.cpu")
	assert.Contains(t, out, "(absent-field)")
	assert.Contains(t, out, "requested 500m, applying nothing")
}

// TestPrintRemediationDiffPrintsEachVerdictOnce guards the duplication the
// operator's own description invites: it is the step sentence followed by every
// safety message, so a renderer that prints both says the same thing twice.
func TestPrintRemediationDiffPrintsEachVerdictOnce(t *testing.T) {
	var buf bytes.Buffer
	printRemediationDiff(&buf, guardedRemediation(t))
	out := buf.String()

	assert.Equal(t, 1, strings.Count(out, clampMessage), "clamp verdict printed more than once")
	assert.Equal(t, 1, strings.Count(out, rejectMessage), "refusal printed more than once")

	// What the step actually does survives the de-duplication.
	assert.Contains(t, out, "[1] persona-update (low; auto): Set spec.resources.limits.memory to 512Mi on the ApplicationPersona.")
}

// TestPrintRemediationDiffWithoutSafetyIsUnchanged is the compatibility half:
// an object from an older operator renders with no guardrail block anywhere.
func TestPrintRemediationDiffWithoutSafetyIsUnchanged(t *testing.T) {
	var r remediationFull
	require.NoError(t, json.Unmarshal([]byte(operatorRemediationFixture), &r))

	var buf bytes.Buffer
	printRemediationDiff(&buf, &r)
	out := buf.String()

	assert.NotContains(t, out, "Dorgu guardrails")
	assert.NotContains(t, out, "applying nothing")
	// The step description is printed exactly as recorded.
	assert.Contains(t, out, "[1] persona-update (low; auto): Increase memory limit to 512Mi")
}

// TestPrintHealPreambleRendersGuardrailVerdict covers the approve/heal preview:
// the confirmation prompt follows this, so a clamp has to be visible before the
// reader answers it.
func TestPrintHealPreambleRendersGuardrailVerdict(t *testing.T) {
	r := guardedRemediation(t)
	plan, err := buildHealPlan(r)
	require.NoError(t, err)
	require.Len(t, plan.Safety, 2)

	var buf bytes.Buffer
	printHealPreamble(&buf, &healExecution{
		Context:  "kind-dorgu",
		Change:   plan.Change,
		Advisory: plan.Advisory,
		Safety:   plan.Safety,
	})
	out := buf.String()

	assert.Contains(t, out, "Heal (context: kind-dorgu)")
	assert.Contains(t, out, "Dorgu guardrails")
	assert.Contains(t, out, "requested 4Gi, baseline 256Mi, 16.0x against a 2.0x ceiling, applying 512Mi")
	assert.Contains(t, out, "requested 500m, applying nothing")
	assert.Contains(t, out, clampMessage)

	// The advisory step still prints, after the verdict.
	assert.Contains(t, out, "Manual steps (not applied automatically):")
	assert.Less(t, strings.Index(out, "Dorgu guardrails"), strings.Index(out, "Manual steps"))
}

// TestPrintHealPreambleWithoutSafetyIsUnchanged pins the old output byte for
// byte, because "older objects render exactly as they do today" is the part of
// this change that is easiest to break by accident.
func TestPrintHealPreambleWithoutSafetyIsUnchanged(t *testing.T) {
	var buf bytes.Buffer
	printHealPreamble(&buf, &healExecution{
		Context: "kind-dorgu",
		Advisory: []advisoryStep{{
			Order:       2,
			Type:        "restart",
			Description: "Restart the deployment",
			Reason:      "restart is handled by the workload controller after the resource patch",
		}},
	})

	assert.Equal(t, "\nHeal (context: kind-dorgu)\n"+
		"\nManual steps (not applied automatically):\n"+
		"  [2] restart: Restart the deployment\n"+
		"      restart is handled by the workload controller after the resource patch\n",
		buf.String())
}

// TestPrintHealPreambleNilExecution: the preamble is printed on paths where the
// execution may not have been resolved, so it prints nothing rather than panics.
func TestPrintHealPreambleNilExecution(t *testing.T) {
	var buf bytes.Buffer
	printHealPreamble(&buf, nil)
	assert.Empty(t, buf.String())
}

// TestPrintOwnedWorkloadRefusalRendersGuardrailVerdict: on an owned workload
// the reader is the one who applies the change, so they need to know the value
// they are being handed is Dorgu's ceiling and not what the plan asked for.
func TestPrintOwnedWorkloadRefusalRendersGuardrailVerdict(t *testing.T) {
	r := guardedRemediation(t)
	r.Spec.WorkloadRef.ManagedBy = managedByHelm
	r.Spec.WorkloadRef.ManagedByDetail = `Helm release "checkout" in namespace apps`

	plan, err := buildHealPlan(r)
	require.NoError(t, err)

	var buf bytes.Buffer
	printOwnedWorkloadRefusal(&buf, r, &ownedWorkloadError{ref: r.Spec.WorkloadRef, plan: plan})
	out := buf.String()

	assert.Contains(t, out, "Dorgu will not patch this workload.")
	assert.Contains(t, out, "Dorgu guardrails")
	assert.Contains(t, out, "requested 4Gi, baseline 256Mi, 16.0x against a 2.0x ceiling, applying 512Mi")
	assert.Contains(t, out, clampMessage)

	// The verdict comes before the instructions that carry the permitted value.
	assert.Less(t, strings.Index(out, "Dorgu guardrails"),
		strings.Index(out, "Apply it where this workload's desired state lives:"))
}

// TestPrintRemediationListShowsGuardrailColumn: the list is where a reader
// decides what to open, so a clamped plan says so there.
func TestPrintRemediationListShowsGuardrailColumn(t *testing.T) {
	guarded := guardedRemediation(t)
	plain := makeTestRemediation("fix-crash-web", "default", "Approved", "persona-update", "72%", "web")

	var buf bytes.Buffer
	printRemediationList(&buf, []remediationFull{*guarded, plain}, false)
	out := buf.String()

	assert.Contains(t, out, "GUARDRAIL")

	// The fixture carries both a clamp and a refusal. The column is one word
	// wide, so it shows the one the reader most needs to open the plan for: a
	// field Dorgu refused outright outranks one it substituted a value for.
	assert.Contains(t, lineContaining(t, out, "fix-oom-checkout"), "rejected")

	// A remediation no guardrail ruled on says so, rather than reading as one
	// that was clamped.
	webLine := lineContaining(t, out, "fix-crash-web")
	assert.NotContains(t, webLine, "clamped")
	assert.NotContains(t, webLine, "rejected")
	assert.Contains(t, webLine, "-")
}

// lineContaining returns the single output line holding needle, so a column
// assertion cannot pass on a value that belongs to a different row.
func lineContaining(t *testing.T, out, needle string) string {
	t.Helper()
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, needle) {
			return line
		}
	}
	t.Fatalf("no output line contains %q", needle)
	return ""
}

// TestPrintRemediationListOmitsGuardrailColumnWhenNoSafety keeps every existing
// cluster's list output identical: the column appears only when there is a
// verdict to put in it.
func TestPrintRemediationListOmitsGuardrailColumnWhenNoSafety(t *testing.T) {
	var buf bytes.Buffer
	printRemediationList(&buf, []remediationFull{
		makeTestRemediation("fix-oom-api", "production", "Pending", "persona-update", "85%", "api-server"),
	}, false)

	assert.NotContains(t, buf.String(), "GUARDRAIL")
}

func TestSafetyFacts(t *testing.T) {
	tests := []struct {
		name  string
		entry stepSafety
		want  string
	}{
		{
			name: "clamped carries every number",
			entry: stepSafety{
				Baseline: "256Mi", Requested: "4Gi", Permitted: "512Mi",
				Ratio: "16.0x", MaxRatio: "2.0x",
			},
			want: "requested 4Gi, baseline 256Mi, 16.0x against a 2.0x ceiling, applying 512Mi",
		},
		{
			name:  "rejected applies nothing",
			entry: stepSafety{Baseline: "256Mi", Requested: "4Gi", Ratio: "16.0x", MaxRatio: "2.0x"},
			want:  "requested 4Gi, baseline 256Mi, 16.0x against a 2.0x ceiling, applying nothing",
		},
		{
			name:  "absent field has only what was asked for",
			entry: stepSafety{Requested: "500m"},
			want:  "requested 500m, applying nothing",
		},
		{
			name:  "derived has only what Dorgu computed",
			entry: stepSafety{Permitted: "512Mi"},
			want:  "applying 512Mi",
		},
		{
			name:  "ratio without a ceiling still reads",
			entry: stepSafety{Requested: "4Gi", Ratio: "16.0x", Permitted: "512Mi"},
			want:  "requested 4Gi, ratio 16.0x, applying 512Mi",
		},
		{
			name:  "ceiling without a ratio still reads",
			entry: stepSafety{Requested: "4Gi", MaxRatio: "2.0x", Permitted: "512Mi"},
			want:  "requested 4Gi, ceiling 2.0x, applying 512Mi",
		},
		{
			name:  "an entry with no numbers has no facts line",
			entry: stepSafety{Rule: "plan-validation", Verdict: "derived", Message: "..."},
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, safetyFacts(tt.entry))
		})
	}
}

func TestSafetyEntryHeading(t *testing.T) {
	tests := []struct {
		name  string
		entry stepSafety
		want  string
	}{
		{
			name:  "verdict, field and rule",
			entry: stepSafety{Verdict: "clamped", Field: "spec.resources.limits.memory", Rule: "blast-radius"},
			want:  "clamped  spec.resources.limits.memory  (blast-radius)",
		},
		{
			// An operator newer than this CLI may add a rule or a verdict. An
			// entry Dorgu cannot classify is still Dorgu's verdict, so it is
			// printed verbatim rather than dropped.
			name:  "an unrecognised rule prints verbatim",
			entry: stepSafety{Verdict: "quarantined", Field: "spec.replicas", Rule: "future-rule"},
			want:  "quarantined  spec.replicas  (future-rule)",
		},
		{
			name:  "a verdict with no field still names its rule",
			entry: stepSafety{Verdict: "rejected", Rule: "plan-validation"},
			want:  "rejected  (plan-validation)",
		},
		{
			name:  "an entry with nothing to name says so",
			entry: stepSafety{Message: "..."},
			want:  "guardrail verdict",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, safetyEntryHeading(tt.entry))
		})
	}
}

func TestDescriptionWithoutSafetyMessages(t *testing.T) {
	tests := []struct {
		name        string
		description string
		entries     []stepSafety
		want        string
	}{
		{
			name:        "no entries leaves the description untouched",
			description: "Increase memory limit to 512Mi",
			want:        "Increase memory limit to 512Mi",
		},
		{
			name:        "the operator's appended messages come off",
			description: "Set memory to 512Mi. " + clampMessage + " " + rejectMessage,
			entries:     []stepSafety{{Message: clampMessage}, {Message: rejectMessage}},
			want:        "Set memory to 512Mi.",
		},
		{
			name:        "a description that never carried the message is unchanged",
			description: "Set memory to 512Mi.",
			entries:     []stepSafety{{Message: clampMessage}},
			want:        "Set memory to 512Mi.",
		},
		{
			name:        "stripping everything leaves the description alone",
			description: clampMessage,
			entries:     []stepSafety{{Message: clampMessage}},
			want:        clampMessage,
		},
		{
			name:        "an entry with no message strips nothing",
			description: "Set memory to 512Mi.",
			entries:     []stepSafety{{Rule: "blast-radius"}},
			want:        "Set memory to 512Mi.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, descriptionWithoutSafetyMessages(tt.description, tt.entries))
		})
	}
}

func TestGuardrailVerdictSummary(t *testing.T) {
	withSteps := func(steps ...remediationStep) remediationFull {
		var r remediationFull
		r.Spec.Steps = steps
		return r
	}
	step := func(entries ...stepSafety) remediationStep {
		return remediationStep{Safety: entries}
	}

	tests := []struct {
		name string
		rem  remediationFull
		want string
	}{
		{
			name: "no steps",
			rem:  withSteps(),
			want: "-",
		},
		{
			name: "steps but no verdict",
			rem:  withSteps(step(), step()),
			want: "-",
		},
		{
			name: "a rejection outranks a clamp",
			rem:  withSteps(step(stepSafety{Verdict: "clamped"}), step(stepSafety{Verdict: "rejected"})),
			want: "rejected",
		},
		{
			name: "a clamp outranks a derived value",
			rem:  withSteps(step(stepSafety{Verdict: "derived"}, stepSafety{Verdict: "clamped"})),
			want: "clamped",
		},
		{
			name: "derived on its own is still worth saying",
			rem:  withSteps(step(stepSafety{Verdict: "derived"})),
			want: "derived",
		},
		{
			name: "an unrecognised verdict is reported, not swallowed",
			rem:  withSteps(step(stepSafety{Verdict: "quarantined"})),
			want: "quarantined",
		},
		{
			name: "a known verdict wins over an unrecognised one",
			rem:  withSteps(step(stepSafety{Verdict: "quarantined"}, stepSafety{Verdict: "clamped"})),
			want: "clamped",
		},
		{
			name: "several unrecognised verdicts pick the same one every run",
			rem:  withSteps(step(stepSafety{Verdict: "zeta"}, stepSafety{Verdict: "alpha"})),
			want: "alpha",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, guardrailVerdictSummary(tt.rem))
		})
	}
}

// TestRemediationDiffJSONCarriesSafety: --json is how a platform team alerts on
// a clamp, so the field has to survive the round trip.
func TestRemediationDiffJSONCarriesSafety(t *testing.T) {
	encoded, err := json.Marshal(guardedRemediation(t))
	require.NoError(t, err)

	var round remediationFull
	require.NoError(t, json.Unmarshal(encoded, &round))
	require.Len(t, round.Spec.Steps[0].Safety, 2)
	assert.Equal(t, "clamped", round.Spec.Steps[0].Safety[0].Verdict)
	assert.Equal(t, "512Mi", round.Spec.Steps[0].Safety[0].Permitted)

	// An object with no verdicts gains no key: older objects serialise exactly
	// as they do today.
	var legacy remediationFull
	require.NoError(t, json.Unmarshal([]byte(operatorRemediationFixture), &legacy))
	legacyJSON, err := json.Marshal(legacy)
	require.NoError(t, err)
	assert.NotContains(t, string(legacyJSON), `"safety"`)
}
