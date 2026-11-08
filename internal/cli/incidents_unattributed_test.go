package cli

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// F-12: the operator records an incident for a crashing workload that no
// ApplicationPersona claims, with spec.attribution "unattributed" and
// spec.personaRef naming the WORKLOAD (no persona of that name exists). The CLI
// never read the attribution, so the PERSONA column showed the workload name as
// if a persona were attached, and `dorgu health --help` promised that nothing
// about an unmonitored Deployment could appear as an incident.
const unattributedIncidentJSON = `{
  "apiVersion": "dorgu.io/v1",
  "kind": "IncidentMemory",
  "metadata": {
    "name": "im-crash-nopersona-crasher-unattributed-crashloopbackoff-de1e390613ac",
    "namespace": "crash-nopersona",
    "creationTimestamp": "2026-11-06T07:50:00Z"
  },
  "spec": {
    "attribution": "unattributed",
    "personaRef": {"kind": "ApplicationPersona", "name": "crasher", "namespace": "crash-nopersona"},
    "category": "health",
    "severity": "critical",
    "detection": {"signal": "CrashLoopBackOff", "source": "pod-failure-detector",
      "firstSeen": "2026-11-06T07:50:00Z", "lastSeen": "2026-11-06T07:55:00Z"},
    "rootCause": {"summary": "The container exits at startup.", "confidence": "0.8", "provider": "ai-enhanced"}
  },
  "status": {"phase": "Detected", "occurrenceCount": 3}
}`

func parseIncident(t *testing.T, raw string) incidentFull {
	t.Helper()
	var inc incidentFull
	require.NoError(t, json.Unmarshal([]byte(raw), &inc))
	return inc
}

func TestIncidentsListSaysWhenNoPersonaIsAttached(t *testing.T) {
	inc := parseIncident(t, unattributedIncidentJSON)

	var buf bytes.Buffer
	printIncidentsList(&buf, []incidentFull{inc}, false)
	out := buf.String()

	assert.Contains(t, out, "crasher", "the workload the incident is attached to")
	assert.Contains(t, out, "no persona", "the column must not present a workload as a persona")
}

func TestIncidentsListShowsAttributedIncidentsAsBefore(t *testing.T) {
	inc := makeTestIncident("im-api-oom", "default", "critical", "resource", "OOMKilled", "api-server", "Detected")

	var buf bytes.Buffer
	printIncidentsList(&buf, []incidentFull{inc}, false)

	assert.NotContains(t, buf.String(), "no persona")
}

func TestIncidentDescribeSaysWhatAnUnattributedIncidentIsAttachedTo(t *testing.T) {
	inc := parseIncident(t, unattributedIncidentJSON)

	var buf bytes.Buffer
	printIncidentDescribe(&buf, &inc)
	out := buf.String()

	assert.Contains(t, out, "Attribution: unattributed")
	assert.Contains(t, out, "Workload:    crasher")
	assert.Contains(t, out, "no ApplicationPersona")
	assert.Contains(t, out, "dorgu persona import -n crash-nopersona")
	assert.NotContains(t, out, "  Kind:      ApplicationPersona\n  Name:      crasher",
		"the persona block must not describe a persona that does not exist")
}

func TestIncidentDescribeAttributedIncidentIsUnchanged(t *testing.T) {
	inc := makeTestIncident("im-api-oom", "default", "critical", "resource", "OOMKilled", "api-server", "Detected")
	inc.Spec.PersonaRef.Kind = "ApplicationPersona"

	var buf bytes.Buffer
	printIncidentDescribe(&buf, &inc)
	out := buf.String()

	assert.NotContains(t, out, "unattributed")
	assert.Contains(t, out, "Persona:")
	assert.Contains(t, out, "  Name:      api-server")
}

func TestHealthIncidentsBriefMarksUnattributedIncidents(t *testing.T) {
	writeFakeKubectl(t, `{"items": [`+unattributedIncidentJSON+`]}`)

	summary := fetchIncidentsBrief(t.Context(), "", "")
	require.NotNil(t, summary)
	require.Len(t, summary.Items, 1)
	var buf bytes.Buffer
	printHealthSummary(&buf, &healthSummary{ActiveIncidents: summary})
	out := buf.String()
	assert.Contains(t, out, "crash-nopersona/crasher")
	assert.Contains(t, out, "no persona")

	raw, err := json.Marshal(summary.Items[0])
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"attribution":"unattributed"`)
}

// The help must say what the operator does, not that nothing can happen.
func TestHelpDoesNotClaimUnmonitoredWorkloadsCannotRaiseIncidents(t *testing.T) {
	health := newHealthCmd().Long
	assert.NotContains(t, health, "nothing about them can appear as an incident")
	assert.Contains(t, health, "unattributed")
	assert.Contains(t, health, "no remediation")

	list := newIncidentsListCmd().Long
	assert.Contains(t, list, "unattributed")
	assert.Contains(t, list, "dorgu persona import")
}
