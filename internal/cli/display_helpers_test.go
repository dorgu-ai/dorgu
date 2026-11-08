package cli

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

// The pure formatters behind persona status and `version`, and the error wrappers
// the heal path unwraps. None of them touch a cluster.

func TestPersonaDisplayFormatters(t *testing.T) {
	assert.Contains(t, formatHealth("Healthy", ""), "Healthy")
	assert.Contains(t, formatHealth("Degraded", "2 pods down"), "(2 pods down)")

	assert.Contains(t, formatValidation(true), "Passed")
	assert.Contains(t, formatValidation(false), "Failed")

	for _, status := range []string{"True", "False", "Unknown"} {
		assert.Contains(t, formatConditionStatus(status), status)
	}

	for severity, glyph := range map[string]string{"error": "✗", "warning": "⚠", "info": "ℹ", "other": "•"} {
		assert.Contains(t, getIssueSeverityIcon(severity), glyph, severity)
	}

	for _, status := range []string{"Synced", "OutOfSync", "Unknown"} {
		assert.Contains(t, formatArgoSyncStatus(status), status)
	}
	for _, status := range []string{"Healthy", "Degraded", "Progressing", "Suspended", "Missing", "Unknown"} {
		assert.Contains(t, formatArgoHealthStatus(status), status)
	}

	assert.Equal(t, "abcdef12", truncateRevision("abcdef1234567890"))
	assert.Equal(t, "abc", truncateRevision("abc"))
}

func TestEffectiveVersionPrefersTheLinkedVersion(t *testing.T) {
	saved := versionInfo
	t.Cleanup(func() { versionInfo = saved })

	SetVersionInfo("v9.9.9", "abc123", "2026-11-08")
	assert.Equal(t, "v9.9.9", effectiveVersion())
	assert.Equal(t, "abc123", versionInfo.Commit)
	assert.Equal(t, "2026-11-08", versionInfo.Date)

	SetVersionInfo("dev", "", "")
	assert.NotEmpty(t, effectiveVersion(), "falls back to the build info, or dev")
}

func TestHealErrorsUnwrapToTheirCause(t *testing.T) {
	cause := errors.New("boom")

	foot := &footprintError{Namespace: "ns", Deployment: "d", Cause: cause}
	assert.ErrorIs(t, foot, cause)
	assert.Contains(t, foot.Error(), "ns/d")

	rec := &recordError{Namespace: "ns", Name: "ra", Cause: cause}
	assert.ErrorIs(t, rec, cause)
	assert.Contains(t, rec.Error(), "ns/ra")
}
