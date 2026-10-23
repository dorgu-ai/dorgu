package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
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
