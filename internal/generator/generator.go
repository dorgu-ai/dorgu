package generator

import (
	"fmt"

	"github.com/dorgu-ai/dorgu/internal/config"
	"github.com/dorgu-ai/dorgu/internal/llm"
	"github.com/dorgu-ai/dorgu/internal/types"
)

// Options contains generation options
type Options struct {
	Namespace   string
	SkipArgoCD  bool
	SkipCI      bool
	SkipPersona bool
	Config      *config.Config
}

// GeneratedFile represents a generated file
type GeneratedFile struct {
	Path    string
	Content string
}

// Generate generates all manifests for an analyzed application
func Generate(analysis *types.AppAnalysis, opts Options) ([]GeneratedFile, error) {
	var files []GeneratedFile

	// Get resource spec based on profile
	resources := opts.Config.GetResourcesForProfile(analysis.ResourceProfile)

	// Generate Deployment
	deployment, err := GenerateDeployment(analysis, opts.Namespace, resources, opts.Config)
	if err != nil {
		return nil, err
	}
	files = append(files, GeneratedFile{
		Path:    "deployment.yaml",
		Content: deployment,
	})

	// Generate Service (only if ports are exposed)
	if len(analysis.Ports) > 0 {
		service, err := GenerateService(analysis, opts.Namespace, opts.Config)
		if err != nil {
			return nil, err
		}
		files = append(files, GeneratedFile{
			Path:    "service.yaml",
			Content: service,
		})

		// Generate Ingress (only for HTTP services)
		if hasHTTPPort(analysis.Ports) {
			ingress, err := GenerateIngress(analysis, opts.Namespace, opts.Config)
			if err != nil {
				return nil, err
			}
			files = append(files, GeneratedFile{
				Path:    "ingress.yaml",
				Content: ingress,
			})
		}
	}

	// Generate HPA (if scaling config present)
	if analysis.Scaling != nil {
		hpa, err := GenerateHPA(analysis, opts.Namespace, opts.Config)
		if err != nil {
			return nil, err
		}
		files = append(files, GeneratedFile{
			Path:    "hpa.yaml",
			Content: hpa,
		})
	}

	// Generate ArgoCD Application
	if !opts.SkipArgoCD {
		argoApp, err := GenerateArgoCD(analysis, opts.Namespace, opts.Config)
		if err != nil {
			return nil, err
		}
		files = append(files, GeneratedFile{
			Path:    "argocd/application.yaml",
			Content: argoApp,
		})
	}

	// Generate GitHub Actions workflow
	if !opts.SkipCI {
		workflow, err := GenerateGitHubActions(analysis, opts.Config)
		if err != nil {
			return nil, err
		}
		files = append(files, GeneratedFile{
			Path:    "../.github/workflows/deploy.yaml",
			Content: workflow,
		})
	}

	// Generate Persona document
	if !opts.SkipPersona {
		persona, err := generatePersona(analysis, opts.Config)
		if err != nil {
			// Non-fatal: use basic persona if LLM fails
			persona = generateBasicPersona(analysis)
		}
		files = append(files, GeneratedFile{
			Path:    "../PERSONA.md",
			Content: persona,
		})

		// Generate structured Persona YAML (ApplicationPersona CRD format)
		personaYAML, err := GeneratePersonaYAML(analysis, opts.Namespace, opts.Config)
		if err != nil {
			// Non-fatal: skip persona YAML if generation fails
			fmt.Printf("Warning: failed to generate persona YAML: %v\n", err)
		} else {
			files = append(files, GeneratedFile{
				Path:    "persona.yaml",
				Content: personaYAML,
			})
		}
	}

	return files, nil
}

// hasHTTPPort checks if any port is likely HTTP
func hasHTTPPort(ports []types.Port) bool {
	httpPorts := map[int]bool{80: true, 443: true, 8080: true, 3000: true, 5000: true, 8000: true}
	for _, p := range ports {
		if httpPorts[p.Port] || p.Purpose == "HTTP" || p.Purpose == "HTTP API" {
			return true
		}
	}
	return len(ports) > 0 // Assume HTTP if any port is exposed
}

// generatePersona generates persona using LLM
func generatePersona(analysis *types.AppAnalysis, cfg *config.Config) (string, error) {
	client, err := llm.NewClient(cfg.LLM.Provider)
	if err != nil {
		return "", err
	}

	return client.GeneratePersona(analysis)
}

// generateBasicPersona generates a basic persona without LLM
func generateBasicPersona(analysis *types.AppAnalysis) string {
	// Build description from app config or fallback
	description := analysis.Description
	if analysis.AppConfig != nil && analysis.AppConfig.Description != "" {
		description = analysis.AppConfig.Description
	}
	if description == "" {
		description = fmt.Sprintf("A containerized %s application", analysis.Type)
	}

	// Build ownership section from app config
	team := "[PLACEHOLDER]"
	contact := "[PLACEHOLDER]"
	repository := "[PLACEHOLDER]"
	if analysis.Team != "" {
		team = analysis.Team
	}
	if analysis.Owner != "" {
		contact = analysis.Owner
	}
	if analysis.Repository != "" {
		repository = analysis.Repository
	}

	// Build operations section
	operationsNotes := "*Add operational notes here after deploying the application.*"
	if analysis.AppConfig != nil && analysis.AppConfig.Operations != nil {
		ops := analysis.AppConfig.Operations
		operationsNotes = ""
		if ops.Runbook != "" {
			operationsNotes += "- **Runbook:** " + ops.Runbook + "\n"
		}
		if ops.OnCall != "" {
			operationsNotes += "- **On-Call:** " + ops.OnCall + "\n"
		}
		if ops.MaintenanceWindow != "" {
			operationsNotes += "- **Maintenance Window:** " + ops.MaintenanceWindow + "\n"
		}
		if len(ops.Alerts) > 0 {
			operationsNotes += "\n### Configured Alerts\n"
			for _, alert := range ops.Alerts {
				operationsNotes += "- " + alert + "\n"
			}
		}
		if operationsNotes == "" {
			operationsNotes = "*Add operational notes here after deploying the application.*"
		}
	}

	// Build dependencies section - use app config dependencies if available
	depsSection := formatDependencies(analysis.Dependencies)
	if analysis.AppConfig != nil && len(analysis.AppConfig.Dependencies) > 0 {
		depsSection = ""
		for _, dep := range analysis.AppConfig.Dependencies {
			required := ""
			if dep.Required {
				required = " (required)"
			}
			depsSection += fmt.Sprintf("- **%s** (%s)%s\n", dep.Name, dep.Type, required)
		}
	}

	// Build context section if instructions available
	contextSection := ""
	if analysis.AppConfig != nil && analysis.AppConfig.Instructions != "" {
		contextSection = `

## Application Context

` + analysis.AppConfig.Instructions
	}

	return `# ` + analysis.Name + `

## Overview

` + description + contextSection + `

## Technical Stack

- **Language:** ` + analysis.Language + `
- **Framework:** ` + analysis.Framework + `
- **Type:** ` + analysis.Type + `

## API/Interfaces

` + formatPorts(analysis.Ports) + `

## External Dependencies

` + depsSection + `

## Resource Profile

- **Profile:** ` + analysis.ResourceProfile + `
- **Scaling:** ` + formatScalingDetails(analysis) + `

## Health & Monitoring

` + formatHealthCheck(analysis.HealthCheck) + `

## Ownership

- **Team:** ` + team + `
- **Contact:** ` + contact + `
- **Repository:** ` + repository + `

## Operational Notes

` + operationsNotes + `
`
}

func formatPorts(ports []types.Port) string {
	if len(ports) == 0 {
		return "No ports exposed."
	}
	result := ""
	for _, p := range ports {
		result += fmt.Sprintf("- Port %d (%s): %s\n", p.Port, p.Protocol, p.Purpose)
	}
	return result
}

func formatDependencies(deps []string) string {
	if len(deps) == 0 {
		return "No external dependencies detected."
	}
	result := ""
	for _, d := range deps {
		result += "- " + d + "\n"
	}
	return result
}

func formatScaling(scaling *types.ScalingConfig) string {
	if scaling == nil {
		return "No auto-scaling configured"
	}
	return "replicas, Max replicas, Target CPU %"
}

func formatScalingDetails(analysis *types.AppAnalysis) string {
	scaling := analysis.Scaling
	if analysis.AppConfig != nil && analysis.AppConfig.Scaling != nil {
		scaling = analysis.AppConfig.Scaling
	}
	if scaling == nil {
		return "No auto-scaling configured"
	}
	result := fmt.Sprintf("Min %d replicas, Max %d replicas", scaling.MinReplicas, scaling.MaxReplicas)
	if scaling.TargetCPU > 0 {
		result += fmt.Sprintf(", Target CPU %d%%", scaling.TargetCPU)
	}
	if scaling.TargetMemory > 0 {
		result += fmt.Sprintf(", Target Memory %d%%", scaling.TargetMemory)
	}
	return result
}

func formatHealthCheck(hc *types.HealthCheck) string {
	if hc == nil {
		return "No health check configured."
	}
	return "- **Health endpoint:** " + hc.Path + "\n"
}
