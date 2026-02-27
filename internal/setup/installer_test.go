package setup

import (
	"strings"
	"testing"
	"time"
)

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
