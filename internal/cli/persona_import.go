package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	appsv1 "k8s.io/api/apps/v1"

	"github.com/dorgu-ai/dorgu/internal/importer"
	"github.com/dorgu-ai/dorgu/internal/output"
)

// F-02: `persona generate|apply` need local source, which a cluster that already
// has apps does not have. Until a persona exists Dorgu sees nothing, so a broken
// app raises no incident and gets no remediation. `persona import` reads the
// Deployments that are already running and writes the personas for them.

// personaImportFlags carries the caller-supplied import parameters.
type personaImportFlags struct {
	namespace  string
	all        bool
	name       string
	outputFile string
	apply      bool
	kubeconfig string
}

func newPersonaImportCmd() *cobra.Command {
	var flags personaImportFlags

	cmd := &cobra.Command{
		Use:   "import",
		Short: "Create ApplicationPersonas from Deployments already running",
		Long: `Read live Deployments and synthesize an ApplicationPersona for each one
from what is already in the spec: resources, probes, replicas, ports, image and
ownership labels.

This is the onboarding path for a cluster that already has apps. Unlike
'persona generate', it needs no local source, no Dockerfile and no relabelling.

Prints the YAML by default so you can read it before anything reaches the
cluster. Use --apply to create the personas, or -o to write them to a file.

Examples:
  dorgu persona import -n apps --all
  dorgu persona import -n apps --name checkout-api
  dorgu persona import -n apps --all -o personas.yaml
  dorgu persona import -n apps --all --apply`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPersonaImport(cmd, flags)
		},
	}

	cmd.Flags().StringVarP(&flags.namespace, "namespace", "n", "", "namespace to import from (required)")
	_ = cmd.MarkFlagRequired("namespace")
	cmd.Flags().BoolVar(&flags.all, "all", false, "import every Deployment in the namespace")
	cmd.Flags().StringVar(&flags.name, "name", "", "import a single Deployment by name")
	cmd.Flags().StringVarP(&flags.outputFile, "output", "o", "", "write the personas to a file instead of stdout")
	cmd.Flags().Bool("dry-run", false, "print the personas without applying (the default)")
	cmd.Flags().BoolVar(&flags.apply, "apply", false, "apply the personas to the cluster")
	cmd.Flags().StringVar(&flags.kubeconfig, "kubeconfig", "", "path to kubeconfig (default: ~/.kube/config)")
	cmd.MarkFlagsMutuallyExclusive("all", "name")
	cmd.MarkFlagsMutuallyExclusive("apply", "dry-run")

	return cmd
}

func runPersonaImport(cmd *cobra.Command, flags personaImportFlags) error {
	if _, err := exec.LookPath("kubectl"); err != nil {
		return fmt.Errorf("kubectl not found in PATH; required for persona import")
	}
	if !flags.all && flags.name == "" {
		return fmt.Errorf("specify --all to import every Deployment in the namespace, or --name <deployment>")
	}

	kubeconfig, err := validateKubeconfig(flags.kubeconfig)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), remediationCmdTimeout)
	defer cancel()

	deployments, err := fetchDeployments(ctx, kubeconfig, flags.namespace)
	if err != nil {
		return err
	}

	targets, err := importTargets(deployments, flags.name)
	if err != nil {
		return err
	}
	if len(targets) == 0 {
		output.Fwarn(cmd.ErrOrStderr(),
			fmt.Sprintf("No Deployments in namespace %s; nothing to import.", flags.namespace))
		return nil
	}

	results := make([]importer.Result, 0, len(targets))
	for i := range targets {
		appName, conflict := personaAppName(&targets[i], deployments)
		result := importer.FromDeployment(&targets[i], appName, flags.namespace)
		if conflict != "" {
			result.Warnings = append(result.Warnings, conflict)
		}
		results = append(results, result)
	}
	results = importer.SortResults(results)

	yamlDoc := importer.JoinYAML(results)

	if err := deliverPersonas(ctx, cmd, kubeconfig, flags, yamlDoc, len(results)); err != nil {
		return err
	}

	// Diagnostics go to stderr: stdout may be carrying YAML the user is
	// redirecting into a file.
	reportImportWarnings(cmd.ErrOrStderr(), results)
	return nil
}

// importTargets narrows the fetched Deployments to the ones being imported.
func importTargets(deployments []appsv1.Deployment, name string) ([]appsv1.Deployment, error) {
	if name == "" {
		return deployments, nil
	}
	for i := range deployments {
		if deployments[i].Name == name {
			return []appsv1.Deployment{deployments[i]}, nil
		}
	}
	present := make([]string, 0, len(deployments))
	for i := range deployments {
		present = append(present, deployments[i].Name)
	}
	if len(present) == 0 {
		return nil, fmt.Errorf("no Deployment named %q; the namespace has none", name)
	}
	return nil, fmt.Errorf("no Deployment named %q (namespace has: %s)", name, strings.Join(present, ", "))
}

// deliverPersonas writes the rendered personas wherever the caller asked for
// them: a file, the cluster, or stdout.
func deliverPersonas(
	ctx context.Context, cmd *cobra.Command, kubeconfig string,
	flags personaImportFlags, yamlDoc string, count int,
) error {
	switch {
	case flags.outputFile != "":
		path := filepath.Clean(flags.outputFile)
		if err := os.WriteFile(path, []byte(yamlDoc), 0o644); err != nil {
			return fmt.Errorf("failed to write %s: %w", path, err)
		}
		output.Success(fmt.Sprintf("Wrote %d persona(s) to %s", count, path))
		output.Info("Apply them with: kubectl apply -f " + path)
	case flags.apply:
		if err := applyPersonaYAML(ctx, kubeconfig, flags.namespace, yamlDoc); err != nil {
			return err
		}
		output.Success(fmt.Sprintf("Applied %d ApplicationPersona(s) to namespace %s", count, flags.namespace))
	default:
		fmt.Fprint(cmd.OutOrStdout(), yamlDoc)
		output.Finfo(cmd.ErrOrStderr(), fmt.Sprintf(
			"%d persona(s) rendered, nothing applied. Re-run with --apply to create them.", count))
	}
	return nil
}

// reportImportWarnings prints everything that was inferred rather than read.
// Invented limits get their own line: the remediation proposer skips personas
// without limits, so a persona that quietly shipped with made-up ones would
// heal against numbers nobody chose.
func reportImportWarnings(w io.Writer, results []importer.Result) {
	invented := make([]string, 0)
	for _, r := range results {
		for _, warning := range r.Warnings {
			output.Fwarn(w, warning)
		}
		if r.LimitsInvented {
			invented = append(invented, r.Deployment)
		}
	}
	if len(invented) > 0 {
		output.Fwarn(w, fmt.Sprintf(
			"Resource limits were inferred for: %s. Remediation resizes from the persona, so check these before you rely on them.",
			strings.Join(invented, ", ")))
	}
}

// personaAppName picks spec.name so the operator's fallback chain resolves the
// persona back to this Deployment and no other. Candidates are tried in the
// same order the chain uses, and each is kept only if it resolves to this
// Deployment across the whole namespace.
//
// Occasionally no candidate works: another Deployment claims the label this one
// would use, and an earlier rung wins. Rather than emit a persona that quietly
// watches the wrong workload, the second return value describes the conflict so
// the caller can say so.
func personaAppName(deploy *appsv1.Deployment, all []appsv1.Deployment) (appName, conflict string) {
	summaries := make([]deploymentSummary, 0, len(all))
	for i := range all {
		summaries = append(summaries, summaryOf(&all[i]))
	}

	var claimedBy string
	for _, candidate := range []string{
		deploy.Labels[labelAppName],
		deploy.Labels[labelApp],
		deploy.Name,
	} {
		if candidate == "" {
			continue
		}
		resolved, err := selectDeployment(summaries, candidate)
		switch {
		case err == nil && resolved.Name == deploy.Name:
			return candidate, ""
		case err == nil:
			claimedBy = resolved.Name
		}
	}

	conflict = fmt.Sprintf(
		"no name resolves to Deployment %s: %q is claimed by another Deployment in the namespace",
		deploy.Name, deploy.Name)
	if claimedBy != "" {
		conflict = fmt.Sprintf(
			"the persona for Deployment %s will resolve to %s instead: %s carries a label that outranks the name match. "+
				"Set %s=%s on %s before importing",
			deploy.Name, claimedBy, claimedBy, labelAppName, deploy.Name, deploy.Name)
	}
	return deploy.Name, conflict
}

// summaryOf reduces a Deployment to the shape the match chain reads.
func summaryOf(deploy *appsv1.Deployment) deploymentSummary {
	containers := make([]string, 0, len(deploy.Spec.Template.Spec.Containers))
	for _, c := range deploy.Spec.Template.Spec.Containers {
		containers = append(containers, c.Name)
	}
	summary := deploymentSummary{
		Name:       deploy.Name,
		Containers: containers,
		Labels:     deploy.Labels,
	}
	if deploy.Spec.Selector != nil {
		summary.SelectorLabels = deploy.Spec.Selector.MatchLabels
	}
	return summary
}

// fetchDeployments lists every Deployment in the namespace as a typed object.
func fetchDeployments(ctx context.Context, kubeconfig, namespace string) ([]appsv1.Deployment, error) {
	out, err := kubectlCmd(ctx, kubeconfig, "get", "deployment", "-n", namespace, "-o", "json").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to list deployments in %s: %s", namespace, strings.TrimSpace(string(out)))
	}
	return parseDeploymentObjects(out)
}

// parseDeploymentObjects decodes a kubectl Deployment list.
func parseDeploymentObjects(raw []byte) ([]appsv1.Deployment, error) {
	var list appsv1.DeploymentList
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, fmt.Errorf("failed to parse deployments: %w", err)
	}
	return list.Items, nil
}

// applyPersonaYAML pipes the rendered personas into kubectl apply.
func applyPersonaYAML(ctx context.Context, kubeconfig, namespace, yamlDoc string) error {
	args := []string{"apply", "-f", "-", "-n", namespace}
	kubectl := kubectlCmd(ctx, kubeconfig, args...)
	kubectl.Stdin = bytes.NewBufferString(yamlDoc)

	var stderr bytes.Buffer
	kubectl.Stdout = io.Discard
	kubectl.Stderr = &stderr
	if err := kubectl.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if strings.Contains(msg, "the server doesn't have a resource type") {
			output.ErrorWithHint("ApplicationPersona CRD not found. Is the dorgu operator installed?",
				"To install the operator: dorgu cluster setup")
			return errSilent
		}
		return fmt.Errorf("kubectl apply failed: %s", msg)
	}
	return nil
}
