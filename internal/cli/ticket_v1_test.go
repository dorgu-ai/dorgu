package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Ticket v1 (P2-D9, P2-D14, P2-D15): the CLI refuses a ticket that lacks
// specHash, approvedBy or expiresAt, is expired, does not hash to the spec as
// served, or names an approver other than the authenticated user, before it
// touches anything.

// ticketedFixture is the fixture RemediationAction in phase Applying with a valid
// v1 ticket, parsed as the CLI parses it from `kubectl get -o json`.
func ticketedFixture(t *testing.T, mutate ...func(ticket map[string]any)) *remediationFull {
	t.Helper()
	return mustParseRemediation(t, withTicket(t, operatorRemediationFixture, "Applying", mutate...))
}

func TestCheckTicketV1AcceptsAGoodTicket(t *testing.T) {
	rem := ticketedFixture(t)

	require.NoError(t, checkTicketV1(rem, fixtureUser, time.Now()))
}

func TestCheckTicketV1RefusesWhatIsNotAValidTicket(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(ticket map[string]any)
		user    string
		wantErr string
	}{
		{"missing specHash", func(m map[string]any) { delete(m, "specHash") }, fixtureUser, "specHash"},
		{"empty specHash", func(m map[string]any) { m["specHash"] = "" }, fixtureUser, "specHash"},
		{"missing approvedBy", func(m map[string]any) { delete(m, "approvedBy") }, fixtureUser, "approvedBy"},
		{"blank approvedBy", func(m map[string]any) { m["approvedBy"] = "  " }, fixtureUser, "approvedBy"},
		{"missing expiresAt", func(m map[string]any) { delete(m, "expiresAt") }, fixtureUser, "expiresAt"},
		{"unreadable expiresAt", func(m map[string]any) { m["expiresAt"] = "tomorrow" }, fixtureUser, "expiry"},
		{"expired", func(m map[string]any) { m["expiresAt"] = time.Now().Add(-time.Second).UTC().Format(time.RFC3339) }, fixtureUser, "expired"},
		{"wrong specHash", func(m map[string]any) { m["specHash"] = strings.Repeat("0", 64) }, fixtureUser, "changed"},
		{"uppercase specHash", func(m map[string]any) {
			m["specHash"] = strings.ToUpper(m["specHash"].(string))
		}, fixtureUser, "changed"},
		{"approved by somebody else", func(m map[string]any) {}, fixtureOtherUser, "alice@example.com"},
		{"approver is not the user", func(m map[string]any) { m["approvedBy"] = fixtureOtherUser }, fixtureUser, "mallory@example.com"},
		{"no authenticated user", func(m map[string]any) {}, "", "authenticated"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rem := ticketedFixture(t, tt.mutate)

			err := checkTicketV1(rem, tt.user, time.Now())

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
			assert.Contains(t, err.Error(), "nothing", "the refusal says nothing was applied")
		})
	}
}

func TestCheckTicketV1HonoursAnyLifetimeUntilExpiresAt(t *testing.T) {
	// The operator's default ticket lifetime is 15 minutes and chart-configurable
	// up to 24 hours. The CLI's old 10 minute cap is gone (C3 review F-1).
	rem := ticketedFixture(t, func(m map[string]any) {
		m["issuedAt"] = time.Now().Add(-time.Second).UTC().Format(time.RFC3339Nano)
		m["expiresAt"] = time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)
	})

	require.NoError(t, checkTicketV1(rem, fixtureUser, time.Now()))
}

func TestTicketLifetimeCapIsGone(t *testing.T) {
	issued := fixtureApprovedAt().Add(time.Second)
	ticket := &applyTicket{
		IssuedAt:  issued.UTC().Format(time.RFC3339Nano),
		ExpiresAt: issued.Add(15 * time.Minute).UTC().Format(time.RFC3339),
	}
	rem := ticketedFixture(t)
	rem.Status.ApprovedAt = fixtureApprovedAt().Format(time.RFC3339)

	ready, err := ticketAnswersApproval(rem, ticket, time.Now())

	require.NoError(t, err, "a 15 minute ticket is what the operator issues")
	assert.True(t, ready)
}

func TestCheckTicketV1ExpiryIsExactToTheSecond(t *testing.T) {
	expires := time.Now().Add(time.Minute).UTC().Truncate(time.Second)
	rem := ticketedFixture(t, func(m map[string]any) { m["expiresAt"] = expires.Format(time.RFC3339) })

	require.NoError(t, checkTicketV1(rem, fixtureUser, expires.Add(-time.Second)))
	require.Error(t, checkTicketV1(rem, fixtureUser, expires), "at expiresAt the ticket is no longer valid")
}

func TestCheckTicketV1HashesTheSpecAsServedNotATypedStruct(t *testing.T) {
	// Fields this CLI's struct does not know, an empty array, a duration the typed
	// operator struct would respell, and a fractional timestamp: a typed round trip
	// would change every one of them, the served object does not.
	fixture := strings.Replace(operatorRemediationFixture, `"explanation"`,
		`"futureField": {"z": 1, "a": [1.0, 2e3]}, "stepsExtra": [], "healthCheckAfter": "5m", "stamp": "2026-11-14T10:00:00.123456Z", "explanation"`, 1)
	require.NotEqual(t, operatorRemediationFixture, fixture, "fixture drifted")
	rem := mustParseRemediation(t, withTicket(t, fixture, "Applying"))

	require.NoError(t, checkTicketV1(rem, fixtureUser, time.Now()))

	// The same spec edited in a field the CLI does not read breaks the hash.
	good := withTicket(t, fixture, "Applying")
	edited := strings.Replace(good, `"z":1}`, `"z":2}`, 1)
	require.NotEqual(t, good, edited, "fixture drifted")
	require.Error(t, checkTicketV1(mustParseRemediation(t, edited), fixtureUser, time.Now()))
}

func TestCheckTicketV1RefusesAnObjectWithNoSpec(t *testing.T) {
	var rem remediationFull
	require.NoError(t, json.Unmarshal([]byte(`{"status":{"phase":"Applying","applyTicket":{"specHash":"`+strings.Repeat("a", 64)+
		`","approvedBy":"alice@example.com","expiresAt":"`+time.Now().Add(time.Hour).UTC().Format(time.RFC3339)+`"}}}`), &rem))

	err := checkTicketV1(&rem, fixtureUser, time.Now())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "spec")
}

// The operator's golden vectors are the contract (api/v1/testdata). The CLI runs
// them through the function it uses, from the file vendored beside this test.
func TestSpecHashGoldenVectors(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "spechash_vectors.json"))
	require.NoError(t, err)
	var doc struct {
		Vectors []struct {
			Name      string          `json:"name"`
			Spec      json.RawMessage `json:"spec"`
			Canonical string          `json:"canonical"`
			SHA256    string          `json:"sha256"`
		} `json:"vectors"`
	}
	require.NoError(t, json.Unmarshal(raw, &doc))
	require.NotEmpty(t, doc.Vectors)

	for _, v := range doc.Vectors {
		t.Run(v.Name, func(t *testing.T) {
			got, err := specHashFromServedSpec(v.Spec)
			require.NoError(t, err)
			assert.Equal(t, v.SHA256, got)
		})
	}
}

// --- through the commands: nothing is applied under a forged ticket ---

func approveWithTicket(t *testing.T, ticketed string) approveRun {
	t.Helper()
	r := baseResponses()
	r.deployment = withIdentity(t, r.deployment)
	r.remAfterApprove = ticketed
	return runApprove(t, r)
}

func TestApproveAppliesNothingUnderAForgedTicket(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(ticket map[string]any)
		wantOut string
	}{
		{"wrong specHash", func(m map[string]any) { m["specHash"] = strings.Repeat("0", 64) }, "changed"},
		{"expired", func(m map[string]any) {
			m["issuedAt"] = fixtureApprovedAt().Add(time.Second).UTC().Format(time.RFC3339Nano)
			m["expiresAt"] = time.Now().Add(-time.Second).UTC().Format(time.RFC3339)
		}, "expired"},
		{"no specHash", func(m map[string]any) { delete(m, "specHash") }, "specHash"},
		{"no approvedBy", func(m map[string]any) { delete(m, "approvedBy") }, "approvedBy"},
		{"approved by another", func(m map[string]any) { m["approvedBy"] = fixtureOtherUser }, "mallory@example.com"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := approveWithTicket(t, withTicket(t, operatorRemediationFixture, "Applying", tt.mutate))

			require.Error(t, res.err)
			assert.NotContains(t, res.patchLog, "patch deployment", "nothing is applied under a forged ticket")
			assert.Contains(t, res.out, tt.wantOut)
		})
	}
}

func TestApproveRefusesWhenTheSpecChangesBetweenTicketAndPatch(t *testing.T) {
	// The ticket was good when read. Before the write the CLI reads the action again:
	// its spec no longer hashes to the ticket's specHash.
	good := withTicket(t, operatorRemediationFixture, "Applying")
	edited := strings.Replace(good, `"explanation":"`, `"explanation":"edited after the ticket: `, 1)
	require.NotEqual(t, good, edited)
	r := baseResponses()
	r.deployment = withIdentity(t, r.deployment)
	r.rem = operatorRemediationFixture
	// reads: 1 preflight, 2 first poll (good), 3 the re-read right before the patch (edited)
	r.remSequence = []string{operatorRemediationFixture, good, edited}

	res := runApprove(t, r)

	require.Error(t, res.err)
	assert.NotContains(t, res.patchLog, "patch deployment")
}

func TestHealByAnotherUserThanTheApproverIsRefused(t *testing.T) {
	r := baseResponses()
	r.deployment = withIdentity(t, r.deployment)
	r.rem = withTicket(t, replacePhase(operatorRemediationFixture, "Approved"), "Ticketed")
	r.whoami = whoamiJSON(fixtureOtherUser)
	patchLog := writeFakeKubectlDispatch(t, r)

	cmd := newRemediationHealCmd()
	cmd.SetArgs([]string{"fix-oom-api-server", "-n", "production", "--yes"})
	var out strings.Builder
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	var err error
	var stderr string
	stdout := captureStdout(t, func() { stderr = captureStderr(t, func() { err = cmd.Execute() }) })

	require.Error(t, err)
	assert.NotContains(t, readPatchLog(t, patchLog), "patch deployment")
	assert.Contains(t, out.String()+stdout+stderr, "alice@example.com")
}

func TestHealWithoutAnIdentityPatchesNothing(t *testing.T) {
	r := baseResponses()
	r.deployment = withIdentity(t, r.deployment)
	r.rem = withTicket(t, replacePhase(operatorRemediationFixture, "Approved"), "Ticketed")
	r.whoamiError = `error: unknown command "whoami"`
	patchLog := writeFakeKubectlDispatch(t, r)

	cmd := newRemediationHealCmd()
	cmd.SetArgs([]string{"fix-oom-api-server", "-n", "production", "--yes"})
	cmd.SetOut(new(strings.Builder))
	cmd.SetErr(new(strings.Builder))
	var err error
	_ = captureStdout(t, func() { _ = captureStderr(t, func() { err = cmd.Execute() }) })

	require.Error(t, err)
	assert.Empty(t, readPatchLog(t, patchLog))
}
