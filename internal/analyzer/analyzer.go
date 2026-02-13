package analyzer

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/dorgu-ai/dorgu/internal/config"
	"github.com/dorgu-ai/dorgu/internal/llm"
	"github.com/dorgu-ai/dorgu/internal/types"
)

// Analyze performs complete analysis of an application at the given path
func Analyze(path string, llmProvider string) (*types.AppAnalysis, error) {
	analysis := &types.AppAnalysis{}

	// Try to detect app name from directory
	analysis.Name = filepath.Base(path)

	// Load app-specific config if available
	appConfig, err := config.LoadAppConfig(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to load app config: %v\n", err)
	}
	if appConfig != nil {
		// Apply app config to analysis
		applyAppConfig(analysis, appConfig)
	}

	// Check for Dockerfile
	dockerfilePath := findDockerfile(path)
	if dockerfilePath != "" {
		dockerAnalysis, err := ParseDockerfile(dockerfilePath)
		if err != nil {
			return nil, fmt.Errorf("failed to parse Dockerfile: %w", err)
		}
		analysis.Dockerfile = dockerAnalysis
	}

	// Check for docker-compose
	composePath := findComposeFile(path)
	if composePath != "" {
		composeAnalysis, err := ParseComposeFile(composePath)
		if err != nil {
			// Non-fatal: continue without compose analysis
			fmt.Fprintf(os.Stderr, "Warning: failed to parse docker-compose: %v\n", err)
		} else {
			analysis.Compose = composeAnalysis
		}
	}

	// Analyze source code
	codeAnalysis, err := AnalyzeCode(path)
	if err != nil {
		// Non-fatal: continue without code analysis
		fmt.Fprintf(os.Stderr, "Warning: failed to analyze code: %v\n", err)
	} else {
		analysis.Code = codeAnalysis
	}

	// If no Dockerfile or compose found, we can't proceed
	if analysis.Dockerfile == nil && analysis.Compose == nil {
		return nil, fmt.Errorf("no Dockerfile or docker-compose.yml found in %s", path)
	}

	// Use LLM to enhance analysis
	if err := enhanceWithLLM(analysis, llmProvider); err != nil {
		// Non-fatal: continue with basic analysis
		fmt.Fprintf(os.Stderr, "Warning: LLM analysis failed, using basic analysis: %v\n", err)
		populateDefaults(analysis)
	}

	return analysis, nil
}

// findDockerfile looks for a Dockerfile in the given path
func findDockerfile(path string) string {
	candidates := []string{
		filepath.Join(path, "Dockerfile"),
		filepath.Join(path, "dockerfile"),
		filepath.Join(path, "Dockerfile.prod"),
		filepath.Join(path, "Dockerfile.production"),
	}

	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return ""
}

// findComposeFile looks for a docker-compose file in the given path
func findComposeFile(path string) string {
	candidates := []string{
		filepath.Join(path, "docker-compose.yml"),
		filepath.Join(path, "docker-compose.yaml"),
		filepath.Join(path, "compose.yml"),
		filepath.Join(path, "compose.yaml"),
	}

	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return ""
}

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

// applyAppConfig applies app-specific configuration to the analysis
func applyAppConfig(analysis *types.AppAnalysis, appConfig *config.AppConfig) {
	// Create app config context
	ctx := &types.AppConfigContext{}

	// App metadata
	if appConfig.App.Name != "" {
		ctx.Name = appConfig.App.Name
		// Override analysis name with app config name
		analysis.Name = appConfig.App.Name
	}
	if appConfig.App.Description != "" {
		ctx.Description = appConfig.App.Description
	}
	if appConfig.App.Team != "" {
		ctx.Team = appConfig.App.Team
		analysis.Team = appConfig.App.Team
	}
	if appConfig.App.Owner != "" {
		ctx.Owner = appConfig.App.Owner
		analysis.Owner = appConfig.App.Owner
	}
	if appConfig.App.Repository != "" {
		ctx.Repository = appConfig.App.Repository
		analysis.Repository = appConfig.App.Repository
	}
	if appConfig.App.Type != "" {
		ctx.Type = appConfig.App.Type
		analysis.Type = appConfig.App.Type
		analysis.ResourceProfile = appConfig.App.Type
	}
	if appConfig.App.Tier != "" {
		ctx.Tier = appConfig.App.Tier
	}
	if appConfig.App.Instructions != "" {
		ctx.Instructions = appConfig.App.Instructions
	}

	// Environment
	if appConfig.Environment != "" {
		ctx.Environment = appConfig.Environment
		analysis.Environment = appConfig.Environment
	}

	// Resource overrides
	if appConfig.Resources != nil {
		ctx.Resources = &types.ResourceOverrides{
			RequestsCPU:    appConfig.Resources.Requests.CPU,
			RequestsMemory: appConfig.Resources.Requests.Memory,
			LimitsCPU:      appConfig.Resources.Limits.CPU,
			LimitsMemory:   appConfig.Resources.Limits.Memory,
		}
	}

	// Scaling overrides
	if appConfig.Scaling != nil {
		ctx.Scaling = &types.ScalingConfig{
			MinReplicas:  appConfig.Scaling.MinReplicas,
			MaxReplicas:  appConfig.Scaling.MaxReplicas,
			TargetCPU:    appConfig.Scaling.TargetCPU,
			TargetMemory: appConfig.Scaling.TargetMemory,
			Behavior:     appConfig.Scaling.Behavior,
		}
		// Also set on analysis for immediate use
		analysis.Scaling = ctx.Scaling
	}

	// Custom labels
	if len(appConfig.Labels) > 0 {
		ctx.Labels = appConfig.Labels
	}

	// Custom annotations
	if len(appConfig.Annotations) > 0 {
		ctx.Annotations = appConfig.Annotations
	}

	// Ingress config
	if appConfig.Ingress != nil && appConfig.Ingress.Enabled {
		ctx.Ingress = &types.IngressContext{
			Enabled:    true,
			Host:       appConfig.Ingress.Host,
			TLSEnabled: appConfig.Ingress.TLS != nil && appConfig.Ingress.TLS.Enabled,
		}
		if appConfig.Ingress.TLS != nil {
			ctx.Ingress.TLSSecret = appConfig.Ingress.TLS.SecretName
		}
		for _, p := range appConfig.Ingress.Paths {
			ctx.Ingress.Paths = append(ctx.Ingress.Paths, types.IngressPathDef{
				Path:     p.Path,
				PathType: p.PathType,
			})
		}
	}

	// Health check config
	if appConfig.Health != nil {
		ctx.Health = &types.HealthContext{}
		if appConfig.Health.Liveness != nil {
			ctx.Health.LivenessPath = appConfig.Health.Liveness.Path
			ctx.Health.LivenessPort = appConfig.Health.Liveness.Port
			ctx.Health.InitialDelay = appConfig.Health.Liveness.InitialDelay
			ctx.Health.Period = appConfig.Health.Liveness.Period
		}
		if appConfig.Health.Readiness != nil {
			ctx.Health.ReadinessPath = appConfig.Health.Readiness.Path
			ctx.Health.ReadinessPort = appConfig.Health.Readiness.Port
		}
		if appConfig.Health.StartupGracePeriod != "" {
			ctx.Health.StartupGracePeriod = appConfig.Health.StartupGracePeriod
		}

		// Also update the analysis health check
		if appConfig.Health.Liveness != nil {
			analysis.HealthCheck = &types.HealthCheck{
				Path:         appConfig.Health.Liveness.Path,
				Port:         appConfig.Health.Liveness.Port,
				InitialDelay: appConfig.Health.Liveness.InitialDelay,
				Period:       appConfig.Health.Liveness.Period,
			}
		}
	}

	// Dependencies
	for _, dep := range appConfig.Dependencies {
		ctx.Dependencies = append(ctx.Dependencies, types.DependencyContext{
			Name:        dep.Name,
			Type:        dep.Type,
			Required:    dep.Required,
			HealthCheck: dep.HealthCheck,
		})
	}

	// Operations
	if appConfig.Operations != nil {
		ctx.Operations = &types.OperationsContext{
			Runbook:           appConfig.Operations.Runbook,
			Alerts:            appConfig.Operations.Alerts,
			MaintenanceWindow: appConfig.Operations.MaintenanceWindow,
			OnCall:            appConfig.Operations.OnCall,
			AutoRestart:       appConfig.Operations.AutoRestart,
		}
	}

	// Deployment policy
	if appConfig.DeploymentPolicy != nil {
		ctx.DeploymentPolicy = &types.DeploymentPolicyContext{
			Strategy:       appConfig.DeploymentPolicy.Strategy,
			MaxSurge:       appConfig.DeploymentPolicy.MaxSurge,
			MaxUnavailable: appConfig.DeploymentPolicy.MaxUnavailable,
		}
	}

	// Set the context on analysis
	analysis.AppConfig = ctx
}
