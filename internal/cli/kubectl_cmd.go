package cli

import (
	"context"
	"os/exec"
)

// kubeContextOverride is the global --context flag. Empty means kubectl's own
// current-context.
var kubeContextOverride string

// kubectlBinary is the program kubectlCmd runs.
const kubectlBinary = "kubectl"

// kubectlCmd builds every kubectl invocation the CLI makes.
//
// It is the only place kubectl is executed (TestNoRawKubectlInvocations
// enforces that), so --kubeconfig and --context are honoured by every command
// rather than by the ones that remembered to pass them. A command that reads one
// cluster and writes another is the failure this prevents.
func kubectlCmd(ctx context.Context, kubeconfig string, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, kubectlBinary, kubectlArgs(kubeconfig, kubeContextOverride, args)...)
}

// kubectlArgs prefixes args with the connection flags that are set.
func kubectlArgs(kubeconfig, kubeContext string, args []string) []string {
	prefix := make([]string, 0, 4)
	if kubeconfig != "" {
		prefix = append(prefix, "--kubeconfig", kubeconfig)
	}
	if kubeContext != "" {
		prefix = append(prefix, "--context", kubeContext)
	}
	return append(prefix, args...)
}
