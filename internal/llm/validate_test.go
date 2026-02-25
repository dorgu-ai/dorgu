package llm

import (
	"testing"

	"github.com/dorgu-ai/dorgu/internal/types"
)

func TestNormalizeLLMAnalysis(t *testing.T) {
	tests := []struct {
		name            string
		inputProfile    string
		inputType       string
		expectedProfile string
		expectedType    string
	}{
		{"valid api profile and type", "api", "api", "api", "api"},
		{"valid worker profile and type", "worker", "worker", "worker", "worker"},
		{"valid web profile and type", "web", "web", "web", "web"},
		{"valid cron type", "api", "cron", "api", "cron"},
		{"empty profile defaults to api", "", "api", "api", "api"},
		{"empty type defaults to api", "api", "", "api", "api"},
		{"both empty default to api", "", "", "api", "api"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			analysis := &types.AppAnalysis{
				Name:            "test-app",
				ResourceProfile: tt.inputProfile,
				Type:            tt.inputType,
			}

			NormalizeLLMAnalysis(analysis)

			if analysis.ResourceProfile != tt.expectedProfile {
				t.Errorf("NormalizeLLMAnalysis() ResourceProfile = %q, want %q", analysis.ResourceProfile, tt.expectedProfile)
			}
			if analysis.Type != tt.expectedType {
				t.Errorf("NormalizeLLMAnalysis() Type = %q, want %q", analysis.Type, tt.expectedType)
			}
		})
	}
}

func TestNormalizeLLMAnalysis_InvalidProfile(t *testing.T) {
	invalidProfiles := []string{"invalid", "unknown", "large", "small", "medium", "API", "Worker", "WEB"}

	for _, profile := range invalidProfiles {
		t.Run("profile="+profile, func(t *testing.T) {
			analysis := &types.AppAnalysis{
				Name:            "test-app",
				ResourceProfile: profile,
				Type:            "api",
			}

			NormalizeLLMAnalysis(analysis)

			if analysis.ResourceProfile != "api" {
				t.Errorf("NormalizeLLMAnalysis() invalid profile %q should default to 'api', got %q", profile, analysis.ResourceProfile)
			}
		})
	}
}

func TestNormalizeLLMAnalysis_InvalidType(t *testing.T) {
	invalidTypes := []string{"invalid", "service", "daemon", "batch", "API", "WEB", "WORKER"}

	for _, appType := range invalidTypes {
		t.Run("type="+appType, func(t *testing.T) {
			analysis := &types.AppAnalysis{
				Name:            "test-app",
				ResourceProfile: "api",
				Type:            appType,
			}

			NormalizeLLMAnalysis(analysis)

			if analysis.Type != "api" {
				t.Errorf("NormalizeLLMAnalysis() invalid type %q should default to 'api', got %q", appType, analysis.Type)
			}
		})
	}
}

func TestNormalizeLLMAnalysis_Nil(t *testing.T) {
	NormalizeLLMAnalysis(nil)
}

func TestNormalizeLLMAnalysis_PreservesOtherFields(t *testing.T) {
	analysis := &types.AppAnalysis{
		Name:            "my-app",
		Language:        "go",
		Framework:       "gin",
		Description:     "Test application",
		ResourceProfile: "invalid",
		Type:            "invalid",
		Team:            "platform",
		Owner:           "john@example.com",
		Repository:      "https://github.com/org/repo",
	}

	NormalizeLLMAnalysis(analysis)

	if analysis.Name != "my-app" {
		t.Errorf("NormalizeLLMAnalysis() should preserve Name, got %q", analysis.Name)
	}
	if analysis.Language != "go" {
		t.Errorf("NormalizeLLMAnalysis() should preserve Language, got %q", analysis.Language)
	}
	if analysis.Framework != "gin" {
		t.Errorf("NormalizeLLMAnalysis() should preserve Framework, got %q", analysis.Framework)
	}
	if analysis.Description != "Test application" {
		t.Errorf("NormalizeLLMAnalysis() should preserve Description, got %q", analysis.Description)
	}
	if analysis.Team != "platform" {
		t.Errorf("NormalizeLLMAnalysis() should preserve Team, got %q", analysis.Team)
	}
	if analysis.Owner != "john@example.com" {
		t.Errorf("NormalizeLLMAnalysis() should preserve Owner, got %q", analysis.Owner)
	}
	if analysis.Repository != "https://github.com/org/repo" {
		t.Errorf("NormalizeLLMAnalysis() should preserve Repository, got %q", analysis.Repository)
	}
	if analysis.ResourceProfile != "api" {
		t.Errorf("NormalizeLLMAnalysis() should normalize ResourceProfile to 'api', got %q", analysis.ResourceProfile)
	}
	if analysis.Type != "api" {
		t.Errorf("NormalizeLLMAnalysis() should normalize Type to 'api', got %q", analysis.Type)
	}
}
