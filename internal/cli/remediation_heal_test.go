package cli

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- extractResourceChange ---

func TestExtractResourceChange(t *testing.T) {
	tests := []struct {
		name         string
		patch        string
		wantLimits   map[string]string
		wantRequests map[string]string
		wantNil      bool
	}{
		{
			name:       "spec-wrapped limits (operator shape)",
			patch:      `{"spec":{"resources":{"limits":{"memory":"512Mi"}}}}`,
			wantLimits: map[string]string{"memory": "512Mi"},
		},
		{
			name:       "spec-relative limits (rule-based shape)",
			patch:      `{"resources":{"limits":{"memory":"128Mi"}}}`,
			wantLimits: map[string]string{"memory": "128Mi"},
		},
		{
			name:         "limits and requests, cpu and memory",
			patch:        `{"spec":{"resources":{"limits":{"cpu":"500m","memory":"512Mi"},"requests":{"cpu":"250m","memory":"256Mi"}}}}`,
			wantLimits:   map[string]string{"cpu": "500m", "memory": "512Mi"},
			wantRequests: map[string]string{"cpu": "250m", "memory": "256Mi"},
		},
		{
			name:    "replicas/scale patch is not a resource change",
			patch:   `{"spec":{"replicas":3}}`,
			wantNil: true,
		},
		{
			name:    "empty patch",
			patch:   ``,
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rc, err := extractResourceChange(json.RawMessage(tt.patch))
			require.NoError(t, err)
			if tt.wantNil {
				assert.True(t, rc.isEmpty())
				return
			}
			require.False(t, rc.isEmpty())
			assert.Equal(t, tt.wantLimits, rc.Limits)
			assert.Equal(t, tt.wantRequests, rc.Requests)
		})
	}
}

func TestExtractResourceChangeInvalidJSON(t *testing.T) {
	_, err := extractResourceChange(json.RawMessage(`not-json`))
	assert.Error(t, err)
}

// --- buildDeploymentResourcePatch (translation) ---

func TestBuildDeploymentResourcePatch(t *testing.T) {
	tests := []struct {
		name      string
		container string
		change    *healResourceChange
		want      string
	}{
		{
			name:      "memory limit only",
			container: "api-server",
			change:    &healResourceChange{Limits: map[string]string{"memory": "512Mi"}},
			want:      `{"spec":{"template":{"spec":{"containers":[{"name":"api-server","resources":{"limits":{"memory":"512Mi"}}}]}}}}`,
		},
		{
			name:      "limits and requests",
			container: "app",
			change: &healResourceChange{
				Limits:   map[string]string{"cpu": "500m", "memory": "512Mi"},
				Requests: map[string]string{"memory": "256Mi"},
			},
			want: `{"spec":{"template":{"spec":{"containers":[{"name":"app","resources":{"limits":{"cpu":"500m","memory":"512Mi"},"requests":{"memory":"256Mi"}}}]}}}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := buildDeploymentResourcePatch(tt.container, tt.change)
			require.NoError(t, err)
			assert.JSONEq(t, tt.want, got)
			// Deterministic (sorted keys) — exact string match too.
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestBuildDeploymentResourcePatchErrors(t *testing.T) {
	_, err := buildDeploymentResourcePatch("app", &healResourceChange{})
	assert.Error(t, err, "empty change must error")

	_, err = buildDeploymentResourcePatch("", &healResourceChange{Limits: map[string]string{"memory": "1Gi"}})
	assert.Error(t, err, "missing container must error")
}

// --- buildHealPlan ---

func TestBuildHealPlanOrderedSteps(t *testing.T) {
	var r remediationFull
	require.NoError(t, json.Unmarshal([]byte(operatorRemediationFixture), &r))

	plan, err := buildHealPlan(&r)
	require.NoError(t, err)

	// persona-update resource step is auto-applied...
	require.False(t, plan.Change.isEmpty())
	assert.Equal(t, "512Mi", plan.Change.Limits["memory"])

	// ...restart + manual steps are advisory (not executed).
	require.Len(t, plan.Advisory, 2)
	types := []string{plan.Advisory[0].Type, plan.Advisory[1].Type}
	assert.Contains(t, types, "restart")
	assert.Contains(t, types, "manual")
}

func TestBuildHealPlanLegacyAction(t *testing.T) {
	var r remediationFull
	require.NoError(t, json.Unmarshal([]byte(legacyRemediationFixture), &r))

	plan, err := buildHealPlan(&r)
	require.NoError(t, err)
	require.False(t, plan.Change.isEmpty())
	assert.Equal(t, "512Mi", plan.Change.Limits["memory"])
	assert.Empty(t, plan.Advisory)
}

func TestBuildHealPlanNonResourcePersonaUpdateIsAdvisory(t *testing.T) {
	var r remediationFull
	r.Spec.Steps = []remediationStep{
		{Order: 1, ID: "s1", Type: "persona-update", Description: "scale to 3",
			AutoExecutable: true, Patch: json.RawMessage(`{"spec":{"replicas":3}}`)},
	}
	plan, err := buildHealPlan(&r)
	require.NoError(t, err)
	assert.True(t, plan.Change.isEmpty(), "replicas change is not auto-applied")
	require.Len(t, plan.Advisory, 1)
	assert.Equal(t, "persona-update", plan.Advisory[0].Type)
}

// --- selectDeployment (workload discovery) ---

// selectDeployment is covered by workload_match_test.go.

// --- selectContainer ---

func TestSelectContainer(t *testing.T) {
	tests := []struct {
		name       string
		containers []string
		appName    string
		flag       string
		want       string
		wantErr    string
	}{
		{name: "single container", containers: []string{"app"}, want: "app"},
		{name: "multi picks app-named", containers: []string{"sidecar", "api"}, appName: "api", want: "api"},
		{name: "multi ambiguous", containers: []string{"a", "b"}, appName: "api", wantErr: "--container"},
		{name: "explicit flag valid", containers: []string{"a", "b"}, flag: "b", want: "b"},
		{name: "explicit flag invalid", containers: []string{"a", "b"}, flag: "zzz", wantErr: "not found"},
		{name: "no containers", containers: nil, wantErr: "no containers"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := selectContainer(tt.containers, tt.appName, tt.flag)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// --- guardKubeContext ---

func TestGuardKubeContext(t *testing.T) {
	tests := []struct {
		name    string
		ctx     string
		wantErr bool
	}{
		{name: "safe kind cluster", ctx: "kind-dorgu-spike"},
		{name: "safe eks", ctx: "arn:aws:eks:us-east-1:123:cluster/dorgu-spike"},
		{name: "prod substring refused", ctx: "my-prod-cluster", wantErr: true},
		{name: "vox-prod refused", ctx: "vox-prod-synthiolabs", wantErr: true},
		{name: "empty refused", ctx: "", wantErr: true},
		{name: "uppercase PROD refused", ctx: "PROD-us-east", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := guardKubeContext(tt.ctx)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// --- parseDeployments ---

func TestParseDeploymentList(t *testing.T) {
	raw := `{"items":[{"metadata":{"name":"api-server"},"spec":{"template":{"spec":{"containers":[{"name":"api-server"},{"name":"sidecar"}]}}}}]}`
	ds, err := parseDeploymentList([]byte(raw))
	require.NoError(t, err)
	require.Len(t, ds, 1)
	assert.Equal(t, "api-server", ds[0].Name)
	assert.Equal(t, []string{"api-server", "sidecar"}, ds[0].Containers)
}

// --- full flow with a dispatching fake kubectl ---

// fakeKubectlResponses configures the dispatching fake kubectl.
type fakeKubectlResponses struct {
	context    string // `config current-context`
	rem        string // get remediationaction
	persona    string // get applicationpersona
	deployment string // get deployment (list or object)

	// failDeploymentPatch makes `kubectl patch deployment` exit non-zero, the
	// way an RBAC denial or an admission webhook rejection would.
	failDeploymentPatch bool

	// deploymentAfterPatch, when set, is served by `get deployment` once the
	// resource patch has landed, and deploymentAfterStrip once the
	// managedFields patch has. They exist so the footprint strip can be driven
	// through its real sequence: patch, read the field managers, remove
	// Dorgu's, read back to confirm.
	deploymentAfterPatch string
	deploymentAfterStrip string

	// failStripPatch makes the managedFields patch fail with a Conflict, the
	// way a concurrent write would.
	failStripPatch bool
}

// writeFakeKubectlDispatch installs a fake kubectl on PATH that dispatches by
// resource kind, and logs every `patch` invocation to the returned file path.
// Cannot be used with t.Parallel.
func writeFakeKubectlDispatch(t *testing.T, r fakeKubectlResponses) (patchLog string) {
	t.Helper()
	dir := t.TempDir()

	write := func(base, content string) string {
		p := filepath.Join(dir, base)
		require.NoError(t, os.WriteFile(p, []byte(content), 0o600))
		return p
	}
	ctxFile := write("context.txt", r.context)
	remFile := write("rem.json", r.rem)
	personaFile := write("persona.json", r.persona)
	deployFile := write("deploy.json", r.deployment)
	afterPatchFile := write("deploy-after-patch.json", r.deploymentAfterPatch)
	afterStripFile := write("deploy-after-strip.json", r.deploymentAfterStrip)
	patchLog = filepath.Join(dir, "patch.log")
	patchFails := "0"
	if r.failDeploymentPatch {
		patchFails = "1"
	}
	stripFails := "0"
	if r.failStripPatch {
		stripFails = "1"
	}

	callLog := filepath.Join(dir, "calls.log")

	script := "#!/bin/sh\n" +
		"echo \"$@\" >> " + callLog + "\n" +
		"if [ \"$1\" = config ] && [ \"$2\" = current-context ]; then cat " + ctxFile + "; exit 0; fi\n" +
		"mode=\"\"; kind=\"\"\n" +
		"for a in \"$@\"; do\n" +
		"  case \"$a\" in\n" +
		"    get) mode=get ;;\n" +
		"    patch) mode=patch ;;\n" +
		"    remediationaction|remediationactions) [ -z \"$kind\" ] && kind=rem ;;\n" +
		"    applicationpersona|applicationpersonas) [ -z \"$kind\" ] && kind=persona ;;\n" +
		"    deployment|deployments|deploy) [ -z \"$kind\" ] && kind=deploy ;;\n" +
		"  esac\n" +
		"done\n" +
		"if [ \"$mode\" = patch ]; then echo \"$@\" >> " + patchLog + "\n" +
		"  case \"$*\" in\n" +
		"    *managedFields*)\n" +
		"      touch " + filepath.Join(dir, "stripped") + "\n" +
		"      if [ " + stripFails + " = 1 ]; then echo 'Error from server (Conflict): the object has been modified' >&2; exit 1; fi\n" +
		"      echo patched; exit 0 ;;\n" +
		"    *) touch " + filepath.Join(dir, "patched") + " ;;\n" +
		"  esac\n" +
		"  if [ \"$kind\" = deploy ] && [ " + patchFails + " = 1 ]; then echo 'Error from server (Forbidden): deployments.apps is forbidden' >&2; exit 1; fi\n" +
		"  echo patched; exit 0; fi\n" +
		"if [ \"$mode\" = get ]; then\n" +
		"  case \"$kind\" in\n" +
		"    rem) cat " + remFile + " ;;\n" +
		"    persona) cat " + personaFile + " ;;\n" +
		"    deploy)\n" +
		"      if [ -f " + filepath.Join(dir, "stripped") + " ] && [ -s " + afterStripFile + " ]; then cat " + afterStripFile + "\n" +
		"      elif [ -f " + filepath.Join(dir, "patched") + " ] && [ -s " + afterPatchFile + " ]; then cat " + afterPatchFile + "\n" +
		"      else cat " + deployFile + "; fi ;;\n" +
		"  esac\n" +
		"  exit 0\n" +
		"fi\n" +
		"exit 0\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "kubectl"), []byte(script), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return patchLog
}

const personaFixture = `{"apiVersion":"dorgu.io/v1","kind":"ApplicationPersona","metadata":{"name":"api-server","namespace":"production"},"spec":{"name":"api-server"}}`

const deploymentListFixture = `{"items":[{"metadata":{"name":"api-server"},"spec":{"template":{"spec":{"containers":[{"name":"api-server"}]}}}}]}`

// readCallLog returns every kubectl invocation the fake saw. It lives beside
// the patch log in the same temp dir.
func readCallLog(t *testing.T, patchLog string) string {
	t.Helper()
	return readPatchLog(t, filepath.Join(filepath.Dir(patchLog), "calls.log"))
}

func readPatchLog(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return ""
	}
	require.NoError(t, err)
	return string(b)
}

func TestRunRemediationApproveHealsWorkload(t *testing.T) {
	patchLog := writeFakeKubectlDispatch(t, fakeKubectlResponses{
		context:    "kind-dorgu-spike",
		rem:        operatorRemediationFixture,
		persona:    personaFixture,
		deployment: deploymentListFixture,
	})

	cmd := newRemediationApproveCmd()
	cmd.SetArgs([]string{"fix-oom-api-server", "-n", "production", "--yes"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	require.NoError(t, cmd.Execute())

	log := readPatchLog(t, patchLog)
	// approve still patches the RemediationAction status.
	assert.Contains(t, log, "patch remediationaction fix-oom-api-server")
	assert.Contains(t, log, "--subresource status")
	// heal patches the Deployment with the translated resource change.
	assert.Contains(t, log, "patch deployment api-server")
	assert.Contains(t, log, "--type strategic")
	assert.Contains(t, log, `"memory":"512Mi"`)
}

func TestRunRemediationApproveNoHealSkipsWorkload(t *testing.T) {
	patchLog := writeFakeKubectlDispatch(t, fakeKubectlResponses{
		context:    "kind-dorgu-spike",
		rem:        operatorRemediationFixture,
		persona:    personaFixture,
		deployment: deploymentListFixture,
	})

	cmd := newRemediationApproveCmd()
	cmd.SetArgs([]string{"fix-oom-api-server", "-n", "production", "--no-heal"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	require.NoError(t, cmd.Execute())

	log := readPatchLog(t, patchLog)
	// status still patched...
	assert.Contains(t, log, "patch remediationaction fix-oom-api-server")
	// ...but the Deployment is NOT touched.
	assert.NotContains(t, log, "patch deployment")
}

func TestRunRemediationApproveHealRefusesProd(t *testing.T) {
	patchLog := writeFakeKubectlDispatch(t, fakeKubectlResponses{
		context:    "vox-prod-synthiolabs",
		rem:        operatorRemediationFixture,
		persona:    personaFixture,
		deployment: deploymentListFixture,
	})

	cmd := newRemediationApproveCmd()
	cmd.SetArgs([]string{"fix-oom-api-server", "-n", "production", "--yes"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	// The prod guard runs in preflight, so nothing is approved either: an
	// Approved action the CLI will not heal is exactly the divergence F-01
	// described.
	err := cmd.Execute()
	assert.ErrorIs(t, err, errSilent)

	log := readPatchLog(t, patchLog)
	assert.NotContains(t, log, "patch remediationaction", "nothing may be approved when the heal is refused")
	assert.NotContains(t, log, "patch deployment", "prod workload must not be patched")
}

func TestRunRemediationHealCommand(t *testing.T) {
	approved := strings.Replace(operatorRemediationFixture, `"phase": "Pending"`, `"phase": "Approved"`, 1)
	patchLog := writeFakeKubectlDispatch(t, fakeKubectlResponses{
		context:    "kind-dorgu-spike",
		rem:        approved,
		persona:    personaFixture,
		deployment: deploymentListFixture,
	})

	cmd := newRemediationHealCmd()
	cmd.SetArgs([]string{"fix-oom-api-server", "-n", "production", "--yes"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	require.NoError(t, cmd.Execute())

	log := readPatchLog(t, patchLog)
	assert.Contains(t, log, "patch deployment api-server")
	assert.Contains(t, log, `"memory":"512Mi"`)
	// heal alone must NOT patch the RemediationAction status.
	assert.NotContains(t, log, "patch remediationaction")
}

const deploymentObjectFixture = `{"metadata":{"name":"custom-api"},"spec":{"template":{"spec":{"containers":[{"name":"main"},{"name":"sidecar"}]}}}}`

// --workload and --container still steer the heal, as long as they agree with
// the workload the operator observed and decided ownership for. Disagreement is
// refused (see remediation_ownership_test.go).
func TestRunRemediationHealExplicitWorkloadAndContainer(t *testing.T) {
	approved := strings.Replace(
		withObservedWorkload(t, operatorRemediationFixture, "custom-api", "main"),
		`"phase":"Pending"`, `"phase":"Approved"`, 1)
	patchLog := writeFakeKubectlDispatch(t, fakeKubectlResponses{
		context:    "kind-dorgu-spike",
		rem:        approved,
		persona:    personaFixture,
		deployment: deploymentObjectFixture, // --workload fetches a single object
	})

	cmd := newRemediationHealCmd()
	cmd.SetArgs([]string{"fix-oom-api-server", "-n", "production", "--yes",
		"--workload", "custom-api", "--container", "sidecar"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	require.NoError(t, cmd.Execute())

	log := readPatchLog(t, patchLog)
	assert.Contains(t, log, "patch deployment custom-api")
	assert.Contains(t, log, `"name":"sidecar"`)
}

func TestParseDeploymentObject(t *testing.T) {
	d, err := parseDeploymentObject([]byte(deploymentObjectFixture))
	require.NoError(t, err)
	assert.Equal(t, "custom-api", d.Name)
	assert.Equal(t, []string{"main", "sidecar"}, d.Containers)

	_, err = parseDeploymentObject([]byte(`{}`))
	assert.Error(t, err, "empty object (not found) must error")
}

func TestConfirmHeal(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{in: "y\n", want: true},
		{in: "yes\n", want: true},
		{in: "Y\n", want: true},
		{in: "n\n", want: false},
		{in: "\n", want: false},
		{in: "", want: false}, // EOF
	}
	for _, tt := range tests {
		got, err := confirmHeal(strings.NewReader(tt.in), io.Discard)
		require.NoError(t, err)
		assert.Equal(t, tt.want, got, "input %q", tt.in)
	}
}

func TestHealWorkloadDeclinedConfirmation(t *testing.T) {
	var r remediationFull
	require.NoError(t, json.Unmarshal([]byte(strings.Replace(
		operatorRemediationFixture, `"phase": "Pending"`, `"phase": "Approved"`, 1)), &r))

	patchLog := writeFakeKubectlDispatch(t, fakeKubectlResponses{
		context:    "kind-dorgu-spike",
		rem:        operatorRemediationFixture,
		persona:    personaFixture,
		deployment: deploymentListFixture,
	})

	// No --yes and "n" on stdin → declines, patches nothing.
	err := healWorkload(t.Context(), &r,
		healOptions{}, strings.NewReader("n\n"), io.Discard)
	require.NoError(t, err)
	assert.NotContains(t, readPatchLog(t, patchLog), "patch deployment")
}

func TestRunRemediationHealRefusesRejected(t *testing.T) {
	rejected := strings.Replace(operatorRemediationFixture, `"phase": "Pending"`, `"phase": "Rejected"`, 1)
	patchLog := writeFakeKubectlDispatch(t, fakeKubectlResponses{
		context:    "kind-dorgu-spike",
		rem:        rejected,
		persona:    personaFixture,
		deployment: deploymentListFixture,
	})

	cmd := newRemediationHealCmd()
	cmd.SetArgs([]string{"fix-oom-api-server", "-n", "production", "--yes"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	// A rejected remediation must never be healed — the approve/reject gate holds.
	err := cmd.Execute()
	assert.ErrorIs(t, err, errSilent)
	assert.NotContains(t, readPatchLog(t, patchLog), "patch deployment")
}

// TestHealWorkloadAdvisoryOnly verifies non-resource remediations print advisory
// steps and patch nothing.
func TestRunRemediationHealAdvisoryOnly(t *testing.T) {
	scaleRem := `{
      "metadata": {"name": "scale-web", "namespace": "production"},
      "spec": {
        "personaRef": {"kind": "ApplicationPersona", "name": "web", "namespace": "production"},
        "action": {"type": "scale", "patch": {"spec": {"replicas": 3}}},
        "explanation": "scale up to handle load"
      },
      "status": {"phase": "Approved"}
    }`
	patchLog := writeFakeKubectlDispatch(t, fakeKubectlResponses{
		context:    "kind-dorgu-spike",
		rem:        scaleRem,
		persona:    personaFixture,
		deployment: deploymentListFixture,
	})

	cmd := newRemediationHealCmd()
	cmd.SetArgs([]string{"scale-web", "-n", "production", "--yes"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	require.NoError(t, cmd.Execute())

	assert.NotContains(t, readPatchLog(t, patchLog), "patch deployment",
		"advisory-only remediation must not patch a workload")
}

// --- F-01: a heal that cannot be applied must never look like success ---

// brownfieldDeploymentListFixture is the clean-room namespace: three
// Deployments, none of them labelled on the Deployment object, and none of them
// named after the persona in the remediation fixture (api-server).
const brownfieldDeploymentListFixture = `{"items":[
 {"metadata":{"name":"web"},"spec":{"selector":{"matchLabels":{"app":"web"}},"template":{"spec":{"containers":[{"name":"nginx"}]}}}},
 {"metadata":{"name":"checkout-api"},"spec":{"selector":{"matchLabels":{"app":"checkout-api"}},"template":{"spec":{"containers":[{"name":"api"}]}}}},
 {"metadata":{"name":"report-worker"},"spec":{"selector":{"matchLabels":{"app":"report-worker"}},"template":{"spec":{"containers":[{"name":"worker"}]}}}}
]}`

// The heal path resolves the Deployment before approval, so a workload it
// cannot find approves nothing at all. Approving first is what left the persona
// at 96Mi, the Deployment at 48Mi, and a 10-minute verification window running
// over a change that was never applied.
func TestRunRemediationApproveRefusesWhenWorkloadUnresolvable(t *testing.T) {
	patchLog := writeFakeKubectlDispatch(t, fakeKubectlResponses{
		context:    "kind-dorgu-spike",
		rem:        operatorRemediationFixture,
		persona:    personaFixture,
		deployment: brownfieldDeploymentListFixture,
	})

	cmd := newRemediationApproveCmd()
	cmd.SetArgs([]string{"fix-oom-api-server", "-n", "production", "--yes"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	var err error
	stdout := captureStdout(t, func() { err = cmd.Execute() })

	assert.ErrorIs(t, err, errSilent, "an unappliable heal must exit non-zero")
	assert.NotContains(t, stdout, "Remediation approved", "approval must not be reported")
	assert.Contains(t, stdout, "Nothing was approved and nothing was changed.")

	log := readPatchLog(t, patchLog)
	assert.Empty(t, log, "no RemediationAction and no Deployment may be patched")
}

// The one the report called the blocker inside the blocker: when the workload
// patch itself fails, approve must not exit 0 and must not claim the workload
// was healed.
func TestRunRemediationApproveFailedWorkloadPatchNeverReportsSuccess(t *testing.T) {
	patchLog := writeFakeKubectlDispatch(t, fakeKubectlResponses{
		context:             "kind-dorgu-spike",
		rem:                 operatorRemediationFixture,
		persona:             personaFixture,
		deployment:          deploymentListFixture,
		failDeploymentPatch: true,
	})

	cmd := newRemediationApproveCmd()
	cmd.SetArgs([]string{"fix-oom-api-server", "-n", "production", "--yes"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	var err error
	stdout := captureStdout(t, func() { err = cmd.Execute() })

	assert.ErrorIs(t, err, errSilent, "a failed workload patch must exit non-zero")
	assert.NotContains(t, stdout, "Healed production/api-server",
		"a failed patch must never be reported as a heal")
	assert.Contains(t, stdout, "was NOT patched",
		"the persona-vs-workload divergence must be surfaced, not hidden")

	log := readPatchLog(t, patchLog)
	assert.Contains(t, log, "patch deployment api-server", "the patch was attempted")
}

// Same failure through `dorgu remediation heal`.
func TestRunRemediationHealFailedPatchNeverReportsSuccess(t *testing.T) {
	approved := strings.Replace(operatorRemediationFixture, `"phase": "Pending"`, `"phase": "Approved"`, 1)
	writeFakeKubectlDispatch(t, fakeKubectlResponses{
		context:             "kind-dorgu-spike",
		rem:                 approved,
		persona:             personaFixture,
		deployment:          deploymentListFixture,
		failDeploymentPatch: true,
	})

	cmd := newRemediationHealCmd()
	cmd.SetArgs([]string{"fix-oom-api-server", "-n", "production", "--yes"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	var err error
	stdout := captureStdout(t, func() { err = cmd.Execute() })

	assert.ErrorIs(t, err, errSilent)
	assert.NotContains(t, stdout, "Healed production/api-server")
}

// The fallback chain, end to end: a Deployment labelled only on the pod template
// is found and patched, where before the heal died with "no Deployment found".
func TestRunRemediationHealResolvesPodTemplateLabelledWorkload(t *testing.T) {
	approved := strings.Replace(operatorRemediationFixture, `"phase": "Pending"`, `"phase": "Approved"`, 1)
	brownfieldAPIServer := `{"items":[
 {"metadata":{"name":"web"},"spec":{"selector":{"matchLabels":{"app":"web"}},"template":{"spec":{"containers":[{"name":"nginx"}]}}}},
 {"metadata":{"name":"api-server"},"spec":{"selector":{"matchLabels":{"app":"api-server"}},"template":{"spec":{"containers":[{"name":"api-server"}]}}}}
]}`

	patchLog := writeFakeKubectlDispatch(t, fakeKubectlResponses{
		context:    "kind-dorgu-spike",
		rem:        approved,
		persona:    personaFixture,
		deployment: brownfieldAPIServer,
	})

	cmd := newRemediationHealCmd()
	cmd.SetArgs([]string{"fix-oom-api-server", "-n", "production", "--yes"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	require.NoError(t, cmd.Execute())

	log := readPatchLog(t, patchLog)
	assert.Contains(t, log, "patch deployment api-server")
	assert.Contains(t, log, `"memory":"512Mi"`)
}

// Discovery must not filter by label server-side: a -l selector can only find
// workloads labelled on the Deployment object, which is the whole bug.
func TestDiscoveryListsDeploymentsWithoutALabelSelector(t *testing.T) {
	approved := strings.Replace(operatorRemediationFixture, `"phase": "Pending"`, `"phase": "Approved"`, 1)
	patchLog := writeFakeKubectlDispatch(t, fakeKubectlResponses{
		context:    "kind-dorgu-spike",
		rem:        approved,
		persona:    personaFixture,
		deployment: deploymentListFixture,
	})

	cmd := newRemediationHealCmd()
	cmd.SetArgs([]string{"fix-oom-api-server", "-n", "production", "--yes"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	require.NoError(t, cmd.Execute())

	for _, call := range strings.Split(strings.TrimSpace(readCallLog(t, patchLog)), "\n") {
		if strings.Contains(call, "get deployment") {
			assert.NotContains(t, call, " -l ", "deployment discovery must not use a label selector")
		}
	}
}

func TestParseDeploymentList_CarriesLabelsAndSelector(t *testing.T) {
	ds, err := parseDeploymentList([]byte(brownfieldDeploymentListFixture))

	require.NoError(t, err)
	require.Len(t, ds, 3)
	assert.Equal(t, "web", ds[0].Name)
	assert.Empty(t, ds[0].Labels, "the brownfield shape has no labels on the Deployment object")
	assert.Equal(t, map[string]string{"app": "web"}, ds[0].SelectorLabels,
		"selector labels must survive parsing; they are rung 4 of the chain")
}
