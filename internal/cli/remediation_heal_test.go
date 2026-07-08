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

func TestSelectDeployment(t *testing.T) {
	single := []deploymentSummary{{Name: "api-server", Containers: []string{"api-server"}}}
	multi := []deploymentSummary{{Name: "api-a"}, {Name: "api-b"}}

	got, err := selectDeployment(single, "api-server")
	require.NoError(t, err)
	assert.Equal(t, "api-server", got.Name)

	_, err = selectDeployment(nil, "api-server")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--workload")

	_, err = selectDeployment(multi, "api-server")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--workload")
	assert.Contains(t, err.Error(), "api-a")
	assert.Contains(t, err.Error(), "api-b")
}

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
	patchLog = filepath.Join(dir, "patch.log")

	script := "#!/bin/sh\n" +
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
		"if [ \"$mode\" = patch ]; then echo \"$@\" >> " + patchLog + "; echo patched; exit 0; fi\n" +
		"if [ \"$mode\" = get ]; then\n" +
		"  case \"$kind\" in\n" +
		"    rem) cat " + remFile + " ;;\n" +
		"    persona) cat " + personaFile + " ;;\n" +
		"    deploy) cat " + deployFile + " ;;\n" +
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

	// Status patch succeeds; heal refuses the prod context → errSilent.
	err := cmd.Execute()
	assert.ErrorIs(t, err, errSilent)

	log := readPatchLog(t, patchLog)
	assert.Contains(t, log, "patch remediationaction", "status is still approved")
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

func TestRunRemediationHealExplicitWorkloadAndContainer(t *testing.T) {
	approved := strings.Replace(operatorRemediationFixture, `"phase": "Pending"`, `"phase": "Approved"`, 1)
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
