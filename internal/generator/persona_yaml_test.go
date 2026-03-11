package generator

import (
	"strings"
	"testing"

	"github.com/dorgu-ai/dorgu/internal/config"
	"github.com/dorgu-ai/dorgu/internal/types"
)

func TestGeneratePersonaYAML_Basic(t *testing.T) {
	analysis := &types.AppAnalysis{
		Name:     "test-app",
		Type:     "api",
		Language: "go",
	}

	cfg := config.Default()

	yaml, err := GeneratePersonaYAML(analysis, "default", cfg)
	if err != nil {
		t.Fatalf("GeneratePersonaYAML() error = %v", err)
	}

	if !strings.Contains(yaml, "apiVersion: dorgu.io/v1") {
		t.Error("GeneratePersonaYAML() missing apiVersion")
	}
	if !strings.Contains(yaml, "kind: ApplicationPersona") {
		t.Error("GeneratePersonaYAML() missing kind")
	}
	if !strings.Contains(yaml, "name: test-app") {
		t.Error("GeneratePersonaYAML() missing name")
	}
	if !strings.Contains(yaml, "namespace: default") {
		t.Error("GeneratePersonaYAML() missing namespace")
	}
	if !strings.Contains(yaml, "type: api") {
		t.Error("GeneratePersonaYAML() missing type")
	}
	if !strings.Contains(yaml, "language: go") {
		t.Error("GeneratePersonaYAML() missing language")
	}
	if !strings.Contains(yaml, "app.kubernetes.io/managed-by: dorgu") {
		t.Error("GeneratePersonaYAML() missing managed-by label")
	}
}

func TestGeneratePersonaYAML_WithScaling(t *testing.T) {
	analysis := &types.AppAnalysis{
		Name: "scaled-app",
		Scaling: &types.ScalingConfig{
			MinReplicas:  2,
			MaxReplicas:  10,
			TargetCPU:    80,
			TargetMemory: 70,
			Behavior:     "aggressive",
		},
	}

	cfg := config.Default()

	yaml, err := GeneratePersonaYAML(analysis, "default", cfg)
	if err != nil {
		t.Fatalf("GeneratePersonaYAML() error = %v", err)
	}

	if !strings.Contains(yaml, "scaling:") {
		t.Error("GeneratePersonaYAML() missing scaling section")
	}
	if !strings.Contains(yaml, "minReplicas: 2") {
		t.Error("GeneratePersonaYAML() missing minReplicas")
	}
	if !strings.Contains(yaml, "maxReplicas: 10") {
		t.Error("GeneratePersonaYAML() missing maxReplicas")
	}
	if !strings.Contains(yaml, "targetCPU: 80") {
		t.Error("GeneratePersonaYAML() missing targetCPU")
	}
	if !strings.Contains(yaml, "targetMemory: 70") {
		t.Error("GeneratePersonaYAML() missing targetMemory")
	}
	if !strings.Contains(yaml, "behavior: aggressive") {
		t.Error("GeneratePersonaYAML() missing behavior")
	}
}

func TestGeneratePersonaYAML_WithDependencies(t *testing.T) {
	analysis := &types.AppAnalysis{
		Name: "app-with-deps",
		AppConfig: &types.AppConfigContext{
			Dependencies: []types.DependencyContext{
				{Name: "postgres", Type: "database", Required: true, HealthCheck: "SELECT 1"},
				{Name: "redis", Type: "cache", Required: false},
			},
		},
	}

	cfg := config.Default()

	yaml, err := GeneratePersonaYAML(analysis, "default", cfg)
	if err != nil {
		t.Fatalf("GeneratePersonaYAML() error = %v", err)
	}

	if !strings.Contains(yaml, "dependencies:") {
		t.Error("GeneratePersonaYAML() missing dependencies section")
	}
	if !strings.Contains(yaml, "name: postgres") {
		t.Error("GeneratePersonaYAML() missing postgres dependency")
	}
	if !strings.Contains(yaml, "type: database") {
		t.Error("GeneratePersonaYAML() missing dependency type")
	}
	if !strings.Contains(yaml, "required: true") {
		t.Error("GeneratePersonaYAML() missing required flag")
	}
	if !strings.Contains(yaml, "healthCheck: \"SELECT 1\"") {
		t.Error("GeneratePersonaYAML() missing healthCheck")
	}
	if !strings.Contains(yaml, "name: redis") {
		t.Error("GeneratePersonaYAML() missing redis dependency")
	}
}

func TestGeneratePersonaYAML_WithOwnership(t *testing.T) {
	analysis := &types.AppAnalysis{
		Name:       "owned-app",
		Team:       "platform",
		Owner:      "john@example.com",
		Repository: "https://github.com/org/repo",
		AppConfig: &types.AppConfigContext{
			Operations: &types.OperationsContext{
				OnCall:  "platform-oncall",
				Runbook: "https://wiki.example.com/runbook",
			},
		},
	}

	cfg := config.Default()

	yaml, err := GeneratePersonaYAML(analysis, "default", cfg)
	if err != nil {
		t.Fatalf("GeneratePersonaYAML() error = %v", err)
	}

	if !strings.Contains(yaml, "ownership:") {
		t.Error("GeneratePersonaYAML() missing ownership section")
	}
	if !strings.Contains(yaml, "team: platform") {
		t.Error("GeneratePersonaYAML() missing team")
	}
	if !strings.Contains(yaml, "owner: john@example.com") {
		t.Error("GeneratePersonaYAML() missing owner")
	}
	if !strings.Contains(yaml, "repository: https://github.com/org/repo") {
		t.Error("GeneratePersonaYAML() missing repository")
	}
	if !strings.Contains(yaml, "oncall: platform-oncall") {
		t.Error("GeneratePersonaYAML() missing oncall")
	}
	if !strings.Contains(yaml, "runbook: https://wiki.example.com/runbook") {
		t.Error("GeneratePersonaYAML() missing runbook")
	}
	if !strings.Contains(yaml, "dorgu.io/team: platform") {
		t.Error("GeneratePersonaYAML() missing team label")
	}
}

func TestGeneratePersonaYAML_MissingName(t *testing.T) {
	analysis := &types.AppAnalysis{
		Name: "",
	}

	cfg := config.Default()

	_, err := GeneratePersonaYAML(analysis, "default", cfg)
	if err == nil {
		t.Error("GeneratePersonaYAML() should return error for missing name")
	}
	if !strings.Contains(err.Error(), "name is required") {
		t.Errorf("GeneratePersonaYAML() error = %q, want to contain 'name is required'", err.Error())
	}
}

func TestGeneratePersonaYAML_DefaultNamespace(t *testing.T) {
	analysis := &types.AppAnalysis{
		Name: "test-app",
	}

	cfg := config.Default()

	yaml, err := GeneratePersonaYAML(analysis, "", cfg)
	if err != nil {
		t.Fatalf("GeneratePersonaYAML() error = %v", err)
	}

	if !strings.Contains(yaml, "namespace: default") {
		t.Error("GeneratePersonaYAML() should default to 'default' namespace")
	}
}

func TestGeneratePersonaYAML_WithHealth(t *testing.T) {
	analysis := &types.AppAnalysis{
		Name: "healthy-app",
		HealthCheck: &types.HealthCheck{
			Path: "/health",
			Port: 8080,
		},
	}

	cfg := config.Default()

	yaml, err := GeneratePersonaYAML(analysis, "default", cfg)
	if err != nil {
		t.Fatalf("GeneratePersonaYAML() error = %v", err)
	}

	if !strings.Contains(yaml, "health:") {
		t.Error("GeneratePersonaYAML() missing health section")
	}
	if !strings.Contains(yaml, "livenessPath: /health") {
		t.Error("GeneratePersonaYAML() missing livenessPath")
	}
	if !strings.Contains(yaml, "readinessPath: /health") {
		t.Error("GeneratePersonaYAML() missing readinessPath")
	}
	if !strings.Contains(yaml, "port: 8080") {
		t.Error("GeneratePersonaYAML() missing health port")
	}
}

func TestGeneratePersonaYAML_WithNetworking(t *testing.T) {
	analysis := &types.AppAnalysis{
		Name: "networked-app",
		Ports: []types.Port{
			{Port: 8080, Protocol: "TCP", Purpose: "HTTP API"},
			{Port: 9090, Protocol: "TCP", Purpose: "metrics"},
		},
	}

	cfg := config.Default()

	yaml, err := GeneratePersonaYAML(analysis, "default", cfg)
	if err != nil {
		t.Fatalf("GeneratePersonaYAML() error = %v", err)
	}

	if !strings.Contains(yaml, "networking:") {
		t.Error("GeneratePersonaYAML() missing networking section")
	}
	if !strings.Contains(yaml, "port: 8080") {
		t.Error("GeneratePersonaYAML() missing port 8080")
	}
	if !strings.Contains(yaml, "port: 9090") {
		t.Error("GeneratePersonaYAML() missing port 9090")
	}
	if !strings.Contains(yaml, "purpose: HTTP API") {
		t.Error("GeneratePersonaYAML() missing port purpose")
	}
}

func TestGeneratePersonaYAML_WithTier(t *testing.T) {
	analysis := &types.AppAnalysis{
		Name: "critical-app",
		AppConfig: &types.AppConfigContext{
			Tier: "critical",
		},
	}

	cfg := config.Default()

	yaml, err := GeneratePersonaYAML(analysis, "default", cfg)
	if err != nil {
		t.Fatalf("GeneratePersonaYAML() error = %v", err)
	}

	if !strings.Contains(yaml, "tier: critical") {
		t.Error("GeneratePersonaYAML() missing tier")
	}
}

func TestGeneratePersonaYAML_DefaultTier(t *testing.T) {
	analysis := &types.AppAnalysis{
		Name: "standard-app",
	}

	cfg := config.Default()

	yaml, err := GeneratePersonaYAML(analysis, "default", cfg)
	if err != nil {
		t.Fatalf("GeneratePersonaYAML() error = %v", err)
	}

	if !strings.Contains(yaml, "tier: standard") {
		t.Error("GeneratePersonaYAML() should default to 'standard' tier")
	}
}

func TestGeneratePersonaYAML_DefaultType(t *testing.T) {
	analysis := &types.AppAnalysis{
		Name: "typeless-app",
		Type: "",
	}

	cfg := config.Default()

	yaml, err := GeneratePersonaYAML(analysis, "default", cfg)
	if err != nil {
		t.Fatalf("GeneratePersonaYAML() error = %v", err)
	}

	if !strings.Contains(yaml, "type: api") {
		t.Error("GeneratePersonaYAML() should default to 'api' type")
	}
}

func TestGeneratePersonaYAML_DNSSafeName(t *testing.T) {
	analysis := &types.AppAnalysis{
		Name: "My_App_Name",
	}

	cfg := config.Default()

	yaml, err := GeneratePersonaYAML(analysis, "default", cfg)
	if err != nil {
		t.Fatalf("GeneratePersonaYAML() error = %v", err)
	}

	if !strings.Contains(yaml, "name: my-app-name") {
		t.Error("GeneratePersonaYAML() should convert name to DNS-safe format")
	}
}

func TestGeneratePersonaYAML_NoImageRunsAsRootInTechnical(t *testing.T) {
	analysis := &types.AppAnalysis{
		Name:     "root-app",
		Language: "go",
	}

	cfg := config.Default()
	yamlStr, err := GeneratePersonaYAML(analysis, "default", cfg)
	if err != nil {
		t.Fatalf("GeneratePersonaYAML() error = %v", err)
	}

	// Split into sections to check technical vs policies.security
	technicalIdx := strings.Index(yamlStr, "  technical:")
	policiesIdx := strings.Index(yamlStr, "  policies:")
	if technicalIdx == -1 {
		t.Fatal("expected technical section in YAML output")
	}
	if policiesIdx == -1 {
		t.Fatal("expected policies section in YAML output")
	}

	technicalSection := yamlStr[technicalIdx:policiesIdx]
	policiesSection := yamlStr[policiesIdx:]

	if strings.Contains(technicalSection, "imageRunsAsRoot") {
		t.Error("imageRunsAsRoot should NOT be in spec.technical section")
	}
	if !strings.Contains(policiesSection, "imageRunsAsRoot") {
		t.Error("imageRunsAsRoot should be in spec.policies.security section")
	}
}

func TestGeneratePersonaYAML_Policies(t *testing.T) {
	analysis := &types.AppAnalysis{
		Name: "policy-app",
		AppConfig: &types.AppConfigContext{
			DeploymentPolicy: &types.DeploymentPolicyContext{
				Strategy:       "Recreate",
				MaxSurge:       "50%",
				MaxUnavailable: "0%",
			},
			Operations: &types.OperationsContext{
				MaintenanceWindow: "Sun 02:00-04:00",
				AutoRestart:       true,
			},
		},
	}

	cfg := config.Default()

	yaml, err := GeneratePersonaYAML(analysis, "default", cfg)
	if err != nil {
		t.Fatalf("GeneratePersonaYAML() error = %v", err)
	}

	if !strings.Contains(yaml, "policies:") {
		t.Error("GeneratePersonaYAML() missing policies section")
	}
	if !strings.Contains(yaml, "strategy: Recreate") {
		t.Error("GeneratePersonaYAML() missing deployment strategy")
	}
	if !strings.Contains(yaml, "maxSurge: \"50%\"") {
		t.Error("GeneratePersonaYAML() missing maxSurge")
	}
	if !strings.Contains(yaml, "maxUnavailable: \"0%\"") {
		t.Error("GeneratePersonaYAML() missing maxUnavailable")
	}
	if !strings.Contains(yaml, "window: \"Sun 02:00-04:00\"") {
		t.Error("GeneratePersonaYAML() missing maintenance window")
	}
	if !strings.Contains(yaml, "autoRestart: true") {
		t.Error("GeneratePersonaYAML() missing autoRestart")
	}
}
