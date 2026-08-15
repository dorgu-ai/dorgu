package cli

import (
	"fmt"
	"sort"
	"strings"
)

// Persona → Deployment resolution, CLI side.
//
// Discovery used to list Deployments by app.kubernetes.io/name and fall back to
// the app label, both read from the Deployment object. Helm, kustomize and most
// hand-written YAML label the pod template only, so on a cluster that already
// has apps the lookup matched nothing and heal died with "no Deployment found".
// The chain below mirrors the operator's internal/workload package: same rungs,
// same order, so the CLI and the operator never disagree about which Deployment
// a persona describes.

const (
	labelAppName = "app.kubernetes.io/name"
	labelApp     = "app"
)

// Rung names, used verbatim in error messages.
const (
	rungLabelAppName = "label " + labelAppName
	rungLabelApp     = "label " + labelApp
	rungName         = "metadata.name"
	rungSelector     = "spec.selector.matchLabels"
)

// workloadMatcher is one rung of the fallback chain.
type workloadMatcher struct {
	rung  string
	match func(deploymentSummary, string) bool
}

// workloadMatchChain is the ordered fallback chain. Earlier rungs are more
// explicit statements of intent, so they win over later ones.
var workloadMatchChain = []workloadMatcher{
	{rungLabelAppName, func(d deploymentSummary, name string) bool {
		return d.Labels[labelAppName] == name
	}},
	{rungLabelApp, func(d deploymentSummary, name string) bool {
		return d.Labels[labelApp] == name
	}},
	{rungName, func(d deploymentSummary, name string) bool {
		return d.Name == name
	}},
	{rungSelector, func(d deploymentSummary, name string) bool {
		return d.SelectorLabels[labelAppName] == name || d.SelectorLabels[labelApp] == name
	}},
}

// workloadChainDescription lists the rungs that were tried, so a "found
// nothing" error explains where we looked.
func workloadChainDescription() string {
	names := make([]string, 0, len(workloadMatchChain))
	for _, m := range workloadMatchChain {
		names = append(names, m.rung)
	}
	return strings.Join(names, ", ")
}

// matchesApp reports whether a Deployment belongs to the named app by any rung.
// Ambiguity does not matter here: this answers "is this workload covered?", not
// "which workload do I patch?".
func matchesApp(d deploymentSummary, appName string) bool {
	if appName == "" {
		return false
	}
	for _, m := range workloadMatchChain {
		if m.match(d, appName) {
			return true
		}
	}
	return false
}

// selectDeployment resolves the target Deployment from every Deployment in the
// namespace, walking the fallback chain in order. Zero matches and a rung that
// matches several Deployments are both errors: they name what is in the
// namespace and point at --workload, because guessing is how the wrong workload
// gets patched.
func selectDeployment(ds []deploymentSummary, appName string) (deploymentSummary, error) {
	if appName == "" {
		return deploymentSummary{}, fmt.Errorf("cannot resolve a workload without an app name")
	}

	for _, m := range workloadMatchChain {
		var matched []deploymentSummary
		for _, d := range ds {
			if m.match(d, appName) {
				matched = append(matched, d)
			}
		}

		switch len(matched) {
		case 0:
			continue
		case 1:
			return matched[0], nil
		default:
			return deploymentSummary{}, fmt.Errorf(
				"%d Deployments match app %q by %s (%s); specify one with --workload",
				len(matched), appName, m.rung, strings.Join(deploymentNames(matched), ", "))
		}
	}

	if len(ds) == 0 {
		return deploymentSummary{}, fmt.Errorf(
			"no Deployments in this namespace; nothing to heal for app %q", appName)
	}
	return deploymentSummary{}, fmt.Errorf(
		"no Deployment matches app %q by %s (namespace has: %s); specify one with --workload",
		appName, workloadChainDescription(), strings.Join(deploymentNames(ds), ", "))
}

// deploymentNames returns the sorted names of the given Deployments.
func deploymentNames(ds []deploymentSummary) []string {
	names := make([]string, 0, len(ds))
	for _, d := range ds {
		names = append(names, d.Name)
	}
	sort.Strings(names)
	return names
}
