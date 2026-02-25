package generator

import (
	"strings"
	"testing"

	"github.com/dorgu-ai/dorgu/internal/config"
	"github.com/dorgu-ai/dorgu/internal/types"
)

func TestParseCPUMillis(t *testing.T) {
	tests := []struct {
		input string
		want  int64
	}{
		{"", 0},
		{"100m", 100},
		{"500m", 500},
		{"1000m", 1000},
		{"1", 1000},
		{"2", 2000},
		{"0.5", 500},
		{"1.5", 1500},
		{"2.5", 2500},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseCPUMillis(tt.input)
			if got != tt.want {
				t.Errorf("parseCPUMillis(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseMemoryBytes(t *testing.T) {
	tests := []struct {
		input string
		want  int64
	}{
		{"", 0},
		{"1024", 1024},
		{"1Ki", 1024},
		{"1Mi", 1024 * 1024},
		{"128Mi", 128 * 1024 * 1024},
		{"1Gi", 1024 * 1024 * 1024},
		{"2Gi", 2 * 1024 * 1024 * 1024},
		{"1Ti", 1024 * 1024 * 1024 * 1024},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseMemoryBytes(tt.input)
			if got != tt.want {
				t.Errorf("parseMemoryBytes(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestValidateGenerated_AllPass(t *testing.T) {
	analysis := &types.AppAnalysis{
		Name:       "test-app",
		Repository: "https://github.com/org/repo",
		Ports:      []types.Port{{Port: 8080, Protocol: "TCP"}},
		HealthCheck: &types.HealthCheck{
			Path: "/health",
			Port: 8080,
		},
		ResourceProfile: "api",
	}

	cfg := config.Default()
	opts := Options{
		Config:    cfg,
		Namespace: "default",
	}

	files := []GeneratedFile{}

	result := ValidateGenerated(analysis, files, opts)

	if result == nil {
		t.Fatal("ValidateGenerated() returned nil")
	}

	var nonKubectlErrors int
	for _, issue := range result.Issues {
		if issue.Severity == SeverityError && issue.Category != "kubectl" {
			nonKubectlErrors++
		}
	}
	if nonKubectlErrors > 0 {
		t.Errorf("ValidateGenerated() has %d non-kubectl errors, want 0", nonKubectlErrors)
		for _, issue := range result.Issues {
			if issue.Severity == SeverityError && issue.Category != "kubectl" {
				t.Logf("  Error: %s - %s", issue.Category, issue.Message)
			}
		}
	}
}

func TestValidateGenerated_ResourceError(t *testing.T) {
	analysis := &types.AppAnalysis{
		Name:       "test-app",
		Repository: "https://github.com/org/repo",
		AppConfig: &types.AppConfigContext{
			Resources: &types.ResourceOverrides{
				RequestsCPU:    "1000m",
				LimitsCPU:      "500m",
				RequestsMemory: "1Gi",
				LimitsMemory:   "512Mi",
			},
		},
	}

	cfg := config.Default()
	opts := Options{
		Config:    cfg,
		Namespace: "default",
	}

	files := []GeneratedFile{}

	result := ValidateGenerated(analysis, files, opts)

	if result.Passed {
		t.Error("ValidateGenerated() passed = true, want false for resource errors")
	}

	var cpuError, memError bool
	for _, issue := range result.Issues {
		if issue.Severity == SeverityError && issue.Category == "resources" {
			if strings.Contains(issue.Message, "CPU") {
				cpuError = true
			}
			if strings.Contains(issue.Message, "memory") {
				memError = true
			}
		}
	}
	if !cpuError {
		t.Error("ValidateGenerated() missing CPU resource error")
	}
	if !memError {
		t.Error("ValidateGenerated() missing memory resource error")
	}
}

func TestValidateGenerated_HPAError(t *testing.T) {
	analysis := &types.AppAnalysis{
		Name:       "test-app",
		Repository: "https://github.com/org/repo",
		Scaling: &types.ScalingConfig{
			MinReplicas: 10,
			MaxReplicas: 5,
		},
	}

	cfg := config.Default()
	opts := Options{
		Config:    cfg,
		Namespace: "default",
	}

	files := []GeneratedFile{}

	result := ValidateGenerated(analysis, files, opts)

	if result.Passed {
		t.Error("ValidateGenerated() passed = true, want false for HPA error")
	}

	var hpaError bool
	for _, issue := range result.Issues {
		if issue.Severity == SeverityError && issue.Category == "scaling" {
			hpaError = true
			break
		}
	}
	if !hpaError {
		t.Error("ValidateGenerated() missing HPA minReplicas > maxReplicas error")
	}
}

func TestValidateGenerated_MissingName(t *testing.T) {
	analysis := &types.AppAnalysis{
		Name:       "",
		Repository: "https://github.com/org/repo",
	}

	cfg := config.Default()
	opts := Options{
		Config:    cfg,
		Namespace: "default",
	}

	files := []GeneratedFile{}

	result := ValidateGenerated(analysis, files, opts)

	if result.Passed {
		t.Error("ValidateGenerated() passed = true, want false for missing name")
	}

	var nameError bool
	for _, issue := range result.Issues {
		if issue.Severity == SeverityError && issue.Category == "metadata" && strings.Contains(issue.Message, "name") {
			nameError = true
			break
		}
	}
	if !nameError {
		t.Error("ValidateGenerated() missing name error")
	}
}

func TestValidateGenerated_NoHealthProbes(t *testing.T) {
	analysis := &types.AppAnalysis{
		Name:        "test-app",
		Repository:  "https://github.com/org/repo",
		HealthCheck: nil,
		AppConfig:   nil,
	}

	cfg := config.Default()
	opts := Options{
		Config:    cfg,
		Namespace: "default",
	}

	files := []GeneratedFile{}

	result := ValidateGenerated(analysis, files, opts)

	var healthWarning bool
	for _, issue := range result.Issues {
		if issue.Category == "health" && strings.Contains(issue.Message, "No health probes") {
			healthWarning = true
			break
		}
	}
	if !healthWarning {
		t.Error("ValidateGenerated() missing health probes warning")
	}
}

func TestValidateGenerated_HealthPortMismatch(t *testing.T) {
	analysis := &types.AppAnalysis{
		Name:       "test-app",
		Repository: "https://github.com/org/repo",
		Ports:      []types.Port{{Port: 8080, Protocol: "TCP"}},
		HealthCheck: &types.HealthCheck{
			Path: "/health",
			Port: 9090,
		},
	}

	cfg := config.Default()
	opts := Options{
		Config:    cfg,
		Namespace: "default",
	}

	files := []GeneratedFile{}

	result := ValidateGenerated(analysis, files, opts)

	var portWarning bool
	for _, issue := range result.Issues {
		if issue.Category == "ports" && strings.Contains(issue.Message, "Health check port") {
			portWarning = true
			break
		}
	}
	if !portWarning {
		t.Error("ValidateGenerated() missing health check port mismatch warning")
	}
}

func TestFormatValidationReport_Empty(t *testing.T) {
	result := &ValidationResult{
		Issues: []ValidationIssue{},
		Passed: true,
	}

	output := FormatValidationReport(result)

	if !strings.Contains(output, "All validation checks passed") {
		t.Errorf("FormatValidationReport() = %q, want to contain 'All validation checks passed'", output)
	}
}

func TestFormatValidationReport_WithIssues(t *testing.T) {
	result := &ValidationResult{
		Issues: []ValidationIssue{
			{
				Severity:   SeverityError,
				Category:   "resources",
				Message:    "CPU request > limit",
				Suggestion: "Fix resources",
			},
			{
				Severity:   SeverityWarning,
				Category:   "health",
				Message:    "No health probes",
				Suggestion: "Add health checks",
			},
			{
				Severity:   SeverityInfo,
				Category:   "image",
				Message:    "Using latest tag",
				Suggestion: "Use specific tags",
			},
		},
		Passed: false,
	}

	output := FormatValidationReport(result)

	if !strings.Contains(output, "✗") {
		t.Error("FormatValidationReport() missing error marker (✗)")
	}
	if !strings.Contains(output, "⚠") {
		t.Error("FormatValidationReport() missing warning marker (⚠)")
	}
	if !strings.Contains(output, "ℹ") {
		t.Error("FormatValidationReport() missing info marker (ℹ)")
	}
	if !strings.Contains(output, "CPU request > limit") {
		t.Error("FormatValidationReport() missing error message")
	}
	if !strings.Contains(output, "No health probes") {
		t.Error("FormatValidationReport() missing warning message")
	}
	if !strings.Contains(output, "→") {
		t.Error("FormatValidationReport() missing suggestion arrow")
	}
}

func TestFormatValidationReport_OrderBySeverity(t *testing.T) {
	result := &ValidationResult{
		Issues: []ValidationIssue{
			{Severity: SeverityInfo, Category: "info", Message: "info message"},
			{Severity: SeverityError, Category: "error", Message: "error message"},
			{Severity: SeverityWarning, Category: "warning", Message: "warning message"},
		},
	}

	output := FormatValidationReport(result)

	errorIdx := strings.Index(output, "error message")
	warningIdx := strings.Index(output, "warning message")
	infoIdx := strings.Index(output, "info message")

	if errorIdx > warningIdx || warningIdx > infoIdx {
		t.Errorf("FormatValidationReport() issues not ordered by severity: error=%d, warning=%d, info=%d", errorIdx, warningIdx, infoIdx)
	}
}

func TestValidateGenerated_Summary_WithErrors(t *testing.T) {
	analysis := &types.AppAnalysis{
		Name:       "",
		Repository: "https://github.com/org/repo",
	}

	cfg := config.Default()
	opts := Options{Config: cfg, Namespace: "default"}

	result := ValidateGenerated(analysis, []GeneratedFile{}, opts)

	if !strings.Contains(result.Summary, "Validation:") {
		t.Errorf("ValidateGenerated().Summary = %q, want to contain 'Validation:'", result.Summary)
	}
	if !strings.Contains(result.Summary, "error") {
		t.Errorf("ValidateGenerated().Summary = %q, want to contain 'error'", result.Summary)
	}
}

func TestValidateGenerated_MissingRepository(t *testing.T) {
	analysis := &types.AppAnalysis{
		Name:       "test-app",
		Repository: "",
	}

	cfg := config.Default()
	opts := Options{
		Config:    cfg,
		Namespace: "default",
	}

	files := []GeneratedFile{}

	result := ValidateGenerated(analysis, files, opts)

	var repoInfo bool
	for _, issue := range result.Issues {
		if issue.Severity == SeverityInfo && issue.Category == "metadata" && strings.Contains(issue.Message, "Repository") {
			repoInfo = true
			break
		}
	}
	if !repoInfo {
		t.Error("ValidateGenerated() missing repository info message")
	}
}
