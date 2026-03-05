package setup

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/dorgu-ai/dorgu/internal/output"
)

// GitOpsConfig holds parameters for GitOps repo scaffolding.
type GitOpsConfig struct {
	OutputDir          string
	ClusterPersonaName string
	Environment        string
	Components         []ComponentConfig
	DryRun             bool
}

// ScaffoldGitOpsRepo creates a directory structure suitable for ArgoCD
// that references upstream Helm chart repos directly.
//
// Generated structure:
//
//	<outputDir>/
//	  README.md
//	  argocd/
//	    root-app.yaml
//	  clusters/
//	    <clusterPersonaName>/
//	      values/
//	        <component>.yaml
//	      apps/
//	        <component>.yaml
func ScaffoldGitOpsRepo(cfg GitOpsConfig) error {
	if cfg.DryRun {
		output.Info("GitOps mode (dry-run): would scaffold to " + cfg.OutputDir)
		fmt.Println()
		printGitOpsStructure(cfg)
		return nil
	}

	output.Header("Scaffolding GitOps repository...")
	fmt.Println()

	dirs := []string{
		filepath.Join(cfg.OutputDir, "argocd"),
		filepath.Join(cfg.OutputDir, "clusters", cfg.ClusterPersonaName, "values"),
		filepath.Join(cfg.OutputDir, "clusters", cfg.ClusterPersonaName, "apps"),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	// README.md
	readmePath := filepath.Join(cfg.OutputDir, "README.md")
	if err := writeTemplateFile(readmePath, gitopsReadmeTmpl, cfg); err != nil {
		return err
	}
	output.Success("README.md")

	// root-app.yaml (ArgoCD App-of-Apps)
	rootAppPath := filepath.Join(cfg.OutputDir, "argocd", "root-app.yaml")
	if err := writeTemplateFile(rootAppPath, gitopsRootAppTmpl, cfg); err != nil {
		return err
	}
	output.Success("argocd/root-app.yaml")

	// Per-component ArgoCD Application + values override files
	for _, c := range cfg.Components {
		valuesPath := filepath.Join(cfg.OutputDir, "clusters", cfg.ClusterPersonaName, "values", string(c.ID)+".yaml")
		if err := writeComponentValues(valuesPath, c); err != nil {
			return err
		}
		output.Success(fmt.Sprintf("clusters/%s/values/%s.yaml", cfg.ClusterPersonaName, c.ID))

		appPath := filepath.Join(cfg.OutputDir, "clusters", cfg.ClusterPersonaName, "apps", string(c.ID)+".yaml")
		appData := struct {
			Component          ComponentConfig
			ClusterPersonaName string
			Environment        string
		}{c, cfg.ClusterPersonaName, cfg.Environment}
		if err := writeTemplateFile(appPath, gitopsArgoAppTmpl, appData); err != nil {
			return err
		}
		output.Success(fmt.Sprintf("clusters/%s/apps/%s.yaml", cfg.ClusterPersonaName, c.ID))
	}

	fmt.Println()
	output.Header("GitOps repository scaffolded")
	fmt.Println()
	output.Info(fmt.Sprintf("Directory: %s", cfg.OutputDir))
	output.Info("Next steps:")
	output.DimPrint("  1. cd " + cfg.OutputDir)
	output.DimPrint("  2. git init && git add -A && git commit -m 'Initial cluster GitOps scaffold'")
	output.DimPrint("  3. Push to your Git remote")
	output.DimPrint("  4. Update argocd/root-app.yaml with your Git repo URL")
	output.DimPrint("  5. Apply the root app: kubectl apply -f argocd/root-app.yaml")
	fmt.Println()

	return nil
}

func printGitOpsStructure(cfg GitOpsConfig) {
	output.DimPrint(fmt.Sprintf("  %s/", cfg.OutputDir))
	output.DimPrint("    README.md")
	output.DimPrint("    argocd/")
	output.DimPrint("      root-app.yaml")
	output.DimPrint(fmt.Sprintf("    clusters/%s/", cfg.ClusterPersonaName))
	output.DimPrint("      values/")
	for _, c := range cfg.Components {
		output.DimPrint(fmt.Sprintf("        %s.yaml", c.ID))
	}
	output.DimPrint("      apps/")
	for _, c := range cfg.Components {
		output.DimPrint(fmt.Sprintf("        %s.yaml", c.ID))
	}
}

func writeTemplateFile(path, tmplStr string, data interface{}) error {
	tmpl, err := template.New(filepath.Base(path)).Parse(tmplStr)
	if err != nil {
		return fmt.Errorf("template parse error for %s: %w", path, err)
	}
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to create %s: %w", path, err)
	}
	defer f.Close()
	if err := tmpl.Execute(f, data); err != nil {
		return fmt.Errorf("template execute error for %s: %w", path, err)
	}
	return nil
}

func writeComponentValues(path string, c ComponentConfig) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to create %s: %w", path, err)
	}
	defer f.Close()

	fmt.Fprintf(f, "# Helm values override for %s\n", c.DisplayName)
	fmt.Fprintf(f, "# Chart: %s\n", c.HelmChart)
	fmt.Fprintf(f, "# Version: %s\n", c.Version)
	fmt.Fprintf(f, "# Edit this file to customize the component for your cluster.\n\n")

	for _, sv := range c.HelmSetValues {
		parts := strings.SplitN(sv, "=", 2)
		if len(parts) == 2 {
			fmt.Fprintf(f, "%s: %s\n", parts[0], parts[1])
		}
	}
	return nil
}

var gitopsReadmeTmpl = `# Dorgu Cluster GitOps Repository

This repository was scaffolded by ` + "`dorgu cluster setup --gitops`" + `.

## Structure

- ` + "`argocd/root-app.yaml`" + ` — ArgoCD App-of-Apps that discovers all component applications
- ` + "`clusters/{{.ClusterPersonaName}}/apps/`" + ` — one ArgoCD Application per component
- ` + "`clusters/{{.ClusterPersonaName}}/values/`" + ` — Helm value overrides per component

## Getting Started

1. Push this repo to your Git remote
2. Update ` + "`argocd/root-app.yaml`" + ` with your Git repo URL (replace ` + "`<YOUR_GIT_REPO_URL>`" + `)
3. Apply the root application: ` + "`kubectl apply -f argocd/root-app.yaml`" + `
4. ArgoCD will sync all components automatically

## Customizing Components

Edit the values files in ` + "`clusters/{{.ClusterPersonaName}}/values/`" + ` to override Helm chart defaults.
Commit and push — ArgoCD will detect and apply the changes.

## Component Versions

To upgrade a component, edit the ` + "`targetRevision`" + ` in its ArgoCD Application file under ` + "`apps/`" + `.
`

var gitopsRootAppTmpl = `apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: {{.ClusterPersonaName}}-root
  namespace: argocd
spec:
  project: default
  source:
    repoURL: <YOUR_GIT_REPO_URL>
    path: clusters/{{.ClusterPersonaName}}/apps
    targetRevision: HEAD
  destination:
    server: https://kubernetes.default.svc
    namespace: argocd
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
`

var gitopsArgoAppTmpl = `apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: {{.Component.HelmReleaseName}}
  namespace: argocd
  labels:
    dorgu.io/cluster-persona: {{.ClusterPersonaName}}
    dorgu.io/environment: {{.Environment}}
spec:
  project: default
  source:
    repoURL: {{.Component.HelmRepo}}
    chart: {{.Component.HelmChart}}
    targetRevision: {{.Component.Version}}
    helm:
      releaseName: {{.Component.HelmReleaseName}}
  destination:
    server: https://kubernetes.default.svc
    namespace: {{.Component.Namespace}}
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
    syncOptions:
      - CreateNamespace=true
`
