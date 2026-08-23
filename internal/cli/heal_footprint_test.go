package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// managedFieldsEntryJSON renders one managedFields entry the way the API server
// does, including the fields this CLI does not read. Those extra fields are the
// point of several tests below: a strip that drops them has rewritten another
// manager's ownership record.
func managedFieldsEntryJSON(manager, operation string) string {
	return fmt.Sprintf(`{"manager":%q,"operation":%q,"apiVersion":"apps/v1",`+
		`"time":"2026-08-24T10:00:00Z","fieldsType":"FieldsV1","fieldsV1":`+
		`{"f:spec":{"f:template":{"f:spec":{"f:containers":`+
		`{"k:{\"name\":\"api-server\"}":{"f:resources":{"f:limits":{"f:memory":{}}}}}}}}}}`,
		manager, operation)
}

// deploymentWithManagers renders `kubectl get deployment -o json
// --show-managed-fields` output carrying the given field managers.
func deploymentWithManagers(resourceVersion string, managers ...string) string {
	entries := make([]string, 0, len(managers))
	for _, m := range managers {
		entries = append(entries, managedFieldsEntryJSON(m, "Update"))
	}
	return fmt.Sprintf(
		`{"metadata":{"name":"api-server","resourceVersion":%q,"managedFields":[%s]},`+
			`"spec":{"template":{"spec":{"containers":[{"name":"api-server"}]}}}}`,
		resourceVersion, strings.Join(entries, ","))
}

func TestParseDeploymentFootprint(t *testing.T) {
	raw := deploymentWithManagers("4471", "kubectl-client-side-apply", "dorgu", "kube-controller-manager")

	got, err := parseDeploymentFootprint([]byte(raw))
	require.NoError(t, err)

	assert.Equal(t, "4471", got.ResourceVersion)
	require.Len(t, got.Entries, 3)
	assert.Equal(t, "kubectl-client-side-apply", got.Entries[0].Manager)
	assert.True(t, got.has("dorgu"))
	assert.True(t, got.has("DORGU"), "manager matching is case-insensitive")
	assert.False(t, got.has("helm"))
}

func TestParseDeploymentFootprintNoManagedFields(t *testing.T) {
	// kubectl hides managedFields unless --show-managed-fields is passed, and a
	// Deployment that genuinely has none reads the same way. Neither is an
	// error, and both mean there is no Dorgu footprint to remove.
	got, err := parseDeploymentFootprint([]byte(`{"metadata":{"name":"api-server"}}`))
	require.NoError(t, err)
	assert.Empty(t, got.Entries)
	assert.False(t, got.has(dorguFieldManager))
}

func TestParseDeploymentFootprintRejectsGarbage(t *testing.T) {
	_, err := parseDeploymentFootprint([]byte(`not json`))
	require.Error(t, err)
}

// TestBuildFootprintStripPatchKeepsEveryOtherManagerByteForByte is the property
// that makes this safe to do at all. Removing one entry means writing the whole
// list back, so every entry Dorgu is not removing has to survive unchanged,
// including the fields this CLI never parses.
func TestBuildFootprintStripPatchKeepsEveryOtherManagerByteForByte(t *testing.T) {
	raw := deploymentWithManagers("4471", "kubectl-client-side-apply", "dorgu", "kube-controller-manager")
	f, err := parseDeploymentFootprint([]byte(raw))
	require.NoError(t, err)

	patch, needed, err := buildFootprintStripPatch(f, dorguFieldManager)
	require.NoError(t, err)
	require.True(t, needed)

	var got struct {
		Metadata struct {
			ResourceVersion string            `json:"resourceVersion"`
			ManagedFields   []json.RawMessage `json:"managedFields"`
		} `json:"metadata"`
	}
	require.NoError(t, json.Unmarshal([]byte(patch), &got))

	assert.Equal(t, "4471", got.Metadata.ResourceVersion,
		"the read resourceVersion is the precondition that stops this clobbering a concurrent write")
	require.Len(t, got.Metadata.ManagedFields, 2)
	assert.JSONEq(t, managedFieldsEntryJSON("kubectl-client-side-apply", "Update"),
		string(got.Metadata.ManagedFields[0]))
	assert.JSONEq(t, managedFieldsEntryJSON("kube-controller-manager", "Update"),
		string(got.Metadata.ManagedFields[1]))
	assert.NotContains(t, patch, `"dorgu"`)
}

func TestBuildFootprintStripPatchNoFootprint(t *testing.T) {
	f, err := parseDeploymentFootprint([]byte(deploymentWithManagers("1", "helm", "kube-controller-manager")))
	require.NoError(t, err)

	_, needed, err := buildFootprintStripPatch(f, dorguFieldManager)
	require.NoError(t, err)
	assert.False(t, needed, "nothing of Dorgu's on the object means nothing to write")
}

// TestBuildFootprintStripPatchSoleEntryUsesTheResetForm pins the one case where
// the obvious value is wrong. An empty list does not reliably clear
// managedFields across API server versions; a list holding one empty entry is
// the documented reset and does.
func TestBuildFootprintStripPatchSoleEntryUsesTheResetForm(t *testing.T) {
	f, err := parseDeploymentFootprint([]byte(deploymentWithManagers("9", "dorgu")))
	require.NoError(t, err)

	patch, needed, err := buildFootprintStripPatch(f, dorguFieldManager)
	require.NoError(t, err)
	require.True(t, needed)
	assert.Contains(t, patch, `"managedFields":[{}]`)
	assert.NotContains(t, patch, `"managedFields":[]`)
}

func TestIsConflictError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{
			name: "the API server's optimistic concurrency rejection",
			err:  fmt.Errorf("Error from server (Conflict): the object has been modified; please apply your changes to the latest version and try again"),
			want: true,
		},
		{
			name: "an RBAC denial is not a conflict and must not be retried",
			err:  fmt.Errorf("Error from server (Forbidden): deployments.apps is forbidden"),
			want: false,
		},
		{
			name: "a field-manager apply conflict also reads as Conflict",
			err:  fmt.Errorf("Error from server (Conflict): Apply failed with 1 conflict"),
			want: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, isConflictError(tc.err))
		})
	}
}

func TestFootprintFieldNames(t *testing.T) {
	names := footprintFieldNames(&healResourceChange{
		Limits:   map[string]string{"memory": "128Mi"},
		Requests: map[string]string{"memory": "64Mi"},
	})
	assert.Equal(t, []string{"resources.limits.memory", "resources.requests.memory"}, names)

	assert.Equal(t, []string{"the fields it patched"}, footprintFieldNames(nil),
		"the warning still has to say something concrete when the change is unreadable")
}

// --- end to end through the real heal path ---

// deploymentAfterDorguPatch is what the Deployment looks like once Dorgu has
// patched it: its own Update-operation entry sitting on the resource fields.
// This is the F-03 footprint.
var deploymentAfterDorguPatch = deploymentWithManagers("4471",
	"kubectl-client-side-apply", "dorgu", "kube-controller-manager")

// deploymentAfterStrip is what it looks like once the entry is removed: no
// manager attributable to Dorgu, so a later server-side apply has nothing of
// Dorgu's to conflict with.
var deploymentAfterStrip = deploymentWithManagers("4472",
	"kubectl-client-side-apply", "kube-controller-manager")

// TestHealPatchesUnderDorgusOwnFieldManager pins the first half of F-03. The
// patch has to be attributable to Dorgu, because an entry that cannot be told
// apart from a `kubectl patch` the user ran is an entry Dorgu must not delete.
func TestHealPatchesUnderDorgusOwnFieldManager(t *testing.T) {
	patchLog := writeFakeKubectlDispatch(t, fakeKubectlResponses{
		context:              "kind-dorgu-spike",
		rem:                  operatorRemediationFixture,
		persona:              personaFixture,
		deployment:           deploymentListFixture,
		deploymentAfterPatch: deploymentAfterDorguPatch,
		deploymentAfterStrip: deploymentAfterStrip,
	})

	cmd := newRemediationHealCmd()
	cmd.SetArgs([]string{"fix-oom-api-server", "-n", "production", "--yes"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	require.NoError(t, cmd.Execute())

	log := readPatchLog(t, patchLog)
	assert.Contains(t, log, "--field-manager dorgu",
		"the resource patch must be attributable to Dorgu, not to kubectl-patch")
}

// TestHealLeavesNoDorguManagedFieldsEntry is F-03 itself: after a heal, nothing
// on the Deployment is attributable to Dorgu.
//
// The tester's reproduction was a server-side apply by a gitops tool failing
// against the entry Dorgu's own patch left behind. The equivalent here is that
// the entry is gone, verified by reading the object back rather than assumed
// from the patch having been accepted.
func TestHealLeavesNoDorguManagedFieldsEntry(t *testing.T) {
	patchLog := writeFakeKubectlDispatch(t, fakeKubectlResponses{
		context:              "kind-dorgu-spike",
		rem:                  operatorRemediationFixture,
		persona:              personaFixture,
		deployment:           deploymentListFixture,
		deploymentAfterPatch: deploymentAfterDorguPatch,
		deploymentAfterStrip: deploymentAfterStrip,
	})

	var out bytes.Buffer
	cmd := newRemediationHealCmd()
	cmd.SetArgs([]string{"fix-oom-api-server", "-n", "production", "--yes"})
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	require.NoError(t, cmd.Execute())

	log := readPatchLog(t, patchLog)
	assert.Contains(t, log, "managedFields", "the footprint has to actually be removed")
	assert.Contains(t, log, `"resourceVersion":"4471"`,
		"the removal is a read-modify-write and needs its precondition")
	assert.Contains(t, log, "kubectl-client-side-apply",
		"every other manager's entry is written back")
	assert.Contains(t, log, "kube-controller-manager")

	// The state Dorgu leaves behind: no entry of its own, so a later
	// `kubectl apply --server-side` has nothing of Dorgu's to conflict with.
	after, err := parseDeploymentFootprint([]byte(deploymentAfterStrip))
	require.NoError(t, err)
	assert.False(t, after.has(dorguFieldManager))

	assert.NotContains(t, out.String(), "could not remove",
		"a clean strip says nothing; the warning is for when it fails")
}

// TestHealWarnsWhenTheFootprintSurvives is the other half of the instruction:
// never silently leave the footprint. If the API server keeps Dorgu's entry
// despite the removal, the heal still succeeded and is reported as such, but
// the user is told exactly what is now owned, what it will break, and how to
// clear it.
func TestHealWarnsWhenTheFootprintSurvives(t *testing.T) {
	writeFakeKubectlDispatch(t, fakeKubectlResponses{
		context:              "kind-dorgu-spike",
		rem:                  operatorRemediationFixture,
		persona:              personaFixture,
		deployment:           deploymentListFixture,
		deploymentAfterPatch: deploymentAfterDorguPatch,
		// The removal is accepted and changes nothing: Dorgu's entry is still
		// there on the read-back.
		deploymentAfterStrip: deploymentAfterDorguPatch,
	})

	var out bytes.Buffer
	cmd := newRemediationHealCmd()
	cmd.SetArgs([]string{"fix-oom-api-server", "-n", "production", "--yes"})
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	require.NoError(t, cmd.Execute(), "the workload was patched, so the heal did not fail")

	text := out.String()
	assert.Contains(t, text, "could not remove its own field-manager entry")
	assert.Contains(t, text, "resources.limits.memory")
	assert.Contains(t, text, "will fail with a conflict")
	assert.Contains(t, text, `--type merge -p '{"metadata":{"managedFields":[{}]}}'`,
		"the warning has to hand over the command that clears it")
}

// TestHealWarnsWhenTheRemovalKeepsConflicting covers the other failure shape: a
// Conflict every time, meaning something else keeps writing to the Deployment.
// Retried, then reported rather than swallowed.
func TestHealWarnsWhenTheRemovalKeepsConflicting(t *testing.T) {
	patchLog := writeFakeKubectlDispatch(t, fakeKubectlResponses{
		context:              "kind-dorgu-spike",
		rem:                  operatorRemediationFixture,
		persona:              personaFixture,
		deployment:           deploymentListFixture,
		deploymentAfterPatch: deploymentAfterDorguPatch,
		deploymentAfterStrip: deploymentAfterDorguPatch,
		failStripPatch:       true,
	})

	var out bytes.Buffer
	cmd := newRemediationHealCmd()
	cmd.SetArgs([]string{"fix-oom-api-server", "-n", "production", "--yes"})
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	require.NoError(t, cmd.Execute())

	assert.Equal(t, stripFootprintAttempts,
		strings.Count(readPatchLog(t, patchLog), "managedFields"),
		"a Conflict is retried against fresh state, and then given up on")
	assert.Contains(t, out.String(), "could not remove its own field-manager entry")
}

// TestHealSkipsTheStripWhenThereIsNoFootprint keeps the common path quiet. A
// Deployment carrying no Dorgu entry gets no managedFields write at all.
func TestHealSkipsTheStripWhenThereIsNoFootprint(t *testing.T) {
	patchLog := writeFakeKubectlDispatch(t, fakeKubectlResponses{
		context:    "kind-dorgu-spike",
		rem:        operatorRemediationFixture,
		persona:    personaFixture,
		deployment: deploymentListFixture,
		deploymentAfterPatch: deploymentWithManagers("4471",
			"kubectl-client-side-apply", "kube-controller-manager"),
	})

	var out bytes.Buffer
	cmd := newRemediationHealCmd()
	cmd.SetArgs([]string{"fix-oom-api-server", "-n", "production", "--yes"})
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	require.NoError(t, cmd.Execute())

	assert.NotContains(t, readPatchLog(t, patchLog), "managedFields")
	assert.NotContains(t, out.String(), "could not remove")
}

// TestHealStatesTheLimitOfTheUnmanagedClassification is the honest half of
// F-08. Dorgu reaches a heal only by classifying the workload unmanaged, and
// that classification cannot see a kustomize overlay. Saying so is cheap;
// letting the silence imply a stronger guarantee is what the homepage claim did.
func TestHealStatesTheLimitOfTheUnmanagedClassification(t *testing.T) {
	writeFakeKubectlDispatch(t, fakeKubectlResponses{
		context:              "kind-dorgu-spike",
		rem:                  operatorRemediationFixture,
		persona:              personaFixture,
		deployment:           deploymentListFixture,
		deploymentAfterPatch: deploymentAfterDorguPatch,
		deploymentAfterStrip: deploymentAfterStrip,
	})

	var out bytes.Buffer
	cmd := newRemediationHealCmd()
	cmd.SetArgs([]string{"fix-oom-api-server", "-n", "production", "--yes"})
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	require.NoError(t, cmd.Execute())

	text := out.String()
	assert.Contains(t, text, "Nothing Dorgu can see reconciles this Deployment")
	assert.Contains(t, text, "kustomize overlay leaves no marker")
	assert.NotContains(t, text, "refuses to patch",
		"the caveat is a limit on the claim, not a refusal")
}
