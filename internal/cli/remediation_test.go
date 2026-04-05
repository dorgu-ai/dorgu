package cli

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

func makeTestRemediation(name, namespace, phase, actionType, severity, confidence, persona string) remediationFull {
	var r remediationFull
	r.Metadata.Name = name
	r.Metadata.Namespace = namespace
	r.Metadata.CreationTimestamp = "2026-04-01T10:00:00Z"
	r.Spec.ActionType = actionType
	r.Spec.Severity = severity
	r.Spec.Confidence = confidence
	r.Spec.PersonaRef.Name = persona
	r.Status.Phase = phase
	return r
}

func TestPrintRemediationListActive(t *testing.T) {
	remediations := []remediationFull{
		makeTestRemediation("fix-oom-api", "production", "Pending", "resource", "critical", "85%", "api-server"),
		makeTestRemediation("fix-crash-web", "default", "Approved", "restart", "warning", "72%", "web-frontend"),
	}

	var buf bytes.Buffer
	printRemediationList(&buf, remediations, false)
	out := buf.String()

	assert.Contains(t, out, "Active Remediations (2)")
	assert.Contains(t, out, "fix-oom-api")
	assert.Contains(t, out, "fix-crash-web")
	assert.Contains(t, out, "critical")
	assert.Contains(t, out, "warning")
	assert.Contains(t, out, "Pending")
	assert.Contains(t, out, "Approved")
	assert.Contains(t, out, "api-server")
	assert.Contains(t, out, "web-frontend")
	assert.Contains(t, out, "85%")
	assert.Contains(t, out, "72%")
}

func TestPrintRemediationListAll(t *testing.T) {
	remediations := []remediationFull{
		makeTestRemediation("fix-completed", "default", "Completed", "resource", "info", "90%", "api"),
		makeTestRemediation("fix-rejected", "default", "Rejected", "restart", "warning", "60%", "web"),
	}

	var buf bytes.Buffer
	printRemediationList(&buf, remediations, true)
	out := buf.String()

	assert.Contains(t, out, "All Remediations (2)")
	assert.Contains(t, out, "fix-completed")
	assert.Contains(t, out, "fix-rejected")
}

func TestPrintRemediationListEmpty(t *testing.T) {
	var buf bytes.Buffer
	printRemediationList(&buf, nil, false)
	out := buf.String()

	assert.Contains(t, out, "Active Remediations (0)")
	assert.Contains(t, out, "No remediations found")
}

func TestPrintRemediationDiffFull(t *testing.T) {
	r := makeTestRemediation("fix-oom-api", "production", "Pending", "resource", "critical", "85%", "api-server")
	r.Spec.PersonaRef.Kind = "ApplicationPersona"
	r.Spec.PersonaRef.Namespace = "production"
	r.Spec.IncidentRef.Name = "im-api-server-oom-20260401"
	r.Spec.IncidentRef.Namespace = "production"
	r.Spec.Explanation = "Container api-server is being OOM-killed because its memory limit\n(256Mi) is insufficient. Increasing to 512Mi provides 2x headroom."
	r.Spec.Action.PrePatchState = "memory:\n  \"256Mi\"\n"
	r.Spec.Action.Patch = "memory:\n  \"512Mi\"\n"
	r.Spec.Rollback = &struct {
		Automatic      bool   `json:"automatic"`
		TimeoutMinutes int    `json:"timeoutMinutes"`
		Condition      string `json:"condition"`
	}{
		Automatic:      true,
		TimeoutMinutes: 10,
		Condition:      "health degrades",
	}

	var buf bytes.Buffer
	printRemediationDiff(&buf, &r)
	out := buf.String()

	assert.Contains(t, out, "Remediation: fix-oom-api")
	assert.Contains(t, out, "ApplicationPersona/api-server (production)")
	assert.Contains(t, out, "critical")
	assert.Contains(t, out, "85%")
	assert.Contains(t, out, "im-api-server-oom-20260401")
	assert.Contains(t, out, "Explanation:")
	assert.Contains(t, out, "OOM-killed")
	assert.Contains(t, out, "256Mi")
	assert.Contains(t, out, "512Mi")
	assert.Contains(t, out, "Rollback:")
	assert.Contains(t, out, "Automatic after 10m")
	assert.Contains(t, out, "health degrades")
	assert.Contains(t, out, "dorgu remediation approve fix-oom-api -n production")
	assert.Contains(t, out, "dorgu remediation reject fix-oom-api -n production")
}

func TestPrintRemediationDiffJSON(t *testing.T) {
	r := makeTestRemediation("fix-oom-api", "production", "Pending", "resource", "critical", "85%", "api-server")

	var buf bytes.Buffer
	printRemediationDiff(&buf, &r)
	out := buf.String()

	assert.Contains(t, out, "Remediation: fix-oom-api")
}

func TestPrintRemediationDiffNoActionsWhenNotPending(t *testing.T) {
	r := makeTestRemediation("fix-completed", "default", "Completed", "resource", "info", "90%", "api")

	var buf bytes.Buffer
	printRemediationDiff(&buf, &r)
	out := buf.String()

	assert.NotContains(t, out, "Actions:")
	assert.NotContains(t, out, "dorgu remediation approve")
}

func TestPrintRemediationDiffManualRollback(t *testing.T) {
	r := makeTestRemediation("fix-manual", "default", "Pending", "resource", "warning", "70%", "api")
	r.Spec.Rollback = &struct {
		Automatic      bool   `json:"automatic"`
		TimeoutMinutes int    `json:"timeoutMinutes"`
		Condition      string `json:"condition"`
	}{
		Automatic: false,
	}

	var buf bytes.Buffer
	printRemediationDiff(&buf, &r)
	out := buf.String()

	assert.Contains(t, out, "Manual rollback required")
}

func TestNewRemediationCmdStructure(t *testing.T) {
	cmd := newRemediationCmd()
	assert.Equal(t, "remediation", cmd.Use)
	assert.Equal(t, []string{"rem"}, cmd.Aliases)
	assert.True(t, cmd.HasSubCommands())

	// Verify list subcommand
	listCmd, _, err := cmd.Find([]string{"list"})
	assert.NoError(t, err)
	assert.Equal(t, "list", listCmd.Use)
	assert.NotNil(t, listCmd.Flags().Lookup("namespace"))
	assert.NotNil(t, listCmd.Flags().Lookup("phase"))
	assert.NotNil(t, listCmd.Flags().Lookup("severity"))
	assert.NotNil(t, listCmd.Flags().Lookup("all"))
	assert.NotNil(t, listCmd.Flags().Lookup("limit"))
	assert.NotNil(t, listCmd.Flags().Lookup("kubeconfig"))

	// Verify diff subcommand
	diffCmd, _, err := cmd.Find([]string{"diff"})
	assert.NoError(t, err)
	assert.Equal(t, "diff <name>", diffCmd.Use)
	assert.NotNil(t, diffCmd.Flags().Lookup("namespace"))
	assert.NotNil(t, diffCmd.Flags().Lookup("kubeconfig"))

	// Verify approve subcommand
	approveCmd, _, err := cmd.Find([]string{"approve"})
	assert.NoError(t, err)
	assert.Equal(t, "approve [name]", approveCmd.Use)
	assert.NotNil(t, approveCmd.Flags().Lookup("namespace"))
	assert.NotNil(t, approveCmd.Flags().Lookup("reason"))
	assert.NotNil(t, approveCmd.Flags().Lookup("next"))
	assert.NotNil(t, approveCmd.Flags().Lookup("kubeconfig"))

	// Verify reject subcommand
	rejectCmd, _, err := cmd.Find([]string{"reject"})
	assert.NoError(t, err)
	assert.Equal(t, "reject <name>", rejectCmd.Use)
	assert.NotNil(t, rejectCmd.Flags().Lookup("namespace"))
	assert.NotNil(t, rejectCmd.Flags().Lookup("reason"))
	assert.NotNil(t, rejectCmd.Flags().Lookup("kubeconfig"))
}

func TestSeverityRank(t *testing.T) {
	tests := []struct {
		severity string
		rank     int
	}{
		{"critical", 3},
		{"Critical", 3},
		{"warning", 2},
		{"info", 1},
		{"unknown", 0},
		{"", 0},
	}
	for _, tc := range tests {
		t.Run(tc.severity, func(t *testing.T) {
			assert.Equal(t, tc.rank, severityRank(tc.severity))
		})
	}
}

func TestActiveRemediationPhases(t *testing.T) {
	assert.True(t, activeRemediationPhases["Pending"])
	assert.True(t, activeRemediationPhases["Approved"])
	assert.True(t, activeRemediationPhases["Applying"])
	assert.True(t, activeRemediationPhases["Verifying"])
	assert.True(t, activeRemediationPhases["Failed"])
	assert.False(t, activeRemediationPhases["Completed"])
	assert.False(t, activeRemediationPhases["Rejected"])
	assert.False(t, activeRemediationPhases["Expired"])
	assert.False(t, activeRemediationPhases["RolledBack"])
}

func TestPrintRemediationDiffDefaultRollbackTimeout(t *testing.T) {
	r := makeTestRemediation("fix-default-timeout", "default", "Pending", "resource", "warning", "70%", "api")
	r.Spec.Rollback = &struct {
		Automatic      bool   `json:"automatic"`
		TimeoutMinutes int    `json:"timeoutMinutes"`
		Condition      string `json:"condition"`
	}{
		Automatic:      true,
		TimeoutMinutes: 0,
	}

	var buf bytes.Buffer
	printRemediationDiff(&buf, &r)
	out := buf.String()

	assert.Contains(t, out, "Automatic after 10m")
}
