package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	appsv1 "k8s.io/api/apps/v1"

	"github.com/dorgu-ai/dorgu/internal/output"
)

// F-02b: with three unmonitored Deployments broken, `dorgu health` reported
// "Active Incidents: 2" (both from the docs' toy app) and never mentioned the
// others. It presented a blind spot as health. A reliability tool has to name
// what it cannot see, so this section counts the Deployments with no
// ApplicationPersona and points at the command that fixes it.

// maxUnmonitoredListed caps the names printed in the human-readable section.
// The count and the JSON output are never capped, and the print says how many
// were left out.
const maxUnmonitoredListed = 10

// systemNamespaces are skipped when no namespace was requested. Cluster
// add-ons are not what the user forgot to onboard, and burying three real apps
// under forty control-plane Deployments would recreate the finding this fixes.
// An explicit -n overrides the skip.
var systemNamespaces = map[string]bool{
	"kube-system":          true,
	"kube-public":          true,
	"kube-node-lease":      true,
	"local-path-storage":   true,
	"dorgu-system":         true,
	"gatekeeper-system":    true,
	"kubernetes-dashboard": true,
}

// unmonitoredSummary reports Deployments that no ApplicationPersona covers.
type unmonitoredSummary struct {
	Count int `json:"count"`
	// Items lists every uncovered Deployment, uncapped.
	Items []unmonitoredDeployment `json:"items"`
	// Namespaces lists the namespaces those Deployments live in, so the import
	// hint can name each one.
	Namespaces []string `json:"namespaces"`
	// PersonaCRDMissing reports that ApplicationPersona is not installed, which
	// makes "nothing is monitored" a statement about the operator, not the apps.
	PersonaCRDMissing bool `json:"personaCRDMissing,omitempty"`
}

type unmonitoredDeployment struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

// personaBrief is the minimum needed to decide whether a persona covers a
// Deployment: its namespace and the spec.name the match chain resolves by.
type personaBrief struct {
	Namespace string
	AppName   string
}

// fetchUnmonitored lists Deployments in scope with no matching ApplicationPersona.
func fetchUnmonitored(ctx context.Context, kubeconfig, namespace string) *unmonitoredSummary {
	deployments, err := fetchDeploymentsInScope(ctx, kubeconfig, namespace)
	if err != nil {
		output.Warn("Could not list Deployments: " + err.Error())
		return nil
	}
	if namespace == "" {
		deployments = withoutSystemNamespaces(deployments)
	}

	personas, crdMissing, err := fetchPersonaBriefs(ctx, kubeconfig, namespace)
	if err != nil {
		output.Warn("Could not list ApplicationPersonas: " + err.Error())
		return nil
	}

	summary := buildUnmonitoredSummary(deployments, personas)
	summary.PersonaCRDMissing = crdMissing
	return summary
}

// buildUnmonitoredSummary is the pure core: which Deployments no persona covers.
func buildUnmonitoredSummary(deployments []appsv1.Deployment, personas []personaBrief) *unmonitoredSummary {
	byNamespace := make(map[string][]string)
	for _, p := range personas {
		byNamespace[p.Namespace] = append(byNamespace[p.Namespace], p.AppName)
	}

	summary := &unmonitoredSummary{}
	namespaces := make(map[string]bool)

	for i := range deployments {
		deploy := &deployments[i]
		if isMonitored(deploy, byNamespace[deploy.Namespace]) {
			continue
		}
		summary.Items = append(summary.Items, unmonitoredDeployment{
			Namespace: deploy.Namespace,
			Name:      deploy.Name,
		})
		namespaces[deploy.Namespace] = true
	}

	sort.Slice(summary.Items, func(i, j int) bool {
		if summary.Items[i].Namespace != summary.Items[j].Namespace {
			return summary.Items[i].Namespace < summary.Items[j].Namespace
		}
		return summary.Items[i].Name < summary.Items[j].Name
	})
	summary.Count = len(summary.Items)

	for ns := range namespaces {
		summary.Namespaces = append(summary.Namespaces, ns)
	}
	sort.Strings(summary.Namespaces)

	return summary
}

// isMonitored reports whether any persona name in the Deployment's namespace
// resolves to it, using the same fallback chain as heal and the operator.
func isMonitored(deploy *appsv1.Deployment, personaNames []string) bool {
	summary := summaryOf(deploy)
	for _, name := range personaNames {
		if matchesApp(summary, name) {
			return true
		}
	}
	return false
}

// withoutSystemNamespaces drops cluster add-on namespaces.
func withoutSystemNamespaces(deployments []appsv1.Deployment) []appsv1.Deployment {
	kept := make([]appsv1.Deployment, 0, len(deployments))
	for _, d := range deployments {
		if systemNamespaces[d.Namespace] {
			continue
		}
		kept = append(kept, d)
	}
	return kept
}

// fetchDeploymentsInScope lists Deployments in one namespace, or cluster-wide
// when namespace is empty.
func fetchDeploymentsInScope(ctx context.Context, kubeconfig, namespace string) ([]appsv1.Deployment, error) {
	args := []string{"get", "deployment", "-o", "json"}
	if namespace != "" {
		args = append(args, "-n", namespace)
	} else {
		args = append(args, "--all-namespaces")
	}

	out, err := kubectlCmd(ctx, kubeconfig, args...).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%s", strings.TrimSpace(string(out)))
	}
	return parseDeploymentObjects(out)
}

// fetchPersonaBriefs lists ApplicationPersonas in scope. A missing CRD is
// reported rather than treated as "no personas": the two mean different things.
func fetchPersonaBriefs(ctx context.Context, kubeconfig, namespace string) ([]personaBrief, bool, error) {
	args := []string{"get", "applicationpersona", "-o", "json"}
	if namespace != "" {
		args = append(args, "-n", namespace)
	} else {
		args = append(args, "--all-namespaces")
	}

	out, err := kubectlCmd(ctx, kubeconfig, args...).CombinedOutput()
	if err != nil {
		text := strings.TrimSpace(string(out))
		if strings.Contains(text, "the server doesn't have a resource type") {
			return nil, true, nil
		}
		return nil, false, fmt.Errorf("%s", text)
	}

	briefs, parseErr := parsePersonaBriefs(out)
	return briefs, false, parseErr
}

// parsePersonaBriefs decodes the namespace and spec.name of each persona.
func parsePersonaBriefs(raw []byte) ([]personaBrief, error) {
	var list struct {
		Items []struct {
			Metadata struct {
				Name      string `json:"name"`
				Namespace string `json:"namespace"`
			} `json:"metadata"`
			Spec struct {
				Name string `json:"name"`
			} `json:"spec"`
		} `json:"items"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, fmt.Errorf("failed to parse personas: %w", err)
	}

	briefs := make([]personaBrief, 0, len(list.Items))
	for _, item := range list.Items {
		appName := item.Spec.Name
		if appName == "" {
			appName = item.Metadata.Name
		}
		briefs = append(briefs, personaBrief{Namespace: item.Metadata.Namespace, AppName: appName})
	}
	return briefs, nil
}

// printUnmonitored writes the section. A fully covered cluster prints nothing:
// there is no blind spot to report.
func printUnmonitored(w io.Writer, s *unmonitoredSummary) {
	if s == nil || s.Count == 0 {
		return
	}

	if s.PersonaCRDMissing {
		fmt.Fprintf(w, "Unmonitored: %s\n", output.Yellow(fmt.Sprintf(
			"all %d Deployments; the ApplicationPersona CRD is not installed", s.Count)))
		fmt.Fprintf(w, "  %s\n", output.Dim("→ install the operator first: dorgu cluster setup"))
		fmt.Fprintln(w)
		return
	}

	fmt.Fprintf(w, "Unmonitored: %s\n", output.Yellow(fmt.Sprintf(
		"%d Deployment(s) have no ApplicationPersona and are not being watched", s.Count)))

	shown := s.Items
	if len(shown) > maxUnmonitoredListed {
		shown = shown[:maxUnmonitoredListed]
	}
	names := make([]string, 0, len(shown))
	for _, item := range shown {
		names = append(names, item.Namespace+"/"+item.Name)
	}
	fmt.Fprintf(w, "  %s\n", strings.Join(names, ", "))
	if len(s.Items) > len(shown) {
		fmt.Fprintf(w, "  and %d more (see --json for the full list)\n", len(s.Items)-len(shown))
	}

	for _, ns := range s.Namespaces {
		fmt.Fprintf(w, "  %s\n", output.Dim(fmt.Sprintf("→ run: dorgu persona import -n %s --all", ns)))
	}
	fmt.Fprintln(w)
}
