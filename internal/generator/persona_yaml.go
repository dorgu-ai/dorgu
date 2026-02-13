package generator

import (
	"fmt"
	"strings"

	"github.com/dorgu-ai/dorgu/internal/config"
	"github.com/dorgu-ai/dorgu/internal/types"
)

// GeneratePersonaYAML generates an ApplicationPersona CRD YAML from analysis results.
// This is the bridge between CLI analysis and the cluster-resident CRD.
func GeneratePersonaYAML(analysis *types.AppAnalysis, namespace string, cfg *config.Config) (string, error) {
	if analysis.Name == "" {
		return "", fmt.Errorf("application name is required for persona generation")
	}

	if namespace == "" {
		namespace = "default"
	}

	var sb strings.Builder

	// Header
	sb.WriteString("apiVersion: dorgu.io/v1\n")
	sb.WriteString("kind: ApplicationPersona\n")
	sb.WriteString("metadata:\n")
	sb.WriteString(fmt.Sprintf("  name: %s\n", analysis.Name))
	sb.WriteString(fmt.Sprintf("  namespace: %s\n", namespace))
	sb.WriteString("  labels:\n")
	sb.WriteString("    app.kubernetes.io/managed-by: dorgu\n")
	if analysis.Team != "" {
		sb.WriteString(fmt.Sprintf("    dorgu.io/team: %s\n", analysis.Team))
	}

	// Spec
	sb.WriteString("spec:\n")
	sb.WriteString(fmt.Sprintf("  name: %s\n", analysis.Name))
	sb.WriteString("  version: \"1\"\n")

	// Type
	appType := analysis.Type
	if appType == "" {
		appType = "api"
	}
	sb.WriteString(fmt.Sprintf("  type: %s\n", appType))

	// Tier
	tier := "standard"
	if analysis.AppConfig != nil && analysis.AppConfig.Tier != "" {
		tier = analysis.AppConfig.Tier
	}
	sb.WriteString(fmt.Sprintf("  tier: %s\n", tier))

	// Technical
	sb.WriteString("  technical:\n")
	if analysis.Language != "" {
		sb.WriteString(fmt.Sprintf("    language: %s\n", analysis.Language))
	}
	if analysis.Framework != "" {
		sb.WriteString(fmt.Sprintf("    framework: %s\n", analysis.Framework))
	}
	if analysis.Description != "" {
		sb.WriteString(fmt.Sprintf("    description: |\n      %s\n", strings.ReplaceAll(analysis.Description, "\n", "\n      ")))
	}

	// Resources
	writeResources(&sb, analysis, cfg)

	// Scaling
	writeScaling(&sb, analysis)

	// Health
	writeHealth(&sb, analysis)

	// Dependencies
	writeDependencies(&sb, analysis)

	// Networking
	writeNetworking(&sb, analysis, cfg)

	// Ownership
	writeOwnership(&sb, analysis)

	// Policies
	writePolicies(&sb, analysis, cfg)

	return sb.String(), nil
}

func writeResources(sb *strings.Builder, analysis *types.AppAnalysis, cfg *config.Config) {
	resources := cfg.GetResourcesForProfile(analysis.ResourceProfile)

	// Apply app config overrides
	if analysis.AppConfig != nil && analysis.AppConfig.Resources != nil {
		r := analysis.AppConfig.Resources
		if r.RequestsCPU != "" {
			resources.Requests.CPU = r.RequestsCPU
		}
		if r.RequestsMemory != "" {
			resources.Requests.Memory = r.RequestsMemory
		}
		if r.LimitsCPU != "" {
			resources.Limits.CPU = r.LimitsCPU
		}
		if r.LimitsMemory != "" {
			resources.Limits.Memory = r.LimitsMemory
		}
	}

	sb.WriteString("  resources:\n")
	sb.WriteString("    requests:\n")
	sb.WriteString(fmt.Sprintf("      cpu: \"%s\"\n", resources.Requests.CPU))
	sb.WriteString(fmt.Sprintf("      memory: \"%s\"\n", resources.Requests.Memory))
	sb.WriteString("    limits:\n")
	sb.WriteString(fmt.Sprintf("      cpu: \"%s\"\n", resources.Limits.CPU))
	sb.WriteString(fmt.Sprintf("      memory: \"%s\"\n", resources.Limits.Memory))

	profile := analysis.ResourceProfile
	if profile == "" {
		profile = "standard"
	}
	sb.WriteString(fmt.Sprintf("    profile: %s\n", profile))
}

func writeScaling(sb *strings.Builder, analysis *types.AppAnalysis) {
	scaling := analysis.Scaling
	if analysis.AppConfig != nil && analysis.AppConfig.Scaling != nil {
		scaling = analysis.AppConfig.Scaling
	}
	if scaling == nil {
		return
	}

	sb.WriteString("  scaling:\n")
	sb.WriteString(fmt.Sprintf("    minReplicas: %d\n", scaling.MinReplicas))
	sb.WriteString(fmt.Sprintf("    maxReplicas: %d\n", scaling.MaxReplicas))
	if scaling.TargetCPU > 0 {
		sb.WriteString(fmt.Sprintf("    targetCPU: %d\n", scaling.TargetCPU))
	}
	if scaling.TargetMemory > 0 {
		sb.WriteString(fmt.Sprintf("    targetMemory: %d\n", scaling.TargetMemory))
	}

	behavior := scaling.Behavior
	if behavior == "" {
		behavior = "balanced"
	}
	sb.WriteString(fmt.Sprintf("    behavior: %s\n", behavior))
}

func writeHealth(sb *strings.Builder, analysis *types.AppAnalysis) {
	// Prefer app config health, fall back to analysis health check
	var livenessPath, readinessPath string
	var healthPort int
	var startupGracePeriod string

	if analysis.AppConfig != nil && analysis.AppConfig.Health != nil {
		h := analysis.AppConfig.Health
		livenessPath = h.LivenessPath
		readinessPath = h.ReadinessPath
		if h.LivenessPort > 0 {
			healthPort = h.LivenessPort
		} else if h.ReadinessPort > 0 {
			healthPort = h.ReadinessPort
		}
		startupGracePeriod = h.StartupGracePeriod
	}

	// Fall back to basic health check
	if livenessPath == "" && analysis.HealthCheck != nil {
		livenessPath = analysis.HealthCheck.Path
		healthPort = analysis.HealthCheck.Port
	}
	if readinessPath == "" {
		readinessPath = livenessPath
	}

	if livenessPath == "" && readinessPath == "" {
		return
	}

	sb.WriteString("  health:\n")
	if livenessPath != "" {
		sb.WriteString(fmt.Sprintf("    livenessPath: %s\n", livenessPath))
	}
	if readinessPath != "" {
		sb.WriteString(fmt.Sprintf("    readinessPath: %s\n", readinessPath))
	}
	if healthPort > 0 {
		sb.WriteString(fmt.Sprintf("    port: %d\n", healthPort))
	}
	if startupGracePeriod == "" {
		startupGracePeriod = "30s"
	}
	sb.WriteString(fmt.Sprintf("    startupGracePeriod: \"%s\"\n", startupGracePeriod))
}

func writeDependencies(sb *strings.Builder, analysis *types.AppAnalysis) {
	if analysis.AppConfig == nil || len(analysis.AppConfig.Dependencies) == 0 {
		return
	}

	sb.WriteString("  dependencies:\n")
	for _, dep := range analysis.AppConfig.Dependencies {
		sb.WriteString(fmt.Sprintf("    - name: %s\n", dep.Name))
		if dep.Type != "" {
			sb.WriteString(fmt.Sprintf("      type: %s\n", dep.Type))
		}
		sb.WriteString(fmt.Sprintf("      required: %t\n", dep.Required))
		if dep.HealthCheck != "" {
			sb.WriteString(fmt.Sprintf("      healthCheck: \"%s\"\n", dep.HealthCheck))
		}
	}
}

func writeNetworking(sb *strings.Builder, analysis *types.AppAnalysis, cfg *config.Config) {
	if len(analysis.Ports) == 0 {
		return
	}

	sb.WriteString("  networking:\n")
	sb.WriteString("    ports:\n")
	for _, p := range analysis.Ports {
		sb.WriteString(fmt.Sprintf("      - port: %d\n", p.Port))
		protocol := p.Protocol
		if protocol == "" {
			protocol = "TCP"
		}
		sb.WriteString(fmt.Sprintf("        protocol: %s\n", protocol))
		if p.Purpose != "" {
			sb.WriteString(fmt.Sprintf("        purpose: %s\n", p.Purpose))
		}
	}

	// Ingress
	if analysis.AppConfig != nil && analysis.AppConfig.Ingress != nil && analysis.AppConfig.Ingress.Enabled {
		ing := analysis.AppConfig.Ingress
		sb.WriteString("    ingress:\n")
		sb.WriteString("      enabled: true\n")
		if ing.Host != "" {
			sb.WriteString(fmt.Sprintf("      host: %s\n", ing.Host))
		} else if analysis.Name != "" {
			sb.WriteString(fmt.Sprintf("      host: %s%s\n", analysis.Name, cfg.Ingress.DomainSuffix))
		}
		if len(ing.Paths) > 0 {
			sb.WriteString("      paths:\n")
			for _, p := range ing.Paths {
				sb.WriteString(fmt.Sprintf("        - %s\n", p.Path))
			}
		}
		sb.WriteString(fmt.Sprintf("      tlsEnabled: %t\n", ing.TLSEnabled))
	}
}

func writeOwnership(sb *strings.Builder, analysis *types.AppAnalysis) {
	hasOwnership := analysis.Team != "" || analysis.Owner != "" || analysis.Repository != ""
	if analysis.AppConfig != nil && analysis.AppConfig.Operations != nil {
		ops := analysis.AppConfig.Operations
		if ops.OnCall != "" || ops.Runbook != "" {
			hasOwnership = true
		}
	}
	if !hasOwnership {
		return
	}

	sb.WriteString("  ownership:\n")
	if analysis.Team != "" {
		sb.WriteString(fmt.Sprintf("    team: %s\n", analysis.Team))
	}
	if analysis.Owner != "" {
		sb.WriteString(fmt.Sprintf("    owner: %s\n", analysis.Owner))
	}
	if analysis.Repository != "" {
		sb.WriteString(fmt.Sprintf("    repository: %s\n", analysis.Repository))
	}
	if analysis.AppConfig != nil && analysis.AppConfig.Operations != nil {
		ops := analysis.AppConfig.Operations
		if ops.OnCall != "" {
			sb.WriteString(fmt.Sprintf("    oncall: %s\n", ops.OnCall))
		}
		if ops.Runbook != "" {
			sb.WriteString(fmt.Sprintf("    runbook: %s\n", ops.Runbook))
		}
	}
}

func writePolicies(sb *strings.Builder, analysis *types.AppAnalysis, cfg *config.Config) {
	sb.WriteString("  policies:\n")

	// Security from org config
	sb.WriteString("    security:\n")
	sb.WriteString(fmt.Sprintf("      runAsNonRoot: %t\n", cfg.Security.PodSecurityContext.RunAsNonRoot))
	sb.WriteString(fmt.Sprintf("      readOnlyRootFilesystem: %t\n", cfg.Security.ContainerSecurityContext.ReadOnlyRootFilesystem))
	sb.WriteString(fmt.Sprintf("      allowPrivilegeEscalation: %t\n", cfg.Security.ContainerSecurityContext.AllowPrivilegeEscalation))

	// Deployment policy
	strategy := "RollingUpdate"
	maxSurge := "25%"
	maxUnavailable := "25%"
	if analysis.AppConfig != nil && analysis.AppConfig.DeploymentPolicy != nil {
		dp := analysis.AppConfig.DeploymentPolicy
		if dp.Strategy != "" {
			strategy = dp.Strategy
		}
		if dp.MaxSurge != "" {
			maxSurge = dp.MaxSurge
		}
		if dp.MaxUnavailable != "" {
			maxUnavailable = dp.MaxUnavailable
		}
	}
	sb.WriteString("    deployment:\n")
	sb.WriteString(fmt.Sprintf("      strategy: %s\n", strategy))
	sb.WriteString(fmt.Sprintf("      maxSurge: \"%s\"\n", maxSurge))
	sb.WriteString(fmt.Sprintf("      maxUnavailable: \"%s\"\n", maxUnavailable))

	// Maintenance
	maintenanceWindow := ""
	autoRestart := false
	if analysis.AppConfig != nil && analysis.AppConfig.Operations != nil {
		ops := analysis.AppConfig.Operations
		maintenanceWindow = ops.MaintenanceWindow
		autoRestart = ops.AutoRestart
	}
	sb.WriteString("    maintenance:\n")
	if maintenanceWindow != "" {
		sb.WriteString(fmt.Sprintf("      window: \"%s\"\n", maintenanceWindow))
	}
	sb.WriteString(fmt.Sprintf("      autoRestart: %t\n", autoRestart))
}
