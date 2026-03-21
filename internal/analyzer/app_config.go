package analyzer

import (
	"github.com/dorgu-ai/dorgu/internal/config"
	"github.com/dorgu-ai/dorgu/internal/types"
)

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
