package setup

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScaffoldGitOpsRepo_DryRun(t *testing.T) {
	dir := t.TempDir()
	outDir := filepath.Join(dir, "gitops-output")

	cfg := GitOpsConfig{
		OutputDir:          outDir,
		ClusterPersonaName: "test-cluster",
		Environment:        "development",
		Components:         blessedComponents()[:2],
		DryRun:             true,
	}
	err := ScaffoldGitOpsRepo(cfg)
	if err != nil {
		t.Fatalf("unexpected error in dry-run: %v", err)
	}
	// Verify nothing was created
	if _, err := os.Stat(outDir); err == nil {
		t.Errorf("dry-run should not create directory %s", outDir)
	}
}

func TestScaffoldGitOpsRepo_CreatesStructure(t *testing.T) {
	dir := t.TempDir()
	outDir := filepath.Join(dir, "gitops-output")

	components := blessedComponents()[:2] // cert-manager and ingress-nginx
	cfg := GitOpsConfig{
		OutputDir:          outDir,
		ClusterPersonaName: "my-cluster",
		Environment:        "production",
		Components:         components,
		DryRun:             false,
	}
	err := ScaffoldGitOpsRepo(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedFiles := []string{
		"README.md",
		"argocd/root-app.yaml",
		"clusters/my-cluster/values/cert-manager.yaml",
		"clusters/my-cluster/values/ingress-nginx.yaml",
		"clusters/my-cluster/apps/cert-manager.yaml",
		"clusters/my-cluster/apps/ingress-nginx.yaml",
	}
	for _, f := range expectedFiles {
		path := filepath.Join(outDir, f)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("expected file %s to exist", f)
		}
	}
}

func TestScaffoldGitOpsRepo_ArgoAppContent(t *testing.T) {
	dir := t.TempDir()
	outDir := filepath.Join(dir, "gitops-output")

	components := blessedComponents()[:1] // just cert-manager
	cfg := GitOpsConfig{
		OutputDir:          outDir,
		ClusterPersonaName: "test-cluster",
		Environment:        "development",
		Components:         components,
		DryRun:             false,
	}
	err := ScaffoldGitOpsRepo(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	appPath := filepath.Join(outDir, "clusters/test-cluster/apps/cert-manager.yaml")
	data, err := os.ReadFile(appPath)
	if err != nil {
		t.Fatalf("failed to read %s: %v", appPath, err)
	}
	content := string(data)

	// Verify key fields
	if !containsStr(content, "kind: Application") {
		t.Error("ArgoCD Application YAML missing 'kind: Application'")
	}
	if !containsStr(content, "chart: jetstack/cert-manager") {
		t.Error("ArgoCD Application YAML missing chart reference")
	}
	if !containsStr(content, "namespace: cert-manager") {
		t.Error("ArgoCD Application YAML missing namespace")
	}
}
