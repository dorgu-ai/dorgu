package cli

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// B5 fix round, CLI half (review of #63): F2 (which ticket answers this approval),
// F3 (re-read before patching, observe mode), F5 (exact field-manager names),
// F6 (generation, not resourceVersion).

// remWith edits the status of a RemediationAction fixture.
func remWith(t *testing.T, fixture string, edit func(status map[string]any)) string {
	t.Helper()
	var doc map[string]any
	require.NoError(t, json.Unmarshal([]byte(fixture), &doc))
	status, _ := doc["status"].(map[string]any)
	if status == nil {
		status = map[string]any{}
	}
	edit(status)
	doc["status"] = status
	out, err := json.Marshal(doc)
	require.NoError(t, err)
	return string(out)
}

// ticketAt moves the issue time of a fixture ticket; it lives ten minutes from it,
// as the operator's do.
func ticketAt(issued time.Time) func(ticket map[string]any) {
	return func(ticket map[string]any) {
		ticket["issuedAt"] = issued.UTC().Format(time.RFC3339Nano)
		ticket["expiresAt"] = issued.Add(10 * time.Minute).UTC().Format(time.RFC3339)
	}
}

// goodResponses is a cluster whose operator answered the approval with a valid
// ticket for the fixture Deployment.
func goodResponses(t *testing.T) fakeKubectlResponses {
	t.Helper()
	r := baseResponses()
	r.remAfterApprove = withTicket(t, operatorRemediationFixture, "Applying")
	return r
}

func approveWithDeployment(t *testing.T, r fakeKubectlResponses) approveRun {
	t.Helper()
	r.deployment = withIdentity(t, r.deployment)
	return runApprove(t, r)
}

// --- F2 ----------------------------------------------------------------------------

func TestF2_ATicketAlreadyOnAPendingRemediationIsNotTheAnswerToTheApproval(t *testing.T) {
	r := baseResponses()
	// Written by someone with remediationactions/status before anyone approved.
	old := time.Now().Add(-time.Hour)
	r.rem = remWith(t, withTicket(t, operatorRemediationFixture, "Pending", ticketAt(old)), func(s map[string]any) {
		delete(s, "approvedAt")
	})
	// The operator has not answered the approval yet: the stale ticket is all there is.
	r.remAfterApprove = remWith(t, withTicket(t, operatorRemediationFixture, "Approved", ticketAt(old)), func(s map[string]any) {})

	res := approveWithDeployment(t, r)

	require.Error(t, res.err)
	assert.NotContains(t, res.patchLog, "patch deployment", "a ticket from before the approval must never be applied")
	assert.Contains(t, res.out, "apply ticket")
}

func TestF2_ATicketIssuedBeforeTheApprovalWasRecordedIsIgnored(t *testing.T) {
	r := baseResponses()
	r.remAfterApprove = withTicket(t, operatorRemediationFixture, "Applying", ticketAt(fixtureApprovedAt().Add(-time.Hour)))

	res := approveWithDeployment(t, r)

	require.Error(t, res.err)
	assert.NotContains(t, res.patchLog, "patch deployment")
	assert.Contains(t, res.out, "before this approval")
}

func TestF2_OnlyTheTicketedPhaseOrTheApplyingItBecomesCarriesAnAnswer(t *testing.T) {
	for _, phase := range []string{"Verifying", "Completed"} {
		t.Run(phase, func(t *testing.T) {
			r := baseResponses()
			r.remAfterApprove = withTicket(t, operatorRemediationFixture, phase)

			res := approveWithDeployment(t, r)

			require.Error(t, res.err)
			assert.NotContains(t, res.patchLog, "patch deployment")
			assert.Contains(t, res.out, phase)
		})
	}
}

func TestF2_ATicketWithoutAnIssueTimeIsRefused(t *testing.T) {
	r := baseResponses()
	r.remAfterApprove = withTicket(t, operatorRemediationFixture, "Applying", func(ticket map[string]any) { delete(ticket, "issuedAt") })

	res := approveWithDeployment(t, r)

	require.Error(t, res.err)
	assert.NotContains(t, res.patchLog, "patch deployment")
	assert.Contains(t, res.out, "issue time")
}

func TestF2_ATicketIssuedInTheFutureOrWithALongLifeIsRefused(t *testing.T) {
	cases := map[string]func(ticket map[string]any){
		"issued in the future": ticketAt(time.Now().Add(time.Hour)),
		"a century of life": func(ticket map[string]any) {
			ticket["expiresAt"] = time.Now().Add(100 * 365 * 24 * time.Hour).UTC().Format(time.RFC3339)
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			r := baseResponses()
			r.remAfterApprove = withTicket(t, operatorRemediationFixture, "Applying", mutate)

			res := approveWithDeployment(t, r)

			require.Error(t, res.err)
			assert.NotContains(t, res.patchLog, "patch deployment")
		})
	}
}

func TestF2_TheApprovalTimeIsTheServersNotTheLocalClock(t *testing.T) {
	// The ticket is issued after the approvedAt the server holds. Nothing about the
	// local clock may matter, so an approvedAt far from local time still works when
	// the ticket follows it.
	r := baseResponses()
	approved := time.Now().Add(-90 * time.Second).UTC().Truncate(time.Second)
	r.remAfterApprove = remWith(t, withTicket(t, operatorRemediationFixture, "Applying", ticketAt(approved.Add(time.Second))),
		func(s map[string]any) { s["approvedAt"] = approved.Format(time.RFC3339) })

	res := approveWithDeployment(t, r)

	require.NoError(t, res.err, res.out)
	assert.Contains(t, res.patchLog, "patch deployment")
}

// --- F3 ----------------------------------------------------------------------------

func TestF3_TheRemediationActionIsReReadImmediatelyBeforeThePatch(t *testing.T) {
	cases := map[string]string{
		"no longer ticketed": remWith(t0(t), withTicket(t0(t), operatorRemediationFixture, "Applying"), func(s map[string]any) { s["phase"] = "Failed" }),
		"ticket expired meanwhile": withTicket(t0(t), operatorRemediationFixture, "Applying", func(ticket map[string]any) {
			ticket["expiresAt"] = time.Now().Add(-time.Second).UTC().Format(time.RFC3339)
		}),
		"ticket replaced meanwhile": withTicket(t0(t), operatorRemediationFixture, "Applying", func(ticket map[string]any) {
			ticket["issuedAt"] = time.Now().UTC().Format(time.RFC3339Nano)
			ticket["patch"] = map[string]any{"spec": map[string]any{"resources": map[string]any{"limits": map[string]string{"memory": "900Mi"}}}}
		}),
	}
	for name, last := range cases {
		t.Run(name, func(t *testing.T) {
			r := baseResponses()
			r.remSequence = []string{
				operatorRemediationFixture,                            // 1: the preflight fetch (Pending)
				withTicket(t, operatorRemediationFixture, "Applying"), // 2: the first poll after approval: a good ticket
				last, // 3: the re-read right before the patch
			}

			res := approveWithDeployment(t, r)

			require.Error(t, res.err)
			assert.NotContains(t, res.patchLog, "patch deployment", "the ticket changed under the CLI; nothing may be patched")
		})
	}
}

// t0 lets a table of fixtures be built before the subtests start.
func t0(t *testing.T) *testing.T { return t }

func observeModePersona() string {
	return `{"items":[{"metadata":{"name":"dorgu-cluster"},"spec":{"policies":{"selfHealing":{"mode":"observe"}}}}]}`
}

func TestF3_ObserveModeRefusesThePatchEvenWithAValidTicket(t *testing.T) {
	r := goodResponses(t)
	r.clusterPersona = observeModePersona()

	res := approveWithDeployment(t, r)

	require.Error(t, res.err)
	assert.NotContains(t, res.patchLog, "patch deployment")
	assert.Contains(t, res.out, "observe")
}

func TestF3_AnUnreadableClusterPersonaFailsClosed(t *testing.T) {
	r := goodResponses(t)
	r.clusterPersona = "not json"

	res := approveWithDeployment(t, r)

	require.Error(t, res.err)
	assert.NotContains(t, res.patchLog, "patch deployment")
}

func TestF3_ProposeModeAndNoClusterPersonaBothProceed(t *testing.T) {
	for name, doc := range map[string]string{
		"propose": `{"items":[{"spec":{"policies":{"selfHealing":{"mode":"propose"}}}}]}`,
		"none":    `{"items":[]}`,
		"unset":   `{"items":[{"spec":{}}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			r := goodResponses(t)
			r.clusterPersona = doc

			res := approveWithDeployment(t, r)

			require.NoError(t, res.err, res.out)
			assert.Contains(t, res.patchLog, "patch deployment")
		})
	}
}

// --- F6 ----------------------------------------------------------------------------

func TestF6_AMovedResourceVersionWithTheSameGenerationIsAGoodTicket(t *testing.T) {
	r := goodResponses(t)
	// A status write (the Deployment controller's, say) moved the resourceVersion;
	// the spec, and so metadata.generation, did not move.
	r.deployment = strings.Replace(withIdentity(t, r.deployment), `"resourceVersion":"100"`, `"resourceVersion":"4711"`, 1)

	res := runApprove(t, r)

	require.NoError(t, res.err, res.out)
	assert.Contains(t, res.patchLog, "patch deployment")
}

func TestF6_AMovedGenerationIsRefused(t *testing.T) {
	r := goodResponses(t)
	r.deployment = strings.Replace(withIdentity(t, r.deployment), `"generation":3`, `"generation":4`, 1)

	res := runApprove(t, r)

	require.Error(t, res.err)
	assert.NotContains(t, res.patchLog, "patch deployment")
	assert.Contains(t, res.out, "spec changed")
}

func TestF6_ATicketWithoutAnObservedGenerationIsNotBound(t *testing.T) {
	r := baseResponses()
	r.remAfterApprove = withTicket(t, operatorRemediationFixture, "Applying", func(ticket map[string]any) { delete(ticket, "observedGeneration") })

	res := approveWithDeployment(t, r)

	require.Error(t, res.err)
	assert.NotContains(t, res.patchLog, "patch deployment")
	assert.Contains(t, res.out, "not bound to a workload")
}

// --- F5 ----------------------------------------------------------------------------

func TestF5_OnlyTheExactDorguManagerIsDorgusToStrip(t *testing.T) {
	raw := `{"metadata":{"resourceVersion":"7","managedFields":[` +
		managedFieldsEntryJSON("Dorgu", "Update") + `,` +
		managedFieldsEntryJSON("DORGU", "Update") + `,` +
		managedFieldsEntryJSON("dorgu-gitops", "Update") + `,` +
		managedFieldsEntryJSON("dorgu", "Apply") + `,` +
		managedFieldsEntryJSON("dorgu", "Update") + `]}}`
	f, err := parseDeploymentFootprint([]byte(raw))
	require.NoError(t, err)

	patch, needed, err := buildFootprintStripPatch(f, dorguFieldManager)

	require.NoError(t, err)
	require.True(t, needed, "Dorgu's own Update entry is there")
	var doc struct {
		Metadata struct {
			ManagedFields []struct {
				Manager   string `json:"manager"`
				Operation string `json:"operation"`
			} `json:"managedFields"`
		} `json:"metadata"`
	}
	require.NoError(t, json.Unmarshal([]byte(patch), &doc))
	var kept []string
	for _, e := range doc.Metadata.ManagedFields {
		kept = append(kept, e.Manager+"/"+e.Operation)
	}
	assert.ElementsMatch(t, []string{"Dorgu/Update", "DORGU/Update", "dorgu-gitops/Update", "dorgu/Apply"}, kept,
		"only the entry the CLI's own `kubectl patch --field-manager dorgu` left is removed")
}

func TestF5_ADifferentlyCasedManagerIsNotDorgusFootprint(t *testing.T) {
	f, err := parseDeploymentFootprint([]byte(deploymentWithManagers("1", "Dorgu", "DORGU", "dorgu-gitops")))
	require.NoError(t, err)
	assert.False(t, f.has("dorgu"))

	_, needed, err := buildFootprintStripPatch(f, dorguFieldManager)
	require.NoError(t, err)
	assert.False(t, needed, "nothing here is Dorgu's: another manager's ownership must not be rewritten")
}
