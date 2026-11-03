package cli

// operatorInstallCommand is the one command every "the operator is not
// installed" hint points at. It is the chart the docs install; there is no
// Dorgu wrapper around it.
const operatorInstallCommand = "helm install dorgu-operator oci://ghcr.io/dorgu-ai/dorgu-operator-charts/dorgu-operator " +
	"--namespace dorgu-system --create-namespace"

// operatorInstallHint is operatorInstallCommand phrased as an ErrorWithHint line.
const operatorInstallHint = "To install the operator: " + operatorInstallCommand
