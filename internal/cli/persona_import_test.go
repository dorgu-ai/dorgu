package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// brownfieldNamespaceJSON is ~/dorgu-cleanroom/2026-08-09/brownfield.yaml as
// kubectl returns it: three Deployments, labels on the pod template only.
const brownfieldNamespaceJSON = `{"items":[
 {"metadata":{"name":"web","namespace":"apps"},
  "spec":{"replicas":2,"selector":{"matchLabels":{"app":"web"}},
   "template":{"metadata":{"labels":{"app":"web"}},"spec":{"containers":[
     {"name":"nginx","image":"nginx:1.27-alpine","ports":[{"containerPort":80}],
      "resources":{"requests":{"cpu":"25m","memory":"32Mi"},"limits":{"cpu":"200m","memory":"96Mi"}}}]}}}},
 {"metadata":{"name":"checkout-api","namespace":"apps"},
  "spec":{"replicas":1,"selector":{"matchLabels":{"app":"checkout-api"}},
   "template":{"metadata":{"labels":{"app":"checkout-api"}},"spec":{"containers":[
     {"name":"api","image":"hashicorp/http-echo:1.0","ports":[{"containerPort":8080}],
      "resources":{"requests":{"cpu":"25m","memory":"24Mi"},"limits":{"cpu":"200m","memory":"64Mi"}}}]}}}},
 {"metadata":{"name":"report-worker","namespace":"apps"},
  "spec":{"replicas":1,"selector":{"matchLabels":{"app":"report-worker"}},
   "template":{"metadata":{"labels":{"app":"report-worker"}},"spec":{"containers":[
     {"name":"worker","image":"busybox:1.36",
      "resources":{"requests":{"cpu":"25m","memory":"16Mi"},"limits":{"cpu":"100m","memory":"48Mi"}}}]}}}}
]}`

const noLimitsNamespaceJSON = `{"items":[
 {"metadata":{"name":"billing","namespace":"apps"},
  "spec":{"replicas":1,"selector":{"matchLabels":{"app":"billing"}},
   "template":{"metadata":{"labels":{"app":"billing"}},"spec":{"containers":[
     {"name":"billing","image":"billing:1.0",
      "resources":{"requests":{"cpu":"25m","memory":"32Mi"}}}]}}}}
]}`

// writeFakeKubectlDeployments installs a fake kubectl that answers
// `get deployment` with the given JSON and logs `apply` invocations.
func writeFakeKubectlDeployments(t *testing.T, deploymentsJSON string) (applyLog string) {
	t.Helper()
	dir := t.TempDir()

	deployFile := filepath.Join(dir, "deployments.json")
	require.NoError(t, os.WriteFile(deployFile, []byte(deploymentsJSON), 0o600))
	applyLog = filepath.Join(dir, "apply.log")

	script := "#!/bin/sh\n" +
		"for a in \"$@\"; do\n" +
		"  case \"$a\" in\n" +
		"    apply) cat >> " + applyLog + "; echo applied; exit 0 ;;\n" +
		"  esac\n" +
		"done\n" +
		"cat " + deployFile + "\n" +
		"exit 0\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "kubectl"), []byte(script), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return applyLog
}

// runImport executes the command with stdout and stderr captured separately:
// the YAML goes to stdout, every diagnostic to stderr, and a test that conflated
// them would not notice a warning corrupting a redirected file.
func runImport(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	cmd := newPersonaImportCmd()
	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	cmd.SetArgs(args)
	err = cmd.Execute()
	return outBuf.String(), errBuf.String(), err
}

// F-02: before this command there was no way to onboard a running app, so a
// broken one produced no incident and no remediation.
func TestPersonaImport_AllRendersEveryDeployment(t *testing.T) {
	writeFakeKubectlDeployments(t, brownfieldNamespaceJSON)

	out, _, err := runImport(t, "-n", "apps", "--all")

	require.NoError(t, err)
	assert.Equal(t, 3, strings.Count(out, "kind: ApplicationPersona"))
	assert.Contains(t, out, "name: web")
	assert.Contains(t, out, "name: checkout-api")
	assert.Contains(t, out, "name: report-worker")
	// Sorted, so repeated runs produce the same file.
	assert.Less(t, strings.Index(out, "name: checkout-api"), strings.Index(out, "name: report-worker"))
	assert.Less(t, strings.Index(out, "name: report-worker"), strings.Index(out, "name: web"))
}

func TestPersonaImport_NothingIsAppliedByDefault(t *testing.T) {
	applyLog := writeFakeKubectlDeployments(t, brownfieldNamespaceJSON)

	_, _, err := runImport(t, "-n", "apps", "--all")

	require.NoError(t, err)
	_, statErr := os.Stat(applyLog)
	assert.True(t, os.IsNotExist(statErr), "import must not touch the cluster without --apply")
}

func TestPersonaImport_ApplyPipesEveryPersonaToKubectl(t *testing.T) {
	applyLog := writeFakeKubectlDeployments(t, brownfieldNamespaceJSON)

	_, _, err := runImport(t, "-n", "apps", "--all", "--apply")

	require.NoError(t, err)
	applied, readErr := os.ReadFile(applyLog)
	require.NoError(t, readErr)
	assert.Equal(t, 3, strings.Count(string(applied), "kind: ApplicationPersona"))
}

func TestPersonaImport_SingleDeploymentByName(t *testing.T) {
	writeFakeKubectlDeployments(t, brownfieldNamespaceJSON)

	out, _, err := runImport(t, "-n", "apps", "--name", "report-worker")

	require.NoError(t, err)
	assert.Equal(t, 1, strings.Count(out, "kind: ApplicationPersona"))
	assert.Contains(t, out, "name: report-worker")
	assert.NotContains(t, out, "name: checkout-api")
	assert.Contains(t, out, "type: worker")
}

func TestPersonaImport_UnknownNameListsWhatIsThere(t *testing.T) {
	writeFakeKubectlDeployments(t, brownfieldNamespaceJSON)

	_, _, err := runImport(t, "-n", "apps", "--name", "nope")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "web")
	assert.Contains(t, err.Error(), "checkout-api")
}

func TestPersonaImport_EmptyNamespace(t *testing.T) {
	writeFakeKubectlDeployments(t, `{"items":[]}`)

	out, _, err := runImport(t, "-n", "apps", "--all")

	require.NoError(t, err)
	assert.NotContains(t, out, "kind: ApplicationPersona")
}

func TestPersonaImport_RequiresAllOrName(t *testing.T) {
	writeFakeKubectlDeployments(t, brownfieldNamespaceJSON)

	_, _, err := runImport(t, "-n", "apps")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "--all")
	assert.Contains(t, err.Error(), "--name")
}

func TestPersonaImport_WritesToFile(t *testing.T) {
	writeFakeKubectlDeployments(t, brownfieldNamespaceJSON)
	path := filepath.Join(t.TempDir(), "personas.yaml")

	_, _, err := runImport(t, "-n", "apps", "--all", "-o", path)

	require.NoError(t, err)
	contents, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	assert.Equal(t, 3, strings.Count(string(contents), "kind: ApplicationPersona"))
	assert.Contains(t, string(contents), "\n---\n", "multiple personas are one multi-document file")
}

// A persona with no resource limits is one the proposer skips, so it can never
// heal. Import fills them in and says so rather than shipping one silently.
func TestPersonaImport_WarnsWhenLimitsWereInferred(t *testing.T) {
	writeFakeKubectlDeployments(t, noLimitsNamespaceJSON)

	stdout, diagnostics, err := runImport(t, "-n", "apps", "--all")

	require.NoError(t, err)
	assert.Contains(t, diagnostics, "sets no cpu limit")
	assert.Contains(t, diagnostics, "Resource limits were inferred for: billing")
	assert.NotContains(t, stdout, "⚠", "warnings must not land in redirected YAML")
}

// --- app-name selection ---

func deploymentFor(name string, labels map[string]string, selector map[string]string) appsv1.Deployment {
	d := appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "apps", Labels: labels},
	}
	if selector != nil {
		d.Spec.Selector = &metav1.LabelSelector{MatchLabels: selector}
	}
	return d
}

// The imported persona has to resolve back to its own Deployment under the
// operator's fallback chain, or the import produces a persona that watches
// nothing (or, worse, the wrong workload).
func TestPersonaAppName(t *testing.T) {
	tests := []struct {
		name string
		all  []appsv1.Deployment
		pick int
		want string
	}{
		{
			name: "recommended label when it is unambiguous",
			all: []appsv1.Deployment{
				deploymentFor("web-deploy", map[string]string{labelAppName: "web"}, nil),
			},
			want: "web",
		},
		{
			name: "short app label when it is unambiguous",
			all: []appsv1.Deployment{
				deploymentFor("web-deploy", map[string]string{"app": "web"}, nil),
			},
			want: "web",
		},
		{
			name: "the Deployment name for a pod-template-labelled workload",
			all: []appsv1.Deployment{
				deploymentFor("report-worker", nil, map[string]string{"app": "report-worker"}),
			},
			want: "report-worker",
		},
		{
			name: "falls back to the Deployment name when the label is shared",
			all: []appsv1.Deployment{
				deploymentFor("web-blue", map[string]string{"app": "web"}, nil),
				deploymentFor("web-green", map[string]string{"app": "web"}, nil),
			},
			want: "web-blue",
		},
		{
			name: "prefers a label the Deployment itself would win",
			all: []appsv1.Deployment{
				deploymentFor("web", map[string]string{labelAppName: "web"}, map[string]string{"app": "web"}),
				deploymentFor("web-canary", map[string]string{"app": "web"}, nil),
			},
			want: "web",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, conflict := personaAppName(&tt.all[tt.pick], tt.all)

			assert.Equal(t, tt.want, got)
			assert.Empty(t, conflict)

			// Whatever we picked must resolve back to this Deployment.
			summaries := make([]deploymentSummary, 0, len(tt.all))
			for i := range tt.all {
				summaries = append(summaries, summaryOf(&tt.all[i]))
			}
			resolved, err := selectDeployment(summaries, got)
			require.NoError(t, err, "the chosen app name must resolve")
			assert.Equal(t, tt.all[tt.pick].Name, resolved.Name,
				"the chosen app name must resolve back to this Deployment")
		})
	}
}

// When another Deployment's label outranks this one's name, no app name
// resolves correctly. Emitting the persona anyway without saying so would point
// Dorgu at the wrong workload.
func TestPersonaAppName_ReportsAnUnresolvableConflict(t *testing.T) {
	all := []appsv1.Deployment{
		deploymentFor("web", nil, map[string]string{"app": "web"}),
		deploymentFor("web-canary", map[string]string{labelAppName: "web"}, nil),
	}

	got, conflict := personaAppName(&all[0], all)

	assert.Equal(t, "web", got)
	assert.Contains(t, conflict, "will resolve to web-canary instead")
	assert.Contains(t, conflict, labelAppName+"=web on web")
}

func TestPersonaImport_SurfacesTheResolutionConflict(t *testing.T) {
	writeFakeKubectlDeployments(t, `{"items":[
 {"metadata":{"name":"web","namespace":"apps"},
  "spec":{"replicas":1,"selector":{"matchLabels":{"app":"web"}},
   "template":{"spec":{"containers":[{"name":"web","image":"web:1.0",
     "resources":{"limits":{"cpu":"200m","memory":"96Mi"}}}]}}}},
 {"metadata":{"name":"web-canary","namespace":"apps","labels":{"app.kubernetes.io/name":"web"}},
  "spec":{"replicas":1,"selector":{"matchLabels":{"app":"web-canary"}},
   "template":{"spec":{"containers":[{"name":"web","image":"web:2.0",
     "resources":{"limits":{"cpu":"200m","memory":"96Mi"}}}]}}}}
]}`)

	_, diagnostics, err := runImport(t, "-n", "apps", "--all")

	require.NoError(t, err)
	assert.Contains(t, diagnostics, "will resolve to web-canary instead")
}

func TestParseDeploymentObjects(t *testing.T) {
	deployments, err := parseDeploymentObjects([]byte(brownfieldNamespaceJSON))

	require.NoError(t, err)
	require.Len(t, deployments, 3)
	assert.Equal(t, "web", deployments[0].Name)
	assert.Equal(t, int32(2), *deployments[0].Spec.Replicas)
	assert.Equal(t, "nginx:1.27-alpine", deployments[0].Spec.Template.Spec.Containers[0].Image)
}
