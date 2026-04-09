package cli

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWatchSubcommands(t *testing.T) {
	subs := watchCmd.Commands()
	names := make([]string, len(subs))
	for i, c := range subs {
		names[i] = c.Use
	}
	for _, expected := range []string{"personas", "incidents", "remediations", "cluster", "events"} {
		assert.True(t, slices.Contains(names, expected), "missing watch subcommand: %s", expected)
	}
}

func TestWatchPersonasCmdFlags(t *testing.T) {
	ns := watchPersonasCmd.Flags().Lookup("namespace")
	assert.NotNil(t, ns)
	assert.Equal(t, "n", ns.Shorthand)

	name := watchPersonasCmd.Flags().Lookup("name")
	assert.NotNil(t, name)
	assert.Equal(t, "", name.DefValue)
}

func TestWatchIncidentsCmdFlags(t *testing.T) {
	ns := watchIncidentsCmd.Flags().Lookup("namespace")
	assert.NotNil(t, ns)
	assert.Equal(t, "n", ns.Shorthand)
}

func TestWatchRemediationsCmdFlags(t *testing.T) {
	ns := watchRemediationsCmd.Flags().Lookup("namespace")
	assert.NotNil(t, ns)
	assert.Equal(t, "n", ns.Shorthand)
}

func TestWatchCmdOperatorURLFlag(t *testing.T) {
	flag := watchCmd.PersistentFlags().Lookup("operator-url")
	assert.NotNil(t, flag)
	assert.Equal(t, "ws://localhost:9090/ws", flag.DefValue)
}
