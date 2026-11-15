package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// P2-D11 / brief C4: `dorgu health` says whether CPU throttling detection is on,
// which source it reads, or why it is off, from ClusterPersona
// status.cpuThrottlingDetection. An operator older than v0.14 writes no such
// field, which is "unknown", not "off".

func personaListWithStatus(statusJSON string) string {
	return `{"items":[{"metadata":{"name":"dorgu-cluster"},"status":` + statusJSON + `}]}`
}

func TestParseCPUThrottling(t *testing.T) {
	tests := []struct {
		name       string
		raw        string
		wantState  string
		wantSource string
		wantReason string
	}{
		{
			"metrics.k8s.io",
			personaListWithStatus(`{"cpuThrottlingDetection":{"enabled":true,"source":"metrics.k8s.io","reason":"MetricsServerActive","message":"reading metrics.k8s.io"}}`),
			cpuThrottlingOn, "metrics.k8s.io", "MetricsServerActive",
		},
		{
			"prometheus",
			personaListWithStatus(`{"cpuThrottlingDetection":{"enabled":true,"source":"prometheus","reason":"PrometheusActive"}}`),
			cpuThrottlingOn, "prometheus", "PrometheusActive",
		},
		{
			"prometheus fell back to metrics.k8s.io",
			personaListWithStatus(`{"cpuThrottlingDetection":{"enabled":true,"source":"metrics.k8s.io","reason":"PrometheusUnreachable"}}`),
			cpuThrottlingOn, "metrics.k8s.io", "PrometheusUnreachable",
		},
		{
			"no source",
			personaListWithStatus(`{"cpuThrottlingDetection":{"enabled":false,"source":"none","reason":"NoMetricsSource","message":"metrics.k8s.io is not served"}}`),
			cpuThrottlingOff, "none", "NoMetricsSource",
		},
		{
			"an older operator writes no field",
			personaListWithStatus(`{"applicationCount":3}`),
			cpuThrottlingUnknown, "", reasonOperatorPredatesCPUThrottling,
		},
		{
			"no status at all",
			`{"items":[{"metadata":{"name":"dorgu-cluster"}}]}`,
			cpuThrottlingUnknown, "", reasonOperatorPredatesCPUThrottling,
		},
		{
			"no ClusterPersona",
			`{"items":[]}`,
			cpuThrottlingUnknown, "", reasonNoClusterPersona,
		},
		{
			"unreadable",
			`not json`,
			cpuThrottlingUnknown, "", reasonClusterPersonaUnreadable,
		},
		{
			"enabled with no source is not on",
			personaListWithStatus(`{"cpuThrottlingDetection":{"enabled":true,"source":"","reason":"X"}}`),
			cpuThrottlingUnknown, "", reasonClusterPersonaUnreadable,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseCPUThrottling([]byte(tt.raw))

			require.NotNil(t, got)
			assert.Equal(t, tt.wantState, got.State)
			assert.Equal(t, tt.wantSource, got.Source)
			assert.Equal(t, tt.wantReason, got.Reason)
		})
	}
}

func TestParseCPUThrottlingTakesTheFirstClusterPersonaThatHasTheField(t *testing.T) {
	raw := `{"items":[{"status":{}},{"status":{"cpuThrottlingDetection":{"enabled":true,"source":"prometheus","reason":"PrometheusActive"}}}]}`

	got := parseCPUThrottling([]byte(raw))

	assert.Equal(t, cpuThrottlingOn, got.State)
	assert.Equal(t, "prometheus", got.Source)
}

func TestPrintCPUThrottling(t *testing.T) {
	tests := []struct {
		name string
		in   *cpuThrottlingStatus
		want []string
	}{
		{"on, metrics.k8s.io", &cpuThrottlingStatus{State: cpuThrottlingOn, Source: "metrics.k8s.io", Reason: "MetricsServerActive"},
			[]string{"CPU throttling detection: on", "source: metrics.k8s.io"}},
		{"on, prometheus", &cpuThrottlingStatus{State: cpuThrottlingOn, Source: "prometheus", Reason: "PrometheusActive"},
			[]string{"CPU throttling detection: on", "source: prometheus"}},
		{"on after a fallback", &cpuThrottlingStatus{State: cpuThrottlingOn, Source: "metrics.k8s.io", Reason: "PrometheusUnreachable"},
			[]string{"on", "source: metrics.k8s.io", "Prometheus did not answer"}},
		{"off, no source", &cpuThrottlingStatus{State: cpuThrottlingOff, Source: "none", Reason: "NoMetricsSource"},
			[]string{"CPU throttling detection: off", "no metrics source"}},
		{"off, an unknown reason shows the token", &cpuThrottlingStatus{State: cpuThrottlingOff, Source: "none", Reason: "SomethingNew", Message: "the operator says why"},
			[]string{"off", "SomethingNew", "the operator says why"}},
		{"unknown, older operator", &cpuThrottlingStatus{State: cpuThrottlingUnknown, Reason: reasonOperatorPredatesCPUThrottling},
			[]string{"CPU throttling detection: unknown", "operator older than v0.14"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer

			printCPUThrottling(&buf, tt.in)

			for _, w := range tt.want {
				assert.Contains(t, buf.String(), w)
			}
			assert.NotContains(t, buf.String(), "—")
		})
	}
}

func TestPrintCPUThrottlingNilPrintsNothing(t *testing.T) {
	var buf bytes.Buffer
	printCPUThrottling(&buf, nil)
	assert.Empty(t, buf.String())
}

func TestPrintCPUThrottlingStripsMarkdownFromTheOperatorMessage(t *testing.T) {
	var buf bytes.Buffer

	printCPUThrottling(&buf, &cpuThrottlingStatus{State: cpuThrottlingOff, Source: "none", Reason: "SomethingNew", Message: "see **docs** and `code`"})

	assert.Contains(t, buf.String(), "see docs and code")
}

func TestHealthSummaryCarriesCPUThrottlingInJSONAndPlainOutput(t *testing.T) {
	s := &healthSummary{
		CPUThrottling:   &cpuThrottlingStatus{State: cpuThrottlingOn, Source: "prometheus", Reason: "PrometheusActive"},
		ActiveIncidents: &incidentsSummary{},
	}

	raw, err := json.Marshal(s)
	require.NoError(t, err)
	var doc map[string]map[string]any
	require.NoError(t, json.Unmarshal(raw, &doc))
	assert.Equal(t, "on", doc["cpuThrottling"]["state"])
	assert.Equal(t, "prometheus", doc["cpuThrottling"]["source"])
	assert.Equal(t, "PrometheusActive", doc["cpuThrottling"]["reason"])

	var buf bytes.Buffer
	printHealthSummary(&buf, s)
	assert.Contains(t, buf.String(), "CPU throttling detection: on")
}

func TestFetchCPUThrottlingReadsTheClusterPersonaThroughKubectl(t *testing.T) {
	patchLog := writeFakeKubectlDispatch(t, fakeKubectlResponses{
		context: "kind-dorgu-spike",
		clusterPersona: personaListWithStatus(
			`{"cpuThrottlingDetection":{"enabled":true,"source":"metrics.k8s.io","reason":"MetricsServerActive"}}`),
	})

	got := fetchCPUThrottling(t.Context(), "")

	require.NotNil(t, got)
	assert.Equal(t, cpuThrottlingOn, got.State)
	assert.Contains(t, readCallLog(t, patchLog), "get clusterpersona -o json")
}

func TestFetchCPUThrottlingWhenTheClusterPersonaCannotBeRead(t *testing.T) {
	writeFakeKubectlDispatch(t, fakeKubectlResponses{context: "kind-dorgu-spike", clusterPersona: `garbage`})

	got := fetchCPUThrottling(t.Context(), "")

	require.NotNil(t, got)
	assert.Equal(t, cpuThrottlingUnknown, got.State)
	assert.False(t, strings.Contains(got.Message, "—"))
}

// failingKubectl installs a kubectl that prints stderr and exits 1 for everything.
func failingKubectl(t *testing.T, stderr string) {
	t.Helper()
	dir := t.TempDir()
	errFile := filepath.Join(dir, "err.txt")
	require.NoError(t, os.WriteFile(errFile, []byte(stderr), 0o600))
	script := "#!/bin/sh\ncat " + errFile + " >&2\nexit 1\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "kubectl"), []byte(script), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestFetchCPUThrottlingWhenKubectlFails(t *testing.T) {
	tests := []struct {
		name       string
		stderr     string
		wantReason string
	}{
		{"the CRD is not installed", `error: the server doesn't have a resource type "clusterpersona"`, reasonNoClusterPersona},
		{"forbidden", `Error from server (Forbidden): clusterpersonas.dorgu.io is forbidden`, reasonClusterPersonaUnreadable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			failingKubectl(t, tt.stderr)

			got := fetchCPUThrottling(t.Context(), "")

			require.NotNil(t, got)
			assert.Equal(t, cpuThrottlingUnknown, got.State, "a read that fails is never reported as off or on")
			assert.Equal(t, tt.wantReason, got.Reason)
		})
	}
}

func TestCPUThrottlingReasonFallbacks(t *testing.T) {
	assert.Equal(t, "X: says why", cpuThrottlingReason(&cpuThrottlingStatus{Reason: "X", Message: "says why"}))
	assert.Equal(t, "X", cpuThrottlingReason(&cpuThrottlingStatus{Reason: "X"}))
	assert.Equal(t, "just a message", cpuThrottlingReason(&cpuThrottlingStatus{Message: "just a message"}))
	assert.Equal(t, "no reason given", cpuThrottlingReason(&cpuThrottlingStatus{}))
}

func TestParseCPUThrottlingEnabledWithoutASourceIsNotOn(t *testing.T) {
	got := parseCPUThrottling([]byte(personaListWithStatus(`{"cpuThrottlingDetection":{"enabled":true,"source":"none","reason":"X"}}`)))

	assert.Equal(t, cpuThrottlingUnknown, got.State)
}

// --- C5 review F-4: a v0.14 operator with a stale CRD ---
//
// Helm does not upgrade crds/, so the API server prunes status.cpuThrottlingDetection
// on an upgraded cluster. The operator notices and keeps a CPUThrottlingDetection
// condition, which an old CRD retains. The CLI reads it rather than call the
// operator old.

func staleCRDPersona(condStatus, reason string) string {
	return personaListWithStatus(`{"conditions":[{"type":"CPUThrottlingDetection","status":"` + condStatus +
		`","reason":"` + reason + `","message":"detection is running"}]}`)
}

func TestParseCPUThrottlingReadsTheConditionWhenTheFieldIsPruned(t *testing.T) {
	tests := []struct {
		name       string
		raw        string
		wantState  string
		wantSource string
		wantReason string
	}{
		{"detection on", staleCRDPersona("True", "MetricsServerActive"), cpuThrottlingOn, "unknown", "MetricsServerActive"},
		{"detection off", staleCRDPersona("False", "NoMetricsSource"), cpuThrottlingOff, "", "NoMetricsSource"},
		{"unknown status", staleCRDPersona("Unknown", "MetricsReadFailed"), cpuThrottlingUnknown, "", "MetricsReadFailed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseCPUThrottling([]byte(tt.raw))

			assert.Equal(t, tt.wantState, got.State)
			assert.Equal(t, tt.wantSource, got.Source)
			assert.Equal(t, tt.wantReason, got.Reason)
			assert.Contains(t, got.Note, "ClusterPersona CRD is older than the operator", "a stale CRD is said so")
			assert.Contains(t, got.Note, "kubectl apply -f")
			assert.NotEqual(t, reasonOperatorPredatesCPUThrottling, got.Reason, "the operator is not old")
		})
	}
}

func TestParseCPUThrottlingTheFieldWinsOverTheConditionAndHasNoNote(t *testing.T) {
	raw := personaListWithStatus(`{"cpuThrottlingDetection":{"enabled":true,"source":"prometheus","reason":"PrometheusActive"},` +
		`"conditions":[{"type":"CPUThrottlingDetection","status":"False","reason":"NoMetricsSource"}]}`)

	got := parseCPUThrottling([]byte(raw))

	assert.Equal(t, cpuThrottlingOn, got.State)
	assert.Equal(t, "prometheus", got.Source)
	assert.Empty(t, got.Note)
}

func TestParseCPUThrottlingAnotherConditionIsNotTheOperatorOldEvidence(t *testing.T) {
	raw := personaListWithStatus(`{"conditions":[{"type":"Ready","status":"True","reason":"Reconciled"}]}`)

	got := parseCPUThrottling([]byte(raw))

	assert.Equal(t, cpuThrottlingUnknown, got.State)
	assert.Equal(t, reasonOperatorPredatesCPUThrottling, got.Reason, "neither the field nor the condition: the operator is old")
}

func TestPrintCPUThrottlingShowsTheStaleCRDNote(t *testing.T) {
	var buf bytes.Buffer

	printCPUThrottling(&buf, parseCPUThrottling([]byte(staleCRDPersona("True", "MetricsServerActive"))))

	out := buf.String()
	assert.Contains(t, out, "CPU throttling detection: on")
	assert.Contains(t, out, "ClusterPersona CRD is older than the operator")
	assert.NotContains(t, out, "operator older than v0.14")
	assert.NotContains(t, out, "—")
}
