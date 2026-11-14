package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// B6 review F-2. KPIS.md section 6: "CLI declines (exit 4, prod guard, ticket
// mismatch): Event on the RemediationAction so they are countable". Only four
// decline types are classified by declineFor; every other refusal to patch is a
// plain fmt.Errorf and leaves no Event, so `dorgu report` cannot count it.
func TestB6Review_EveryTicketMismatchDeclineWritesAnEvent(t *testing.T) {
	cases := map[string]func(ticket map[string]any){
		"ticket names another workload (UID)": func(ticket map[string]any) { ticket["workloadUID"] = "uid-someone-else" },
		"spec generation moved since ticket":  func(ticket map[string]any) { ticket["observedGeneration"] = 99 },
		"ticket not bound to a workload":      func(ticket map[string]any) { ticket["workloadUID"] = "" },
		"ticket names a container we lack":    func(ticket map[string]any) { ticket["container"] = "sidecar" },
		"ticket with no resource change":      func(ticket map[string]any) { ticket["patch"] = map[string]any{} },
		"ticket with an unusable expiry":      func(ticket map[string]any) { ticket["expiresAt"] = "" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			r := declineBase(t)
			r.remAfterApprove = withTicket(t, operatorRemediationFixture, "Applying", mutate)

			res := runApprove(t, r)

			require.Error(t, res.err)
			assert.NotContains(t, res.patchLog, "patch deployment", "the decline itself is correct")
			assert.Len(t, readEventLog(t, res.logPath), 1,
				"a CLI decline (%s) must leave an Event on the RemediationAction or the report cannot count it", name)
		})
	}
}

// B6 review F-3. The detect-to-proposal stage is measured from the incident's
// first sighting to the creation of EVERY RemediationAction that answers it. A
// second proposal for the same incident (after a failed or rejected first one)
// is measured from the original detection, so it adds hours to a machine
// latency whose target is p90 <= 90 s (KPIS Q1).
func TestB6Review_DetectToProposalIsMeasuredOncePerIncident(t *testing.T) {
	h, m := time.Hour, time.Minute
	first := raOpts{name: "ra-first", phase: "Failed", created: 10*h - m, approvedAt: 10*h - 2*m, incident: "inc-1"}
	retry := raOpts{name: "ra-retry", phase: "Pending", created: 1 * h, incident: "inc-1"}
	in := reportInputs{
		Remediations: []reportRA{mustRA(t, first), mustRA(t, retry)},
		Incidents:    []reportIncident{mustIncident(t, incidentJSON("inc-1", 10*h, 10*h, ""))},
	}

	got := buildReportForTest(in, reportNow, reportFixtureWindow).TierQ.Q1TimeToHeal.DetectToProposal

	assert.Equal(t, 1, got.N, "one incident, one detect-to-proposal sample (the machine's first proposal)")
	if got.P90Seconds != nil {
		assert.LessOrEqual(t, *got.P90Seconds, 90.0, "the retry must not add 9 hours to the machine latency")
	}
}

// B6 review F-4. "A cluster read that fails is named in the output rather than
// turned into a zero." The north star and the refusal mix are still printed as
// measured zeros when IncidentMemory cannot be read (RBAC): every credited heal
// becomes "outcome_unconfirmed", and proposal-time refusals are silently absent.
func TestB6Review_UnreadableIncidentsDoNotPrintAMeasuredZero(t *testing.T) {
	extra := `,"approvedAt":"` + reportTimeAgo(3) + `","gate":` + approvalGatePass +
		`,"conditions":[{"type":"HealCredited","status":"True","reason":"WorkloadPatchedByTicket","lastTransitionTime":"` +
		reportTimeAgo(1) + `"}]`
	installFakeReportKubectl(t, fakeClusterReads{
		remediations: list(recentRA("a", "Completed", 4, extra)),
		personas:     list(),
		// incidents are unreadable (empty file = Forbidden in the fake)
	})

	out, _, err := runReportCmd(t, false)

	require.NoError(t, err)
	require.Contains(t, out, "IncidentMemories UNREADABLE")
	assert.NotContains(t, out, "Safe heals: 0", "a credited heal exists; the number cannot be known without the incidents")
	assert.False(t, strings.Contains(out, "no refusals across"),
		"proposal-time refusals live on the incidents; 'no refusals' is not a measurement when they were not read")
}

// B6 review F-5. The protected-context guard exists so the CLI does not write to
// a cluster the user marked as production. The decline path now runs
// `kubectl create -f -` (an Event) against that same cluster, and the command
// still tells the user "Nothing was approved and nothing was changed." Either the
// guard must not write, or the sentence must not claim it.
func TestB6Review_AProtectedContextDeclineDoesNotWriteToTheProtectedCluster(t *testing.T) {
	r := declineBase(t)
	r.context = "acme-prod-cluster"

	res := runApprove(t, r)

	require.Error(t, res.err)
	events := readEventLog(t, res.logPath)
	if len(events) > 0 {
		assert.NotContains(t, res.out, "nothing was changed",
			"%d Event(s) were created on the protected cluster, so 'nothing was changed' is false", len(events))
	}
}
