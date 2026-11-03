package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Adversarial bypass suite, CLI group (harness plan P0, task 0.2).
//
// Each test plays one attack from ADVERSARIAL.md and asserts the SAFE outcome.
// A scenario that is not yet fixed is listed in openScenarios with the
// checkpoint expected to close it; its test skips with "OPEN" so CI stays green
// while the status stays explicit. `make adversarial` prints blocked/total.
//
// The bookkeeping is strict in both directions:
//   - open and still bypassable: skipped, reported OPEN;
//   - open but now blocked: fails, so the list cannot drift out of date;
//   - not open and bypassable: fails, because that is a regression.

// openScenarios maps a scenario ID to why it is still open and when it is
// expected to be green.
var openScenarios = map[string]string{
	"CLI-01": "expected green by CP3 (stopgap P1): heal trusts spec.workloadRef.managedBy instead of re-detecting ownership on the live Deployment",
	"CLI-02": "expected green by CP3 (stopgap P1): an empty workloadRef.name with managedBy unmanaged counts as not owned, so the fallback chain picks a Deployment nobody checked",
	"CLI-03": "expected green by CP3: heal treats an empty phase as an implicit approval; there is no apply ticket to require",
	"CLI-04": "expected green by CP3: the only context check is a name match, so a renamed context passes; no ticket or identity binds the write",
	"CLI-06": "expected green by CP3: ownership is read from the proposal, so a workload adopted by Argo CD after it was written is still patched",
}

// cliScenarioIDs is the CLI catalogue; every ID has exactly one test below.
var cliScenarioIDs = []string{"CLI-01", "CLI-02", "CLI-03", "CLI-04", "CLI-05", "CLI-06"}

// expectBlocked records one scenario's result against openScenarios.
func expectBlocked(t *testing.T, id string, blocked bool, detail string) {
	t.Helper()
	reason, open := openScenarios[id]
	switch {
	case open && !blocked:
		t.Skipf("OPEN %s: %s [%s]", id, detail, reason)
	case open && blocked:
		t.Fatalf("%s is now blocked (%s): remove it from openScenarios", id, detail)
	case !blocked:
		t.Fatalf("%s bypass NOT blocked: %s", id, detail)
	default:
		t.Logf("BLOCKED %s: %s", id, detail)
	}
}

func TestOpenScenariosAreCatalogued(t *testing.T) {
	known := map[string]bool{}
	for _, id := range cliScenarioIDs {
		known[id] = true
	}
	var unknown []string
	for id := range openScenarios {
		if !known[id] {
			unknown = append(unknown, id)
		}
	}
	sort.Strings(unknown)
	assert.Empty(t, unknown, "openScenarios lists IDs that are not in the CLI catalogue")
}

// --- fixtures ---

// healResult is what one heal attempt did to the fake cluster.
type healResult struct {
	err         error
	patchLog    string
	deployPatch bool
}

// runAdversarialHeal runs `dorgu remediation heal --yes` against the fake
// kubectl and reports whether any Deployment was patched.
func runAdversarialHeal(t *testing.T, r fakeKubectlResponses, extraArgs ...string) healResult {
	t.Helper()
	patchLog := writeFakeKubectlDispatch(t, r)

	cmd := newRemediationHealCmd()
	cmd.SetArgs(append([]string{"fix-oom-api-server", "-n", "production", "--yes"}, extraArgs...))
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	err := cmd.Execute()

	log := readPatchLog(t, patchLog)
	return healResult{err: err, patchLog: log, deployPatch: strings.Contains(log, "patch deployment")}
}

// approvedRemediation is the operator fixture moved to Approved, so the phase
// gate is not what stops a heal.
func approvedRemediation() string {
	return withPhase(operatorRemediationFixture, `"phase": "Approved"`)
}

func withPhase(fixture, phase string) string {
	return strings.Replace(fixture, `"phase": "Pending"`, phase, 1)
}

// liveDeployments renders a Deployment list whose objects carry the given
// labels and annotations, the way the API server would return them.
func liveDeployments(name, labels, annotations string) string {
	return `{"items":[{"metadata":{"name":"` + name + `","labels":{` + labels + `},"annotations":{` + annotations + `}},` +
		`"spec":{"template":{"spec":{"containers":[{"name":"` + name + `","resources":{"limits":{"memory":"256Mi"}}}]}}}}]}`
}

const helmLabels = `"app.kubernetes.io/managed-by":"Helm"`
const helmAnnotations = `"meta.helm.sh/release-name":"api-server","meta.helm.sh/release-namespace":"production"`
const argoLabels = `"app.kubernetes.io/instance":"api-server","argocd.argoproj.io/instance":"api-server"`
const argoAnnotations = `"argocd.argoproj.io/tracking-id":"api-server:apps/Deployment:production/api-server"`

func mustParseRemediation(t *testing.T, fixture string) *remediationFull {
	t.Helper()
	var r remediationFull
	require.NoError(t, json.Unmarshal([]byte(fixture), &r))
	return &r
}

// --- scenarios ---

// CLI-01: a RemediationAction claims managedBy: unmanaged for a Deployment that
// Helm owns. Safe outcome: the actuator reads ownership from the live object
// and refuses.
func TestAdversarialCLI01ForgedUnmanagedOnHelmWorkload(t *testing.T) {
	res := runAdversarialHeal(t, fakeKubectlResponses{
		context:    "kind-dorgu-spike",
		rem:        approvedRemediation(),
		persona:    personaFixture,
		deployment: liveDeployments("api-server", helmLabels, helmAnnotations),
	})
	expectBlocked(t, "CLI-01", !res.deployPatch,
		"spec.workloadRef.managedBy=unmanaged on a Deployment labelled app.kubernetes.io/managed-by=Helm")
}

// CLI-02: an empty workloadRef.name with managedBy: unmanaged. Safe outcome:
// refused, because no workload's ownership was ever checked.
func TestAdversarialCLI02EmptyWorkloadNameUnmanaged(t *testing.T) {
	rem := strings.Replace(approvedRemediation(), `"name": "api-server",
      "namespace": "production",
      "container"`, `"name": "",
      "namespace": "production",
      "container"`, 1)
	if rem == approvedRemediation() {
		t.Fatal("fixture drifted: could not blank workloadRef.name")
	}
	res := runAdversarialHeal(t, fakeKubectlResponses{
		context:    "kind-dorgu-spike",
		rem:        rem,
		persona:    personaFixture,
		deployment: liveDeployments("api-server", "", ""),
	})
	expectBlocked(t, "CLI-02", !res.deployPatch,
		"workloadRef.name empty with managedBy=unmanaged; the fallback chain chose the Deployment")
}

// CLI-03: `heal` on a RemediationAction whose status.phase is empty, i.e. one
// nobody approved and the operator never processed. Safe outcome: refused,
// there is no ticket.
func TestAdversarialCLI03HealOnEmptyPhase(t *testing.T) {
	res := runAdversarialHeal(t, fakeKubectlResponses{
		context:    "kind-dorgu-spike",
		rem:        withPhase(operatorRemediationFixture, `"phase": ""`),
		persona:    personaFixture,
		deployment: deploymentListFixture,
	})
	expectBlocked(t, "CLI-03", !res.deployPatch, "status.phase empty; heal patched the Deployment")
}

// CLI-04: the production cluster's kube context is renamed so it no longer
// matches the protected pattern. Safe outcome: safety does not hinge on the
// context name. Without a ticket bound to identity, an approved action with no
// ticket must not be applied at all.
func TestAdversarialCLI04RenamedContext(t *testing.T) {
	t.Setenv(protectedContextsEnv, "")

	// The guard itself does its job on the real name...
	if err := guardKubeContext("acme-prod-cluster"); err == nil {
		t.Fatal("precondition: the unrenamed production context must be refused")
	}

	// ...and the same cluster under another name is healed.
	res := runAdversarialHeal(t, fakeKubectlResponses{
		context:    "acme-cluster",
		rem:        approvedRemediation(),
		persona:    personaFixture,
		deployment: deploymentListFixture,
	})
	expectBlocked(t, "CLI-04", !res.deployPatch,
		"context acme-prod-cluster renamed to acme-cluster; heal proceeded with no ticket")
}

// CLI-05: a flag value equal to a verb name. Safe outcome: the command is
// classified as a write, so it is never printed as runnable on an owned
// workload.
func TestAdversarialCLI05FlagValueNamedAfterAVerb(t *testing.T) {
	owned := &workloadRef{Name: "api-server", ManagedBy: managedByHelm}
	attacks := []string{
		"kubectl -n logs delete deployment/api-server",
		"kubectl --namespace get patch deployment/api-server -p {}",
		"kubectl -o describe delete deployment/api-server",
		"kubectl --context top drain node-a",
	}

	var leaked []string
	for _, cmd := range attacks {
		if readsOnly(cmd) || runnableStepCommand(cmd, owned) != "" {
			leaked = append(leaked, cmd)
		}
	}

	// End to end too: the diff rendered for an owned workload must not carry
	// the command. The unmanaged render proves the renderer prints step
	// commands at all, so the owned check is not vacuous.
	if !strings.Contains(renderDiffWithCommand(t, "unmanaged", attacks[0]), attacks[0]) {
		t.Fatal("precondition: the diff must print a step command on an unmanaged workload")
	}
	if strings.Contains(renderDiffWithCommand(t, "helm", attacks[0]), attacks[0]) {
		leaked = append(leaked, "rendered on owned workload: "+attacks[0])
	}

	detail := "every write was classified as a write"
	if len(leaked) > 0 {
		detail = "write commands treated as read-only: " + strings.Join(leaked, "; ")
	}
	expectBlocked(t, "CLI-05", len(leaked) == 0, detail)
}

// renderDiffWithCommand renders `remediation diff` for the operator fixture
// with the given ownership and the restart step carrying command.
func renderDiffWithCommand(t *testing.T, managedBy, command string) string {
	t.Helper()
	rem := mustParseRemediation(t, strings.Replace(operatorRemediationFixture,
		`"managedBy": "unmanaged"`, `"managedBy": "`+managedBy+`"`, 1))
	rem.Spec.Steps[0].Command = command
	var buf bytes.Buffer
	printRemediationDiff(&buf, rem)
	return buf.String()
}

// CLI-06: the workload was unmanaged when Dorgu proposed, and Argo CD adopted it
// before the heal. Safe outcome: the actuator re-checks ownership live and
// refuses.
func TestAdversarialCLI06AdoptedByArgoAfterProposal(t *testing.T) {
	res := runAdversarialHeal(t, fakeKubectlResponses{
		context:    "kind-dorgu-spike",
		rem:        approvedRemediation(),
		persona:    personaFixture,
		deployment: liveDeployments("api-server", argoLabels, argoAnnotations),
	})
	expectBlocked(t, "CLI-06", !res.deployPatch,
		"proposal recorded managedBy=unmanaged; the live Deployment now carries Argo CD tracking")
}
