package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// F-12: `dorgu remediation frobnicate` printed help and exited 0. Cobra only
// produces "unknown command" for the root command, so every subcommand group
// silently accepted nonsense.
func TestNoUnknownSubcommand(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr bool
		wantMsg string
	}{
		{name: "no args is the group's own help", args: nil, wantErr: false},
		{
			name:    "unknown subcommand is an error",
			args:    []string{"frobnicate"},
			wantErr: true,
			wantMsg: `unknown command "frobnicate" for "dorgu remediation"`,
		},
		{
			name:    "near miss suggests the real subcommand",
			args:    []string{"lst"},
			wantErr: true,
			wantMsg: "Did you mean this?",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := &cobra.Command{Use: "dorgu"}
			group := newRemediationCmd()
			root.AddCommand(group)

			err := noUnknownSubcommand(group, tt.args)

			if !tt.wantErr {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantMsg)
		})
	}
}

// Every command that exists only to group subcommands must reject stray
// arguments, so none of them can report success on a typo.
func TestSubcommandGroupsRejectUnknownArgs(t *testing.T) {
	groups := []*cobra.Command{
		newRemediationCmd(),
		newIncidentsCmd(),
		personaCmd,
		clusterCmd,
		watchCmd,
		configCmd,
		platformCmd,
	}

	for _, group := range groups {
		t.Run(group.Name(), func(t *testing.T) {
			require.NotNil(t, group.Args, "%s accepts any argument and would exit 0 on a typo", group.Name())
			err := group.Args(group, []string{"definitely-not-a-command"})
			require.Error(t, err)
			assert.Contains(t, err.Error(), "unknown command")
		})
	}
}

// End to end through cobra: the error must propagate out of Execute so the
// process exits non-zero, rather than being swallowed by the help fallback.
func TestUnknownSubcommandFailsExecution(t *testing.T) {
	root := &cobra.Command{Use: "dorgu", SilenceErrors: true, SilenceUsage: true}
	root.AddCommand(newRemediationCmd())
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"remediation", "frobnicate"})

	err := root.Execute()

	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "unknown command"), "got: %v", err)
	assert.Equal(t, ExitError, ExitCode(err))
}
