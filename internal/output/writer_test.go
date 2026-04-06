package output

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dorgu-ai/dorgu/internal/generator"
)

// mockGeneratedAll mirrors generator.Generate output when ArgoCD/CI/Persona are enabled:
// k8s-local files plus project-root CI and persona paths (relative to output dir).
func mockGeneratedAll() []generator.GeneratedFile {
	return []generator.GeneratedFile{
		{Path: "deployment.yaml", Content: "apiVersion: apps/v1\nkind: Deployment\n"},
		{Path: "argocd/application.yaml", Content: "apiVersion: argoproj.io/v1alpha1\nkind: Application\n"},
		{Path: "../.github/workflows/deploy.yaml", Content: "name: deploy\non: push\n"},
		{Path: "../PERSONA.md", Content: "# Application Persona\n"},
		{Path: "persona.yaml", Content: "apiVersion: dorgu.io/v1\nkind: ApplicationPersona\n"},
	}
}

// TestWriteFiles_AllOutputs_SucceedsOutsideOutputDir is the write-path equivalent of
// TestGenerate_AllOutputs_WriteSucceedsOutsideOutputDir (BUG-4-1): temp project with
// subdir k8s, same relative paths as generator.Generate, call WriteFiles(k8sDir, files).
// On unfixed code this fails with path traversal detected (expected until fix lands).
func TestWriteFiles_AllOutputs_SucceedsOutsideOutputDir(t *testing.T) {
	t.Parallel()

	projectRoot := t.TempDir()
	k8sDir := filepath.Join(projectRoot, "k8s")

	err := WriteFiles(projectRoot, k8sDir, mockGeneratedAll())
	require.NoError(t, err, "writing CI/persona outside k8s output dir should succeed")

	require.FileExists(t, filepath.Join(k8sDir, "deployment.yaml"))
	require.FileExists(t, filepath.Join(k8sDir, "argocd", "application.yaml"))
	require.FileExists(t, filepath.Join(k8sDir, "persona.yaml"))
	require.FileExists(t, filepath.Join(projectRoot, ".github", "workflows", "deploy.yaml"))
	require.FileExists(t, filepath.Join(projectRoot, "PERSONA.md"))
}

// Edge cases: CI-only and persona-only outputs must still reach the project root (skip flags).
// On unfixed code these fail the same traversal guard (tests fail until fix).
func TestWriteFiles_SkipPersonaOnly_CISucceeds(t *testing.T) {
	t.Parallel()

	projectRoot := t.TempDir()
	k8sDir := filepath.Join(projectRoot, "k8s")

	files := []generator.GeneratedFile{
		{Path: "deployment.yaml", Content: "kind: Deployment\n"},
		{Path: "../.github/workflows/deploy.yaml", Content: "name: ci\n"},
	}

	err := WriteFiles(projectRoot, k8sDir, files)
	require.NoError(t, err)

	b, err := os.ReadFile(filepath.Join(projectRoot, ".github", "workflows", "deploy.yaml"))
	require.NoError(t, err)
	require.Contains(t, string(b), "name: ci")
}

func TestWriteFiles_SkipCIOnly_PersonaSucceeds(t *testing.T) {
	t.Parallel()

	projectRoot := t.TempDir()
	k8sDir := filepath.Join(projectRoot, "k8s")

	files := []generator.GeneratedFile{
		{Path: "deployment.yaml", Content: "kind: Deployment\n"},
		{Path: "../PERSONA.md", Content: "# Doc\n"},
		{Path: "persona.yaml", Content: "kind: ApplicationPersona\n"},
	}

	err := WriteFiles(projectRoot, k8sDir, files)
	require.NoError(t, err)

	require.FileExists(t, filepath.Join(projectRoot, "PERSONA.md"))
	require.FileExists(t, filepath.Join(k8sDir, "persona.yaml"))
}

// Regression: when both CI and persona are skipped, generator emits only in-output-dir paths;
// writes must succeed (passes on unfixed code).
func TestWriteFiles_SkipCISkipPersona_OnlyUnderOutputDir(t *testing.T) {
	t.Parallel()

	projectRoot := t.TempDir()
	k8sDir := filepath.Join(projectRoot, "k8s")

	files := []generator.GeneratedFile{
		{Path: "deployment.yaml", Content: "kind: Deployment\n"},
		{Path: "argocd/application.yaml", Content: "kind: Application\n"},
	}

	err := WriteFiles(projectRoot, k8sDir, files)
	require.NoError(t, err)

	require.FileExists(t, filepath.Join(k8sDir, "deployment.yaml"))
	require.FileExists(t, filepath.Join(k8sDir, "argocd", "application.yaml"))
}

// TestWriteFiles_RejectsEscapeAboveProjectRoot ensures paths that escape projectRoot are blocked.
func TestWriteFiles_RejectsEscapeAboveProjectRoot(t *testing.T) {
	t.Parallel()

	projectRoot := t.TempDir()
	k8sDir := filepath.Join(projectRoot, "k8s")

	files := []generator.GeneratedFile{
		{Path: "../../etc/passwd", Content: "malicious"},
	}

	err := WriteFiles(projectRoot, k8sDir, files)
	require.Error(t, err)
	require.Contains(t, err.Error(), "path traversal detected")
}
