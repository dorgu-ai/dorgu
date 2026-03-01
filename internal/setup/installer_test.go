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
			{output: "", err: fmt.Errorf("context deadline exceeded")},  // first attempt fails
			{output: "Release installed successfully", err: nil},         // retry succeeds
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
	if ex.callIdx != 2 {
		t.Errorf("expected 2 calls (initial + retry), got %d", ex.callIdx)
	}
}

func TestCheckChartAvailability_Missing(t *testing.T) {
	ex := &sequentialExecutor{
		calls: []seqCall{
			{output: "[]", err: nil},                                         // helm search repo (empty result)
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
