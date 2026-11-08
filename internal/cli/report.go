package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/dorgu-ai/dorgu/internal/output"
)

// reportCmdTimeout bounds all of the report's cluster reads together.
const reportCmdTimeout = 60 * time.Second

// defaultReportWindow is the window when --since is not given.
const defaultReportWindow = "7d"

func newReportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "report",
		Short: "Print Dorgu's KPI summary for this cluster",
		Long: `Print the numbers Dorgu is judged on, computed from this cluster's own records:
RemediationActions, IncidentMemories, personas and Events. Nothing leaves the
cluster and nothing is sent anywhere; the output is for you to read, or to paste
to the Dorgu team if you are a design partner.

What it reports, for the window (default the last 7 days):
  - safe heals (the north star) and the accounting behind it: a heal is clean only
    when the RemediationAction Completed, HealCredited is True and the incident's
    outcome is resolved. Partial outcomes, recoveries that did not need Dorgu's
    change, and HealCredited=False are listed separately and never counted
  - approval rate, review depth, proposal precision (rejections by --reason)
  - time to heal: detect to proposal, proposal to decision, decision to applied,
    applied to verified, as p50 / p90
  - the refusal mix: what the gate refused, by stage and reason, at proposal,
    approval and apply, and what the CLI declined
  - the clamps the guardrails applied before you saw a plan

Where there is nothing to measure it says "no data". It never prints a zero as if
it were a measurement, and it lists the KPIs a cluster cannot report at all.

Events are the one record that expires (Kubernetes keeps them about an hour by
default), so CLI declines older than that are not counted. Everything else is read
from CRDs, which persist.

Examples:
  dorgu report
  dorgu report --since 24h
  dorgu report --since 30d --json
  dorgu report -n production`,
		Args: cobra.NoArgs,
		RunE: runReport,
	}
	cmd.Flags().String("since", defaultReportWindow, "window to report on: 7d, 24h, 2w, 90m, any Go duration")
	cmd.Flags().StringP("namespace", "n", "", "limit to one namespace (default: all namespaces)")
	cmd.Flags().String("kubeconfig", "", kubeconfigFlagUsage)
	return cmd
}

func runReport(cmd *cobra.Command, _ []string) error {
	sinceFlag, _ := cmd.Flags().GetString("since")
	namespace, _ := cmd.Flags().GetString("namespace")
	kubeconfigFlag, _ := cmd.Flags().GetString("kubeconfig")

	since, err := parseSince(sinceFlag)
	if err != nil {
		return err
	}
	if _, err := exec.LookPath("kubectl"); err != nil {
		return fmt.Errorf("kubectl not found in PATH; required for report")
	}
	kubeconfig, err := validateKubeconfig(kubeconfigFlag)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), reportCmdTimeout)
	defer cancel()

	reads, err := readReportInputs(ctx, kubeconfig, namespace)
	if err != nil {
		return err
	}
	reads.inputs.OperatorVersion = reads.cluster.OperatorVersion
	reads.inputs.Errors = reads.errors()
	doc := buildReport(reads.inputs, time.Now().UTC(), since)
	doc.Cluster = reads.cluster
	doc.Sources = reads.sources

	if output.IsJSON() {
		return renderReportJSON(cmd.OutOrStdout(), doc)
	}
	renderReportText(cmd.OutOrStdout(), doc)
	return nil
}

// reportReads is what the cluster told us, and which reads worked.
type reportReads struct {
	inputs  reportInputs
	cluster reportCluster
	sources reportSources
}

// readReportInputs reads the cluster. RemediationActions are the report's core: if
// they cannot be read there is no report, and that is an error. Every other read
// is allowed to fail, and the report says which did, so a gap is never a zero.
func readReportInputs(ctx context.Context, kubeconfig, namespace string) (*reportReads, error) {
	reads := &reportReads{cluster: reportCluster{Namespace: "all", SelfHealing: []string{}}}
	if namespace != "" {
		reads.cluster.Namespace = namespace
	}
	if name, err := currentKubeContext(ctx, kubeconfig); err == nil {
		reads.cluster.Context = name
	}

	ras, err := readReportList[reportRA](ctx, kubeconfig, namespace, "remediationaction")
	if err != nil {
		if strings.Contains(err.Error(), "the server doesn't have a resource type") {
			output.ErrorWithHint("RemediationAction CRD not found. Is the dorgu operator installed?", operatorInstallHint)
			return nil, errSilent
		}
		return nil, fmt.Errorf("failed to list remediations: %w", err)
	}
	reads.inputs.Remediations = ras
	reads.sources.RemediationActions = reportSource{OK: true, Count: len(ras)}

	incidents, err := readReportList[reportIncident](ctx, kubeconfig, namespace, "incidentmemory")
	reads.sources.Incidents = sourceOf(len(incidents), err)
	reads.inputs.Incidents = incidents

	events, err := readReportList[reportEvent](ctx, kubeconfig, namespace, "events",
		"--field-selector", "involvedObject.kind=RemediationAction")
	reads.sources.Events = sourceOf(len(events), err)
	reads.inputs.Events = events
	reads.inputs.EventsRead = err == nil

	reads.readPersonas(ctx, kubeconfig, namespace)
	reads.cluster.OperatorVersion = readOperatorVersion(ctx, kubeconfig)
	return reads, nil
}

func sourceOf(n int, err error) reportSource {
	if err != nil {
		return reportSource{Error: err.Error()}
	}
	return reportSource{OK: true, Count: n}
}

// errors lists the inputs that could not be read, each with a bounded class, in a
// fixed order. The RemediationActions are not here: without them there is no report.
func (r *reportReads) errors() []reportInputError {
	out := []reportInputError{}
	for _, in := range []struct {
		name string
		src  reportSource
	}{
		{"incidents", r.sources.Incidents},
		{"events", r.sources.Events},
		{"personas", r.sources.Personas},
		{"cluster_personas", r.sources.ClusterPersonas},
	} {
		if !in.src.OK {
			out = append(out, reportInputError{Input: in.name, Error: errorClass(in.src.Error)})
		}
	}
	return out
}

// errorClass names the kind of a failed read in a bounded vocabulary, so the
// report's JSON carries a value a reader can group on and no cluster text.
func errorClass(msg string) string {
	lower := strings.ToLower(msg)
	switch {
	case strings.Contains(lower, "forbidden"):
		return "forbidden"
	case strings.Contains(lower, "doesn't have a resource type"), strings.Contains(lower, "not found"):
		return "not_found"
	case strings.Contains(lower, "timed out"), strings.Contains(lower, "deadline exceeded"):
		return "timeout"
	case strings.Contains(lower, "could not parse"):
		return "unparseable"
	}
	return "error"
}

// readReportList lists a resource as items of T, in one namespace or all.
func readReportList[T any](ctx context.Context, kubeconfig, namespace, resource string, extra ...string) ([]T, error) {
	args := []string{"get", resource, "-o", "json"}
	if namespace != "" {
		args = append(args, "-n", namespace)
	} else {
		args = append(args, "--all-namespaces")
	}
	args = append(args, extra...)
	out, err := kubectlCmd(ctx, kubeconfig, args...).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%s", kubectlErrText(out))
	}
	items, err := parseReportList[T](out)
	if err != nil {
		return nil, fmt.Errorf("could not parse %s: %w", resource, err)
	}
	return items, nil
}

// readPersonas fills in the application persona count and the self-healing modes.
func (r *reportReads) readPersonas(ctx context.Context, kubeconfig, namespace string) {
	personas, err := readReportList[json.RawMessage](ctx, kubeconfig, namespace, "applicationpersona")
	r.sources.Personas = sourceOf(len(personas), err)
	if err == nil {
		n := len(personas)
		r.cluster.Personas = &n
	}

	out, cerr := kubectlCmd(ctx, kubeconfig, "get", "clusterpersona", "-o", "json").CombinedOutput()
	if cerr != nil {
		r.sources.ClusterPersonas = reportSource{Error: kubectlErrText(out)}
		return
	}
	var list struct {
		Items []struct {
			Spec struct {
				Policies struct {
					SelfHealing struct {
						Mode string `json:"mode"`
					} `json:"selfHealing"`
				} `json:"policies"`
			} `json:"spec"`
		} `json:"items"`
	}
	if err := json.Unmarshal(out, &list); err != nil {
		r.sources.ClusterPersonas = reportSource{Error: "could not parse clusterpersona: " + err.Error()}
		return
	}
	r.sources.ClusterPersonas = reportSource{OK: true, Count: len(list.Items)}
	for _, item := range list.Items {
		mode := item.Spec.Policies.SelfHealing.Mode
		if mode == "" {
			mode = "propose"
		}
		r.cluster.SelfHealing = append(r.cluster.SelfHealing, mode)
	}
}

// readOperatorVersion is the image tag of the operator Deployment, or nil when it
// cannot be found. It is a convenience for the reader of the report, not a
// measurement anything depends on.
func readOperatorVersion(ctx context.Context, kubeconfig string) *string {
	out, err := kubectlCmd(ctx, kubeconfig, "get", "deployment", "--all-namespaces",
		"-l", "app.kubernetes.io/name=dorgu-operator", "-o", "json").CombinedOutput()
	if err != nil {
		return nil
	}
	var list struct {
		Items []struct {
			Spec struct {
				Template struct {
					Spec struct {
						Containers []struct {
							Image string `json:"image"`
						} `json:"containers"`
					} `json:"spec"`
				} `json:"template"`
			} `json:"spec"`
		} `json:"items"`
	}
	if json.Unmarshal(out, &list) != nil {
		return nil
	}
	for _, item := range list.Items {
		for _, c := range item.Spec.Template.Spec.Containers {
			if tag := imageTag(c.Image); tag != "" {
				return &tag
			}
		}
	}
	return nil
}

// imageTag is the tag of an image reference, or "" when it has none or is pinned
// by digest.
func imageTag(image string) string {
	if strings.Contains(image, "@") {
		return ""
	}
	slash := strings.LastIndex(image, "/")
	if colon := strings.LastIndex(image, ":"); colon > slash {
		return image[colon+1:]
	}
	return ""
}
