package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// C6 (P2-D10): `dorgu remediation rollback <action>` asks for a ticketed rollback. It
// writes a RemediationProposal with rollbackOf, as the authenticated user, waits for the
// operator's decision, and prints the resulting action (pending approval) or the reason it
// was refused. It never writes a value of its own: the operator derives the restore.

const (
	rollbackTargetRA = "fix-oom-api-server"
	rollbackNewRA    = "ra-api-server-1a2b3c"
	rollbackProposal = "rollback-fix-oom-api-server-x7k2p"
)

// rollbackWorld is a fake cluster for the rollback command.
type rollbackWorld struct {
	context string
	whoami  string
	// target is the action being rolled back; created is the action the operator makes.
	target  string
	created string
	// createError is what `kubectl create` of the proposal prints and fails with.
	createError string
	// proposals are the answers to successive `get remediationproposal`, the last one repeating.
	proposals []string
	// noProposalCRD makes the proposal resource unknown to the server.
	noProposalCRD bool
	// noTarget makes `get remediationaction` of the target answer not found.
	noTarget bool
}

type rollbackRun struct {
	err    error
	out    string
	calls  string
	create string // the proposal body kubectl was given
}

func rollbackConverted() string {
	return `{"metadata":{"name":"` + rollbackProposal + `","namespace":"production"},` +
		`"status":{"phase":"Converted","remediationActionRef":{"name":"` + rollbackNewRA + `"}}}`
}

func rollbackRejected(reason, message string) string {
	return rollbackRejectedAt("Intake", reason, message)
}

func rollbackRejectedAt(stage, reason, message string) string {
	return `{"metadata":{"name":"` + rollbackProposal + `","namespace":"production"},` +
		`"status":{"phase":"Rejected","rejection":{"stage":"` + stage + `","reason":"` + reason + `"},` +
		`"conditions":[{"type":"Converted","status":"False","reason":"` + reason + `","message":"` + message + `"}]}}`
}

func rollbackTargetFixture() string {
	return withPhase(operatorRemediationFixture, `"phase": "Completed"`)
}

func rollbackCreatedFixture() string {
	doc := strings.Replace(operatorRemediationFixture, `"name": "fix-oom-api-server"`, `"name": "`+rollbackNewRA+`"`, 1)
	doc = strings.Replace(doc, `"metadata": {`, `"metadata": {"annotations": {"dorgu.io/rollback-of": "`+rollbackTargetRA+`"},`, 1)
	return strings.Replace(doc, `"spec": {`, `"spec": {"provenance": {"proposal": "`+rollbackProposal+
		`", "proposer": {"username": "system:serviceaccount:dorgu-system:dorgu-operator"}, "source": "rollback", "requestedBy": "`+fixtureUser+`"},`, 1)
}

func (w rollbackWorld) install(t *testing.T) (dir string) {
	t.Helper()
	dir = t.TempDir()
	write := func(name, content string) string {
		p := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(p, []byte(content), 0o600))
		return p
	}
	if w.context == "" {
		w.context = "kind-dorgu-spike"
	}
	if w.whoami == "" {
		w.whoami = whoamiJSON(fixtureUser)
	}
	if w.target == "" {
		w.target = rollbackTargetFixture()
	}
	if w.created == "" {
		w.created = rollbackCreatedFixture()
	}
	if len(w.proposals) == 0 {
		w.proposals = []string{rollbackConverted()}
	}
	for i, doc := range w.proposals {
		write("proposal-"+string(rune('1'+i))+".json", doc)
	}
	script := "#!/bin/sh\n" +
		"echo \"$@\" >> " + filepath.Join(dir, "calls.log") + "\n" +
		"if [ \"$1\" = config ] && [ \"$2\" = current-context ]; then cat " + write("ctx", w.context) + "; exit 0; fi\n" +
		"case \"$*\" in\n" +
		"  *whoami*) cat " + write("whoami.json", w.whoami) + "; exit 0 ;;\n" +
		"  *create*)\n" +
		"    cat > " + filepath.Join(dir, "create.json") + "\n"
	if w.createError != "" {
		script += "    cat " + write("create-err", w.createError) + " >&2; exit 1 ;;\n"
	} else {
		script += "    echo '{\"metadata\":{\"name\":\"" + rollbackProposal + "\",\"namespace\":\"production\"}}'; exit 0 ;;\n"
	}
	script += "  *remediationproposal*)\n"
	if w.noProposalCRD {
		script += "    echo \"error: the server doesn't have a resource type \\\"remediationproposal\\\"\" >&2; exit 1 ;;\n"
	} else {
		script += "    n=0; [ -f " + filepath.Join(dir, "n") + " ] && n=$(cat " + filepath.Join(dir, "n") + ")\n" +
			"    n=$((n+1)); echo $n > " + filepath.Join(dir, "n") + "\n" +
			"    [ $n -gt " + string(rune('0'+len(w.proposals))) + " ] && n=" + string(rune('0'+len(w.proposals))) + "\n" +
			"    cat " + dir + "/proposal-$n.json; exit 0 ;;\n"
	}
	if w.noTarget {
		script += "  *remediationaction*) echo 'Error from server (NotFound): remediationactions.dorgu.io \"x\" not found' >&2; exit 1 ;;\n"
	}
	script += "  *remediationaction*" + rollbackNewRA + "*) cat " + write("created.json", w.created) + "; exit 0 ;;\n" +
		"  *remediationaction*) cat " + write("target.json", w.target) + "; exit 0 ;;\n" +
		"esac\nexit 0\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "kubectl"), []byte(script), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return dir
}

func runRollback(t *testing.T, w rollbackWorld, args ...string) rollbackRun {
	t.Helper()
	operatorVersionReader = func(context.Context, string) *string { return strp("v0.14.0") }
	t.Cleanup(func() { operatorVersionReader = readOperatorVersion })
	old, oldPoll := proposalWaitTimeout, proposalPollInterval
	proposalWaitTimeout, proposalPollInterval = 3*time.Second, 10*time.Millisecond
	t.Cleanup(func() { proposalWaitTimeout, proposalPollInterval = old, oldPoll })

	dir := w.install(t)
	cmd := newRemediationRollbackCmd()
	cmd.SetArgs(args)
	var out strings.Builder
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	err := cmd.Execute()

	read := func(name string) string {
		b, _ := os.ReadFile(filepath.Join(dir, name))
		return string(b)
	}
	return rollbackRun{err: err, out: out.String(), calls: read("calls.log"), create: read("create.json")}
}

func TestRollback_CreatesTheProposalAsTheAuthenticatedUserAndPrintsThePendingAction(t *testing.T) {
	run := runRollback(t, rollbackWorld{}, rollbackTargetRA, "-n", "production", "--reason", "the node ran out of memory")

	require.NoError(t, run.err, run.out)
	var body struct {
		APIVersion string `json:"apiVersion"`
		Kind       string `json:"kind"`
		Metadata   struct {
			GenerateName string `json:"generateName"`
			Namespace    string `json:"namespace"`
		} `json:"metadata"`
		Spec struct {
			Proposer       struct{ Username string } `json:"proposer"`
			Source         string                    `json:"source"`
			IdempotencyKey string                    `json:"idempotencyKey"`
			RollbackOf     struct{ Name string }     `json:"rollbackOf"`
			Target         struct{ Kind, Namespace, Name string }
			Rationale      string
			Steps          []any `json:"steps"`
			IncidentRef    any   `json:"incidentRef"`
			Agent          any   `json:"agent"`
		} `json:"spec"`
	}
	require.NoError(t, json.Unmarshal([]byte(run.create), &body), run.create)
	assert.Equal(t, "dorgu.io/v1alpha1", body.APIVersion)
	assert.Equal(t, "RemediationProposal", body.Kind)
	assert.Equal(t, "production", body.Metadata.Namespace)
	assert.True(t, strings.HasPrefix(body.Metadata.GenerateName, "rollback-"+rollbackTargetRA+"-"), body.Metadata.GenerateName)
	assert.Equal(t, fixtureUser, body.Spec.Proposer.Username, "the proposer is whoever the cluster says the user is")
	assert.Equal(t, "rollback", body.Spec.Source)
	assert.Equal(t, rollbackTargetRA, body.Spec.RollbackOf.Name)
	assert.Equal(t, "api-server", body.Spec.Target.Name)
	assert.Equal(t, "production", body.Spec.Target.Namespace)
	assert.Equal(t, "ApplicationPersona", body.Spec.Target.Kind)
	assert.Contains(t, body.Spec.Rationale, "the node ran out of memory")
	assert.NotEmpty(t, body.Spec.IdempotencyKey)
	assert.Empty(t, body.Spec.Steps, "the CLI never writes steps: the operator derives them")
	assert.Nil(t, body.Spec.IncidentRef)
	assert.Nil(t, body.Spec.Agent)

	assert.Contains(t, run.out, "Requesting a rollback of production/"+rollbackTargetRA+" as "+fixtureUser)
	assert.Contains(t, run.out, rollbackNewRA, "the resulting action is named")
	assert.Contains(t, run.out, "Pending", "and shown as waiting for approval")
	assert.Contains(t, run.out, "dorgu remediation approve "+rollbackNewRA+" -n production",
		"with the normal approve flow")
	assert.Contains(t, run.out, "you may approve it yourself")
	assert.NotContains(t, run.out, "kubectl set resources")
}

func TestRollback_WaitsForTheOperatorsDecision(t *testing.T) {
	received := `{"metadata":{"name":"` + rollbackProposal + `"},"status":{"phase":"Received"}}`
	run := runRollback(t, rollbackWorld{proposals: []string{received, received, rollbackConverted()}},
		rollbackTargetRA, "-n", "production")

	require.NoError(t, run.err, run.out)
	assert.Contains(t, run.out, rollbackNewRA)
	assert.GreaterOrEqual(t, strings.Count(run.calls, "get remediationproposal"), 3)
}

func TestRollback_ARefusalIsExplainedAndExitsDeclined(t *testing.T) {
	cases := map[string]string{
		"RollbackNotApplied":       "Nothing was applied",
		"RollbackOfRollback":       "itself a rollback",
		"RollbackNoRecordedState":  "did not record",
		"RollbackWorkloadDrifted":  "changed since",
		"RollbackNothingToRestore": "already has the values",
		"RollbackAlreadyOpen":      "already open",
		"RollbackTargetNotFound":   "does not exist",
		"RollbackNotHuman":         "person, not a service account",
		"RollbackTargetMismatch":   "different application",
		"RollbackWouldBePartial":   "all of it or none of it",
	}
	for reason, want := range cases {
		t.Run(reason, func(t *testing.T) {
			run := runRollback(t, rollbackWorld{proposals: []string{rollbackRejected(reason, "operator text about names")}},
				rollbackTargetRA, "-n", "production")

			require.Error(t, run.err)
			assert.Equal(t, ExitDeclined, ExitCode(run.err), "a refusal is a decision, not a breakage")
			assert.Contains(t, run.out, want)
			assert.Contains(t, run.out, "Nothing was changed")
			assert.NotContains(t, run.out, "dorgu remediation approve")
		})
	}
}

func TestRollback_AGateRefusalNamesTheStageAndReason(t *testing.T) {
	rejected := `{"metadata":{"name":"` + rollbackProposal + `"},"status":{"phase":"Rejected",` +
		`"rejection":{"stage":"Guardrails","reason":"concurrent-remediation"},` +
		`"conditions":[{"type":"Converted","status":"False","reason":"ConcurrentRemediation","message":"another remediation is active"}]}}`
	run := runRollback(t, rollbackWorld{proposals: []string{rejected}}, rollbackTargetRA, "-n", "production")

	require.Error(t, run.err)
	assert.Equal(t, ExitDeclined, ExitCode(run.err))
	assert.Contains(t, run.out, "safety gate refused")
	assert.Contains(t, run.out, "Guardrails")
	assert.Contains(t, run.out, "concurrent-remediation")
	assert.Contains(t, run.out, "another remediation is active")
}

func TestRollback_AdmissionDenialsAreFriendlyAndExitDeclined(t *testing.T) {
	cases := map[string]string{
		"ServiceAccountCannotApprove": "person, not a service account",
		"MissingApproveVerb":          "approve permission on remediations in production",
		"ProposerMismatch":            "proposer other than the account",
	}
	for token, want := range cases {
		t.Run(token, func(t *testing.T) {
			raw := `Error from server (Forbidden): error when creating "STDIN": remediationproposals.dorgu.io "x" is forbidden: ` +
				`ValidatingAdmissionPolicy 'dorgu-proposal-identity' with binding 'dorgu-proposal-identity' denied request: ` +
				`dorgu: ` + token + `: the policy author's text`
			run := runRollback(t, rollbackWorld{createError: raw}, rollbackTargetRA, "-n", "production")

			require.Error(t, run.err)
			assert.Equal(t, ExitDeclined, ExitCode(run.err))
			assert.Contains(t, run.out, want)
			assert.NotContains(t, run.out, "the policy author's text", "only the token is read")
			assert.NotContains(t, run.calls, "get remediationproposal", "nothing to wait for")
		})
	}
}

func TestRollback_AServiceAccountIsRefusedBeforeAnythingIsWritten(t *testing.T) {
	run := runRollback(t, rollbackWorld{whoami: whoamiJSON("system:serviceaccount:ci:bot")}, rollbackTargetRA, "-n", "production")

	require.Error(t, run.err)
	assert.Equal(t, ExitDeclined, ExitCode(run.err))
	assert.Contains(t, run.out, "person, not a service account")
	assert.Empty(t, run.create, "no proposal was written")
}

func TestRollback_NoIdentityMeansNothingIsWritten(t *testing.T) {
	run := runRollback(t, rollbackWorld{whoami: `{"status":{"userInfo":{}}}`}, rollbackTargetRA, "-n", "production")

	require.Error(t, run.err)
	assert.Contains(t, run.out, "Cannot tell who you are")
	assert.Empty(t, run.create)
}

func TestRollback_ARejectedContextIsRefusedLocally(t *testing.T) {
	run := runRollback(t, rollbackWorld{context: "acme-prod-cluster"}, rollbackTargetRA, "-n", "production")

	require.Error(t, run.err)
	assert.Contains(t, run.out, "refusing to roll back")
	assert.Empty(t, run.create)
}

func TestRollback_NeedsAnOperatorThatKnowsRollback(t *testing.T) {
	t.Run("an operator older than v0.14", func(t *testing.T) {
		operatorVersionReader = func(context.Context, string) *string { return strp("v0.13.1") }
		t.Cleanup(func() { operatorVersionReader = readOperatorVersion })
		dir := rollbackWorld{}.install(t)
		cmd := newRemediationRollbackCmd()
		cmd.SetArgs([]string{rollbackTargetRA, "-n", "production"})
		var out strings.Builder
		cmd.SetOut(&out)
		cmd.SetErr(&out)

		require.Error(t, cmd.Execute())
		assert.Contains(t, out.String(), "operator v0.14")
		_, err := os.Stat(filepath.Join(dir, "create.json"))
		assert.True(t, os.IsNotExist(err), "nothing was created")
	})
	t.Run("a cluster without the proposal resource", func(t *testing.T) {
		run := runRollback(t, rollbackWorld{noProposalCRD: true, createError: `error: the server doesn't have a resource type "remediationproposals"`},
			rollbackTargetRA, "-n", "production")

		require.Error(t, run.err)
		assert.Contains(t, run.out, "RemediationProposal")
		assert.Contains(t, run.out, "operator")
	})
}

func TestRollback_ATimeoutSaysWhatToLookAt(t *testing.T) {
	received := `{"metadata":{"name":"` + rollbackProposal + `"},"status":{"phase":"Received"}}`
	run := runRollback(t, rollbackWorld{proposals: []string{received}}, rollbackTargetRA, "-n", "production")

	require.Error(t, run.err)
	assert.Contains(t, run.out, "did not decide")
	assert.Contains(t, run.out, "kubectl get remediationproposal "+rollbackProposal+" -n production",
		"the proposal is named so it can be looked at")
	assert.Contains(t, run.out, "ProposalIntakeReady")
}

func TestRollback_RequiresANameAndANamespace(t *testing.T) {
	run := runRollback(t, rollbackWorld{}, "-n", "production")
	require.Error(t, run.err)
	run = runRollback(t, rollbackWorld{}, rollbackTargetRA)
	require.Error(t, run.err)
	assert.Contains(t, run.err.Error(), "--namespace")
	assert.Empty(t, run.create)
}

func TestRollback_AnUnknownActionIsReportedBeforeAProposalIsWritten(t *testing.T) {
	run := runRollback(t, rollbackWorld{noTarget: true}, "does-not-exist", "-n", "production")

	require.ErrorIs(t, run.err, errSilent, "fetchRemediation has already said the action was not found")
	assert.Empty(t, run.create, "no proposal was written")
	assert.NotContains(t, run.calls, "create")
}

func TestRollback_IsListedUnderRemediation(t *testing.T) {
	names := []string{}
	for _, c := range newRemediationCmd().Commands() {
		names = append(names, c.Name())
	}
	assert.Contains(t, names, "rollback")
}

func TestRollback_TheDiffNamesWhereARollbackCameFromAndWhoAskedForIt(t *testing.T) {
	var rb remediationFull
	require.NoError(t, json.Unmarshal([]byte(rollbackCreatedFixture()), &rb))
	var out strings.Builder

	printRemediationDiff(&out, &rb)

	assert.Contains(t, out.String(), "Origin:     Rollback of "+rollbackTargetRA+", requested by "+fixtureUser)
}

func TestRollback_AnOrdinaryRemediationHasNoOriginLine(t *testing.T) {
	var r remediationFull
	require.NoError(t, json.Unmarshal([]byte(operatorRemediationFixture), &r))
	var out strings.Builder

	printRemediationDiff(&out, &r)

	assert.NotContains(t, out.String(), "Origin:")
}

func TestRollback_TheOperatorsWordsCannotMoveTheCursor(t *testing.T) {
	hostile := "evil\\u001b[2Jtext\\u202eend"
	run := runRollback(t, rollbackWorld{proposals: []string{rollbackRejected("RollbackWorkloadDrifted", hostile)}},
		rollbackTargetRA, "-n", "production")

	require.Error(t, run.err)
	assert.NotContains(t, run.out, "\x1b")
	assert.NotContains(t, run.out, "\u202e")
	assert.Contains(t, run.out, "evil[2Jtextend", "the characters that make it a command are gone; the rest is plain text")
}

func TestRollback_ReasonAndNameAreBounded(t *testing.T) {
	t.Run("a reason that is too long is refused before anything is written", func(t *testing.T) {
		run := runRollback(t, rollbackWorld{}, rollbackTargetRA, "-n", "production", "--reason", strings.Repeat("x", 4000))
		require.Error(t, run.err)
		assert.Contains(t, run.err.Error(), "--reason is too long")
		assert.Empty(t, run.create)
	})
	t.Run("a long action name cannot make a name the API server refuses", func(t *testing.T) {
		assert.LessOrEqual(t, len(rollbackGenerateName(strings.Repeat("a", 250))), 70)
		assert.Equal(t, "rollback-fix-", rollbackGenerateName("fix-" + strings.Repeat("a", 100))[:13])
	})
	t.Run("the default reason is sent when none is given", func(t *testing.T) {
		run := runRollback(t, rollbackWorld{}, rollbackTargetRA, "-n", "production")
		require.NoError(t, run.err)
		assert.Contains(t, run.create, "dorgu remediation rollback")
	})
	t.Run("every request has its own idempotency key", func(t *testing.T) {
		a, err := randomToken()
		require.NoError(t, err)
		b, err := randomToken()
		require.NoError(t, err)
		assert.NotEqual(t, a, b)
	})
}

func TestRollback_OtherCreateFailures(t *testing.T) {
	t.Run("an RBAC refusal is declined, not a breakage", func(t *testing.T) {
		run := runRollback(t, rollbackWorld{createError: `Error from server (Forbidden): remediationproposals.dorgu.io is forbidden: User "bob" cannot create resource`},
			rollbackTargetRA, "-n", "production")
		require.Error(t, run.err)
		assert.Equal(t, ExitDeclined, ExitCode(run.err))
		assert.Contains(t, run.out, "dorgu-approver")
	})
	t.Run("anything else is an error that says what kubectl said", func(t *testing.T) {
		run := runRollback(t, rollbackWorld{createError: "Unable to connect to the server"}, rollbackTargetRA, "-n", "production")
		require.Error(t, run.err)
		assert.Contains(t, run.err.Error(), "Unable to connect to the server")
		assert.NotEqual(t, ExitDeclined, ExitCode(run.err))
	})
}

// C6 review F-7a: a refusal that is not the safety gate's must not be called the gate's, and
// one that is temporary says so.
func TestRollback_AnIntakeRefusalWithoutASentenceIsNotCalledTheGates(t *testing.T) {
	for _, reason := range []string{"RateLimited", "ProposerInvalid", "DuplicateIdempotencyKey"} {
		t.Run(reason, func(t *testing.T) {
			run := runRollback(t, rollbackWorld{proposals: []string{rollbackRejected(reason, "operator text")}},
				rollbackTargetRA, "-n", "production")

			require.Error(t, run.err)
			assert.Equal(t, ExitDeclined, ExitCode(run.err))
			assert.NotContains(t, run.out, "safety gate", "the intake refused this one, not the gate")
			assert.Contains(t, run.out, "intake")
			assert.Contains(t, run.out, reason)
			assert.Contains(t, run.out, "operator text")
			assert.Contains(t, run.out, "Nothing was changed")
		})
	}
}

func TestRollback_AnOperatorFromBeforeRollbackSaysToUpgradeNotThatTheGateRefused(t *testing.T) {
	// A development build from before ticketed rollback answers NotYetSupported at intake.
	run := runRollback(t, rollbackWorld{proposals: []string{rollbackRejected("NotYetSupported", "rollbackOf is reserved")}},
		rollbackTargetRA, "-n", "production")

	require.Error(t, run.err)
	assert.Equal(t, ExitDeclined, ExitCode(run.err))
	assert.NotContains(t, run.out, "safety gate")
	assert.Contains(t, run.out, "too old for rollback")
	assert.Contains(t, run.out, "upgrade the operator")
}

func TestRollback_ARefusalThatClearsOnItsOwnSaysItIsTemporary(t *testing.T) {
	for _, guardrail := range []string{"failed-cooldown", "rejection-cooldown", "rate-limit", "concurrent", "active-remediation"} {
		t.Run(guardrail, func(t *testing.T) {
			run := runRollback(t, rollbackWorld{proposals: []string{rollbackRejectedAt("Guardrails", guardrail, "operator text")}},
				rollbackTargetRA, "-n", "production")

			require.Error(t, run.err)
			assert.Equal(t, ExitDeclined, ExitCode(run.err))
			assert.Contains(t, run.out, "safety gate refused")
			assert.Contains(t, run.out, "temporary")
			assert.Contains(t, run.out, "ask again")
		})
	}
}

func TestRollback_APermanentGateRefusalDoesNotPromiseItWillClear(t *testing.T) {
	for _, guardrail := range []string{"blast-radius", "live-workload", "self-healing-mode"} {
		t.Run(guardrail, func(t *testing.T) {
			stage := "Guardrails"
			if guardrail == "self-healing-mode" {
				stage = "ModePolicy"
			}
			run := runRollback(t, rollbackWorld{proposals: []string{rollbackRejectedAt(stage, guardrail, "operator text")}},
				rollbackTargetRA, "-n", "production")

			require.Error(t, run.err)
			assert.NotContains(t, run.out, "temporary")
		})
	}
}

// F-7b: a forbidden create names the role, and when the role is bound, the chart.
func TestRollback_AForbiddenCreateAlsoPointsAtAnOlderChart(t *testing.T) {
	run := runRollback(t, rollbackWorld{createError: `Error from server (Forbidden): remediationproposals.dorgu.io is forbidden: ` +
		`User "alice@example.com" cannot create resource "remediationproposals" in API group "dorgu.io" in the namespace "production"`},
		rollbackTargetRA, "-n", "production")

	require.Error(t, run.err)
	assert.Equal(t, ExitDeclined, ExitCode(run.err))
	assert.Contains(t, run.out, "dorgu-approver role in production")
	assert.Contains(t, run.out, "already have", "it speaks to someone who is bound to the role")
	assert.Contains(t, run.out, "chart", "the role gained create on proposals in the same release as rollback")
	assert.Contains(t, run.out, "upgrade")
}
