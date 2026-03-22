package analyzer

import (
	"fmt"

	"github.com/dorgu-ai/dorgu/internal/llm"
	"github.com/dorgu-ai/dorgu/internal/types"
)

// enhanceWithLLM uses an LLM to provide deeper analysis
func enhanceWithLLM(analysis *types.AppAnalysis, provider string) error {
	client, err := llm.NewClient(provider)
	if err != nil {
		return err
	}

	enhanced, err := client.AnalyzeApp(analysis)
	if err != nil {
		return err
	}

	// Merge LLM analysis with existing analysis
	if enhanced.Type != "" {
		analysis.Type = enhanced.Type
	}
	if enhanced.Description != "" {
		analysis.Description = enhanced.Description
	}
	if enhanced.Framework != "" {
		analysis.Framework = enhanced.Framework
	}
	if enhanced.Language != "" {
		analysis.Language = enhanced.Language
	}
	if len(enhanced.Dependencies) > 0 {
		analysis.Dependencies = enhanced.Dependencies
	}
	if enhanced.ResourceProfile != "" {
		analysis.ResourceProfile = enhanced.ResourceProfile
	}
	if enhanced.Scaling != nil {
		analysis.Scaling = enhanced.Scaling
	}
	if enhanced.HealthCheck != nil {
		analysis.HealthCheck = enhanced.HealthCheck
	}
	if len(enhanced.Ports) > 0 {
		analysis.Ports = enhanced.Ports
	}
	// Merge runs_as_root from LLM (generator uses ImageRunsAsRoot(analysis), so Dockerfile wins when present)
	analysis.RunsAsRoot = enhanced.RunsAsRoot

	// Ensure we still have ports from Dockerfile if LLM didn't provide them
	if len(analysis.Ports) == 0 && analysis.Dockerfile != nil {
		for _, port := range analysis.Dockerfile.Ports {
			analysis.Ports = append(analysis.Ports, types.Port{
				Port:     port,
				Protocol: "TCP",
				Purpose:  "HTTP",
			})
		}
	}

	// Ensure we have defaults for required fields
	if analysis.Type == "" {
		analysis.Type = "api"
	}
	if analysis.ResourceProfile == "" {
		analysis.ResourceProfile = "api"
	}
	if analysis.Scaling == nil {
		analysis.Scaling = &types.ScalingConfig{
			MinReplicas: 2,
			MaxReplicas: 10,
			TargetCPU:   70,
		}
	}

	// Set health check from code analysis if not provided by LLM
	if analysis.HealthCheck == nil && analysis.Code != nil && analysis.Code.HealthPath != "" {
		port := 8080
		if len(analysis.Ports) > 0 {
			port = analysis.Ports[0].Port
		}
		analysis.HealthCheck = &types.HealthCheck{
			Path: analysis.Code.HealthPath,
			Port: port,
		}
	}

	return nil
}

// populateDefaults fills in default values when LLM is not available
func populateDefaults(analysis *types.AppAnalysis) {
	if analysis.Type == "" {
		analysis.Type = "api"
	}
	if analysis.ResourceProfile == "" {
		analysis.ResourceProfile = "api"
	}
	if analysis.Scaling == nil {
		analysis.Scaling = &types.ScalingConfig{
			MinReplicas: 2,
			MaxReplicas: 10,
			TargetCPU:   70,
		}
	}
	if analysis.Description == "" {
		analysis.Description = fmt.Sprintf("A containerized %s application", analysis.Type)
	}

	// Extract ports from Dockerfile if available
	if analysis.Dockerfile != nil && len(analysis.Ports) == 0 {
		for _, port := range analysis.Dockerfile.Ports {
			analysis.Ports = append(analysis.Ports, types.Port{
				Port:     port,
				Protocol: "TCP",
				Purpose:  "HTTP",
			})
		}
	}

	// Extract language/framework from code analysis if available
	if analysis.Code != nil {
		if analysis.Language == "" {
			analysis.Language = analysis.Code.Language
		}
		if analysis.Framework == "" {
			analysis.Framework = analysis.Code.Framework
		}
		if analysis.HealthCheck == nil && analysis.Code.HealthPath != "" {
			port := 8080
			if len(analysis.Ports) > 0 {
				port = analysis.Ports[0].Port
			}
			analysis.HealthCheck = &types.HealthCheck{
				Path: analysis.Code.HealthPath,
				Port: port,
			}
		}
	}
}
