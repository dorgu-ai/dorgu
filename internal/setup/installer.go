package setup

import (
	"fmt"
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
// Exported for testing. Use BuildHelmArgsWithEnv for environment-aware builds.
func BuildHelmArgs(c ComponentConfig, version string) []string {
	return buildHelmArgsInternal(c, version, "")
}

// BuildHelmArgsWithEnv constructs Helm args including environment-specific overrides.
func BuildHelmArgsWithEnv(c ComponentConfig, version, environment string) []string {
	return buildHelmArgsInternal(c, version, environment)
}

func buildHelmArgsInternal(c ComponentConfig, version, environment string) []string {
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
	// Apply environment-specific overrides (e.g., local mode for OpenObserve in dev/sandbox)
	if environment != "" && c.EnvironmentOverrides != nil {
		if overrides, ok := c.EnvironmentOverrides[environment]; ok {
			for _, sv := range overrides {
				args = append(args, "--set", sv)
			}
		}
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

	args := BuildHelmArgsWithEnv(c, version, cfg.Environment)
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
