package cli

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dorgu-ai/dorgu/internal/setup"
)

// stubExecutor implements setup.Executor with fixed output for a single kubectl-style call.
type stubExecutor struct {
	out string
	err error
}

func (s *stubExecutor) Run(name string, args ...string) (string, error) {
	return s.out, s.err
}

// TestClusterSetupPreflight_MissingClusterPersonaFails reproduces BUG-4-1:
// `dorgu cluster setup --cluster-persona does-not-exist --driver helm --dry-run`
// must fail preflight before any install plan; ClusterPersona existence must be checked in dry-run.
func TestClusterSetupPreflight_MissingClusterPersonaFails(t *testing.T) {
	t.Parallel()
	ex := &stubExecutor{
		out: `Error from server (NotFound): clusterpersonas.dorgu.io "does-not-exist" not found`,
		err: fmt.Errorf("exit status 1"),
	}
	err := setup.ValidateClusterPersonaExists(ex, "does-not-exist")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does-not-exist")
	assert.True(t, strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "NotFound"),
		"error should indicate missing resource: %v", err)

	// When this gate is wired for dry-run, missing persona fails before helm plan rendering.
	require.True(t, clusterSetupShouldValidateExplicitClusterPersona(true, "does-not-exist"),
		"preflight must run ClusterPersona existence check when --cluster-persona is set in --dry-run")
}

// TestClusterSetupPreflight_ValidPersonaWhenValidated covers the edge case: existing persona passes validation.
func TestClusterSetupPreflight_ValidPersonaWhenValidated(t *testing.T) {
	t.Parallel()
	ex := &stubExecutor{
		out: "qa-helm  development  Ready  1  0  5m",
		err: nil,
	}
	err := setup.ValidateClusterPersonaExists(ex, "qa-helm")
	require.NoError(t, err)
}

// TestClusterSetupPreflight_NoExplicitPersonaSkipsExistenceCheck documents dry-run without --cluster-persona:
// placeholder path is used; explicit ClusterPersona kubectl lookup is not required for an unset flag.
func TestClusterSetupPreflight_NoExplicitPersonaSkipsExistenceCheck(t *testing.T) {
	t.Parallel()
	assert.False(t, clusterSetupShouldValidateExplicitClusterPersona(true, ""),
		"with no --cluster-persona flag, explicit existence check is not applicable to this gate")
	assert.False(t, clusterSetupShouldValidateExplicitClusterPersona(false, ""),
		"with no --cluster-persona flag, explicit existence check is not applicable to this gate")
}

// TestClusterSetupShouldValidateExplicitClusterPersona_expectedBehavior encodes edge cases for the
// explicit-persona gate (BUG-4-1 dry-run + explicit case is covered by TestClusterSetupPreflight_MissingClusterPersonaFails).
func TestClusterSetupShouldValidateExplicitClusterPersona_expectedBehavior(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		dryRun     bool
		explicit   string
		wantVerify bool
	}{
		{
			name:       "non-dry-run with explicit persona",
			dryRun:     false,
			explicit:   "qa-helm",
			wantVerify: true,
		},
		{
			name:       "dry-run without explicit flag placeholder path",
			dryRun:     true,
			explicit:   "",
			wantVerify: false,
		},
		{
			name:       "non-dry-run without explicit flag uses auto-detect elsewhere",
			dryRun:     false,
			explicit:   "",
			wantVerify: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := clusterSetupShouldValidateExplicitClusterPersona(tt.dryRun, tt.explicit)
			assert.Equal(t, tt.wantVerify, got,
				"clusterSetupShouldValidateExplicitClusterPersona(%v, %q)", tt.dryRun, tt.explicit)
		})
	}
}
