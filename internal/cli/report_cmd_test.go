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

	"github.com/dorgu-ai/dorgu/internal/output"
)

// fakeClusterReads is what the fake kubectl answers `get <kind>` with. A kind left
// empty fails, the way an uninstalled CRD or a forbidden read does.
type fakeClusterReads struct {
	remediations, incidents, events, personas, clusterpersonas, deployments string
	failEventsWith                                                          string
	crdMissing                                                              bool

	// remError, when set, is what `get remediationaction` fails with.
	remError string
}

func list(items ...string) string { return `{"items":[` + strings.Join(items, ",") + `]}` }

func installFakeReportKubectl(t *testing.T, r fakeClusterReads) {
	t.Helper()
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(p, []byte(body), 0o600))
		return p
	}
	files := map[string]string{
		"rem": write("rem.json", r.remediations), "inc": write("inc.json", r.incidents),
		"ev": write("ev.json", r.events), "persona": write("persona.json", r.personas),
		"cp": write("cp.json", r.clusterpersonas), "deploy": write("deploy.json", r.deployments),
		"evErr": write("ev.err", r.failEventsWith), "remErr": write("rem.err", r.remError),
	}
	remFails := "0"
	if r.remError != "" {
		remFails = "1"
	}
	missing := "0"
	if r.crdMissing {
		missing = "1"
	}
	script := `#!/bin/sh
if [ "$1" = config ] && [ "$2" = current-context ]; then echo kind-demo; exit 0; fi
kind=""
for a in "$@"; do
  case "$a" in
    remediationaction) [ -z "$kind" ] && kind=rem ;;
    incidentmemory) [ -z "$kind" ] && kind=inc ;;
    events) [ -z "$kind" ] && kind=ev ;;
    applicationpersona) [ -z "$kind" ] && kind=persona ;;
    clusterpersona) [ -z "$kind" ] && kind=cp ;;
    deployment) [ -z "$kind" ] && kind=deploy ;;
  esac
done
case "$kind" in
  rem) if [ ` + remFails + ` = 1 ]; then cat ` + files["remErr"] + ` >&2; exit 1; fi; if [ ` + missing + ` = 1 ]; then echo "error: the server doesn't have a resource type \"remediationaction\"" >&2; exit 1; fi; cat ` + files["rem"] + ` ;;
  inc) [ -s ` + files["inc"] + ` ] || { echo 'Error from server (Forbidden): incidentmemories is forbidden' >&2; exit 1; }; cat ` + files["inc"] + ` ;;
  ev) [ -s ` + files["ev"] + ` ] || { cat ` + files["evErr"] + ` >&2; exit 1; }; cat ` + files["ev"] + ` ;;
  persona) cat ` + files["persona"] + ` ;;
  cp) [ -s ` + files["cp"] + ` ] || exit 1; cat ` + files["cp"] + ` ;;
  deploy) cat ` + files["deploy"] + ` ;;
esac
exit 0
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "kubectl"), []byte(script), 0o755))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func runReportCmd(t *testing.T, jsonOut bool, args ...string) (string, string, error) {
	t.Helper()
	output.Init(jsonOut, true)
	t.Cleanup(func() { output.Init(false, true) })
	cmd := newReportCmd()
	cmd.SetArgs(args)
	var out bytes.Buffer
	cmd.SetOut(&out)
	var err error
	stderr := captureStderr(t, func() { err = cmd.Execute() })
	return out.String(), stderr, err
}

// raNow builds a RemediationAction created `h` hours before the real clock, which
// is what the command windows against.
func recentRA(name, phase string, hoursAgo int, extra string) string {
	created := reportTimeAgo(hoursAgo)
	return `{"metadata":{"name":"` + name + `","namespace":"shop","creationTimestamp":"` + created + `"},` +
		`"spec":{"planSource":"rule-based","incidentRef":{"name":"inc-` + name + `","namespace":"shop"}},` +
		`"status":{"phase":"` + phase + `"` + extra + `}}`
}

func TestReportCommandReadsTheClusterAndPrintsJSON(t *testing.T) {
	installFakeReportKubectl(t, fakeClusterReads{
		remediations: list(
			recentRA("a", "Rejected", 3, `,"conditions":[{"type":"RejectionReason","status":"True","reason":"TooRisky","lastTransitionTime":"`+reportTimeAgo(2)+`"}]`),
			recentRA("b", "Pending", 1, ``),
			recentRA("old", "Pending", 24*30, ``),
		),
		incidents: list(`{"metadata":{"name":"i","namespace":"shop","creationTimestamp":"` + reportTimeAgo(5) + `"},"spec":{"detection":{"firstSeen":"` + reportTimeAgo(5) + `"}}}`),
		events: list(`{"reason":"DorguCLIDeclined","lastTimestamp":"` + reportTimeAgo(0) + `","count":2,` +
			`"involvedObject":{"kind":"RemediationAction","name":"a"}}`),
		personas:        list(`{"metadata":{"name":"p1"}}`, `{"metadata":{"name":"p2"}}`),
		clusterpersonas: list(`{"spec":{"policies":{"selfHealing":{"mode":"observe"}}}}`, `{"spec":{}}`),
		deployments: list(`{"spec":{"template":{"spec":{"containers":[` +
			`{"image":"ghcr.io/dorgu-ai/dorgu-operator:v0.13.0"}]}}}}`),
	})

	out, _, err := runReportCmd(t, true, "--since", "7d")

	require.NoError(t, err)
	var doc reportDoc
	require.NoError(t, json.Unmarshal([]byte(out), &doc), out)
	assert.Equal(t, reportSchema, doc.Schema)
	assert.Equal(t, "7d", doc.Window.Since)
	assert.Equal(t, 2, doc.HealAccounting.Total, "the 30 day old RemediationAction is outside the window")
	assert.Equal(t, 1, doc.TierQ.Q2Precision.RejectionsByReason["too-risky"])
	assert.Equal(t, map[string]int{"DorguCLIDeclined": 2}, doc.CLIDeclines.ByReason)
	assert.Equal(t, "kind-demo", doc.Cluster.Context)
	assert.Equal(t, "all", doc.Cluster.Namespace)
	require.NotNil(t, doc.Cluster.OperatorVersion)
	assert.Equal(t, "v0.13.0", *doc.Cluster.OperatorVersion)
	require.NotNil(t, doc.Cluster.Personas)
	assert.Equal(t, 2, *doc.Cluster.Personas)
	assert.Equal(t, []string{"observe", "propose"}, doc.Cluster.SelfHealing, "an unset mode is the default, propose")
	assert.True(t, doc.Sources.RemediationActions.OK)
	assert.Equal(t, 3, doc.Sources.RemediationActions.Count)
}

func TestReportCommandPrintsTextByDefault(t *testing.T) {
	installFakeReportKubectl(t, fakeClusterReads{
		remediations: list(recentRA("b", "Pending", 1, ``)),
		incidents:    list(),
		events:       list(),
		personas:     list(),
	})

	out, _, err := runReportCmd(t, false, "-n", "shop")

	require.NoError(t, err)
	assert.Contains(t, out, "Dorgu report (dorgu.report/v1)")
	assert.Contains(t, out, "namespace scope: shop")
	assert.Contains(t, out, "A6 approval rate:     no data")
	assert.Contains(t, out, "none recorded in the window", "an Events list that was read and is empty is a measured zero")
}

func TestReportCommandSaysWhichReadsFailedInsteadOfPrintingZeros(t *testing.T) {
	installFakeReportKubectl(t, fakeClusterReads{
		remediations: list(recentRA("b", "Pending", 1, ``)),
		// incidents and events are unreadable.
		failEventsWith: "Error from server (Forbidden): events is forbidden",
		personas:       list(),
	})

	out, _, err := runReportCmd(t, false)

	require.NoError(t, err)
	assert.Contains(t, out, "IncidentMemories UNREADABLE")
	assert.Contains(t, out, "Events UNREADABLE")
	assert.Contains(t, out, "no data: the Events could not be read")
}

func TestReportCommandNeedsTheCRD(t *testing.T) {
	installFakeReportKubectl(t, fakeClusterReads{crdMissing: true})

	_, stderr, err := runReportCmd(t, false)

	require.ErrorIs(t, err, errSilent)
	assert.Contains(t, stderr, "RemediationAction CRD not found")
}

func TestReportCommandRejectsABadWindow(t *testing.T) {
	_, _, err := runReportCmd(t, false, "--since", "forever")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--since")

	_, _, err = runReportCmd(t, false, "--since", "-3d")
	require.Error(t, err)
}

func TestImageTag(t *testing.T) {
	assert.Equal(t, "v0.13.0", imageTag("ghcr.io/dorgu-ai/dorgu-operator:v0.13.0"))
	assert.Equal(t, "v1", imageTag("localhost:5000/dorgu:v1"))
	assert.Equal(t, "", imageTag("localhost:5000/dorgu"), "a registry port is not a tag")
	assert.Equal(t, "", imageTag("ghcr.io/x/y@sha256:abc"), "a digest is not a tag")
	assert.Equal(t, "", imageTag("dorgu"))
}

func TestReportIsRegisteredOnTheRootCommand(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"report"})
	require.NoError(t, err)
	assert.Equal(t, "report", cmd.Name())
}
