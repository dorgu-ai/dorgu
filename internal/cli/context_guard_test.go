package cli

import (
	"io"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProtectedContextPatterns(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want []string
	}{
		{name: "unset falls back to prod", raw: "", want: []string{"prod"}},
		{name: "blank falls back to prod", raw: "  ,  , ", want: []string{"prod"}},
		{name: "single pattern", raw: "live", want: []string{"live"}},
		{name: "list is trimmed and lowercased", raw: " Production , LIVE,eu-main ", want: []string{"production", "live", "eu-main"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, protectedContextPatterns(tt.raw))
		})
	}
}

func TestGuardKubeContext(t *testing.T) {
	tests := []struct {
		name    string
		env     string
		ctx     string
		wantErr bool
	}{
		// Default behaviour: anything containing "prod" is refused.
		{name: "safe kind cluster", ctx: "kind-dorgu-spike"},
		{name: "safe eks", ctx: "arn:aws:eks:us-east-1:123:cluster/dorgu-spike"},
		{name: "prod substring refused", ctx: "my-prod-cluster", wantErr: true},
		{name: "named prod cluster refused", ctx: "acme-prod-cluster", wantErr: true},
		{name: "empty refused", ctx: "", wantErr: true},
		{name: "whitespace refused", ctx: "   ", wantErr: true},
		{name: "uppercase PROD refused", ctx: "PROD-us-east", wantErr: true},

		// Configured patterns replace the default.
		{name: "custom pattern refuses", env: "live", ctx: "eu-live-1", wantErr: true},
		{name: "custom pattern is case-insensitive", env: "LIVE", ctx: "eu-Live-1", wantErr: true},
		{name: "any listed pattern refuses", env: "live,critical", ctx: "payments-critical", wantErr: true},
		{name: "custom list no longer refuses prod", env: "live", ctx: "acme-prod-cluster"},
		{name: "blank env keeps the default", env: " , ", ctx: "acme-prod-cluster", wantErr: true},
		{name: "empty context refused whatever the list", env: "live", ctx: "", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(protectedContextsEnv, tt.env)
			err := guardKubeContext(tt.ctx)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestGuardKubeContextNamesTheMatchAndTheVariable(t *testing.T) {
	t.Setenv(protectedContextsEnv, "")
	err := guardKubeContext("acme-prod-cluster")
	if assert.Error(t, err) {
		assert.Contains(t, err.Error(), `"prod"`)
		assert.Contains(t, err.Error(), protectedContextsEnv)
	}
}

// F-8: the refusal said "refusing to heal" whichever command the user ran, so
// `approve --no-heal` was told Dorgu would not heal when it had never been asked
// to. The first line has to name the verb the user actually typed.
func TestProtectedContextRefusalNamesTheCommandThatWasRun(t *testing.T) {
	t.Setenv(protectedContextsEnv, "")
	healed := strings.Replace(operatorRemediationFixture, `"phase": "Pending"`, `"phase": "Approved"`, 1)

	tests := []struct {
		name       string
		rem        string
		newCmd     func() *cobra.Command
		args       []string
		wantPrefix string
	}{
		{
			name:       "approve --no-heal only approves",
			rem:        operatorRemediationFixture,
			newCmd:     newRemediationApproveCmd,
			args:       []string{"fix-oom-api-server", "-n", "production", "--no-heal", "--yes"},
			wantPrefix: "refusing to approve: ",
		},
		{
			name:       "approve heals by default",
			rem:        operatorRemediationFixture,
			newCmd:     newRemediationApproveCmd,
			args:       []string{"fix-oom-api-server", "-n", "production", "--yes"},
			wantPrefix: "refusing to approve and heal: ",
		},
		{
			name:       "heal heals",
			rem:        healed,
			newCmd:     newRemediationHealCmd,
			args:       []string{"fix-oom-api-server", "-n", "production", "--yes"},
			wantPrefix: "refusing to heal: ",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			writeFakeKubectlDispatch(t, fakeKubectlResponses{
				context:    "acme-prod-cluster",
				rem:        tt.rem,
				persona:    personaFixture,
				deployment: deploymentListFixture,
			})
			cmd := tt.newCmd()
			cmd.SetArgs(tt.args)
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)

			var err error
			stderr := captureStderr(t, func() { err = cmd.Execute() })

			require.Error(t, err)
			assert.Contains(t, stderr+err.Error(), tt.wantPrefix+`kube-context "acme-prod-cluster"`)
			if !strings.Contains(tt.wantPrefix, "heal") {
				assert.NotContains(t, stderr+err.Error(), "heal",
					"a refusal of a plain approval must not talk about healing")
			}
		})
	}
}
