package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// F-04: against a cluster it could not reach, `dorgu health` printed an empty node
// table, "Active Incidents: 0" and exited 0. With four active criticals it also
// exited 0.
func TestHealthExit(t *testing.T) {
	critical := &healthSummary{ActiveIncidents: &incidentsSummary{
		Count: 2,
		Items: []incidentBrief{
			{Severity: "critical", Signal: "OOMKilled"},
			{Severity: "warning", Signal: "ContainerRestart"},
		},
	}}
	warningsOnly := &healthSummary{ActiveIncidents: &incidentsSummary{
		Count: 1,
		Items: []incidentBrief{{Severity: "warning", Signal: "ContainerRestart"}},
	}}
	healthy := &healthSummary{ActiveIncidents: &incidentsSummary{}}
	unknown := &healthSummary{ActiveIncidents: &incidentsSummary{
		Unavailable: true,
		Reason:      "the dorgu operator is not installed",
	}}

	tests := []struct {
		name     string
		summary  *healthSummary
		exitFlag bool
		want     int
	}{
		{name: "healthy without the flag", summary: healthy, exitFlag: false, want: ExitOK},
		{name: "criticals without the flag stay quiet", summary: critical, exitFlag: false, want: ExitOK},
		{name: "unknown without the flag stays quiet", summary: unknown, exitFlag: false, want: ExitOK},
		{name: "healthy with the flag", summary: healthy, exitFlag: true, want: ExitOK},
		{name: "warnings are not criticals", summary: warningsOnly, exitFlag: true, want: ExitOK},
		{name: "criticals with the flag", summary: critical, exitFlag: true, want: ExitCritical},
		{name: "unreadable incidents with the flag", summary: unknown, exitFlag: true, want: ExitUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ExitCode(healthExit(tt.summary, tt.exitFlag)))
		})
	}
}

func TestCriticalCount(t *testing.T) {
	var nilSummary *incidentsSummary
	assert.Equal(t, 0, nilSummary.criticalCount())

	s := &incidentsSummary{Items: []incidentBrief{
		{Severity: "critical"},
		{Severity: "Critical"},
		{Severity: "warning"},
		{Severity: ""},
	}}
	assert.Equal(t, 2, s.criticalCount(), "severity comparison must be case insensitive")
}

// An unreadable incident list must never render as "0". Reporting a clean bill of
// health from a failed API call is the failure this guards.
func TestHealthSummaryRendersUnknownIncidents(t *testing.T) {
	var buf bytes.Buffer
	printHealthSummary(&buf, &healthSummary{
		ActiveIncidents: &incidentsSummary{
			Unavailable: true,
			Reason:      "the dorgu operator is not installed, so there are no incident records to read",
		},
		PendingRemediations: &remediationSummary{Count: 0},
	})
	out := buf.String()

	assert.Contains(t, out, "Active Incidents: ")
	assert.Contains(t, out, "unknown")
	assert.NotContains(t, out, "Active Incidents: 0")
	assert.Contains(t, out, "Pending Remediations: 0")
}

func TestUnreadableIncidents(t *testing.T) {
	missingCRD := unreadableIncidents([]byte(
		`error: the server doesn't have a resource type "incidentmemory"`))
	require.True(t, missingCRD.Unavailable)
	assert.Contains(t, missingCRD.Reason, "dorgu operator is not installed")
	assert.Contains(t, missingCRD.Reason, "dorgu cluster setup")

	forbidden := unreadableIncidents([]byte(
		`Error from server (Forbidden): incidentmemories.dorgu.io is forbidden`))
	require.True(t, forbidden.Unavailable)
	assert.Contains(t, forbidden.Reason, "could not be read")
	assert.Contains(t, forbidden.Reason, "Forbidden")

	// Whatever the reason, an unavailable list carries no count.
	assert.Zero(t, forbidden.Count)
	assert.Empty(t, forbidden.Items)
}

// F-09: "CPU: n/a requests / allocatable ( / 3860m)". The left operand was empty
// because the cluster never reported a used figure and the CLI printed it raw.
//
// The source of the figures changed under this in CF6-2 (the CLI computes them
// now rather than reading ClusterPersona, see CR-03), so the invariant is
// re-pinned against the new pipeline: whatever the cluster does or does not
// report, no figure the summary prints has an empty operand.
func TestSaturationNeverPrintsAnEmptyOperand(t *testing.T) {
	tests := []struct {
		name         string
		nodesJSON    string
		podsJSON     string
		metricsJSON  string
		wantContains []string
		wantAbsent   []string
	}{
		{
			name:         "no metrics-server, so no used figure",
			nodesJSON:    twoNodeListFixture,
			podsJSON:     saturationPodListFixture,
			wantContains: []string{"950m / 3860m (25%)", "n/a (no metrics-server)"},
			wantAbsent:   []string{"( / 3860m)", "(0m / 3860m (0%))"},
		},
		{
			name:        "everything reported",
			nodesJSON:   twoNodeListFixture,
			podsJSON:    saturationPodListFixture,
			metricsJSON: nodeMetricsFixture,
			wantContains: []string{
				"950m / 3860m (25%)",
				"38m / 3860m (1%)",
			},
			wantAbsent: []string{"n/a"},
		},
		{
			name:      "an idle cluster reports a real zero, not n/a",
			nodesJSON: twoNodeListFixture,
			podsJSON:  `{"items":[]}`,
			metricsJSON: `{"items":[{"metadata":{"name":"ip-10-0-1-10"},
				"usage":{"cpu":"0","memory":"0"}}]}`,
			wantContains: []string{"0m / 3860m (0%)"},
			wantAbsent:   []string{"n/a"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			capacity, err := parseNodeCapacity([]byte(tt.nodesJSON))
			require.NoError(t, err)
			claims, err := parsePodClaims([]byte(tt.podsJSON))
			require.NoError(t, err)

			var usage *nodeUsage
			if tt.metricsJSON != "" {
				usage, err = parseNodeUsage([]byte(tt.metricsJSON))
				require.NoError(t, err)
			}

			var buf bytes.Buffer
			printHealthSummary(&buf, &healthSummary{
				ResourceSaturation: buildSaturation(capacity, claims, usage, nil)})
			out := buf.String()

			for _, want := range tt.wantContains {
				assert.Contains(t, out, want)
			}
			for _, absent := range tt.wantAbsent {
				assert.NotContains(t, out, absent)
			}
			// No rendered figure may have an empty operand.
			for _, line := range strings.Split(out, "\n") {
				if !strings.Contains(line, " / ") {
					continue
				}
				assert.NotContains(t, line, "( /")
				assert.NotContains(t, line, "/ )")
			}
		})
	}
}
