package cli

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

// F-04: a monitoring script could not tell "healthy" from "on fire" from "cannot
// reach the cluster", because every outcome was exit 0 or an undifferentiated 1.
func TestExitCode(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{name: "success", err: nil, want: ExitOK},
		{name: "plain error", err: errors.New("boom"), want: ExitError},
		{name: "silent error", err: errSilent, want: ExitError},
		{name: "unreachable cluster", err: withExitCode(ExitError, errors.New("cannot reach cluster")), want: ExitError},
		{name: "criticals active", err: withExitCode(ExitCritical, nil), want: ExitCritical},
		{name: "health unknown", err: withExitCode(ExitUnknown, nil), want: ExitUnknown},
		{name: "wrapped coded error", err: fmt.Errorf("context: %w", withExitCode(ExitCritical, nil)), want: ExitCritical},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ExitCode(tt.err))
		})
	}
}

// A coded error with no message must stay silent: the command has already printed
// its own formatted output, and Execute must not print an empty error line.
func TestWithExitCodeSilentByDefault(t *testing.T) {
	err := withExitCode(ExitCritical, nil)

	assert.Equal(t, "", err.Error())
	assert.True(t, errors.Is(err, errSilent))
}

// The message of a coded error survives, so a real failure still explains itself.
func TestWithExitCodeKeepsMessage(t *testing.T) {
	err := withExitCode(ExitError, errors.New("cannot reach cluster; check your kubeconfig/context"))

	assert.Contains(t, err.Error(), "cannot reach cluster")
}
