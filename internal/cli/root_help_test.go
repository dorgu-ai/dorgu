package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// F-21: `dorgu --help` described a manifest generator and gave only
// generate/init examples, contradicting dorgu.run, both READMEs and the docs.
// The first thing a stranger reads has to describe the product that ships.
func TestRootHelpLeadsWithSelfHealing(t *testing.T) {
	assert.Equal(t, "Open-source AI SRE for Kubernetes", rootCmd.Short)

	long := rootCmd.Long

	for _, want := range []string{
		"dorgu health",
		"dorgu incidents list",
		"dorgu remediation diff",
		"dorgu remediation approve",
		"dorgu persona import",
	} {
		assert.Contains(t, long, want, "root help must show the self-healing loop")
	}

	assert.Contains(t, long, "nothing is applied without your approval",
		"the approval promise belongs in the first thing a user reads")

	// Manifest generation was removed in v0.13.0; help must not advertise it.
	assert.NotContains(t, long, "dorgu generate")

	// The undocumented prerequisite the clean-room tester hit (F-22).
	assert.Contains(t, long, "kubectl on your PATH")
}
