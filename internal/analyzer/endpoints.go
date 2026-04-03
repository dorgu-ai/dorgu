package analyzer

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

var (
	routeRelevantExts = map[string]bool{
		".js": true, ".ts": true, ".py": true, ".go": true,
		".rb": true, ".java": true, ".rs": true,
	}
	metricsRelevantExts = map[string]bool{
		".js": true, ".ts": true, ".py": true, ".go": true,
	}
)

// detectHealthEndpoint looks for common health check endpoints
func detectHealthEndpoint(path string, language string) string {
	// Common health endpoint paths to search for
	healthPatterns := []string{
		"/health",
		"/healthz",
		"/ready",
		"/readiness",
		"/live",
		"/liveness",
		"/_health",
		"/api/health",
	}

	// Walk through source files looking for route definitions
	var foundPath string
	_ = filepath.Walk(path, func(filePath string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		base := filepath.Base(filePath)
		if info.IsDir() {
			if base == "node_modules" || base == "vendor" || base == ".git" {
				return filepath.SkipDir
			}
			return nil
		}

		ext := filepath.Ext(filePath)
		if !routeRelevantExts[ext] {
			return nil
		}

		file, err := os.Open(filePath)
		if err != nil {
			return nil
		}
		defer file.Close()

		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			line := scanner.Text()
			for _, pattern := range healthPatterns {
				if strings.Contains(line, pattern) {
					foundPath = pattern
					return filepath.SkipAll
				}
			}
		}
		return nil
	})

	if foundPath != "" {
		return foundPath
	}

	// Default to /health if language suggests a web app
	webLanguages := map[string]bool{
		"javascript": true, "python": true, "go": true,
		"ruby": true, "java": true,
	}
	if webLanguages[language] {
		return "/health"
	}

	return ""
}

// detectMetricsEndpoint looks for Prometheus metrics endpoint
func detectMetricsEndpoint(path string, language string) string {
	// Walk through source files looking for /metrics
	var foundPath string
	_ = filepath.Walk(path, func(filePath string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		base := filepath.Base(filePath)
		if info.IsDir() {
			if base == "node_modules" || base == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}

		ext := filepath.Ext(filePath)
		if !metricsRelevantExts[ext] {
			return nil
		}

		data, err := os.ReadFile(filePath)
		if err != nil {
			return nil
		}

		if strings.Contains(string(data), "/metrics") {
			foundPath = "/metrics"
			return filepath.SkipAll
		}
		return nil
	})

	return foundPath
}
