package setup

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// sequentialExecutor returns canned responses based on call index.
type sequentialExecutor struct {
	calls   []seqCall
	callIdx int
}

type seqCall struct {
	output string
	err    error
}

func (m *sequentialExecutor) Run(name string, args ...string) (string, error) {
	if m.callIdx >= len(m.calls) {
		return "", fmt.Errorf("unexpected call %d: %s %v", m.callIdx, name, args)
	}
	c := m.calls[m.callIdx]
	m.callIdx++
	return c.output, c.err
}

func TestDryRunExecutorLogs(t *testing.T) {
	ex := &DryRunExecutor{}
	out, err := ex.Run("helm", "repo", "add", "jetstack", "https://charts.jetstack.io")
	if err != nil {
		t.Fatalf("DryRunExecutor.Run returned error: %v", err)
	}
	if len(ex.Log) != 1 {
		t.Fatalf("expected 1 log entry, got %d", len(ex.Log))
	}
	if !strings.Contains(ex.Log[0], "helm") {
		t.Errorf("log entry missing 'helm': %q", ex.Log[0])
	}
	if !strings.Contains(out, "dry-run") {
		t.Errorf("output missing 'dry-run': %q", out)
	}
}

func TestBuildHelmArgs_SetValues(t *testing.T) {
	c := ComponentConfig{
		HelmReleaseName: "cert-manager",
		HelmChart:       "jetstack/cert-manager",
		Namespace:       "cert-manager",
		HelmSetValues:   []string{"installCRDs=true"},
	}
	args := BuildHelmArgs(c, "v1.16.3")

	// Check --set appears
	found := false
	for i, a := range args {
		if a == "--set" && i+1 < len(args) && args[i+1] == "installCRDs=true" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("--set installCRDs=true not found in args: %v", args)
	}
}

func TestBuildHelmArgs_NoValuesFile(t *testing.T) {
	c := ComponentConfig{
		HelmReleaseName: "ingress-nginx",
		HelmChart:       "ingress-nginx/ingress-nginx",
		Namespace:       "ingress-nginx",
		HelmValuesFile:  "",
	}
	args := BuildHelmArgs(c, "4.11.3")
	for _, a := range args {
		if a == "--values" {
			t.Errorf("--values flag present but HelmValuesFile is empty; args: %v", args)
		}
	}
}

func TestBuildHelmArgs_ValuesFile(t *testing.T) {
	c := ComponentConfig{
		HelmReleaseName: "openobserve",
		HelmChart:       "openobserve/openobserve",
		Namespace:       "openobserve",
		HelmValuesFile:  "/tmp/oo-values.yaml",
	}
	args := BuildHelmArgs(c, "0.10.2")

	found := false
	for i, a := range args {
		if a == "--values" && i+1 < len(args) && args[i+1] == "/tmp/oo-values.yaml" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("--values /tmp/oo-values.yaml not found in args: %v", args)
	}
}

func TestBuildHelmArgs_CreateNamespace(t *testing.T) {
	c := ComponentConfig{
		HelmReleaseName: "cert-manager",
		HelmChart:       "jetstack/cert-manager",
		Namespace:       "cert-manager",
		CreateNamespace: true,
	}
	args := BuildHelmArgs(c, "v1.16.3")

	found := false
	for _, a := range args {
		if a == "--create-namespace" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("--create-namespace not found in args: %v", args)
	}

	// Verify it's absent when CreateNamespace=false
	c.CreateNamespace = false
	args = BuildHelmArgs(c, "v1.16.3")
	for _, a := range args {
		if a == "--create-namespace" {
			t.Errorf("--create-namespace present but CreateNamespace=false; args: %v", args)
		}
	}
}

func TestVersionOverrideInInstallComponent(t *testing.T) {
	ex := &DryRunExecutor{}
	comp := ComponentConfig{
		ID:              ComponentCertManager,
		HelmReleaseName: "cert-manager",
		HelmChart:       "jetstack/cert-manager",
		Namespace:       "cert-manager",
		Version:         "v1.16.3",
		CreateNamespace: true,
	}
	cfg := SetupConfig{
		VersionOverrides: map[ComponentID]string{
			ComponentCertManager: "v1.17.0",
		},
		Timestamp: time.Now(),
	}

	result := InstallComponent(ex, comp, cfg)
	if !result.Succeeded {
		t.Fatalf("expected success, got error: %v", result.Error)
	}

	// The override version v1.17.0 must appear; default v1.16.3 must not for the --version flag
	found := false
	for _, cmd := range ex.Log {
		if strings.Contains(cmd, "v1.17.0") {
			found = true
		}
	}
	if !found {
		t.Errorf("override version v1.17.0 not found in logged commands: %v", ex.Log)
	}
}

func TestBuildHelmArgs_ComponentTimeout(t *testing.T) {
	c := ComponentConfig{
		HelmReleaseName: "ingress-nginx",
		HelmChart:       "ingress-nginx/ingress-nginx",
		Namespace:       "ingress-nginx",
		Timeout:         "10m0s",
	}
	args := BuildHelmArgs(c, "4.11.3")

	found := false
	for i, a := range args {
		if a == "--timeout" && i+1 < len(args) && args[i+1] == "10m0s" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("--timeout 10m0s not found in args: %v", args)
	}
}

func TestBuildHelmArgs_DefaultTimeout(t *testing.T) {
	c := ComponentConfig{
		HelmReleaseName: "cert-manager",
		HelmChart:       "jetstack/cert-manager",
		Namespace:       "cert-manager",
		Timeout:         "",
	}
	args := BuildHelmArgs(c, "v1.16.3")

	found := false
	for i, a := range args {
		if a == "--timeout" && i+1 < len(args) && args[i+1] == "5m0s" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("--timeout 5m0s (default) not found in args: %v", args)
	}
}

func TestInstallComponent_RetryOnTimeout(t *testing.T) {
	origDelay := retryDelay
	retryDelay = 0
	defer func() { retryDelay = origDelay }()

	ex := &sequentialExecutor{
		calls: []seqCall{
			{output: "Error: release: not found", err: fmt.Errorf("not found")}, // pre-install status check
			{output: "", err: fmt.Errorf("context deadline exceeded")},          // first attempt fails
			{output: "Error: release: not found", err: fmt.Errorf("not found")}, // retry status check
			{output: "Release installed successfully", err: nil},                // retry succeeds
		},
	}
	comp := ComponentConfig{
		ID:              ComponentIngressNginx,
		HelmReleaseName: "ingress-nginx",
		HelmChart:       "ingress-nginx/ingress-nginx",
		Namespace:       "ingress-nginx",
		Version:         "4.11.3",
		Timeout:         "10m0s",
	}
	cfg := SetupConfig{Timestamp: time.Now()}

	result := InstallComponent(ex, comp, cfg)
	if !result.Succeeded {
		t.Fatalf("expected success after retry, got error: %v", result.Error)
	}
	if ex.callIdx != 4 {
		t.Errorf("expected 4 calls (status + install + retry-status + retry-install), got %d", ex.callIdx)
	}
}

func TestCheckChartAvailability_Missing(t *testing.T) {
	ex := &sequentialExecutor{
		calls: []seqCall{
			{output: "[]", err: nil}, // helm search repo (empty result)
			{output: `[{"name":"openobserve/openobserve","version":"0.60.0"}]`, err: nil}, // helm search repo --versions (available versions)
		},
	}
	components := []ComponentConfig{
		{ID: ComponentOpenObserve, HelmChart: "openobserve/openobserve", Version: "0.10.2"},
	}
	err := CheckChartAvailability(ex, components)
	if err == nil {
		t.Fatal("expected error for missing chart version, got nil")
	}
	if !strings.Contains(err.Error(), "chart version not found") {
		t.Errorf("expected 'chart version not found' in error, got: %v", err)
	}
}

func TestCheckChartAvailability_Found(t *testing.T) {
	ex := &sequentialExecutor{
		calls: []seqCall{
			{output: `[{"name":"openobserve/openobserve","version":"0.60.0"}]`, err: nil},
		},
	}
	components := []ComponentConfig{
		{ID: ComponentOpenObserve, HelmChart: "openobserve/openobserve", Version: "0.60.0"},
	}
	err := CheckChartAvailability(ex, components)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestCheckReleaseStatus_NotFound(t *testing.T) {
	ex := &sequentialExecutor{
		calls: []seqCall{
			{output: "Error: release: not found", err: fmt.Errorf("exit code 1")},
		},
	}
	status := CheckReleaseStatus(ex, "cert-manager", "cert-manager")
	if status != ReleaseNotFound {
		t.Errorf("expected ReleaseNotFound, got %q", status)
	}
}

func TestCheckReleaseStatus_Deployed(t *testing.T) {
	ex := &sequentialExecutor{
		calls: []seqCall{
			{output: `{"info":{"status":"deployed"}}`, err: nil},
		},
	}
	status := CheckReleaseStatus(ex, "cert-manager", "cert-manager")
	if status != ReleaseDeployed {
		t.Errorf("expected ReleaseDeployed, got %q", status)
	}
}

func TestCheckReleaseStatus_Failed(t *testing.T) {
	ex := &sequentialExecutor{
		calls: []seqCall{
			{output: `{"info":{"status":"failed"}}`, err: nil},
		},
	}
	status := CheckReleaseStatus(ex, "ingress-nginx", "ingress-nginx")
	if status != ReleaseFailed {
		t.Errorf("expected ReleaseFailed, got %q", status)
	}
}

func TestCleanFailedRelease_Success(t *testing.T) {
	ex := &sequentialExecutor{
		calls: []seqCall{
			{output: "release \"ingress-nginx\" uninstalled", err: nil},
		},
	}
	err := CleanFailedRelease(ex, "ingress-nginx", "ingress-nginx")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCleanFailedRelease_Error(t *testing.T) {
	ex := &sequentialExecutor{
		calls: []seqCall{
			{output: "Error: uninstall failed", err: fmt.Errorf("exit code 1")},
		},
	}
	err := CleanFailedRelease(ex, "ingress-nginx", "ingress-nginx")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestInstallComponent_CleansFailedReleaseBeforeInstall(t *testing.T) {
	origDelay := retryDelay
	retryDelay = 0
	defer func() { retryDelay = origDelay }()

	ex := &sequentialExecutor{
		calls: []seqCall{
			// CheckReleaseStatus
			{output: `{"info":{"status":"failed"}}`, err: nil},
			// CleanFailedRelease (helm uninstall)
			{output: "release uninstalled", err: nil},
			// helm upgrade --install (success)
			{output: "Release installed", err: nil},
		},
	}
	comp := ComponentConfig{
		ID:              ComponentCertManager,
		HelmReleaseName: "cert-manager",
		HelmChart:       "jetstack/cert-manager",
		Namespace:       "cert-manager",
		Version:         "v1.16.3",
	}
	cfg := SetupConfig{Timestamp: time.Now()}
	result := InstallComponent(ex, comp, cfg)
	if !result.Succeeded {
		t.Fatalf("expected success, got error: %v", result.Error)
	}
	if ex.callIdx != 3 {
		t.Errorf("expected 3 calls (status + uninstall + install), got %d", ex.callIdx)
	}
}

func TestInstallComponent_FreshInstall(t *testing.T) {
	origDelay := retryDelay
	retryDelay = 0
	defer func() { retryDelay = origDelay }()

	ex := &sequentialExecutor{
		calls: []seqCall{
			// CheckReleaseStatus (not found)
			{output: "Error: release: not found", err: fmt.Errorf("not found")},
			// helm upgrade --install (success)
			{output: "Release installed", err: nil},
		},
	}
	comp := ComponentConfig{
		ID:              ComponentCertManager,
		HelmReleaseName: "cert-manager",
		HelmChart:       "jetstack/cert-manager",
		Namespace:       "cert-manager",
		Version:         "v1.16.3",
	}
	cfg := SetupConfig{Timestamp: time.Now()}
	result := InstallComponent(ex, comp, cfg)
	if !result.Succeeded {
		t.Fatalf("expected success, got error: %v", result.Error)
func TestGetCurrentKubeContext(t *testing.T) {
	ex := &sequentialExecutor{
		calls: []seqCall{
			{output: "kind-dorgu-dev\n", err: nil},
		},
	}
	ctx, err := GetCurrentKubeContext(ex)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ctx != "kind-dorgu-dev" {
		t.Errorf("got context %q, want %q", ctx, "kind-dorgu-dev")
	}
}

func TestGetCurrentKubeContext_Error(t *testing.T) {
	ex := &sequentialExecutor{
		calls: []seqCall{
			{output: "", err: fmt.Errorf("exit code 1")},
		},
	}
	_, err := GetCurrentKubeContext(ex)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestValidateKubeContext_ProductionWarning(t *testing.T) {
	tests := []struct {
		context string
		want    bool
	}{
		{"arn:aws:eks:us-east-1:123:cluster/prod-main", true},
		{"gke_myorg_us-central1_prd-cluster", true},
		{"arn:aws:eks:us-west-2:123:cluster/live-api", true},
		{"kind-dorgu-dev", false},
		{"minikube", false},
		{"staging-cluster", false},
	}
	for _, tt := range tests {
		needsConfirm, _ := ValidateKubeContext(tt.context)
		if needsConfirm != tt.want {
			t.Errorf("ValidateKubeContext(%q) = %v, want %v", tt.context, needsConfirm, tt.want)
		}
	}
}

func TestCheckOperatorInstalled_CRDMissing(t *testing.T) {
	ex := &sequentialExecutor{
		calls: []seqCall{
			{output: "", err: fmt.Errorf("exit code 1")},
		},
	}
	err := CheckOperatorInstalled(ex)
	if err == nil {
		t.Fatal("expected error when CRD is missing")
	}
	if !strings.Contains(err.Error(), "CRD not found") {
		t.Errorf("error should mention CRD, got: %v", err)
	}
}

func TestCheckOperatorInstalled_PodNotRunning(t *testing.T) {
	ex := &sequentialExecutor{
		calls: []seqCall{
			{output: "clusterpersonas   dorgu.io/v1", err: nil},
			{output: "", err: nil},
		},
	}
	err := CheckOperatorInstalled(ex)
	if err == nil {
		t.Fatal("expected error when operator pod is not running")
	}
}

func TestCheckOperatorInstalled_AllGood(t *testing.T) {
	ex := &sequentialExecutor{
		calls: []seqCall{
			{output: "clusterpersonas   dorgu.io/v1", err: nil},
			{output: "dorgu-operator-abc123   1/1   Running   0   5m", err: nil},
		},
	}
	err := CheckOperatorInstalled(ex)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
