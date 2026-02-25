package generator

import (
	"strings"
	"testing"

	"github.com/dorgu-ai/dorgu/internal/config"
	"github.com/dorgu-ai/dorgu/internal/types"
)

func TestGenerateDeployment_Basic(t *testing.T) {
	analysis := &types.AppAnalysis{
		Name:     "test-app",
		Language: "go",
	}

	cfg := config.Default()
	resources := cfg.GetResourcesForProfile("api")

	yaml, err := GenerateDeployment(analysis, "default", resources, cfg)
	if err != nil {
		t.Fatalf("GenerateDeployment() error = %v", err)
	}

	if !strings.Contains(yaml, "apiVersion: apps/v1") {
		t.Error("GenerateDeployment() missing apiVersion")
	}
	if !strings.Contains(yaml, "kind: Deployment") {
		t.Error("GenerateDeployment() missing kind")
	}
	if !strings.Contains(yaml, "name: test-app") {
		t.Error("GenerateDeployment() missing name")
	}
	if !strings.Contains(yaml, "namespace: default") {
		t.Error("GenerateDeployment() missing namespace")
	}
	if !strings.Contains(yaml, "app.kubernetes.io/name: test-app") {
		t.Error("GenerateDeployment() missing app label")
	}
	if !strings.Contains(yaml, "app.kubernetes.io/managed-by: dorgu") {
		t.Error("GenerateDeployment() missing managed-by label")
	}
}

func TestGenerateDeployment_WithPorts(t *testing.T) {
	analysis := &types.AppAnalysis{
		Name: "api-service",
		Ports: []types.Port{
			{Port: 8080, Protocol: "TCP", Purpose: "HTTP"},
			{Port: 9090, Protocol: "TCP", Purpose: "metrics"},
		},
	}

	cfg := config.Default()
	resources := cfg.GetResourcesForProfile("api")

	yaml, err := GenerateDeployment(analysis, "default", resources, cfg)
	if err != nil {
		t.Fatalf("GenerateDeployment() error = %v", err)
	}

	if !strings.Contains(yaml, "containerPort: 8080") {
		t.Error("GenerateDeployment() missing port 8080")
	}
	if !strings.Contains(yaml, "containerPort: 9090") {
		t.Error("GenerateDeployment() missing port 9090")
	}
}

func TestGenerateDeployment_WithEnvVars(t *testing.T) {
	analysis := &types.AppAnalysis{
		Name: "app-with-env",
		EnvVars: []types.EnvVar{
			{Name: "LOG_LEVEL", Value: "info"},
			{Name: "DATABASE_URL", Secret: true},
		},
	}

	cfg := config.Default()
	resources := cfg.GetResourcesForProfile("api")

	yaml, err := GenerateDeployment(analysis, "default", resources, cfg)
	if err != nil {
		t.Fatalf("GenerateDeployment() error = %v", err)
	}

	if !strings.Contains(yaml, "name: LOG_LEVEL") {
		t.Error("GenerateDeployment() missing LOG_LEVEL env var")
	}
	if !strings.Contains(yaml, "value: info") {
		t.Error("GenerateDeployment() missing LOG_LEVEL value")
	}
	if !strings.Contains(yaml, "name: DATABASE_URL") {
		t.Error("GenerateDeployment() missing DATABASE_URL env var")
	}
	if !strings.Contains(yaml, "secretKeyRef") {
		t.Error("GenerateDeployment() missing secretKeyRef for secret env var")
	}
}

func TestGenerateDeployment_WithHealthProbes(t *testing.T) {
	analysis := &types.AppAnalysis{
		Name: "healthy-app",
		HealthCheck: &types.HealthCheck{
			Path: "/health",
			Port: 8080,
		},
	}

	cfg := config.Default()
	resources := cfg.GetResourcesForProfile("api")

	yaml, err := GenerateDeployment(analysis, "default", resources, cfg)
	if err != nil {
		t.Fatalf("GenerateDeployment() error = %v", err)
	}

	if !strings.Contains(yaml, "livenessProbe:") {
		t.Error("GenerateDeployment() missing livenessProbe")
	}
	if !strings.Contains(yaml, "readinessProbe:") {
		t.Error("GenerateDeployment() missing readinessProbe")
	}
	if !strings.Contains(yaml, "path: /health") {
		t.Error("GenerateDeployment() missing health path")
	}
	if !strings.Contains(yaml, "port: 8080") {
		t.Error("GenerateDeployment() missing health port")
	}
}

func TestGenerateDeployment_WithAppConfig(t *testing.T) {
	analysis := &types.AppAnalysis{
		Name:        "configured-app",
		Team:        "platform",
		Environment: "production",
		AppConfig: &types.AppConfigContext{
			Resources: &types.ResourceOverrides{
				RequestsCPU:    "200m",
				RequestsMemory: "256Mi",
				LimitsCPU:      "1000m",
				LimitsMemory:   "1Gi",
			},
			Labels: map[string]string{
				"custom-label": "custom-value",
			},
			Annotations: map[string]string{
				"custom-annotation": "annotation-value",
			},
			Scaling: &types.ScalingConfig{
				MinReplicas: 3,
				MaxReplicas: 10,
			},
		},
	}

	cfg := config.Default()
	resources := cfg.GetResourcesForProfile("api")

	yaml, err := GenerateDeployment(analysis, "default", resources, cfg)
	if err != nil {
		t.Fatalf("GenerateDeployment() error = %v", err)
	}

	if !strings.Contains(yaml, "cpu: 200m") {
		t.Error("GenerateDeployment() missing custom CPU request")
	}
	if !strings.Contains(yaml, "memory: 256Mi") {
		t.Error("GenerateDeployment() missing custom memory request")
	}
	if !strings.Contains(yaml, "custom-label: custom-value") {
		t.Error("GenerateDeployment() missing custom label")
	}
	if !strings.Contains(yaml, "custom-annotation: annotation-value") {
		t.Error("GenerateDeployment() missing custom annotation")
	}
	if !strings.Contains(yaml, "replicas: 3") {
		t.Error("GenerateDeployment() missing custom replicas from scaling")
	}
	if !strings.Contains(yaml, "app.kubernetes.io/team: platform") {
		t.Error("GenerateDeployment() missing team label")
	}
	if !strings.Contains(yaml, "app.kubernetes.io/environment: production") {
		t.Error("GenerateDeployment() missing environment label")
	}
}

func TestGenerateDeployment_LocalImage(t *testing.T) {
	analysis := &types.AppAnalysis{
		Name: "local-app",
	}

	cfg := config.Default()
	cfg.CI.Registry = ""
	resources := cfg.GetResourcesForProfile("api")

	yaml, err := GenerateDeployment(analysis, "default", resources, cfg)
	if err != nil {
		t.Fatalf("GenerateDeployment() error = %v", err)
	}

	if !strings.Contains(yaml, "image: local-app:latest") {
		t.Error("GenerateDeployment() should use local image name without registry")
	}
	if !strings.Contains(yaml, "imagePullPolicy: Never") {
		t.Error("GenerateDeployment() should set imagePullPolicy: Never for local images")
	}
}

func TestGenerateDeployment_WithRegistry(t *testing.T) {
	analysis := &types.AppAnalysis{
		Name: "registry-app",
	}

	cfg := config.Default()
	cfg.CI.Registry = "gcr.io/my-project"
	resources := cfg.GetResourcesForProfile("api")

	yaml, err := GenerateDeployment(analysis, "default", resources, cfg)
	if err != nil {
		t.Fatalf("GenerateDeployment() error = %v", err)
	}

	if !strings.Contains(yaml, "image: gcr.io/my-project/registry-app:latest") {
		t.Error("GenerateDeployment() should include registry in image name")
	}
}

func TestGenerateDeployment_SecurityContext(t *testing.T) {
	analysis := &types.AppAnalysis{
		Name: "secure-app",
	}

	cfg := config.Default()
	resources := cfg.GetResourcesForProfile("api")

	yaml, err := GenerateDeployment(analysis, "default", resources, cfg)
	if err != nil {
		t.Fatalf("GenerateDeployment() error = %v", err)
	}

	if !strings.Contains(yaml, "runAsNonRoot: true") {
		t.Error("GenerateDeployment() missing runAsNonRoot")
	}
	if !strings.Contains(yaml, "allowPrivilegeEscalation: false") {
		t.Error("GenerateDeployment() missing allowPrivilegeEscalation")
	}
	if !strings.Contains(yaml, "readOnlyRootFilesystem: true") {
		t.Error("GenerateDeployment() missing readOnlyRootFilesystem")
	}
	if !strings.Contains(yaml, "drop:") {
		t.Error("GenerateDeployment() missing capabilities drop")
	}
}

func TestBuildLabelsWithAppConfig(t *testing.T) {
	analysis := &types.AppAnalysis{
		Name:        "label-test",
		Team:        "platform",
		Environment: "staging",
		AppConfig: &types.AppConfigContext{
			Labels: map[string]string{
				"custom": "value",
			},
		},
	}

	cfg := config.Default()
	cfg.Labels.Custom = map[string]string{
		"org-label": "org-value",
	}

	labels := buildLabelsWithAppConfig(analysis, cfg)

	if labels["app.kubernetes.io/name"] != "label-test" {
		t.Errorf("buildLabelsWithAppConfig() name = %q, want %q", labels["app.kubernetes.io/name"], "label-test")
	}
	if labels["app.kubernetes.io/managed-by"] != "dorgu" {
		t.Errorf("buildLabelsWithAppConfig() managed-by = %q, want %q", labels["app.kubernetes.io/managed-by"], "dorgu")
	}
	if labels["app.kubernetes.io/team"] != "platform" {
		t.Errorf("buildLabelsWithAppConfig() team = %q, want %q", labels["app.kubernetes.io/team"], "platform")
	}
	if labels["app.kubernetes.io/environment"] != "staging" {
		t.Errorf("buildLabelsWithAppConfig() environment = %q, want %q", labels["app.kubernetes.io/environment"], "staging")
	}
	if labels["org-label"] != "org-value" {
		t.Errorf("buildLabelsWithAppConfig() org-label = %q, want %q", labels["org-label"], "org-value")
	}
	if labels["custom"] != "value" {
		t.Errorf("buildLabelsWithAppConfig() custom = %q, want %q", labels["custom"], "value")
	}
}

func TestBuildLabelsWithAppConfig_Override(t *testing.T) {
	analysis := &types.AppAnalysis{
		Name: "override-test",
		AppConfig: &types.AppConfigContext{
			Labels: map[string]string{
				"shared-key": "app-value",
			},
		},
	}

	cfg := config.Default()
	cfg.Labels.Custom = map[string]string{
		"shared-key": "org-value",
	}

	labels := buildLabelsWithAppConfig(analysis, cfg)

	if labels["shared-key"] != "app-value" {
		t.Errorf("buildLabelsWithAppConfig() app config should override org config: got %q, want %q", labels["shared-key"], "app-value")
	}
}

func TestBuildAnnotationsWithAppConfig(t *testing.T) {
	analysis := &types.AppAnalysis{
		Name: "annotation-test",
		AppConfig: &types.AppConfigContext{
			Annotations: map[string]string{
				"app-annotation": "app-value",
			},
		},
	}

	cfg := config.Default()
	cfg.Annotations.Custom = map[string]string{
		"org-annotation": "org-value",
	}

	annotations := buildAnnotationsWithAppConfig(analysis, cfg)

	if annotations["org-annotation"] != "org-value" {
		t.Errorf("buildAnnotationsWithAppConfig() org-annotation = %q, want %q", annotations["org-annotation"], "org-value")
	}
	if annotations["app-annotation"] != "app-value" {
		t.Errorf("buildAnnotationsWithAppConfig() app-annotation = %q, want %q", annotations["app-annotation"], "app-value")
	}
}

func TestBuildAnnotationsWithAppConfig_Empty(t *testing.T) {
	analysis := &types.AppAnalysis{
		Name: "no-annotations",
	}

	cfg := config.Default()

	annotations := buildAnnotationsWithAppConfig(analysis, cfg)

	if annotations != nil {
		t.Errorf("buildAnnotationsWithAppConfig() should return nil for empty annotations, got %v", annotations)
	}
}

func TestGenerateDeployment_DNSSafeName(t *testing.T) {
	analysis := &types.AppAnalysis{
		Name: "My_App_Name",
	}

	cfg := config.Default()
	resources := cfg.GetResourcesForProfile("api")

	yaml, err := GenerateDeployment(analysis, "default", resources, cfg)
	if err != nil {
		t.Fatalf("GenerateDeployment() error = %v", err)
	}

	if !strings.Contains(yaml, "name: my-app-name") {
		t.Error("GenerateDeployment() should convert name to DNS-safe format")
	}
}

func TestGenerateDeployment_AppConfigHealth(t *testing.T) {
	analysis := &types.AppAnalysis{
		Name: "health-config-app",
		AppConfig: &types.AppConfigContext{
			Health: &types.HealthContext{
				LivenessPath:  "/live",
				LivenessPort:  8080,
				ReadinessPath: "/ready",
				ReadinessPort: 8080,
				InitialDelay:  30,
				Period:        15,
			},
		},
	}

	cfg := config.Default()
	resources := cfg.GetResourcesForProfile("api")

	yaml, err := GenerateDeployment(analysis, "default", resources, cfg)
	if err != nil {
		t.Fatalf("GenerateDeployment() error = %v", err)
	}

	if !strings.Contains(yaml, "path: /live") {
		t.Error("GenerateDeployment() missing liveness path from app config")
	}
	if !strings.Contains(yaml, "path: /ready") {
		t.Error("GenerateDeployment() missing readiness path from app config")
	}
	if !strings.Contains(yaml, "initialDelaySeconds: 30") {
		t.Error("GenerateDeployment() missing custom initialDelaySeconds")
	}
	if !strings.Contains(yaml, "periodSeconds: 15") {
		t.Error("GenerateDeployment() missing custom periodSeconds")
	}
}
