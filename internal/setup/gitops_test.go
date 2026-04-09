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
	if !strings.Contains(content, "chart: cert-manager") {
		t.Error("ArgoCD Application YAML missing chart reference")
	}
	if strings.Contains(content, "chart: jetstack/cert-manager") {
		t.Error("chart field should not include repo prefix")
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

func TestScaffoldGitOpsRepo_ChartNamesWithoutRepoPrefix(t *testing.T) {
	dir := t.TempDir()
	outDir := filepath.Join(dir, "gitops-output")

	components := blessedComponents()
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

	for _, c := range components {
		appPath := filepath.Join(outDir, "clusters/test-cluster/apps", string(c.ID)+".yaml")
		data, err := os.ReadFile(appPath)
		if err != nil {
			t.Fatalf("failed to read %s: %v", appPath, err)
		}
		content := string(data)

		// Chart name should NOT contain "/"
		for _, line := range strings.Split(content, "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "chart:") {
				chartValue := strings.TrimSpace(strings.TrimPrefix(trimmed, "chart:"))
				if strings.Contains(chartValue, "/") {
					t.Errorf("component %s: chart field %q should not contain repo prefix", c.ID, chartValue)
				}
			}
		}
	}
}

func TestScaffoldGitOpsRepo_DevEnvironmentOverrides(t *testing.T) {
	dir := t.TempDir()
	outDir := filepath.Join(dir, "gitops-output")

	components := blessedComponents()
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

	// OpenObserve should have dev overrides
	ooPath := filepath.Join(outDir, "clusters/test-cluster/values/openobserve.yaml")
	data, err := os.ReadFile(ooPath)
	if err != nil {
		t.Fatalf("failed to read openobserve values: %v", err)
	}
	content := string(data)

	if !strings.Contains(content, "config.ZO_LOCAL_MODE") {
		t.Error("openobserve values should contain ZO_LOCAL_MODE for development environment")
	}
}

func TestScaffoldGitOpsRepo_ProductionNoDevOverrides(t *testing.T) {
	dir := t.TempDir()
	outDir := filepath.Join(dir, "gitops-output")

	components := blessedComponents()
	cfg := GitOpsConfig{
		OutputDir:          outDir,
		ClusterPersonaName: "test-cluster",
		Environment:        "production",
		Components:         components,
		DryRun:             false,
		RepoURL:            "https://github.com/org/test-repo.git",
	}
	err := ScaffoldGitOpsRepo(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ooPath := filepath.Join(outDir, "clusters/test-cluster/values/openobserve.yaml")
	data, err := os.ReadFile(ooPath)
	if err != nil {
		t.Fatalf("failed to read openobserve values: %v", err)
	}
	content := string(data)

	if strings.Contains(content, "ZO_LOCAL_MODE") {
		t.Error("openobserve values should NOT contain ZO_LOCAL_MODE for production environment")
	}
}

func TestGitOpsREADME_ExcludesValuesFromDryRun(t *testing.T) {
	dir := t.TempDir()
	outDir := filepath.Join(dir, "gitops-output")
	cfg := GitOpsConfig{
		OutputDir:          outDir,
		ClusterPersonaName: "test-cluster",
		Environment:        "development",
		Components:         blessedComponents()[:1],
		DryRun:             false,
		RepoURL:            "https://github.com/org/test-repo.git",
	}
	if err := ScaffoldGitOpsRepo(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(outDir, "README.md"))
	if err != nil {
		t.Fatalf("failed to read README.md: %v", err)
	}
	content := string(data)

	// Validate section must reference apps/ for dry-run
	if !strings.Contains(content, "apps/") {
		t.Error("README should reference apps/ in validation instructions")
	}
	// README must warn against applying values/
	if !strings.Contains(content, "Do NOT") || !strings.Contains(content, "values/") {
		t.Error("README should warn against applying values/ directly")
	}
}

func TestGitOpsApplication_HasServerSideApply(t *testing.T) {
	dir := t.TempDir()
	outDir := filepath.Join(dir, "gitops-output")
	cfg := GitOpsConfig{
		OutputDir:          outDir,
		ClusterPersonaName: "test-cluster",
		Environment:        "development",
		Components:         blessedComponents()[:1],
		DryRun:             false,
		RepoURL:            "https://github.com/org/test-repo.git",
	}
	if err := ScaffoldGitOpsRepo(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	appPath := filepath.Join(outDir, "clusters/test-cluster/apps/cert-manager.yaml")
	data, err := os.ReadFile(appPath)
	if err != nil {
		t.Fatalf("failed to read app: %v", err)
	}
	if !strings.Contains(string(data), "ServerSideApply=true") {
		t.Error("ArgoCD Application should include ServerSideApply=true sync option")
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
