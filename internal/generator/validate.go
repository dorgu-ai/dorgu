package generator

import (
	"fmt"
	"strings"

	"github.com/dorgu-ai/dorgu/internal/types"
)

// ValidationSeverity is the severity of a validation issue
type ValidationSeverity string

const (
	SeverityError   ValidationSeverity = "error"
	SeverityWarning ValidationSeverity = "warning"
	SeverityInfo    ValidationSeverity = "info"
)

// ValidationIssue is a single validation finding
type ValidationIssue struct {
	Severity   ValidationSeverity
	Category   string
	File       string
	Message    string
	Suggestion string
}

// ValidationResult is the full validation report
type ValidationResult struct {
	Issues  []ValidationIssue
	Passed  bool
	Summary string
}

// ValidateGenerated runs post-generation validation and returns a report
func ValidateGenerated(analysis *types.AppAnalysis, files []GeneratedFile, opts Options) *ValidationResult {
	result := &ValidationResult{Passed: true}

	validateImagePlaceholder(analysis, opts, result)
	validateResourceRequestsVsLimits(analysis, opts, result)
	validateServicePortMatch(analysis, result)
	validateHPAMinMax(result, analysis)
	validateIngressHost(analysis, opts, result)
	validateHealthProbes(analysis, result)
	validateMissingRequiredFields(analysis, result)

	for _, issue := range result.Issues {
		if issue.Severity == SeverityError {
			result.Passed = false
			break
		}
	}

	errors, warnings, infos := 0, 0, 0
	for _, issue := range result.Issues {
		switch issue.Severity {
		case SeverityError:
			errors++
		case SeverityWarning:
			warnings++
		case SeverityInfo:
			infos++
		}
	}
	if len(result.Issues) == 0 {
		result.Summary = "All validation checks passed"
	} else {
		var parts []string
		if errors > 0 {
			parts = append(parts, fmt.Sprintf("%d error(s)", errors))
		}
		if warnings > 0 {
			parts = append(parts, fmt.Sprintf("%d warning(s)", warnings))
		}
		if infos > 0 {
			parts = append(parts, fmt.Sprintf("%d info(s)", infos))
		}
		result.Summary = "Validation: " + strings.Join(parts, ", ")
	}
	return result
}

func validateImagePlaceholder(analysis *types.AppAnalysis, opts Options, result *ValidationResult) {
	registry := opts.Config.CI.Registry
	if registry == "" {
		result.Issues = append(result.Issues, ValidationIssue{
			Severity:   SeverityWarning,
			Category:   "image",
			File:       "deployment.yaml",
			Message:    fmt.Sprintf("Container image is placeholder '%s' (no registry set)", analysis.Name+":latest"),
			Suggestion: "Set CI registry via 'dorgu config set defaults.registry <registry>' or in .dorgu.yaml",
		})
	}
	result.Issues = append(result.Issues, ValidationIssue{
		Severity:   SeverityInfo,
		Category:   "image",
		File:       "deployment.yaml",
		Message:    "Image uses ':latest' tag",
		Suggestion: "Use specific image tags in production for reproducible deployments",
	})
}

func validateResourceRequestsVsLimits(analysis *types.AppAnalysis, opts Options, result *ValidationResult) {
	resources := opts.Config.GetResourcesForProfile(analysis.ResourceProfile)
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
	reqCPU := parseCPUMillis(resources.Requests.CPU)
	limCPU := parseCPUMillis(resources.Limits.CPU)
	if reqCPU > 0 && limCPU > 0 && reqCPU > limCPU {
		result.Issues = append(result.Issues, ValidationIssue{
			Severity:   SeverityError,
			Category:   "resources",
			File:       "deployment.yaml",
			Message:    fmt.Sprintf("Resource requests > limits: CPU request (%s) > limit (%s)", resources.Requests.CPU, resources.Limits.CPU),
			Suggestion: "CPU request must be <= CPU limit",
		})
	}
	reqMem := parseMemoryBytes(resources.Requests.Memory)
	limMem := parseMemoryBytes(resources.Limits.Memory)
	if reqMem > 0 && limMem > 0 && reqMem > limMem {
		result.Issues = append(result.Issues, ValidationIssue{
			Severity:   SeverityError,
			Category:   "resources",
			File:       "deployment.yaml",
			Message:    fmt.Sprintf("Resource requests > limits: memory request (%s) > limit (%s)", resources.Requests.Memory, resources.Limits.Memory),
			Suggestion: "Memory request must be <= memory limit",
		})
	}
}

func validateServicePortMatch(analysis *types.AppAnalysis, result *ValidationResult) {
	if len(analysis.Ports) == 0 {
		return
	}
	portSet := make(map[int]bool)
	for _, p := range analysis.Ports {
		portSet[p.Port] = true
	}
	if analysis.HealthCheck != nil && !portSet[analysis.HealthCheck.Port] {
		result.Issues = append(result.Issues, ValidationIssue{
			Severity:   SeverityWarning,
			Category:   "ports",
			File:       "deployment.yaml",
			Message:    fmt.Sprintf("Health check port %d does not match any container port", analysis.HealthCheck.Port),
			Suggestion: "Ensure health check port matches one of the exposed container ports",
		})
	}
}

func validateHPAMinMax(result *ValidationResult, analysis *types.AppAnalysis) {
	scaling := analysis.Scaling
	if analysis.AppConfig != nil && analysis.AppConfig.Scaling != nil {
		scaling = analysis.AppConfig.Scaling
	}
	if scaling == nil {
		return
	}
	if scaling.MinReplicas > scaling.MaxReplicas {
		result.Issues = append(result.Issues, ValidationIssue{
			Severity:   SeverityError,
			Category:   "scaling",
			File:       "hpa.yaml",
			Message:    fmt.Sprintf("HPA minReplicas (%d) > maxReplicas (%d)", scaling.MinReplicas, scaling.MaxReplicas),
			Suggestion: "Set minReplicas <= maxReplicas",
		})
	}
}

func validateIngressHost(analysis *types.AppAnalysis, opts Options, result *ValidationResult) {
	host := analysis.Name + opts.Config.Ingress.DomainSuffix
	if analysis.AppConfig != nil && analysis.AppConfig.Ingress != nil && analysis.AppConfig.Ingress.Host != "" {
		host = analysis.AppConfig.Ingress.Host
	}
	if host == "" {
		result.Issues = append(result.Issues, ValidationIssue{
			Severity:   SeverityWarning,
			Category:   "ingress",
			File:       "ingress.yaml",
			Message:    "Ingress host is empty",
			Suggestion: "Set ingress.host in .dorgu.yaml or ensure naming.domain_suffix is set in org config",
		})
	}
}

func validateHealthProbes(analysis *types.AppAnalysis, result *ValidationResult) {
	hasHealth := (analysis.AppConfig != nil && analysis.AppConfig.Health != nil) || analysis.HealthCheck != nil
	if !hasHealth {
		result.Issues = append(result.Issues, ValidationIssue{
			Severity:   SeverityWarning,
			Category:   "health",
			File:       "deployment.yaml",
			Message:    "No health probes configured",
			Suggestion: "Add health.liveness/readiness in .dorgu.yaml or implement a /health endpoint",
		})
	}
}

func validateMissingRequiredFields(analysis *types.AppAnalysis, result *ValidationResult) {
	if analysis.Name == "" {
		result.Issues = append(result.Issues, ValidationIssue{
			Severity:   SeverityError,
			Category:   "metadata",
			File:       "deployment.yaml",
			Message:    "Missing required field: application name",
			Suggestion: "Set app.name in .dorgu.yaml or use --name",
		})
	}
	if analysis.Repository == "" {
		result.Issues = append(result.Issues, ValidationIssue{
			Severity:   SeverityInfo,
			Category:   "metadata",
			File:       "argocd/application.yaml",
			Message:    "Repository URL not set",
			Suggestion: "Set app.repository in .dorgu.yaml or ensure git remote origin is configured",
		})
	}
}

func parseCPUMillis(cpu string) int64 {
	if cpu == "" {
		return 0
	}
	if strings.HasSuffix(cpu, "m") {
		var millis int64
		fmt.Sscanf(strings.TrimSuffix(cpu, "m"), "%d", &millis)
		return millis
	}
	var cores float64
	fmt.Sscanf(cpu, "%f", &cores)
	return int64(cores * 1000)
}

func parseMemoryBytes(mem string) int64 {
	if mem == "" {
		return 0
	}
	multipliers := map[string]int64{
		"Ki": 1024, "Mi": 1024 * 1024, "Gi": 1024 * 1024 * 1024, "Ti": 1024 * 1024 * 1024 * 1024,
	}
	for suffix, mult := range multipliers {
		if strings.HasSuffix(mem, suffix) {
			var num int64
			fmt.Sscanf(strings.TrimSuffix(mem, suffix), "%d", &num)
			return num * mult
		}
	}
	var bytes int64
	fmt.Sscanf(mem, "%d", &bytes)
	return bytes
}

// FormatValidationReport formats the validation result for terminal output
func FormatValidationReport(result *ValidationResult) string {
	if len(result.Issues) == 0 {
		return "  All validation checks passed"
	}
	var sb strings.Builder
	for _, sev := range []ValidationSeverity{SeverityError, SeverityWarning, SeverityInfo} {
		for _, issue := range result.Issues {
			if issue.Severity != sev {
				continue
			}
			prefix := "  ℹ"
			switch sev {
			case SeverityError:
				prefix = "  ✗"
			case SeverityWarning:
				prefix = "  ⚠"
			}
			sb.WriteString(fmt.Sprintf("%s [%s] %s\n", prefix, issue.Category, issue.Message))
			if issue.Suggestion != "" {
				sb.WriteString(fmt.Sprintf("    → %s\n", issue.Suggestion))
			}
		}
	}
	return sb.String()
}
