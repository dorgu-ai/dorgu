package llm

import (
	"github.com/dorgu-ai/dorgu/internal/types"
)

var (
	validResourceProfiles = map[string]bool{"api": true, "worker": true, "web": true}
	validTypes            = map[string]bool{"api": true, "web": true, "worker": true, "cron": true}
)

// NormalizeLLMAnalysis enforces valid enum values for resource_profile and type
// so that all providers return CRD-compatible analysis.
func NormalizeLLMAnalysis(result *types.AppAnalysis) {
	if result == nil {
		return
	}
	if !validResourceProfiles[result.ResourceProfile] {
		result.ResourceProfile = "api"
	}
	if !validTypes[result.Type] {
		result.Type = "api"
	}
}
