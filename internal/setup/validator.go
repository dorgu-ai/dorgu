package setup

import (
	"fmt"
	"strings"
	"time"
)

// ValidationResult holds the health check outcome for a single component.
type ValidationResult struct {
	ComponentID ComponentID
	Namespace   string
	Healthy     bool
	Message     string // e.g. "2 pod(s) running in cert-manager"
}

// CheckPodsRunning runs: kubectl get pods -n <ns> -l app.kubernetes.io/instance=<release>
// --field-selector=status.phase=Running --no-headers
// Returns (true, "N pod(s) running") or (false, reason).
// Exported for testing with sample output.
func CheckPodsRunning(ex Executor, namespace, releaseName string) (bool, string) {
	out, err := ex.Run(
		"kubectl", "get", "pods",
		"-n", namespace,
		"-l", fmt.Sprintf("app.kubernetes.io/instance=%s", releaseName),
		"--field-selector=status.phase=Running",
		"--no-headers",
	)
	if err != nil {
		return false, fmt.Sprintf("kubectl error: %s", strings.TrimSpace(out))
	}

	lines := strings.Split(strings.TrimSpace(out), "\n")
	var running int
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			running++
		}
	}
	if running == 0 {
		return false, "no pods found"
	}
	return true, fmt.Sprintf("%d pod(s) running", running)
}

// ValidateComponent polls kubectl get pods -n <ns> -l app.kubernetes.io/instance=<release>
// until all pods are Running or timeout is reached.
// Poll interval: 5s. Default timeout passed in (3 minutes recommended).
func ValidateComponent(ex Executor, c ComponentConfig, timeout time.Duration) ValidationResult {
	deadline := time.Now().Add(timeout)
	for {
		ok, msg := CheckPodsRunning(ex, c.Namespace, c.HelmReleaseName)
		if ok {
			return ValidationResult{
				ComponentID: c.ID,
				Namespace:   c.Namespace,
				Healthy:     true,
				Message:     msg,
			}
		}
		if time.Now().After(deadline) {
			return ValidationResult{
				ComponentID: c.ID,
				Namespace:   c.Namespace,
				Healthy:     false,
				Message:     fmt.Sprintf("timed out after %s: %s", timeout, msg),
			}
		}
		time.Sleep(5 * time.Second)
	}
}

// ValidateAll runs ValidateComponent for each non-skipped InstallResult.
// If skip=true, returns ValidationResults with Healthy=true and Message="skipped" for all.
func ValidateAll(ex Executor, results []InstallResult, skip bool) []ValidationResult {
	var vrs []ValidationResult
	for _, r := range results {
		if skip || r.Skipped {
			vrs = append(vrs, ValidationResult{
				ComponentID: r.Component.ID,
				Namespace:   r.Component.Namespace,
				Healthy:     true,
				Message:     "skipped",
			})
			continue
		}
		if !r.Succeeded {
			vrs = append(vrs, ValidationResult{
				ComponentID: r.Component.ID,
				Namespace:   r.Component.Namespace,
				Healthy:     false,
				Message:     fmt.Sprintf("install failed: %v", r.Error),
			})
			continue
		}
		vrs = append(vrs, ValidateComponent(ex, r.Component, 3*time.Minute))
	}
	return vrs
}
