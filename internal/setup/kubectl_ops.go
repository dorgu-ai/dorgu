package setup

import (
	"fmt"
	"strings"
)

// ValidateClusterPersonaExists checks that a ClusterPersona with the given name exists.
func ValidateClusterPersonaExists(ex Executor, name string) error {
	out, err := ex.Run("kubectl", "get", "clusterpersona", name, "--no-headers")
	if err != nil {
		return fmt.Errorf("ClusterPersona %q not found: %w\n%s", name, err, out)
	}
	return nil
}

// AutoDetectClusterPersonaName runs: kubectl get clusterpersona --no-headers -o name
// Returns the name if exactly one ClusterPersona exists.
// Returns an error with a hint if zero or more than one exist.
func AutoDetectClusterPersonaName(ex Executor) (string, error) {
	out, err := ex.Run("kubectl", "get", "clusterpersona", "--no-headers", "-o", "name")
	if err != nil {
		return "", fmt.Errorf("kubectl get clusterpersona: %w\n%s", err, out)
	}

	lines := strings.Split(strings.TrimSpace(out), "\n")
	var names []string
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		// strip "clusterpersona.dorgu.io/" prefix if present
		if idx := strings.LastIndex(l, "/"); idx >= 0 {
			l = l[idx+1:]
		}
		names = append(names, l)
	}

	switch len(names) {
	case 0:
		return "", fmt.Errorf("no ClusterPersona found — run 'dorgu cluster init' first")
	case 1:
		return names[0], nil
	default:
		return "", fmt.Errorf("multiple ClusterPersonas found (%s) — specify --cluster-persona flag", strings.Join(names, ", "))
	}
}

// AnnotateClusterPersona runs: kubectl annotate clusterpersona <name> --overwrite \
//
//	dorgu.io/setup-stack=<comma-list of installed> \
//	dorgu.io/setup-skipped=<comma-list of skipped/failed> \
//	dorgu.io/setup-environment=<env> \
//	dorgu.io/setup-timestamp=<rfc3339>
//
// Only successfully installed components appear in setup-stack.
func AnnotateClusterPersona(ex Executor, name string, cfg SetupConfig, results []InstallResult) error {
	args := []string{
		"annotate", "clusterpersona", name,
		"--overwrite",
		fmt.Sprintf("dorgu.io/setup-stack=%s", AnnotationStackFromResults(results)),
		fmt.Sprintf("dorgu.io/setup-environment=%s", cfg.Environment),
		fmt.Sprintf("dorgu.io/setup-timestamp=%s", cfg.Timestamp.UTC().Format("2006-01-02T15:04:05Z")),
	}
	skipped := AnnotationSkippedFromResults(results)
	if skipped != "" {
		args = append(args, fmt.Sprintf("dorgu.io/setup-skipped=%s", skipped))
	}
	out, err := ex.Run("kubectl", args...)
	if err != nil {
		return fmt.Errorf("kubectl annotate clusterpersona %s: %w\n%s", name, err, out)
	}
	return nil
}

// AnnotateDriver records which setup driver was used on the ClusterPersona.
func AnnotateDriver(ex Executor, name, driver string) error {
	_, err := ex.Run("kubectl", "annotate", "clusterpersona", name,
		"dorgu.io/setup-driver="+driver, "--overwrite")
	return err
}

// GetCurrentKubeContext runs: kubectl config current-context
func GetCurrentKubeContext(ex Executor) (string, error) {
	out, err := ex.Run("kubectl", "config", "current-context")
	if err != nil {
		return "", fmt.Errorf("failed to get current kube-context: %w\n%s", err, out)
	}
	return strings.TrimSpace(out), nil
}

// ValidateKubeContext checks whether the context name looks like a production cluster.
// Returns (needsConfirmation, warning).
func ValidateKubeContext(contextName string) (bool, string) {
	lower := strings.ToLower(contextName)
	for _, substr := range []string{"prod", "prd", "live"} {
		if strings.Contains(lower, substr) {
			return true, fmt.Sprintf("Current kube-context %q appears to be a production cluster", contextName)
		}
	}
	return false, ""
}

// CheckOperatorInstalled verifies the dorgu operator is present by checking
// for the ClusterPersona CRD and a running operator pod in dorgu-system.
func CheckOperatorInstalled(ex Executor) error {
	out, err := ex.Run("kubectl", "api-resources", "--api-group=dorgu.io", "--no-headers")
	if err != nil || !strings.Contains(out, "clusterpersonas") {
		return fmt.Errorf("dorgu operator CRD not found — install the operator first:\n  helm install dorgu-operator oci://ghcr.io/dorgu-ai/dorgu-operator-charts/dorgu-operator -n dorgu-system --create-namespace")
	}

	// Check both known operator namespaces: dorgu-system (released) and dorgu-operator-system (local dev)
	for _, ns := range []string{"dorgu-system", "dorgu-operator-system"} {
		out, err = ex.Run("kubectl", "get", "pods", "-n", ns,
			"--field-selector=status.phase=Running", "--no-headers")
		if err == nil && strings.Contains(out, "dorgu-operator") {
			return nil
		}
	}

	return fmt.Errorf("dorgu operator pod not running in dorgu-system or dorgu-operator-system namespace — ensure the operator is deployed and healthy")
}

// IsArgoCDInstalled checks if the ArgoCD Application CRD exists in the cluster.
func IsArgoCDInstalled(ex Executor) bool {
	out, err := ex.Run("kubectl", "api-resources", "--api-group=argoproj.io", "--no-headers")
	if err != nil {
		return false
	}
	return strings.Contains(out, "applications")
}

// InstallArgoCDBootstrap installs ArgoCD via Helm as a prerequisite for GitOps mode.
func InstallArgoCDBootstrap(ex Executor) error {
	if err := AddHelmRepo(ex, "argo", "https://argoproj.github.io/argo-helm"); err != nil {
		return fmt.Errorf("failed to add argo helm repo: %w", err)
	}

	if err := UpdateHelmRepos(ex); err != nil {
		return fmt.Errorf("failed to update helm repos: %w", err)
	}

	argoCDConfig := ComponentConfig{
		ID:              ComponentArgoCd,
		HelmReleaseName: "argocd",
		HelmChart:       "argo/argo-cd",
		Namespace:       "argocd",
		Version:         "7.8.28",
		CreateNamespace: true,
		Timeout:         "5m0s",
	}

	args := BuildHelmArgs(argoCDConfig, "7.8.28")
	out, err := ex.Run("helm", args...)
	if err != nil {
		return fmt.Errorf("helm install argocd failed: %w\nOutput: %s", err, out)
	}

	return nil
}
