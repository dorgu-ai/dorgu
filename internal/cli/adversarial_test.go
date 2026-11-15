package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"sort"
	"strings"
	"testing"
	"time"

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
var openScenarios = map[string]string{}

// cliScenarioIDs is the CLI catalogue; every ID has exactly one test below.
var cliScenarioIDs = []string{"CLI-01", "CLI-02", "CLI-03", "CLI-04", "CLI-05", "CLI-06", "CLI-07", "CLI-08", "CLI-09"}

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
		noTicket:   true,
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
		noTicket:   true, // the operator issued none: approved by a status write, never re-checked
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

// CLI-07 (ID-03 from the CLI side, P2 task 2.7): the CLI is made to write an
// approvedBy that is not the authenticated user: the old literal `cli-user`, a name
// from the environment or the kubeconfig, or a made-up one when the cluster
// cannot say who the user is. Safe outcome: approve, reject and the heal record
// write the SelfSubjectReview username, and when there is none they write nothing.
func TestAdversarialCLI07ApprovedByIsNeverAnythingButTheAuthenticatedUser(t *testing.T) {
	var forged []string
	record := func(what, patchLog, wantUser string) {
		for _, bad := range []string{"cli-user", "approvedBy\":\"\""} {
			if strings.Contains(patchLog, bad) {
				forged = append(forged, what+" wrote "+bad)
			}
		}
		if wantUser != "" && !strings.Contains(patchLog, `"approvedBy":"`+wantUser+`"`) {
			forged = append(forged, what+" did not write "+wantUser)
		}
	}

	// The user's environment and kubeconfig suggest other names; none may be used.
	t.Setenv("USER", "someone-else")
	t.Setenv("LOGNAME", "someone-else")

	// approve, as the authenticated user.
	r := baseResponses()
	r.whoami = whoamiJSON(fixtureUser)
	r.rem = withResourceVersion(operatorRemediationFixture, "4242")
	r.remAfterApprove = withApprovePhase(r.rem, "Approved")
	record("approve", approveNoHeal(t, r).patchLog, fixtureUser)

	// reject, as the authenticated user.
	record("reject", runRejectAs(t, fixtureUser, operatorRemediationFixture, "--reason", "too-risky").patchLog, fixtureUser)

	// the heal record's implicit approval, as the authenticated user.
	patch, err := buildHealRecordPatch(recordState{Phase: "Pending"}, healRecordFor(), "2026-08-27T12:00:00Z", fixtureUser)
	require.NoError(t, err)
	record("heal record", patch, fixtureUser)

	// no identity: nothing is written at all.
	for _, mutate := range []func(*fakeKubectlResponses){
		func(r *fakeKubectlResponses) { r.whoamiError = `error: unknown command "whoami"` }, // SelfSubjectReview unavailable
		func(r *fakeKubectlResponses) { r.whoami = whoamiJSON("") },                         // no username
	} {
		nr := baseResponses()
		nr.rem = withResourceVersion(operatorRemediationFixture, "4242")
		mutate(&nr)
		if log := approveNoHeal(t, nr).patchLog; log != "" {
			forged = append(forged, "approve without an identity wrote: "+log)
		}
		rr := fakeKubectlResponses{context: "kind-dorgu-spike", rem: operatorRemediationFixture, persona: personaFixture,
			deployment: deploymentListFixture, noTicket: true}
		mutate(&rr)
		if log := runRejectWith(t, rr, "--reason", "too-risky").patchLog; log != "" {
			forged = append(forged, "reject without an identity wrote: "+log)
		}
	}
	if _, err := buildHealRecordPatch(recordState{Phase: "Pending"}, healRecordFor(), "2026-08-27T12:00:00Z", ""); err == nil {
		forged = append(forged, "heal record without an identity built a patch")
	}

	detail := "every approvedBy the CLI writes is the SelfSubjectReview username; none without one"
	if len(forged) > 0 {
		detail = strings.Join(forged, "; ")
	}
	expectBlocked(t, "CLI-07", len(forged) == 0, detail)
}

// CLI-08: a forged apply ticket, written by anyone with remediationactions/status
// (the P1 gap), is refused before the CLI touches the Deployment: a wrong
// specHash (the plan was edited after the operator cleared it), an expired
// ticket, a ticket missing specHash, approvedBy or expiresAt, and a ticket
// whose approvedBy is somebody else.
func TestAdversarialCLI08ForgedTicketIsRefusedBeforeApply(t *testing.T) {
	forgeries := map[string]func(ticket map[string]any){
		"wrong specHash": func(m map[string]any) { m["specHash"] = strings.Repeat("0", 64) },
		"expired": func(m map[string]any) {
			m["issuedAt"] = fixtureApprovedAt().Add(time.Second).UTC().Format(time.RFC3339Nano)
			m["expiresAt"] = time.Now().Add(-time.Second).UTC().Format(time.RFC3339)
		},
		"no specHash":      func(m map[string]any) { delete(m, "specHash") },
		"no approvedBy":    func(m map[string]any) { delete(m, "approvedBy") },
		"no expiresAt":     func(m map[string]any) { delete(m, "expiresAt") },
		"another approver": func(m map[string]any) { m["approvedBy"] = fixtureOtherUser },
	}

	// Positive control: the same flow with a good ticket does patch, so the refusals
	// below are the ticket checks and not a broken fixture.
	good := baseResponses()
	good.deployment = withIdentity(t, good.deployment)
	good.remAfterApprove = withTicket(t, operatorRemediationFixture, "Applying")
	if !strings.Contains(runApprove(t, good).patchLog, "patch deployment") {
		t.Fatal("precondition: a good ticket must be applied")
	}

	var applied []string
	for name, forge := range forgeries {
		r := baseResponses()
		r.deployment = withIdentity(t, r.deployment)
		r.remAfterApprove = withTicket(t, operatorRemediationFixture, "Applying", forge)
		if strings.Contains(runApprove(t, r).patchLog, "patch deployment") {
			applied = append(applied, name)
		}
	}
	sort.Strings(applied)

	detail := "every forged ticket was refused before the Deployment was patched"
	if len(applied) > 0 {
		detail = "the Deployment was patched under: " + strings.Join(applied, ", ")
	}
	expectBlocked(t, "CLI-08", len(applied) == 0, detail)
}

// CLI-09: `dorgu remediation rollback` is made to write more than a request: a restore
// value the CLI chose (here the hostile action's own spec.prePatchState, a 64Gi limit), a
// requester that is not the authenticated user, or a request at all when the cluster cannot
// say who the user is or the user is a service account. A rollback request carries the name
// of the action, a reason and the SelfSubjectReview username, and nothing else: the operator
// derives the restore from what it recorded.
func TestAdversarialCLI09RollbackWritesOnlyTheRequest(t *testing.T) {
	hostile := strings.Replace(rollbackTargetFixture(), `"action": {`,
		`"action": {"prePatchState": {"spec":{"resources":{"limits":{"memory":"64Gi"}}}},`, 1)
	hostile = strings.Replace(hostile, `"personaRef"`, `"requestedBy": "victim@example.com", "personaRef"`, 1)

	ok := runRollback(t, rollbackWorld{target: hostile, whoami: whoamiJSON(fixtureUser)}, rollbackTargetRA, "-n", "production")
	var body struct {
		Spec map[string]json.RawMessage `json:"spec"`
	}
	require.NoError(t, json.Unmarshal([]byte(ok.create), &body), ok.create)
	keys := make([]string, 0, len(body.Spec))
	for k := range body.Spec {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var proposer struct{ Username string }
	_ = json.Unmarshal(body.Spec["proposer"], &proposer)

	bot := runRollback(t, rollbackWorld{whoami: whoamiJSON("system:serviceaccount:ci:bot")}, rollbackTargetRA, "-n", "production")
	nobody := runRollback(t, rollbackWorld{whoami: `{"status":{"userInfo":{}}}`}, rollbackTargetRA, "-n", "production")

	blocked := strings.Join(keys, ",") == "idempotencyKey,proposer,rationale,rollbackOf,source,target" &&
		proposer.Username == fixtureUser && !strings.Contains(ok.create, "64Gi") &&
		!strings.Contains(ok.create, "victim@example.com") &&
		bot.create == "" && nobody.create == ""
	expectBlocked(t, "CLI-09", blocked, "spec keys "+strings.Join(keys, ",")+", proposer "+proposer.Username+
		", service account wrote "+bot.create+", unknown user wrote "+nobody.create)
}
