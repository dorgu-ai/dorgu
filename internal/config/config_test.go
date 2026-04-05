package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefault(t *testing.T) {
	cfg := Default()

	if cfg.Version != "1" {
		t.Errorf("Default().Version = %q, want %q", cfg.Version, "1")
	}
	if cfg.Naming.Pattern != "{app}" {
		t.Errorf("Default().Naming.Pattern = %q, want %q", cfg.Naming.Pattern, "{app}")
	}
	if !cfg.Naming.DNSSafe {
		t.Error("Default().Naming.DNSSafe = false, want true")
	}
	if cfg.Resources.Defaults.Requests.CPU != "100m" {
		t.Errorf("Default().Resources.Defaults.Requests.CPU = %q, want %q", cfg.Resources.Defaults.Requests.CPU, "100m")
	}
	if cfg.Resources.Defaults.Requests.Memory != "128Mi" {
		t.Errorf("Default().Resources.Defaults.Requests.Memory = %q, want %q", cfg.Resources.Defaults.Requests.Memory, "128Mi")
	}
	if cfg.Resources.Defaults.Limits.CPU != "500m" {
		t.Errorf("Default().Resources.Defaults.Limits.CPU = %q, want %q", cfg.Resources.Defaults.Limits.CPU, "500m")
	}
	if cfg.Resources.Defaults.Limits.Memory != "512Mi" {
		t.Errorf("Default().Resources.Defaults.Limits.Memory = %q, want %q", cfg.Resources.Defaults.Limits.Memory, "512Mi")
	}
	if cfg.Ingress.Class != "nginx" {
		t.Errorf("Default().Ingress.Class = %q, want %q", cfg.Ingress.Class, "nginx")
	}
	if cfg.Ingress.DomainSuffix != ".local" {
		t.Errorf("Default().Ingress.DomainSuffix = %q, want %q", cfg.Ingress.DomainSuffix, ".local")
	}
	if cfg.ArgoCD.Project != "default" {
		t.Errorf("Default().ArgoCD.Project = %q, want %q", cfg.ArgoCD.Project, "default")
	}
	if cfg.ArgoCD.Destination.Server != "https://kubernetes.default.svc" {
		t.Errorf("Default().ArgoCD.Destination.Server = %q, want %q", cfg.ArgoCD.Destination.Server, "https://kubernetes.default.svc")
	}
	if cfg.CI.Provider != "github-actions" {
		t.Errorf("Default().CI.Provider = %q, want %q", cfg.CI.Provider, "github-actions")
	}
	if cfg.LLM.Provider != "openai" {
		t.Errorf("Default().LLM.Provider = %q, want %q", cfg.LLM.Provider, "openai")
	}
	if cfg.LLM.Model != "gpt-4" {
		t.Errorf("Default().LLM.Model = %q, want %q", cfg.LLM.Model, "gpt-4")
	}
}

func TestDefault_ResourceProfiles(t *testing.T) {
	cfg := Default()

	profiles := []string{"api", "worker", "web"}
	for _, profile := range profiles {
		if _, ok := cfg.Resources.Profiles[profile]; !ok {
			t.Errorf("Default().Resources.Profiles[%q] not found", profile)
		}
	}

	apiProfile := cfg.Resources.Profiles["api"]
	if apiProfile.Requests.CPU != "100m" {
		t.Errorf("api profile Requests.CPU = %q, want %q", apiProfile.Requests.CPU, "100m")
	}
	if apiProfile.Limits.Memory != "1Gi" {
		t.Errorf("api profile Limits.Memory = %q, want %q", apiProfile.Limits.Memory, "1Gi")
	}

	workerProfile := cfg.Resources.Profiles["worker"]
	if workerProfile.Requests.CPU != "500m" {
		t.Errorf("worker profile Requests.CPU = %q, want %q", workerProfile.Requests.CPU, "500m")
	}
	if workerProfile.Limits.Memory != "2Gi" {
		t.Errorf("worker profile Limits.Memory = %q, want %q", workerProfile.Limits.Memory, "2Gi")
	}
}

func TestApplyDefaults_PartialConfig(t *testing.T) {
	cfg := &Config{
		Version: "2",
		Ingress: IngressConfig{
			Class: "traefik",
		},
	}
	applyDefaults(cfg)

	if cfg.Version != "2" {
		t.Errorf("applyDefaults preserved Version = %q, want %q", cfg.Version, "2")
	}
	if cfg.Ingress.Class != "traefik" {
		t.Errorf("applyDefaults preserved Ingress.Class = %q, want %q", cfg.Ingress.Class, "traefik")
	}
	if cfg.Ingress.DomainSuffix != ".local" {
		t.Errorf("applyDefaults set Ingress.DomainSuffix = %q, want %q", cfg.Ingress.DomainSuffix, ".local")
	}
	if cfg.Naming.Pattern != "{app}" {
		t.Errorf("applyDefaults set Naming.Pattern = %q, want %q", cfg.Naming.Pattern, "{app}")
	}
}

func TestGetResourcesForProfile(t *testing.T) {
	cfg := Default()

	tests := []struct {
		profile     string
		wantCPU     string
		wantMemory  string
		description string
	}{
		{"api", "100m", "256Mi", "api profile"},
		{"worker", "500m", "512Mi", "worker profile"},
		{"web", "50m", "128Mi", "web profile"},
		{"unknown", "100m", "128Mi", "unknown profile falls back to defaults"},
		{"", "100m", "128Mi", "empty profile falls back to defaults"},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			spec := cfg.GetResourcesForProfile(tt.profile)
			if spec.Requests.CPU != tt.wantCPU {
				t.Errorf("GetResourcesForProfile(%q).Requests.CPU = %q, want %q", tt.profile, spec.Requests.CPU, tt.wantCPU)
			}
			if spec.Requests.Memory != tt.wantMemory {
				t.Errorf("GetResourcesForProfile(%q).Requests.Memory = %q, want %q", tt.profile, spec.Requests.Memory, tt.wantMemory)
			}
		})
	}
}

func TestLoadAppConfig(t *testing.T) {
	tmpDir := t.TempDir()

	configContent := `version: "1"
app:
  name: test-app
  description: A test application
  team: platform
  type: api
environment: production
`
	configPath := filepath.Join(tmpDir, ".dorgu.yaml")
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	cfg, err := LoadAppConfig(tmpDir)
	if err != nil {
		t.Fatalf("LoadAppConfig() error = %v", err)
	}
	if cfg == nil {
		t.Fatal("LoadAppConfig() returned nil config")
	}

	if cfg.Version != "1" {
		t.Errorf("LoadAppConfig().Version = %q, want %q", cfg.Version, "1")
	}
	if cfg.App.Name != "test-app" {
		t.Errorf("LoadAppConfig().App.Name = %q, want %q", cfg.App.Name, "test-app")
	}
	if cfg.App.Description != "A test application" {
		t.Errorf("LoadAppConfig().App.Description = %q, want %q", cfg.App.Description, "A test application")
	}
	if cfg.App.Team != "platform" {
		t.Errorf("LoadAppConfig().App.Team = %q, want %q", cfg.App.Team, "platform")
	}
	if cfg.App.Type != "api" {
		t.Errorf("LoadAppConfig().App.Type = %q, want %q", cfg.App.Type, "api")
	}
	if cfg.Environment != "production" {
		t.Errorf("LoadAppConfig().Environment = %q, want %q", cfg.Environment, "production")
	}
}

func TestLoadAppConfig_NotFound(t *testing.T) {
	tmpDir := t.TempDir()

	cfg, err := LoadAppConfig(tmpDir)
	if err != nil {
		t.Errorf("LoadAppConfig() error = %v, want nil", err)
	}
	if cfg != nil {
		t.Errorf("LoadAppConfig() = %v, want nil for missing config", cfg)
	}
}

func TestLoadAppConfig_Empty(t *testing.T) {
	tmpDir := t.TempDir()

	configPath := filepath.Join(tmpDir, ".dorgu.yaml")
	if err := os.WriteFile(configPath, []byte(""), 0644); err != nil {
		t.Fatalf("Failed to write empty config: %v", err)
	}

	cfg, err := LoadAppConfig(tmpDir)
	if err != nil {
		t.Errorf("LoadAppConfig() error = %v, want nil", err)
	}
	if cfg != nil {
		t.Errorf("LoadAppConfig() = %v, want nil for empty config", cfg)
	}
}

func TestLoadAppConfig_InvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()

	configPath := filepath.Join(tmpDir, ".dorgu.yaml")
	if err := os.WriteFile(configPath, []byte("invalid: yaml: content: ["), 0644); err != nil {
		t.Fatalf("Failed to write invalid config: %v", err)
	}

	_, err := LoadAppConfig(tmpDir)
	if err == nil {
		t.Error("LoadAppConfig() error = nil, want error for invalid YAML")
	}
}

func TestHasAppConfig(t *testing.T) {
	tmpDir := t.TempDir()

	if HasAppConfig(tmpDir) {
		t.Error("HasAppConfig() = true, want false for missing config")
	}

	configPath := filepath.Join(tmpDir, ".dorgu.yaml")
	if err := os.WriteFile(configPath, []byte(""), 0644); err != nil {
		t.Fatalf("Failed to write empty config: %v", err)
	}
	if HasAppConfig(tmpDir) {
		t.Error("HasAppConfig() = true, want false for empty config")
	}

	if err := os.WriteFile(configPath, []byte("version: 1"), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}
	if !HasAppConfig(tmpDir) {
		t.Error("HasAppConfig() = false, want true for non-empty config")
	}
}

func TestGetInstructionsContext(t *testing.T) {
	cfg := &AppConfig{
		App: AppMetadata{
			Name:         "order-service",
			Description:  "Handles order processing",
			Team:         "commerce",
			Type:         "api",
			Instructions: "This service requires MySQL connection",
		},
		Environment: "production",
		Dependencies: []AppDependency{
			{Name: "mysql", Type: "database", Required: true},
			{Name: "redis", Type: "cache", Required: false},
		},
	}

	context := cfg.GetInstructionsContext()

	expectedParts := []string{
		"Application Name: order-service",
		"Description: Handles order processing",
		"Team: commerce",
		"Application Type: api",
		"Environment: production",
		"Known Dependencies:",
		"- mysql (database) (required)",
		"- redis (cache)",
		"Application-Specific Context:",
		"This service requires MySQL connection",
	}

	for _, part := range expectedParts {
		if !strings.Contains(context, part) {
			t.Errorf("GetInstructionsContext() missing %q", part)
		}
	}
}

func TestGetInstructionsContext_Nil(t *testing.T) {
	var cfg *AppConfig
	context := cfg.GetInstructionsContext()
	if context != "" {
		t.Errorf("GetInstructionsContext() on nil = %q, want empty string", context)
	}
}

func TestGetInstructionsContext_Empty(t *testing.T) {
	cfg := &AppConfig{}
	context := cfg.GetInstructionsContext()
	if context != "" {
		t.Errorf("GetInstructionsContext() on empty config = %q, want empty string", context)
	}
}

func TestLoadAppConfig_WithDependencies(t *testing.T) {
	tmpDir := t.TempDir()

	configContent := `version: "1"
app:
  name: test-app
dependencies:
  - name: postgres
    type: database
    required: true
    health_check: "SELECT 1"
  - name: redis
    type: cache
    required: false
`
	configPath := filepath.Join(tmpDir, ".dorgu.yaml")
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	cfg, err := LoadAppConfig(tmpDir)
	if err != nil {
		t.Fatalf("LoadAppConfig() error = %v", err)
	}

	if len(cfg.Dependencies) != 2 {
		t.Fatalf("LoadAppConfig().Dependencies length = %d, want 2", len(cfg.Dependencies))
	}

	if cfg.Dependencies[0].Name != "postgres" {
		t.Errorf("Dependencies[0].Name = %q, want %q", cfg.Dependencies[0].Name, "postgres")
	}
	if cfg.Dependencies[0].Type != "database" {
		t.Errorf("Dependencies[0].Type = %q, want %q", cfg.Dependencies[0].Type, "database")
	}
	if !cfg.Dependencies[0].Required {
		t.Error("Dependencies[0].Required = false, want true")
	}
	if cfg.Dependencies[0].HealthCheck != "SELECT 1" {
		t.Errorf("Dependencies[0].HealthCheck = %q, want %q", cfg.Dependencies[0].HealthCheck, "SELECT 1")
	}
}
