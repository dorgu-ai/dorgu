package setup

import (
	"bytes"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"
)

// retryDelay is the delay between install retries. Overridable in tests.
var retryDelay = 30 * time.Second

// ErrorCategory classifies installation errors for retry decisions.
// Future AI agents can use this for autonomous healing.
type ErrorCategory string

const (
	ErrorCategoryTransient     ErrorCategory = "transient"     // network, timeout - safe to auto-retry
	ErrorCategoryConfiguration ErrorCategory = "configuration" // S3, secrets, permissions - needs manual fix
	ErrorCategoryUnknown       ErrorCategory = "unknown"       // default - prompt user
)

// ClassifiedError wraps an error with its category for retry decisions.
type ClassifiedError struct {
	Category ErrorCategory
	Original error
	Output   string // full helm/kubectl output
}

func (e *ClassifiedError) Error() string {
	return e.Original.Error()
}

func (e *ClassifiedError) Unwrap() error {
	return e.Original
}

// classifyError analyzes error output and categorizes it.
// Returns ErrorCategory for structured decision-making by retry logic or AI agents.
func classifyError(err error, output string) ErrorCategory {
	if err == nil {
		return ErrorCategoryUnknown
	}

	outputLower := strings.ToLower(output)
	errLower := strings.ToLower(err.Error())

	// Configuration/permanent errors (do NOT auto-retry)
	configPatterns := []string{
		"access denied",
		"forbidden",
		"unauthorized",
		"permission denied",
		"invalid configuration",
		"secret not found",
		"s3", "bucket",
		"credentials", "authentication failed",
	}

	for _, pattern := range configPatterns {
		if strings.Contains(outputLower, pattern) || strings.Contains(errLower, pattern) {
			return ErrorCategoryConfiguration
		}
	}

	// Transient errors (safe to auto-retry)
	transientPatterns := []string{
		"context deadline exceeded",
		"connection refused",
		"i/o timeout",
		"temporary failure",
		"network unreachable",
		"dial tcp",
	}

	for _, pattern := range transientPatterns {
		if strings.Contains(outputLower, pattern) || strings.Contains(errLower, pattern) {
			return ErrorCategoryTransient
		}
	}

	return ErrorCategoryUnknown
}

// Executor abstracts shell command execution for testability and dry-run support.
type Executor interface {
	Run(name string, args ...string) (string, error)
}

// OSExecutor calls os/exec — used in production.
type OSExecutor struct{}

func (e *OSExecutor) Run(name string, args ...string) (string, error) {
	out, err := exec.Command(name, args...).CombinedOutput()
	return string(out), err
}

// StreamingExecutor runs commands and streams output to a writer in real-time
// with optional dim styling applied per line.
type StreamingExecutor struct {
	StreamTo io.Writer // where to stream (e.g., os.Stderr)
	Dim      bool      // if true, applies dim ANSI codes per line
}

func (e *StreamingExecutor) Run(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	var buf bytes.Buffer

	var writer io.Writer = e.StreamTo
	if e.Dim {
		writer = newDimLineWriter(writer)
	}

	mw := io.MultiWriter(&buf, writer)
	cmd.Stdout = mw
	cmd.Stderr = mw

	err := cmd.Run()
	return buf.String(), err
}

// dimLineWriter wraps an io.Writer and applies ANSI dim styling + 4-space indent
// to each complete line written to it.
type dimLineWriter struct {
	w   io.Writer
	buf []byte
}

func newDimLineWriter(w io.Writer) io.Writer {
	return &dimLineWriter{w: w}
}

const (
	ansiDim   = "\033[2m"
	ansiReset = "\033[0m"
)

func (d *dimLineWriter) Write(p []byte) (n int, err error) {
	d.buf = append(d.buf, p...)
	for {
		idx := bytes.IndexByte(d.buf, '\n')
		if idx < 0 {
			break
		}
		line := string(d.buf[:idx])
		d.buf = d.buf[idx+1:]
		if strings.TrimSpace(line) == "" {
			continue
		}
		_, err = fmt.Fprintf(d.w, "%s    %s%s\n", ansiDim, line, ansiReset)
		if err != nil {
			return len(p), err
		}
	}
	return len(p), nil
}

// DryRunExecutor logs commands without executing them — used with --dry-run flag.
type DryRunExecutor struct {
	Log []string
}

func (e *DryRunExecutor) Run(name string, args ...string) (string, error) {
	cmd := name + " " + strings.Join(args, " ")
	e.Log = append(e.Log, cmd)
	return fmt.Sprintf("[dry-run] would execute: %s", cmd), nil
}

// AddHelmRepo runs: helm repo add <repoName> <repoURL>
// Idempotent: ignores "already exists" errors.
func AddHelmRepo(ex Executor, repoName, repoURL string) error {
	out, err := ex.Run("helm", "repo", "add", repoName, repoURL)
	if err != nil {
		// helm repo add returns exit code 1 with "already exists" when the repo is already registered
		if strings.Contains(out, "already exists") {
			return nil
		}
		return fmt.Errorf("helm repo add %s: %w\n%s", repoName, err, out)
	}
	return nil
}

// UpdateHelmRepos runs: helm repo update
func UpdateHelmRepos(ex Executor) error {
	out, err := ex.Run("helm", "repo", "update")
	if err != nil {
		return fmt.Errorf("helm repo update: %w\n%s", err, out)
	}
	return nil
}

// BuildHelmArgs constructs the full arg slice for helm upgrade --install.
// Exported for testing.
func BuildHelmArgs(c ComponentConfig, version string) []string {
	timeout := c.Timeout
	if timeout == "" {
		timeout = "5m0s"
	}
	args := []string{
		"upgrade", "--install", c.HelmReleaseName, c.HelmChart,
		"--namespace", c.Namespace,
		"--version", version,
		"--wait",
		"--timeout", timeout,
	}
	if c.CreateNamespace {
		args = append(args, "--create-namespace")
	}
	for _, sv := range c.HelmSetValues {
		args = append(args, "--set", sv)
	}
	if c.HelmValuesFile != "" {
		args = append(args, "--values", c.HelmValuesFile)
	}
	return args
}

// ReleaseStatus represents the state of a Helm release.
type ReleaseStatus string

const (
	ReleaseNotFound ReleaseStatus = "not-found"
	ReleaseDeployed ReleaseStatus = "deployed"
	ReleaseFailed   ReleaseStatus = "failed"
	ReleasePending  ReleaseStatus = "pending-install"
	ReleaseOther    ReleaseStatus = "other"
)

// CheckReleaseStatus runs: helm status <release> -n <namespace> -o json
// Returns the release status or ReleaseNotFound if no release exists.
func CheckReleaseStatus(ex Executor, releaseName, namespace string) ReleaseStatus {
	out, err := ex.Run("helm", "status", releaseName, "-n", namespace, "-o", "json")
	if err != nil {
		if strings.Contains(out, "not found") || strings.Contains(fmt.Sprintf("%v", err), "not found") {
			return ReleaseNotFound
		}
		return ReleaseOther
	}
	lower := strings.ToLower(out)
	switch {
	case strings.Contains(lower, `"status":"deployed"`):
		return ReleaseDeployed
	case strings.Contains(lower, `"status":"failed"`):
		return ReleaseFailed
	case strings.Contains(lower, `"status":"pending-install"`):
		return ReleasePending
	default:
		return ReleaseOther
	}
}

// CleanFailedRelease runs: helm uninstall <release> -n <namespace>
// Only call when CheckReleaseStatus returns ReleaseFailed or ReleasePending.
func CleanFailedRelease(ex Executor, releaseName, namespace string) error {
	out, err := ex.Run("helm", "uninstall", releaseName, "-n", namespace)
	if err != nil {
		return fmt.Errorf("helm uninstall %s -n %s: %w\n%s", releaseName, namespace, err, out)
	}
	return nil
}

// InstallComponent runs: helm upgrade --install <release> <chart> \
//
//	--namespace <ns> --version <ver> --create-namespace --wait --timeout 5m0s \
//	[--set k=v ...] [--values file]
//
// Before installing, checks for failed/pending releases and cleans them up.
// Applies VersionOverrides from cfg before building args.
// Returns InstallResult with Duration and HelmOutput.
func InstallComponent(ex Executor, c ComponentConfig, cfg SetupConfig) InstallResult {
	start := time.Now()

	// Pre-install: check for existing failed release and clean it up
	status := CheckReleaseStatus(ex, c.HelmReleaseName, c.Namespace)
	switch status {
	case ReleaseFailed, ReleasePending:
		if err := CleanFailedRelease(ex, c.HelmReleaseName, c.Namespace); err != nil {
			return InstallResult{
				Component: c,
				Succeeded: false,
				Error:     fmt.Errorf("failed to clean broken release %s: %w", c.HelmReleaseName, err),
				Duration:  time.Since(start),
			}
		}
	case ReleaseDeployed:
		// Already installed — proceed with upgrade to apply any version changes
	case ReleaseNotFound:
		// Fresh install
	}

	version := c.Version
	if cfg.VersionOverrides != nil {
		if v, ok := cfg.VersionOverrides[c.ID]; ok && v != "" {
			version = v
		}
	}

	args := BuildHelmArgs(c, version)
	out, err := ex.Run("helm", args...)

	// Smart retry: only auto-retry transient errors (network, timeout)
	if err != nil {
		category := classifyError(err, out)

		if category == ErrorCategoryTransient {
			// Clean up failed release before retry
			retryStatus := CheckReleaseStatus(ex, c.HelmReleaseName, c.Namespace)
			if retryStatus == ReleaseFailed || retryStatus == ReleasePending {
				_ = CleanFailedRelease(ex, c.HelmReleaseName, c.Namespace)
			}
			time.Sleep(retryDelay)
			out, err = ex.Run("helm", args...)
		}
	}

	duration := time.Since(start)

	if err != nil {
		category := classifyError(err, out)
		errMsg := fmt.Sprintf("helm install %s: %v\n%s", c.HelmReleaseName, err, out)
		if category == ErrorCategoryTransient {
			errMsg += fmt.Sprintf("\nHint: check pod status with: kubectl get pods -n %s", c.Namespace)
		}
		return InstallResult{
			Component:  c,
			Succeeded:  false,
			Error:      &ClassifiedError{Category: category, Original: fmt.Errorf("%s", errMsg), Output: out},
			Duration:   duration,
			HelmOutput: out,
		}
	}
	return InstallResult{
		Component:  c,
		Succeeded:  true,
		Duration:   duration,
		HelmOutput: out,
	}
}

// CheckChartAvailability verifies that each component's chart+version exists in the
// configured Helm repos. Run after AddHelmRepo + UpdateHelmRepos, before installing.
func CheckChartAvailability(ex Executor, components []ComponentConfig) error {
	for _, c := range components {
		out, err := ex.Run("helm", "search", "repo", c.HelmChart, "--version", c.Version, "--output", "json")
		if err != nil || strings.TrimSpace(out) == "[]" || strings.TrimSpace(out) == "" {
			// Try to list available versions for a helpful message
			avail, _ := ex.Run("helm", "search", "repo", c.HelmChart, "--versions", "--output", "json")
			return fmt.Errorf("chart version not found: %s %s\n  Available versions (from helm search repo):\n  %s\n  Update the version in stack.go or use --set-version %s=<version>",
				c.HelmChart, c.Version, strings.TrimSpace(avail), c.ID)
		}
	}
	return nil
}

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
//	dorgu.io/setup-stack=<comma-list> \
//	dorgu.io/setup-environment=<env> \
//	dorgu.io/setup-timestamp=<rfc3339>
func AnnotateClusterPersona(ex Executor, name string, cfg SetupConfig) error {
	args := []string{
		"annotate", "clusterpersona", name,
		"--overwrite",
		fmt.Sprintf("dorgu.io/setup-stack=%s", cfg.AnnotationStack()),
		fmt.Sprintf("dorgu.io/setup-environment=%s", cfg.Environment),
		fmt.Sprintf("dorgu.io/setup-timestamp=%s", cfg.Timestamp.UTC().Format("2006-01-02T15:04:05Z")),
	}
	out, err := ex.Run("kubectl", args...)
	if err != nil {
		return fmt.Errorf("kubectl annotate clusterpersona %s: %w\n%s", name, err, out)
	}
	return nil
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
