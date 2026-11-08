package cli

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
)

// F-6: every --kubeconfig said "(default: ~/.kube/config)". The CLI passes no
// --kubeconfig to kubectl when the flag is empty, so kubectl's own resolution
// applies and $KUBECONFIG wins over ~/.kube/config. The help text promised a
// file the CLI never reads on its own.
func TestKubeconfigFlagHelpDescribesKubectlResolution(t *testing.T) {
	var seen int
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		if f := c.Flags().Lookup("kubeconfig"); f != nil {
			seen++
			assert.NotContains(t, f.Usage, "default: ~/.kube/config",
				"%s: --kubeconfig has no default of its own", c.CommandPath())
			assert.Contains(t, f.Usage, "$KUBECONFIG",
				"%s: --kubeconfig help must name the variable kubectl honours first", c.CommandPath())
			assert.Contains(t, f.Usage, "~/.kube/config",
				"%s: --kubeconfig help must still name the fallback", c.CommandPath())
		}
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(rootCmd)

	assert.GreaterOrEqual(t, seen, 10, "the walk should have reached every command that takes --kubeconfig")
}
