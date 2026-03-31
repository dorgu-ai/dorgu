package cli

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestPrintIncidentsListActive(t *testing.T) {
	incidents := []incidentFull{
		makeTestIncident("im-api-oom", "default", "critical", "resource", "OOMKilled", "api-server", "Detected"),
		makeTestIncident("im-web-crash", "default", "warning", "health", "CrashLoopBackOff", "web-frontend", "Investigating"),
	}

	var buf bytes.Buffer
	printIncidentsList(&buf, incidents, false)
	out := buf.String()

	assert.Contains(t, out, "Active Incidents (2)")
	assert.Contains(t, out, "im-api-oom")
	assert.Contains(t, out, "im-web-crash")
	assert.Contains(t, out, "critical")
	assert.Contains(t, out, "warning")
	assert.Contains(t, out, "OOMKilled")
	assert.Contains(t, out, "CrashLoopBackOff")
}

func TestPrintIncidentsListAll(t *testing.T) {
	incidents := []incidentFull{
		makeTestIncident("im-resolved", "default", "info", "resource", "HighCPU", "api", "Resolved"),
	}

	var buf bytes.Buffer
	printIncidentsList(&buf, incidents, true)
	out := buf.String()

	assert.Contains(t, out, "All Incidents (1)")
	assert.Contains(t, out, "im-resolved")
}

func TestPrintIncidentsListEmpty(t *testing.T) {
	var buf bytes.Buffer
	printIncidentsList(&buf, nil, false)
	out := buf.String()

	assert.Contains(t, out, "Active Incidents (0)")
	assert.Contains(t, out, "No incidents found")
}

func TestPrintIncidentDescribe(t *testing.T) {
	inc := makeTestIncident("im-api-oom", "default", "critical", "resource", "OOMKilled", "api-server", "Detected")
	inc.Spec.PersonaRef.Kind = "ApplicationPersona"
	inc.Spec.PersonaRef.Namespace = "default"
	inc.Spec.Detection.Source = "pod-failure-detector"
	inc.Spec.Detection.FirstSeen = "2026-03-27T14:23:00Z"
	inc.Spec.Detection.LastSeen = "2026-03-27T14:28:00Z"
	inc.Spec.Detection.AffectedResources = []struct {
		Kind      string `json:"kind"`
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
		Role      string `json:"role"`
	}{
		{Kind: "Pod", Name: "api-server-7d9f8b6c4-x2k9l", Namespace: "default"},
	}
	inc.Spec.RootCause = &struct {
		Summary      string `json:"summary"`
		Confidence   string `json:"confidence"`
		Provider     string `json:"provider"`
		Contributing []struct {
			Signal string `json:"signal"`
			Detail string `json:"detail"`
		} `json:"contributing"`
	}{
		Summary:    "Container memory limit (256Mi) insufficient",
		Confidence: "0.85",
		Provider:   "rule-engine",
		Contributing: []struct {
			Signal string `json:"signal"`
			Detail string `json:"detail"`
		}{
			{Signal: "OOMKilled", Detail: "Container terminated with OOMKilled reason"},
		},
	}
	inc.Status.OccurrenceCount = 3

	var buf bytes.Buffer
	printIncidentDescribe(&buf, &inc)
	out := buf.String()

	assert.Contains(t, out, "Incident: im-api-oom")
	assert.Contains(t, out, "critical")
	assert.Contains(t, out, "resource")
	assert.Contains(t, out, "OOMKilled")
	assert.Contains(t, out, "ApplicationPersona")
	assert.Contains(t, out, "api-server")
	assert.Contains(t, out, "pod-failure-detector")
	assert.Contains(t, out, "Pod/api-server-7d9f8b6c4-x2k9l")
	assert.Contains(t, out, "Container memory limit (256Mi) insufficient")
	assert.Contains(t, out, "85%")
	assert.Contains(t, out, "rule-engine")
	assert.Contains(t, out, "Contributing Signals:")
	assert.Contains(t, out, "Occurrences: 3")
}

func TestPrintIncidentDescribeMinimal(t *testing.T) {
	inc := makeTestIncident("im-simple", "default", "info", "health", "HighLatency", "web", "Detected")
	inc.Status.OccurrenceCount = 1

	var buf bytes.Buffer
	printIncidentDescribe(&buf, &inc)
	out := buf.String()

	assert.Contains(t, out, "Incident: im-simple")
	assert.Contains(t, out, "info")
	assert.Contains(t, out, "Occurrences: 1")
	assert.NotContains(t, out, "Root Cause:")
	assert.NotContains(t, out, "Resolution:")
}

func TestPrintIncidentDescribeWithResolution(t *testing.T) {
	inc := makeTestIncident("im-resolved", "default", "warning", "resource", "HighCPU", "api", "Resolved")
	inc.Spec.Resolution = &struct {
		Action    string `json:"action"`
		Outcome   string `json:"outcome"`
		AppliedAt string `json:"appliedAt"`
	}{
		Action:    "Increased memory limit to 512Mi",
		Outcome:   "resolved",
		AppliedAt: "2026-03-27T15:00:00Z",
	}
	inc.Status.OccurrenceCount = 2

	var buf bytes.Buffer
	printIncidentDescribe(&buf, &inc)
	out := buf.String()

	assert.Contains(t, out, "Resolution:")
	assert.Contains(t, out, "Increased memory limit to 512Mi")
	assert.Contains(t, out, "resolved")
}

func TestFormatTimestamp(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		contains string
	}{
		{"empty", "", "-"},
		{"invalid", "not-a-date", "not-a-date"},
		{"valid", "2026-03-27T14:23:00Z", "2026-03-27 14:23:00"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := formatTimestamp(tc.input)
			assert.Contains(t, result, tc.contains)
		})
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		name     string
		d        int // seconds
		expected string
	}{
		{"just now", 30, "just now"},
		{"minutes", 300, "5m ago"},
		{"hours", 7200, "2h ago"},
		{"days", 172800, "2d ago"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := formatDuration(secondsToDuration(tc.d))
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestNewIncidentsCmdStructure(t *testing.T) {
	cmd := newIncidentsCmd()
	assert.Equal(t, "incidents", cmd.Use)
	assert.True(t, cmd.HasSubCommands())

	// Verify list subcommand
	listCmd, _, err := cmd.Find([]string{"list"})
	assert.NoError(t, err)
	assert.Equal(t, "list", listCmd.Use)

	// Verify list flags
	assert.NotNil(t, listCmd.Flags().Lookup("namespace"))
	assert.NotNil(t, listCmd.Flags().Lookup("severity"))
	assert.NotNil(t, listCmd.Flags().Lookup("category"))
	assert.NotNil(t, listCmd.Flags().Lookup("phase"))
	assert.NotNil(t, listCmd.Flags().Lookup("all"))
	assert.NotNil(t, listCmd.Flags().Lookup("limit"))

	// Verify describe subcommand
	descCmd, _, err := cmd.Find([]string{"describe"})
	assert.NoError(t, err)
	assert.Equal(t, "describe <name>", descCmd.Use)
	assert.NotNil(t, descCmd.Flags().Lookup("namespace"))
}

// Helpers

func makeTestIncident(name, namespace, severity, category, signal, persona, phase string) incidentFull {
	var inc incidentFull
	inc.Metadata.Name = name
	inc.Metadata.Namespace = namespace
	inc.Metadata.CreationTimestamp = "2026-03-27T14:00:00Z"
	inc.Spec.Severity = severity
	inc.Spec.Category = category
	inc.Spec.Detection.Signal = signal
	inc.Spec.PersonaRef.Name = persona
	inc.Status.Phase = phase
	return inc
}

func secondsToDuration(s int) time.Duration {
	return time.Duration(s) * time.Second
}
