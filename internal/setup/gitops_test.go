package setup

import (
	"os"
	"path/filepath"
	"strings"
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
		RepoURL:            "https://github.com/org/my-gitops.git",
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
		RepoURL:            "https://github.com/org/test-repo.git",
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
	if !strings.Contains(content, "kind: Application") {
		t.Error("ArgoCD Application YAML missing 'kind: Application'")
	}
	if !strings.Contains(content, "chart: jetstack/cert-manager") {
		t.Error("ArgoCD Application YAML missing chart reference")
	}
	if !strings.Contains(content, "namespace: cert-manager") {
		t.Error("ArgoCD Application YAML missing namespace")
	}
}

func TestScaffoldGitOpsRepo_RepoURLPopulated(t *testing.T) {
	dir := t.TempDir()
	outDir := filepath.Join(dir, "gitops-output")
	cfg := GitOpsConfig{
		OutputDir:          outDir,
		ClusterPersonaName: "test-cluster",
		Environment:        "development",
		Components:         blessedComponents()[:1],
		DryRun:             false,
		RepoURL:            "https://github.com/org/my-gitops.git",
	}
	err := ScaffoldGitOpsRepo(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	rootApp, err := os.ReadFile(filepath.Join(outDir, "argocd", "root-app.yaml"))
	if err != nil {
		t.Fatalf("failed to read root-app.yaml: %v", err)
	}
	if !strings.Contains(string(rootApp), "https://github.com/org/my-gitops.git") {
		t.Error("root-app.yaml should contain the actual repo URL")
	}
	if strings.Contains(string(rootApp), "<YOUR_GIT_REPO_URL>") {
		t.Error("root-app.yaml should not contain placeholder")
	}
}

func TestScaffoldGitOpsRepo_MissingRepoURL(t *testing.T) {
	dir := t.TempDir()
	cfg := GitOpsConfig{
		OutputDir:          filepath.Join(dir, "out"),
		ClusterPersonaName: "test",
		Environment:        "development",
		Components:         blessedComponents()[:1],
		DryRun:             false,
		RepoURL:            "",
	}
	err := ScaffoldGitOpsRepo(cfg)
	if err == nil {
		t.Fatal("expected error for empty RepoURL")
	}
}
