package setup

import (
	"fmt"
	"os/exec"
	"strings"
	"time"
)

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
	args := []string{
		"upgrade", "--install", c.HelmReleaseName, c.HelmChart,
		"--namespace", c.Namespace,
		"--version", version,
		"--wait",
		"--timeout", "5m0s",
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

// InstallComponent runs: helm upgrade --install <release> <chart> \
//
//	--namespace <ns> --version <ver> --create-namespace --wait --timeout 5m0s \
//	[--set k=v ...] [--values file]
//
// Applies VersionOverrides from cfg before building args.
// Returns InstallResult with Duration and HelmOutput.
func InstallComponent(ex Executor, c ComponentConfig, cfg SetupConfig) InstallResult {
	start := time.Now()

	version := c.Version
	if cfg.VersionOverrides != nil {
		if v, ok := cfg.VersionOverrides[c.ID]; ok && v != "" {
			version = v
		}
	}

	args := BuildHelmArgs(c, version)
	out, err := ex.Run("helm", args...)
	duration := time.Since(start)

	if err != nil {
		return InstallResult{
			Component:  c,
			Succeeded:  false,
			Error:      fmt.Errorf("helm install %s: %w\n%s", c.HelmReleaseName, err, out),
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
