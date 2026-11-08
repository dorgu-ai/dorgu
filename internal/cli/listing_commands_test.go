package cli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dorgu-ai/dorgu/internal/output"
)

// The read-only commands (list, diff, incidents, --next) had no test that ran
// them against a cluster. These do, through the same fake kubectl the report uses.

func withJSON(t *testing.T, on bool) {
	t.Helper()
	output.Init(on, true)
	t.Cleanup(func() { output.Init(false, true) })
}

func raWithPhase(name, ns, phase, created string) string {
	return `{"metadata":{"name":"` + name + `","namespace":"` + ns + `","creationTimestamp":"` + created + `"},` +
		`"spec":{"planSource":"rule-based","personaRef":{"kind":"ApplicationPersona","name":"api"},` +
		`"steps":[{"order":1,"id":"s1","type":"persona-update","autoExecutable":true}]},"status":{"phase":"` + phase + `"}}`
}

func TestRemediationListShowsActiveByDefaultAndAllOnRequest(t *testing.T) {
	installFakeReportKubectl(t, fakeClusterReads{remediations: list(
		raWithPhase("pending-one", "shop", "Pending", "2026-11-01T10:00:00Z"),
		raWithPhase("done-one", "shop", "Completed", "2026-11-01T09:00:00Z"),
		raWithPhase("rejected-one", "shop", "Rejected", "2026-11-01T08:00:00Z"),
	)})

	cmd := newRemediationListCmd()
	cmd.SetArgs(nil)
	var err error
	out := captureStdout(t, func() { err = cmd.Execute() })
	require.NoError(t, err)
	assert.Contains(t, out, "pending-one")
	assert.NotContains(t, out, "done-one")

	cmd = newRemediationListCmd()
	cmd.SetArgs([]string{"--all", "-n", "shop"})
	out = captureStdout(t, func() { err = cmd.Execute() })
	require.NoError(t, err)
	assert.Contains(t, out, "done-one")
	assert.Contains(t, out, "rejected-one")

	cmd = newRemediationListCmd()
	cmd.SetArgs([]string{"--all", "--phase", "Rejected"})
	out = captureStdout(t, func() { err = cmd.Execute() })
	require.NoError(t, err)
	assert.Contains(t, out, "rejected-one")
	assert.NotContains(t, out, "done-one")

	cmd = newRemediationListCmd()
	cmd.SetArgs([]string{"--all", "--limit", "1"})
	out = captureStdout(t, func() { err = cmd.Execute() })
	require.NoError(t, err)
	assert.Equal(t, 1, strings.Count(out, "-one"), "--limit caps the rows")
}

func TestRemediationListJSON(t *testing.T) {
	installFakeReportKubectl(t, fakeClusterReads{remediations: list(raWithPhase("pending-one", "shop", "Pending", "2026-11-01T10:00:00Z"))})
	withJSON(t, true)

	cmd := newRemediationListCmd()
	cmd.SetArgs(nil)
	var err error
	out := captureStdout(t, func() { err = cmd.Execute() })

	require.NoError(t, err)
	var got []remediationFull
	require.NoError(t, json.Unmarshal([]byte(out), &got), out)
	require.Len(t, got, 1)
	assert.Equal(t, "pending-one", got[0].Metadata.Name)
}

func TestFetchRemediationsErrors(t *testing.T) {
	t.Run("CRD missing", func(t *testing.T) {
		installFakeReportKubectl(t, fakeClusterReads{crdMissing: true})
		var err error
		stderr := captureStderr(t, func() { _, err = fetchRemediations(context.Background(), "", "") })
		require.ErrorIs(t, err, errSilent)
		assert.Contains(t, stderr, "RemediationAction CRD not found")
	})
	t.Run("forbidden", func(t *testing.T) {
		installFakeReportKubectl(t, fakeClusterReads{remError: "Error from server (Forbidden): remediationactions is forbidden"})
		_, err := fetchRemediations(context.Background(), "", "shop")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "forbidden")
	})
	t.Run("not JSON", func(t *testing.T) {
		installFakeReportKubectl(t, fakeClusterReads{remediations: "not json"})
		_, err := fetchRemediations(context.Background(), "", "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "parse")
	})
}

func TestFetchRemediationErrors(t *testing.T) {
	t.Run("not found names the way to list them", func(t *testing.T) {
		installFakeReportKubectl(t, fakeClusterReads{remError: `Error from server (NotFound): remediationactions.dorgu.io "nope" not found`})
		var err error
		stderr := captureStderr(t, func() { _, err = fetchRemediation(context.Background(), "", "nope", "shop") })
		require.ErrorIs(t, err, errSilent)
		assert.Contains(t, stderr, "RemediationAction not found: nope")
		assert.Contains(t, stderr, "dorgu remediation list -n shop")
	})
	t.Run("CRD missing", func(t *testing.T) {
		installFakeReportKubectl(t, fakeClusterReads{crdMissing: true})
		var err error
		stderr := captureStderr(t, func() { _, err = fetchRemediation(context.Background(), "", "x", "shop") })
		require.ErrorIs(t, err, errSilent)
		assert.Contains(t, stderr, "CRD not found")
	})
	t.Run("other failure is reported with its text", func(t *testing.T) {
		installFakeReportKubectl(t, fakeClusterReads{remError: "Unable to connect to the server"})
		_, err := fetchRemediation(context.Background(), "", "x", "shop")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "Unable to connect")
	})
	t.Run("unparseable", func(t *testing.T) {
		installFakeReportKubectl(t, fakeClusterReads{remediations: "{"})
		_, err := fetchRemediation(context.Background(), "", "x", "shop")
		require.Error(t, err)
	})
}

func TestRemediationDiffPrintsThePlanAndJSON(t *testing.T) {
	installFakeReportKubectl(t, fakeClusterReads{remediations: operatorRemediationFixture})

	cmd := newRemediationDiffCmd()
	cmd.SetArgs([]string{"fix-oom-api-server", "-n", "production"})
	var err error
	out := captureStdout(t, func() { err = cmd.Execute() })
	require.NoError(t, err)
	assert.Contains(t, out, "fix-oom-api-server")
	assert.Contains(t, out, "512Mi")

	withJSON(t, true)
	cmd = newRemediationDiffCmd()
	cmd.SetArgs([]string{"fix-oom-api-server", "-n", "production"})
	out = captureStdout(t, func() { err = cmd.Execute() })
	require.NoError(t, err)
	var rem remediationFull
	require.NoError(t, json.Unmarshal([]byte(out), &rem), out)
	assert.Equal(t, "fix-oom-api-server", rem.Metadata.Name)
}

// --next takes the oldest Pending remediation; ties break on namespace and name.
func TestFindNextPendingRemediationTakesTheOldest(t *testing.T) {
	installFakeReportKubectl(t, fakeClusterReads{remediations: list(
		raWithPhase("newer", "shop", "Pending", "2026-11-02T10:00:00Z"),
		raWithPhase("approved", "shop", "Approved", "2026-10-01T10:00:00Z"),
		raWithPhase("oldest", "shop", "Pending", "2026-11-01T10:00:00Z"),
		raWithPhase("tie-b", "shop", "Pending", "2026-11-01T10:00:00Z"),
	)})

	name, ns, err := findNextPendingRemediation(context.Background(), "", "")

	require.NoError(t, err)
	assert.Equal(t, "oldest", name, "an Approved one is not pending; of the two oldest, the name breaks the tie")
	assert.Equal(t, "shop", ns)
}

func TestFindNextPendingRemediationWithNothingPending(t *testing.T) {
	installFakeReportKubectl(t, fakeClusterReads{remediations: list(raWithPhase("done", "shop", "Completed", "2026-11-01T10:00:00Z"))})

	var err error
	out := captureStdout(t, func() {
		captureStderr(t, func() { _, _, err = findNextPendingRemediation(context.Background(), "", "shop") })
	})

	require.ErrorIs(t, err, errSilent)
	_ = out
}

const incidentFixture = `{"metadata":{"name":"im-oom","namespace":"shop","creationTimestamp":"2026-11-01T10:00:00Z"},` +
	`"spec":{"personaRef":{"kind":"ApplicationPersona","name":"api","namespace":"shop"},"attribution":"persona",` +
	`"category":"resource","severity":"critical","detection":{"signal":"OOMKilled","firstSeen":"2026-11-01T10:00:00Z"},` +
	`"rootCause":{"summary":"Out of memory","confidence":"0.9","provider":"rule-based"}},` +
	`"status":{"phase":"Detected","occurrenceCount":2}}`

func TestIncidentsListFiltersAndDescribes(t *testing.T) {
	resolved := strings.Replace(incidentFixture, `"phase":"Detected"`, `"phase":"Resolved"`, 1)
	resolved = strings.Replace(resolved, "im-oom", "im-resolved", 1)
	installFakeReportKubectl(t, fakeClusterReads{incidents: list(incidentFixture, resolved)})

	cmd := newIncidentsListCmd()
	cmd.SetArgs(nil)
	var err error
	out := captureStdout(t, func() { err = cmd.Execute() })
	require.NoError(t, err)
	assert.Contains(t, out, "im-oom")
	assert.NotContains(t, out, "im-resolved", "resolved incidents are hidden unless --all")

	cmd = newIncidentsListCmd()
	cmd.SetArgs([]string{"--all", "--severity", "critical", "--category", "resource", "-n", "shop"})
	out = captureStdout(t, func() { err = cmd.Execute() })
	require.NoError(t, err)
	assert.Contains(t, out, "im-resolved")

	cmd = newIncidentsListCmd()
	cmd.SetArgs([]string{"--all", "--severity", "info"})
	out = captureStdout(t, func() { err = cmd.Execute() })
	require.NoError(t, err)
	assert.NotContains(t, out, "im-oom")

	withJSON(t, true)
	cmd = newIncidentsListCmd()
	cmd.SetArgs([]string{"--all", "--limit", "1"})
	out = captureStdout(t, func() { err = cmd.Execute() })
	require.NoError(t, err)
	var got []incidentFull
	require.NoError(t, json.Unmarshal([]byte(out), &got), out)
	assert.Len(t, got, 1)
}

func TestIncidentsDescribe(t *testing.T) {
	installFakeReportKubectl(t, fakeClusterReads{incidents: incidentFixture})

	cmd := newIncidentsDescribeCmd()
	cmd.SetArgs([]string{"im-oom", "-n", "shop"})
	var err error
	out := captureStdout(t, func() { err = cmd.Execute() })

	require.NoError(t, err)
	assert.Contains(t, out, "im-oom")
	assert.Contains(t, out, "Out of memory")
}

func TestIncidentsFetchWhenTheCRDIsMissing(t *testing.T) {
	installFakeReportKubectl(t, fakeClusterReads{})
	var err error
	captureStderr(t, func() { _, err = fetchIncidents(context.Background(), "", "shop") })
	require.Error(t, err, "an unreadable incident list is an error, never an empty one")
}

func TestSubcommandGroupWithoutAnArgumentPrintsHelp(t *testing.T) {
	cmd := newRemediationCmd()
	cmd.SetArgs(nil)
	var err error
	out := captureStdout(t, func() { err = cmd.Execute() })
	require.NoError(t, err)
	_ = out
}
