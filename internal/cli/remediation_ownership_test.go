package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// CF4-3 · clean-room run #2, F-02: approving an OOM fix on a Helm-managed app
// patched the Deployment directly, and the next `helm upgrade` hard-failed on a
// field-manager conflict. The workload was never Dorgu's to write.
//
// The operator now records who owns the live workload in spec.workloadRef
// (dorgu-operator #42). These tests hold the CLI to it: everything except an
// explicitly unmanaged workload is recommended, never patched.

// helmOwnedRemediationFixture is the shape the operator now writes for a
// Helm-managed workload: the persona name ("frontend") is not the Deployment
// name ("frontend-podinfo"), the plan's workload step is already owner-shaped
// with no command, and observedResources is the LIVE container block, which
// sets a memory limit and no CPU limit at all.
const helmOwnedRemediationFixture = `{
  "apiVersion": "dorgu.io/v1",
  "kind": "RemediationAction",
  "metadata": {"name": "fix-oom-frontend", "namespace": "apps", "creationTimestamp": "2026-08-22T10:00:00Z"},
  "spec": {
    "incidentRef": {"name": "im-oom-frontend", "namespace": "apps"},
    "personaRef": {"kind": "ApplicationPersona", "name": "frontend", "namespace": "apps"},
    "trustLevel": 2,
    "workloadRef": {
      "kind": "Deployment",
      "name": "frontend-podinfo",
      "namespace": "apps",
      "container": "podinfo",
      "managedBy": "helm",
      "managedByDetail": "Helm release \"frontend\" in namespace apps",
      "observedResources": {"limits": {"memory": "32Mi"}, "requests": {"memory": "16Mi"}},
      "observedImage": "ghcr.io/stefanprodan/podinfo:6.14.1",
      "observedAt": "2026-08-22T09:59:00Z"
    },
    "action": {
      "type": "persona-update",
      "patch": {"resources": {"limits": {"memory": "128Mi", "cpu": "100m"}}},
      "prePatchState": {"resources": {"limits": {"memory": "32Mi"}}}
    },
    "steps": [
      {"order": 1, "id": "s1", "type": "persona-update", "description": "Raise the memory limit to 128Mi", "risk": "low", "autoExecutable": true, "patch": {"resources": {"limits": {"memory": "128Mi", "cpu": "100m"}}}, "prePatchState": {"resources": {"limits": {"memory": "32Mi"}}}},
      {"order": 2, "id": "s2", "type": "workload-apply", "description": "Set resources.limits.memory: 128Mi in the values for Helm release \"frontend\" in namespace apps (the key is chart-specific, commonly under ` + "`resources`" + `), then run your usual helm upgrade for that release.", "rationale": "Dorgu will not patch this Deployment because Helm owns it.", "risk": "low", "autoExecutable": false},
      {"order": 3, "id": "s3", "type": "manual", "description": "Watch for further OOM kills for 30m", "risk": "low", "autoExecutable": false}
    ],
    "planSource": "ai-anthropic",
    "planSummary": "podinfo is OOMKilled at a 32Mi memory limit.",
    "explanation": "OOM remediation for frontend",
    "confidence": "0.9",
    "approval": {"required": true},
    "rollback": {"enabled": true, "healthCheckAfter": "10m0s", "maxRetries": 1}
  },
  "status": {"phase": "Pending"}
}`

// helmOwnedDeploymentListFixture is the namespace as kubectl sees it: the
// Deployment is labelled on the pod template only, the Helm way.
const helmOwnedDeploymentListFixture = `{"items":[
 {"metadata":{"name":"frontend-podinfo","labels":{"app.kubernetes.io/name":"frontend"}},
  "spec":{"selector":{"matchLabels":{"app.kubernetes.io/name":"frontend"}},
  "template":{"spec":{"containers":[{"name":"podinfo"}]}}}}
]}`

const helmOwnedPersonaFixture = `{"apiVersion":"dorgu.io/v1","kind":"ApplicationPersona","metadata":{"name":"frontend","namespace":"apps"},"spec":{"name":"frontend"}}`

// parseRemediation is a test helper: fixtures are JSON because that is what the
// CLI actually reads out of the cluster.
func parseRemediation(t *testing.T, fixture string) *remediationFull {
	t.Helper()
	var r remediationFull
	require.NoError(t, json.Unmarshal([]byte(fixture), &r))
	return &r
}

// withManagedBy returns a copy of the fixture whose ownership is rewritten, so
// one plan can be tested against every owner.
func withManagedBy(t *testing.T, fixture, managedBy, detail string) string {
	t.Helper()
	var obj map[string]any
	require.NoError(t, json.Unmarshal([]byte(fixture), &obj))
	spec := obj["spec"].(map[string]any)
	ref := spec["workloadRef"].(map[string]any)
	ref["managedBy"] = managedBy
	ref["managedByDetail"] = detail
	out, err := json.Marshal(obj)
	require.NoError(t, err)
	return string(out)
}

// withObservedWorkload returns a copy of the fixture whose observed workload is
// rewritten, for cases whose target Deployment is not the one the shared fixture
// names.
func withObservedWorkload(t *testing.T, fixture, name, container string) string {
	t.Helper()
	var obj map[string]any
	require.NoError(t, json.Unmarshal([]byte(fixture), &obj))
	ref := obj["spec"].(map[string]any)["workloadRef"].(map[string]any)
	ref["name"] = name
	ref["container"] = container
	out, err := json.Marshal(obj)
	require.NoError(t, err)
	return string(out)
}

// withoutWorkloadRef returns a copy with no workloadRef at all: what an operator
// older than dorgu-operator #42 writes.
func withoutWorkloadRef(t *testing.T, fixture string) string {
	t.Helper()
	var obj map[string]any
	require.NoError(t, json.Unmarshal([]byte(fixture), &obj))
	delete(obj["spec"].(map[string]any), "workloadRef")
	out, err := json.Marshal(obj)
	require.NoError(t, err)
	return string(out)
}

// --- the contract itself ---

func TestWorkloadRefIsOwned(t *testing.T) {
	tests := []struct {
		name      string
		ref       *workloadRef
		wantOwned bool
	}{
		{name: "nil ref is owned, fail-safe", ref: nil, wantOwned: true},
		{name: "helm", ref: &workloadRef{Name: "d", ManagedBy: managedByHelm}, wantOwned: true},
		{name: "argocd", ref: &workloadRef{Name: "d", ManagedBy: managedByArgoCD}, wantOwned: true},
		{name: "flux", ref: &workloadRef{Name: "d", ManagedBy: managedByFlux}, wantOwned: true},
		{name: "kustomize", ref: &workloadRef{Name: "d", ManagedBy: managedByKustomize}, wantOwned: true},
		{name: "unknown is owned", ref: &workloadRef{Name: "d", ManagedBy: managedByUnknown}, wantOwned: true},
		{name: "empty managedBy is owned", ref: &workloadRef{Name: "d"}, wantOwned: true},
		{name: "unmanaged is the only writable case", ref: &workloadRef{Name: "d", ManagedBy: managedByUnmanaged}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.wantOwned, tt.ref.isOwned())
		})
	}
}

func TestOwnerNamingAndLocation(t *testing.T) {
	tests := []struct {
		name         string
		ref          *workloadRef
		wantOwner    string
		wantLocation string
		wantDescribe string
	}{
		{
			name: "managedByDetail is used verbatim",
			ref: &workloadRef{Kind: "Deployment", Name: "podinfo", Namespace: "apps", Container: "podinfo",
				ManagedBy: managedByHelm, ManagedByDetail: `Helm release "frontend" in namespace apps`},
			wantOwner:    `Helm release "frontend" in namespace apps`,
			wantLocation: `the values for Helm release "frontend" in namespace apps`,
			wantDescribe: "Deployment apps/podinfo (container podinfo)",
		},
		{
			name:         "helm with no detail still names the kind of owner",
			ref:          &workloadRef{Kind: "Deployment", Name: "podinfo", ManagedBy: managedByHelm},
			wantOwner:    "a Helm release",
			wantLocation: "the values for a Helm release",
			wantDescribe: "Deployment podinfo",
		},
		{
			name:         "flux",
			ref:          &workloadRef{Kind: "Deployment", Name: "api", ManagedBy: managedByFlux},
			wantOwner:    "a Flux controller",
			wantLocation: "the Git source reconciled by a Flux controller",
			wantDescribe: "Deployment api",
		},
		{
			name:         "kustomize",
			ref:          &workloadRef{Kind: "Deployment", Name: "api", ManagedBy: managedByKustomize},
			wantOwner:    "a kustomize overlay",
			wantLocation: "your kustomize overlay for this Deployment",
			wantDescribe: "Deployment api",
		},
		{
			name:         "resolved but unidentified",
			ref:          &workloadRef{Name: "api", ManagedBy: managedByUnknown},
			wantOwner:    "an owner Dorgu could not identify",
			wantLocation: "whatever manages Deployment api",
			wantDescribe: "Deployment api",
		},
		{
			name:         "no observation at all",
			ref:          nil,
			wantOwner:    "an owner Dorgu could not identify",
			wantLocation: "whatever manages this application",
			wantDescribe: "not recorded",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.wantOwner, ownerName(tt.ref))
			assert.Contains(t, ownerChangeLocation(tt.ref), tt.wantLocation)
			assert.Equal(t, tt.wantDescribe, tt.ref.describe())
		})
	}
}

func TestOwnedWorkloadErrorMessage(t *testing.T) {
	err := &ownedWorkloadError{ref: &workloadRef{
		Name: "podinfo", ManagedBy: managedByHelm, ManagedByDetail: `Helm release "frontend"`}}

	assert.Equal(t, `Dorgu will not patch this workload: Helm release "frontend" owns it`, err.Error())
}

// --- the heal preflight refuses ---

func TestPlanHealRefusesOwnedWorkload(t *testing.T) {
	owners := []struct {
		managedBy string
		detail    string
		wantOwner string
		wantWhy   string
	}{
		{managedByHelm, `Helm release "frontend" in namespace apps`,
			`Helm release "frontend" in namespace apps`, "helm upgrade"},
		{managedByArgoCD, `ArgoCD application "frontend"`,
			`ArgoCD application "frontend"`, "next sync"},
		{managedByFlux, `Flux Kustomization "apps"`, `Flux Kustomization "apps"`, "reconciles"},
		{managedByKustomize, "", "a kustomize overlay", "overlay"},
		{managedByUnknown, "", "an owner Dorgu could not identify", "Unknown is treated as owned"},
	}

	for _, owner := range owners {
		t.Run(owner.managedBy, func(t *testing.T) {
			fixture := withManagedBy(t, helmOwnedRemediationFixture, owner.managedBy, owner.detail)
			writeFakeKubectlDispatch(t, fakeKubectlResponses{
				context:    "kind-dorgu-spike",
				rem:        fixture,
				persona:    helmOwnedPersonaFixture,
				deployment: helmOwnedDeploymentListFixture,
			})

			_, err := planHeal(t.Context(), parseRemediation(t, fixture), healOptions{})

			var declined *ownedWorkloadError
			require.ErrorAs(t, err, &declined, "an owned workload must be declined, not patched")
			assert.Equal(t, owner.wantOwner, ownerName(declined.ref))
			assert.Contains(t, whyDorguWillNotPatch(declined.ref), owner.wantWhy)
		})
	}
}

// A RemediationAction written before the operator recorded ownership carries no
// workloadRef. No observation is no evidence that patching is safe.
func TestPlanHealRefusesRemediationWithNoWorkloadRef(t *testing.T) {
	fixture := withoutWorkloadRef(t, helmOwnedRemediationFixture)
	writeFakeKubectlDispatch(t, fakeKubectlResponses{
		context:    "kind-dorgu-spike",
		rem:        fixture,
		persona:    helmOwnedPersonaFixture,
		deployment: helmOwnedDeploymentListFixture,
	})

	rem := parseRemediation(t, fixture)
	require.Nil(t, rem.Spec.WorkloadRef)

	_, err := planHeal(t.Context(), rem, healOptions{})

	var declined *ownedWorkloadError
	require.ErrorAs(t, err, &declined, "a missing workloadRef must be treated as owned")
	assert.Contains(t, whyDorguWillNotPatch(declined.ref), "No record is treated as owned.")
}

// The one case that still patches: nothing reconciles the workload.
func TestPlanHealProceedsForUnmanagedWorkload(t *testing.T) {
	fixture := withManagedBy(t, helmOwnedRemediationFixture, managedByUnmanaged, "")
	writeFakeKubectlDispatch(t, fakeKubectlResponses{
		context:    "kind-dorgu-spike",
		rem:        fixture,
		persona:    helmOwnedPersonaFixture,
		deployment: helmOwnedDeploymentListFixture,
	})

	exec, err := planHeal(t.Context(), parseRemediation(t, fixture), healOptions{})

	require.NoError(t, err)
	require.True(t, exec.hasWorkloadChange())
	assert.Equal(t, "frontend-podinfo", exec.Deployment)
	assert.Equal(t, "podinfo", exec.Container, "the container comes from the live observation")
}

// --- approve refuses, end to end ---

func TestRunRemediationApproveRefusesOwnedWorkload(t *testing.T) {
	patchLog := writeFakeKubectlDispatch(t, fakeKubectlResponses{
		context:    "kind-dorgu-spike",
		rem:        helmOwnedRemediationFixture,
		persona:    helmOwnedPersonaFixture,
		deployment: helmOwnedDeploymentListFixture,
	})

	var out bytes.Buffer
	cmd := newRemediationApproveCmd()
	cmd.SetArgs([]string{"fix-oom-frontend", "-n", "apps", "--yes"})
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)

	var err error
	stdout := captureStdout(t, func() { err = cmd.Execute() })
	text := out.String() + stdout

	// Declined by design, not a failure.
	assert.Equal(t, ExitDeclined, ExitCode(err), "a refusal is a decision, not an error")
	assert.NotEqual(t, ExitError, ExitCode(err))

	// The trust moment: name the owner verbatim, say why, hand over the steps.
	assert.Contains(t, text, "Dorgu will not patch this workload.")
	assert.Contains(t, text, `Helm release "frontend" in namespace apps`)
	assert.Contains(t, text, "field-manager conflict")
	assert.Contains(t, text, "then run your usual helm upgrade for that release.")
	assert.Contains(t, text, "Nothing was approved and nothing in the cluster was changed.")

	// And nothing at all was written.
	log := readPatchLog(t, patchLog)
	assert.NotContains(t, log, "patch deployment", "a Helm-owned Deployment must never be patched")
	assert.NotContains(t, log, "patch remediationaction", "nothing may be approved when the heal is declined")
}

func TestRunRemediationHealRefusesOwnedWorkload(t *testing.T) {
	approved := strings.Replace(helmOwnedRemediationFixture, `"phase": "Pending"`, `"phase": "Approved"`, 1)
	patchLog := writeFakeKubectlDispatch(t, fakeKubectlResponses{
		context:    "kind-dorgu-spike",
		rem:        approved,
		persona:    helmOwnedPersonaFixture,
		deployment: helmOwnedDeploymentListFixture,
	})

	var out bytes.Buffer
	cmd := newRemediationHealCmd()
	cmd.SetArgs([]string{"fix-oom-frontend", "-n", "apps", "--yes"})
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)

	var err error
	stdout := captureStdout(t, func() { err = cmd.Execute() })
	text := out.String() + stdout

	assert.Equal(t, ExitDeclined, ExitCode(err))
	assert.Contains(t, text, "Dorgu will not patch this workload.")
	assert.Contains(t, text, `Helm release "frontend" in namespace apps`)
	assert.NotContains(t, text, "Workload heal failed", "a refusal must not read as a breakage")
	assert.NotContains(t, text, "Nothing was approved",
		"heal runs after approval, so it must not claim the approval was withheld")
	assert.Contains(t, text, "Nothing in the cluster was changed.")
	assert.NotContains(t, readPatchLog(t, patchLog), "patch deployment")
}

// The refusal shows the reader what would have happened to the Deployment, so
// declining is an informed decision rather than a dead end.
func TestOwnedRefusalShowsTheWorkloadDiff(t *testing.T) {
	rem := parseRemediation(t, helmOwnedRemediationFixture)
	plan, err := buildHealPlan(rem)
	require.NoError(t, err)

	var buf bytes.Buffer
	printOwnedWorkloadRefusal(&buf, rem, &ownedWorkloadError{ref: rem.Spec.WorkloadRef, plan: plan})
	out := buf.String()

	assert.Contains(t, out, "Deployment apps/frontend-podinfo (container podinfo)")
	assert.Contains(t, out, "resources.limits.memory")
	assert.Contains(t, out, "32Mi -> 128Mi")
	assert.Contains(t, out, "dorgu remediation approve fix-oom-frontend -n apps --no-heal")
}

// A plan with no owner-shaped step of its own still ends in something the reader
// can carry out.
func TestOwnedRefusalFallsBackToTheConcreteChange(t *testing.T) {
	ref := &workloadRef{
		Kind: "Deployment", Name: "checkout", Namespace: "shop", Container: "api",
		ManagedBy: managedByArgoCD, ManagedByDetail: `ArgoCD application "checkout"`,
	}
	plan := &healPlan{Change: &healResourceChange{Limits: map[string]string{"memory": "256Mi"}}}

	var buf bytes.Buffer
	printOwnedWorkloadRefusal(&buf, nil, &ownedWorkloadError{ref: ref, plan: plan})
	out := buf.String()

	assert.Contains(t, out, "Set resources.limits.memory: 256Mi in the Git manifests for "+
		`ArgoCD application "checkout", then commit and let ArgoCD sync.`)
}

// --- the diff is workload-vs-workload ---

// F-05: approving a memory fix silently added a CPU limit. It was invisible
// because the review diff compared persona to persona, and the persona was not
// the thing being changed.
func TestRemediationDiffSurfacesAKeyTheWorkloadDoesNotHave(t *testing.T) {
	rem := parseRemediation(t, helmOwnedRemediationFixture)

	var buf bytes.Buffer
	printRemediationDiff(&buf, rem)
	out := buf.String()

	assert.Contains(t, out, "Deployment change (apps/frontend-podinfo, container podinfo):")
	assert.Contains(t, out, "resources.limits.memory")
	assert.Contains(t, out, "32Mi -> 128Mi")
	// The added CPU limit, and the fact that it is an addition.
	assert.Contains(t, out, "resources.limits.cpu")
	assert.Contains(t, out, "not set -> 100m")
	assert.Contains(t, out, "adds a key this workload does not set")
	// An untouched observed key is shown as it is, never as blank or zero.
	assert.Contains(t, out, "resources.requests.memory")
	assert.Contains(t, out, "16Mi")
}

func TestBuildWorkloadResourceDiff(t *testing.T) {
	observedMemoryOnly := &workloadRef{
		Name: "frontend-podinfo", ManagedBy: managedByHelm,
		ObservedResources: &observedResources{Limits: &resourceValues{Memory: "32Mi"}},
	}

	tests := []struct {
		name   string
		ref    *workloadRef
		change *healResourceChange
		want   []workloadResourceKey
	}{
		{
			name:   "changed key",
			ref:    observedMemoryOnly,
			change: &healResourceChange{Limits: map[string]string{"memory": "128Mi"}},
			want: []workloadResourceKey{
				{Key: "resources.limits.memory", Before: "32Mi", After: "128Mi", Changed: true},
			},
		},
		{
			name:   "added key reads as absent, not zero and not blank",
			ref:    observedMemoryOnly,
			change: &healResourceChange{Limits: map[string]string{"cpu": "100m"}},
			want: []workloadResourceKey{
				{Key: "resources.limits.cpu", Before: "not set", After: "100m", Added: true, Changed: true},
				{Key: "resources.limits.memory", Before: "32Mi"},
			},
		},
		{
			name:   "unobserved workload reads as unknown, not as absent",
			ref:    nil,
			change: &healResourceChange{Limits: map[string]string{"memory": "128Mi"}},
			want: []workloadResourceKey{
				{Key: "resources.limits.memory", Before: "unknown", After: "128Mi", Changed: true},
			},
		},
		{
			name:   "a value the workload already has is not a change",
			ref:    observedMemoryOnly,
			change: &healResourceChange{Limits: map[string]string{"memory": "32Mi"}},
			want: []workloadResourceKey{
				{Key: "resources.limits.memory", Before: "32Mi", After: "32Mi"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, buildWorkloadResourceDiff(tt.ref, tt.change))
		})
	}
}

// --- commands are rendered only where they are runnable ---

// The operator strips workload-writing commands from an owned plan, but the CLI
// reads whatever is in the cluster: an older operator or a hand-written object
// can still carry a `kubectl patch`.
func TestNoKubectlPatchIsRenderedForAnOwnedWorkload(t *testing.T) {
	rem := parseRemediation(t, helmOwnedRemediationFixture)
	rem.Spec.Steps[1].Command = "kubectl patch deployment frontend-podinfo -n apps --type strategic -p {}"

	var buf bytes.Buffer
	printRemediationDiff(&buf, rem)
	out := buf.String()

	assert.NotContains(t, out, "kubectl patch",
		"a command that breaks the owner's next deploy must never be printed")
	assert.NotContains(t, out, "Run: ")
}

// The other half of the rule: a command that only reads is exactly what a
// reader wants on a workload Dorgu will not patch, because reading is all that
// is left to them.
func TestReadOnlyCommandIsRenderedForAnOwnedWorkload(t *testing.T) {
	rem := parseRemediation(t, helmOwnedRemediationFixture)
	rem.Spec.Steps[2].Command = "kubectl logs deployment/frontend-podinfo -n apps --previous"

	var buf bytes.Buffer
	printRemediationDiff(&buf, rem)

	assert.Contains(t, buf.String(),
		"Run: kubectl logs deployment/frontend-podinfo -n apps --previous")
}

// And it reaches the refusal too, which is the moment the reader has nothing
// else to go on.
func TestOwnedRefusalRendersAReadOnlyCommand(t *testing.T) {
	rem := parseRemediation(t, helmOwnedRemediationFixture)
	rem.Spec.Steps[2].Command = "kubectl get events -n apps --field-selector involvedObject.name=frontend-podinfo"
	plan, err := buildHealPlan(rem)
	require.NoError(t, err)

	var buf bytes.Buffer
	printOwnedWorkloadRefusal(&buf, rem, &ownedWorkloadError{ref: rem.Spec.WorkloadRef, plan: plan})

	assert.Contains(t, buf.String(),
		"Run: kubectl get events -n apps --field-selector involvedObject.name=frontend-podinfo")
}

func TestCommandIsRenderedForAnUnmanagedWorkload(t *testing.T) {
	rem := parseRemediation(t, withManagedBy(t, helmOwnedRemediationFixture, managedByUnmanaged, ""))
	rem.Spec.Steps[1].Command = "kubectl rollout restart deployment frontend-podinfo -n apps"

	var buf bytes.Buffer
	printRemediationDiff(&buf, rem)

	// Unmanaged is unchanged: a writing command is the right answer there.
	assert.Contains(t, buf.String(), "Run: kubectl rollout restart deployment frontend-podinfo -n apps")
}

// The CLI classifies the command itself rather than trusting that the operator
// stripped the dangerous ones: the text is model-authored, and an older
// operator or a hand-written object never went through that stripping at all.
func TestRunnableStepCommand(t *testing.T) {
	owned := &workloadRef{Name: "frontend-podinfo", ManagedBy: managedByHelm}
	unmanaged := &workloadRef{Name: "frontend-podinfo", ManagedBy: managedByUnmanaged}

	tests := []struct {
		name        string
		command     string
		wantOnOwned bool
	}{
		{name: "get", command: "kubectl get pods -n apps", wantOnOwned: true},
		{name: "logs", command: "kubectl logs deployment/frontend-podinfo -n apps", wantOnOwned: true},
		{name: "events", command: "kubectl get events -n apps", wantOnOwned: true},
		{name: "describe", command: "kubectl describe deployment frontend-podinfo -n apps", wantOnOwned: true},
		{name: "top", command: "kubectl top pod -n apps", wantOnOwned: true},
		{name: "rollout status", command: "kubectl rollout status deployment/frontend-podinfo -n apps", wantOnOwned: true},
		{name: "rollout history", command: "kubectl rollout history deployment/frontend-podinfo", wantOnOwned: true},
		{name: "verb behind a flag with a value",
			command: "kubectl -n apps get deployment frontend-podinfo", wantOnOwned: true},

		{name: "patch", command: "kubectl patch deployment frontend-podinfo -n apps -p {}"},
		{name: "patch behind a flag with a value",
			command: "kubectl -n apps patch deployment frontend-podinfo -p {}"},
		{name: "set image", command: "kubectl set image deployment/frontend-podinfo podinfo=podinfo:6.14.1"},
		{name: "apply", command: "kubectl apply -f deploy.yaml"},
		{name: "delete", command: "kubectl delete pod frontend-podinfo-abc -n apps"},
		{name: "scale", command: "kubectl scale deployment frontend-podinfo --replicas 3"},
		{name: "edit", command: "kubectl edit deployment frontend-podinfo -n apps"},
		{name: "rollout restart", command: "kubectl rollout restart deployment/frontend-podinfo"},
		{name: "rollout undo", command: "kubectl rollout undo deployment/frontend-podinfo"},
		{name: "bare rollout names no subcommand", command: "kubectl rollout deployment/frontend-podinfo"},
		{name: "unrecognised verb is not assumed harmless", command: "kubectl frobnicate deployment"},
		{name: "kubectl alone", command: "kubectl"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := runnableStepCommand(tt.command, owned)
			if tt.wantOnOwned {
				assert.Equal(t, tt.command, got, "a read-only command is runnable on an owned workload")
			} else {
				assert.Empty(t, got, "a command that writes must never be printed for an owned workload")
			}

			// Unmanaged is unchanged by any of this: the sanitizer is the only
			// gate, because there is no owner to break.
			assert.Equal(t, displayableStepCommand(tt.command), runnableStepCommand(tt.command, unmanaged))
		})
	}
}

// The sanitizer still runs first, on owned and unmanaged alike.
func TestRunnableStepCommandStillSanitizes(t *testing.T) {
	owned := &workloadRef{Name: "frontend-podinfo", ManagedBy: managedByHelm}
	unmanaged := &workloadRef{Name: "frontend-podinfo", ManagedBy: managedByUnmanaged}

	for _, cmd := range []string{
		"kubectl get pods -n apps; rm -rf /",
		"kubectl get pods -n apps && curl evil.example.com",
		"helm upgrade frontend ./chart",
		"  ",
	} {
		assert.Empty(t, runnableStepCommand(cmd, owned), "owned: %q", cmd)
		assert.Empty(t, runnableStepCommand(cmd, unmanaged), "unmanaged: %q", cmd)
	}
}

func TestHealPlanDropsWritingCommandsForOwnedWorkloads(t *testing.T) {
	rem := parseRemediation(t, helmOwnedRemediationFixture)
	rem.Spec.Steps[1].Command = "kubectl patch deployment frontend-podinfo -n apps -p {}"
	rem.Spec.Steps[2].Command = "kubectl logs deployment/frontend-podinfo -n apps"

	plan, err := buildHealPlan(rem)
	require.NoError(t, err)
	require.Len(t, plan.Advisory, 2)

	assert.Empty(t, plan.Advisory[0].Command, "a writing command is dropped on an owned workload")
	assert.Equal(t, "kubectl logs deployment/frontend-podinfo -n apps", plan.Advisory[1].Command,
		"a read-only command survives, because reading is what is left here")
}

// --- diff does not walk the reader into the refusal ---

func TestRemediationDiffDoesNotOfferApproveForAnOwnedWorkload(t *testing.T) {
	rem := parseRemediation(t, helmOwnedRemediationFixture)

	var buf bytes.Buffer
	printRemediationDiff(&buf, rem)
	out := buf.String()

	assert.NotContains(t, out, "dorgu remediation approve fix-oom-frontend -n apps\n",
		"offering the command that will be declined is how a guard reads as breakage")
	assert.Contains(t, out, `Helm release "frontend" in namespace apps`)
	assert.Contains(t, out, "dorgu remediation reject fix-oom-frontend -n apps")
}

func TestRemediationDiffNamesTheOwnerInTheHeader(t *testing.T) {
	rem := parseRemediation(t, helmOwnedRemediationFixture)

	var buf bytes.Buffer
	printRemediationDiff(&buf, rem)
	out := buf.String()

	assert.Contains(t, out, "Workload:   Deployment apps/frontend-podinfo (container podinfo)")
	assert.Contains(t, out, `Owner:      Helm release "frontend" in namespace apps`)
}

// --- the guard cannot be walked around ---

// Ownership was checked for the workload the operator observed. Pointing the
// heal at a different Deployment would patch one whose owner Dorgu never looked
// at, which is the guard with a hole in it.
func TestPlanHealRefusesAWorkloadFlagThatDisagreesWithTheObservation(t *testing.T) {
	fixture := withManagedBy(t, helmOwnedRemediationFixture, managedByUnmanaged, "")
	writeFakeKubectlDispatch(t, fakeKubectlResponses{
		context:    "kind-dorgu-spike",
		rem:        fixture,
		persona:    helmOwnedPersonaFixture,
		deployment: deploymentObjectFixture, // --workload fetches a single object
	})

	_, err := planHeal(t.Context(), parseRemediation(t, fixture),
		healOptions{workload: "custom-api"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "frontend-podinfo")
	assert.Contains(t, err.Error(), "custom-api")
}

// --- house style ---

func TestOwnershipCopyHasNoEmDashes(t *testing.T) {
	rem := parseRemediation(t, helmOwnedRemediationFixture)
	plan, err := buildHealPlan(rem)
	require.NoError(t, err)

	var buf bytes.Buffer
	printRemediationDiff(&buf, rem)
	printOwnedWorkloadRefusal(&buf, rem, &ownedWorkloadError{ref: rem.Spec.WorkloadRef, plan: plan})

	require.NotContains(t, buf.String(), "—")
}

func TestExitDeclinedIsDistinctFromFailure(t *testing.T) {
	declined := withExitCode(ExitDeclined, errSilent)

	assert.Equal(t, 4, ExitDeclined)
	assert.Equal(t, ExitDeclined, ExitCode(declined))
	assert.NotEqual(t, ExitError, ExitCode(declined))
	assert.True(t, errors.Is(declined, errSilent), "the refusal prints itself, so Execute must not re-print it")
}
