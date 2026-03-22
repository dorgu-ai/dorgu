package analyzer

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/dorgu-ai/dorgu/internal/config"
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
