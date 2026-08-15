package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// brownfieldDeploymentsIn builds the clean-room namespace: three Deployments
// labelled on the pod template only, which is what the tester's manifest
// produced and what `dorgu health` said nothing about.
func brownfieldDeploymentsIn(namespace string, names ...string) []appsv1.Deployment {
	deployments := make([]appsv1.Deployment, 0, len(names))
	for _, name := range names {
		deployments = append(deployments, appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
			Spec: appsv1.DeploymentSpec{
				Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": name}},
			},
		})
	}
	return deployments
}

func TestBuildUnmonitoredSummary_ReportsUncoveredDeployments(t *testing.T) {
	deployments := brownfieldDeploymentsIn("apps", "web", "checkout-api", "report-worker")

	summary := buildUnmonitoredSummary(deployments, nil)

	assert.Equal(t, 3, summary.Count)
	assert.Equal(t, []unmonitoredDeployment{
		{Namespace: "apps", Name: "checkout-api"},
		{Namespace: "apps", Name: "report-worker"},
		{Namespace: "apps", Name: "web"},
	}, summary.Items)
	assert.Equal(t, []string{"apps"}, summary.Namespaces)
}

// A persona covers a pod-template-labelled Deployment through the same fallback
// chain heal uses, so health must not report it as unmonitored.
func TestBuildUnmonitoredSummary_PersonaCoversByEveryRung(t *testing.T) {
	tests := []struct {
		name       string
		deployment appsv1.Deployment
		persona    personaBrief
	}{
		{
			name: "recommended label",
			deployment: appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{
				Name: "web-deploy", Namespace: "apps",
				Labels: map[string]string{labelAppName: "web"},
			}},
			persona: personaBrief{Namespace: "apps", AppName: "web"},
		},
		{
			name: "short app label",
			deployment: appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{
				Name: "web-deploy", Namespace: "apps",
				Labels: map[string]string{"app": "web"},
			}},
			persona: personaBrief{Namespace: "apps", AppName: "web"},
		},
		{
			name:       "Deployment name",
			deployment: brownfieldDeploymentsIn("apps", "web")[0],
			persona:    personaBrief{Namespace: "apps", AppName: "web"},
		},
		{
			name: "selector matchLabels",
			deployment: appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Name: "web-v2", Namespace: "apps"},
				Spec: appsv1.DeploymentSpec{
					Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "web"}},
				},
			},
			persona: personaBrief{Namespace: "apps", AppName: "web"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			summary := buildUnmonitoredSummary([]appsv1.Deployment{tt.deployment}, []personaBrief{tt.persona})

			assert.Equal(t, 0, summary.Count)
		})
	}
}

func TestBuildUnmonitoredSummary_FullyCoveredClusterIsEmpty(t *testing.T) {
	deployments := brownfieldDeploymentsIn("apps", "web", "checkout-api")
	personas := []personaBrief{
		{Namespace: "apps", AppName: "web"},
		{Namespace: "apps", AppName: "checkout-api"},
	}

	summary := buildUnmonitoredSummary(deployments, personas)

	assert.Equal(t, 0, summary.Count)
	assert.Empty(t, summary.Items)
	assert.Empty(t, summary.Namespaces)
}

// A persona only covers workloads in its own namespace.
func TestBuildUnmonitoredSummary_PersonasDoNotCrossNamespaces(t *testing.T) {
	deployments := brownfieldDeploymentsIn("apps", "web")
	personas := []personaBrief{{Namespace: "staging", AppName: "web"}}

	summary := buildUnmonitoredSummary(deployments, personas)

	assert.Equal(t, 1, summary.Count)
}

func TestBuildUnmonitoredSummary_SortsAcrossNamespaces(t *testing.T) {
	deployments := append(
		brownfieldDeploymentsIn("staging", "web"),
		brownfieldDeploymentsIn("apps", "web", "checkout-api")...,
	)

	summary := buildUnmonitoredSummary(deployments, nil)

	assert.Equal(t, []unmonitoredDeployment{
		{Namespace: "apps", Name: "checkout-api"},
		{Namespace: "apps", Name: "web"},
		{Namespace: "staging", Name: "web"},
	}, summary.Items)
	assert.Equal(t, []string{"apps", "staging"}, summary.Namespaces)
}

func TestWithoutSystemNamespaces(t *testing.T) {
	deployments := append(
		brownfieldDeploymentsIn("kube-system", "coredns", "metrics-server"),
		brownfieldDeploymentsIn("apps", "web")...,
	)

	kept := withoutSystemNamespaces(deployments)

	require.Len(t, kept, 1)
	assert.Equal(t, "web", kept[0].Name)
}

// --- printing ---

func TestPrintUnmonitored_NamesTheDeploymentsAndTheFix(t *testing.T) {
	summary := buildUnmonitoredSummary(
		brownfieldDeploymentsIn("apps", "web", "checkout-api", "report-worker"), nil)

	var buf bytes.Buffer
	printUnmonitored(&buf, summary)
	out := buf.String()

	assert.Contains(t, out, "Unmonitored")
	assert.Contains(t, out, "3 Deployment(s) have no ApplicationPersona")
	assert.Contains(t, out, "apps/checkout-api, apps/report-worker, apps/web")
	assert.Contains(t, out, "dorgu persona import -n apps --all")
}

func TestPrintUnmonitored_OmittedWhenNothingIsMissing(t *testing.T) {
	var buf bytes.Buffer

	printUnmonitored(&buf, &unmonitoredSummary{})
	printUnmonitored(&buf, nil)

	assert.Empty(t, buf.String())
}

func TestPrintUnmonitored_OneHintPerNamespace(t *testing.T) {
	deployments := append(
		brownfieldDeploymentsIn("apps", "web"),
		brownfieldDeploymentsIn("staging", "web")...,
	)

	var buf bytes.Buffer
	printUnmonitored(&buf, buildUnmonitoredSummary(deployments, nil))
	out := buf.String()

	assert.Contains(t, out, "dorgu persona import -n apps --all")
	assert.Contains(t, out, "dorgu persona import -n staging --all")
}

// A long list is capped for readability, but the print says how many it left
// out: a silent truncation reads as "that is all of them".
func TestPrintUnmonitored_SaysWhatItTruncated(t *testing.T) {
	names := make([]string, 0, maxUnmonitoredListed+3)
	for i := 0; i < maxUnmonitoredListed+3; i++ {
		names = append(names, string(rune('a'+i))+"-service")
	}
	summary := buildUnmonitoredSummary(brownfieldDeploymentsIn("apps", names...), nil)

	var buf bytes.Buffer
	printUnmonitored(&buf, summary)
	out := buf.String()

	assert.Equal(t, maxUnmonitoredListed+3, summary.Count, "the count is never capped")
	assert.Len(t, summary.Items, maxUnmonitoredListed+3, "--json carries every item")
	assert.Contains(t, out, "and 3 more")
}

func TestPrintUnmonitored_MissingCRDIsAboutTheOperatorNotTheApps(t *testing.T) {
	summary := buildUnmonitoredSummary(brownfieldDeploymentsIn("apps", "web", "checkout-api"), nil)
	summary.PersonaCRDMissing = true

	var buf bytes.Buffer
	printUnmonitored(&buf, summary)
	out := buf.String()

	assert.Contains(t, out, "the ApplicationPersona CRD is not installed")
	assert.Contains(t, out, "install the operator first: dorgu cluster setup")
	assert.NotContains(t, out, "persona import")
}

// --- parsing ---

func TestParsePersonaBriefs(t *testing.T) {
	raw := `{"items":[
	 {"metadata":{"name":"web","namespace":"apps"},"spec":{"name":"web"}},
	 {"metadata":{"name":"sample-app","namespace":"default"},"spec":{}}
	]}`

	briefs, err := parsePersonaBriefs([]byte(raw))

	require.NoError(t, err)
	require.Len(t, briefs, 2)
	assert.Equal(t, personaBrief{Namespace: "apps", AppName: "web"}, briefs[0])
	assert.Equal(t, personaBrief{Namespace: "default", AppName: "sample-app"}, briefs[1],
		"a persona with no spec.name falls back to metadata.name")
}

// --- end to end through the health command ---

func TestHealthSummary_JSONCarriesTheUnmonitoredCount(t *testing.T) {
	summary := &healthSummary{}
	summary.Unmonitored = buildUnmonitoredSummary(
		brownfieldDeploymentsIn("apps", "web", "checkout-api", "report-worker"), nil)

	encoded, err := json.Marshal(summary)

	require.NoError(t, err)
	assert.Contains(t, string(encoded), `"unmonitored"`)
	assert.Contains(t, string(encoded), `"count":3`)
	assert.Contains(t, string(encoded), `"namespace":"apps"`)
}

func TestFetchUnmonitored_ReadsTheClusterAndMatchesPersonas(t *testing.T) {
	writeFakeKubectlHealth(t, brownfieldNamespaceJSON,
		`{"items":[{"metadata":{"name":"web","namespace":"apps"},"spec":{"name":"web"}}]}`)

	summary := fetchUnmonitored(t.Context(), "", "apps")

	require.NotNil(t, summary)
	assert.Equal(t, 2, summary.Count, "web has a persona; the other two do not")
	assert.Equal(t, "checkout-api", summary.Items[0].Name)
	assert.Equal(t, "report-worker", summary.Items[1].Name)
}

func TestFetchUnmonitored_ReportsAMissingCRD(t *testing.T) {
	writeFakeKubectlHealthMissingCRD(t, brownfieldNamespaceJSON)

	summary := fetchUnmonitored(t.Context(), "", "apps")

	require.NotNil(t, summary)
	assert.True(t, summary.PersonaCRDMissing)
	assert.Equal(t, 3, summary.Count)
}

// writeFakeKubectlTwoResources installs a fake kubectl that answers
// `get deployment` and `get applicationpersona` from files. When missingCRD is
// set, the persona query fails the way an uninstalled CRD does.
func writeFakeKubectlTwoResources(t *testing.T, deploymentsJSON, personasJSON string, missingCRD bool) {
	t.Helper()
	dir := t.TempDir()

	deployFile := filepath.Join(dir, "deployments.json")
	require.NoError(t, os.WriteFile(deployFile, []byte(deploymentsJSON), 0o600))
	personaFile := filepath.Join(dir, "personas.json")
	require.NoError(t, os.WriteFile(personaFile, []byte(personasJSON), 0o600))

	personaBranch := "cat " + personaFile
	if missingCRD {
		personaBranch = `echo 'error: the server doesn'"'"'t have a resource type "applicationpersona"' >&2; exit 1`
	}

	script := "#!/bin/sh\n" +
		"kind=\"\"\n" +
		"for a in \"$@\"; do\n" +
		"  case \"$a\" in\n" +
		"    deployment|deployments|deploy) [ -z \"$kind\" ] && kind=deploy ;;\n" +
		"    applicationpersona|applicationpersonas) [ -z \"$kind\" ] && kind=persona ;;\n" +
		"  esac\n" +
		"done\n" +
		"case \"$kind\" in\n" +
		"  deploy) cat " + deployFile + " ;;\n" +
		"  persona) " + personaBranch + " ;;\n" +
		"esac\n" +
		"exit 0\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "kubectl"), []byte(script), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func writeFakeKubectlHealth(t *testing.T, deploymentsJSON, personasJSON string) {
	t.Helper()
	writeFakeKubectlTwoResources(t, deploymentsJSON, personasJSON, false)
}

func writeFakeKubectlHealthMissingCRD(t *testing.T, deploymentsJSON string) {
	t.Helper()
	writeFakeKubectlTwoResources(t, deploymentsJSON, "", true)
}
