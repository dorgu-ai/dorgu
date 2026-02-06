package analyzer

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/dorgu-ai/dorgu/internal/types"
)

// AnalyzeCode analyzes the source code in a directory
func AnalyzeCode(path string) (*types.CodeAnalysis, error) {
	analysis := &types.CodeAnalysis{}

	// Detect language and framework based on files present
	if err := detectLanguageAndFramework(path, analysis); err != nil {
		return nil, err
	}

	// Look for health endpoints
	analysis.HealthPath = detectHealthEndpoint(path, analysis.Language)
	analysis.MetricsPath = detectMetricsEndpoint(path, analysis.Language)

	return analysis, nil
}

// detectLanguageAndFramework detects the primary language and framework
func detectLanguageAndFramework(path string, analysis *types.CodeAnalysis) error {
	// Check for Node.js
	packageJSON := filepath.Join(path, "package.json")
	if _, err := os.Stat(packageJSON); err == nil {
		analysis.Language = "javascript"
		analysis.Framework = detectNodeFramework(packageJSON)
		analysis.Dependencies = extractNodeDependencies(packageJSON)
		return nil
	}

	// Check for Python
	for _, pyFile := range []string{"requirements.txt", "pyproject.toml", "setup.py", "Pipfile"} {
		pyPath := filepath.Join(path, pyFile)
		if _, err := os.Stat(pyPath); err == nil {
			analysis.Language = "python"
			analysis.Framework = detectPythonFramework(path)
			analysis.Dependencies = extractPythonDependencies(path)
			return nil
		}
	}

	// Check for Go
	goMod := filepath.Join(path, "go.mod")
	if _, err := os.Stat(goMod); err == nil {
		analysis.Language = "go"
		analysis.Framework = detectGoFramework(goMod)
		analysis.Dependencies = extractGoDependencies(goMod)
		return nil
	}

	// Check for Java (Maven)
	pomXML := filepath.Join(path, "pom.xml")
	if _, err := os.Stat(pomXML); err == nil {
		analysis.Language = "java"
		analysis.Framework = "spring" // Most common
		return nil
	}

	// Check for Java (Gradle)
	buildGradle := filepath.Join(path, "build.gradle")
	if _, err := os.Stat(buildGradle); err == nil {
		analysis.Language = "java"
		analysis.Framework = "spring"
		return nil
	}

	// Check for Ruby
	gemfile := filepath.Join(path, "Gemfile")
	if _, err := os.Stat(gemfile); err == nil {
		analysis.Language = "ruby"
		analysis.Framework = detectRubyFramework(gemfile)
		return nil
	}

	// Check for Rust
	cargoToml := filepath.Join(path, "Cargo.toml")
	if _, err := os.Stat(cargoToml); err == nil {
		analysis.Language = "rust"
		return nil
	}

	// Default to unknown
	analysis.Language = "unknown"
	return nil
}

// detectNodeFramework detects the Node.js framework from package.json
func detectNodeFramework(packageJSON string) string {
	data, err := os.ReadFile(packageJSON)
	if err != nil {
		return ""
	}

	var pkg struct {
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}

	if err := json.Unmarshal(data, &pkg); err != nil {
		return ""
	}

	// Check dependencies for known frameworks
	frameworks := map[string]string{
		"next":           "nextjs",
		"express":        "express",
		"fastify":        "fastify",
		"@nestjs/core":   "nestjs",
		"koa":            "koa",
		"hapi":           "hapi",
		"@hapi/hapi":     "hapi",
		"nuxt":           "nuxt",
		"gatsby":         "gatsby",
		"react":          "react",
		"vue":            "vue",
		"@angular/core":  "angular",
	}

	for dep, framework := range frameworks {
		if _, ok := pkg.Dependencies[dep]; ok {
			return framework
		}
	}

	return ""
}

// extractNodeDependencies extracts dependencies from package.json
func extractNodeDependencies(packageJSON string) []string {
	data, err := os.ReadFile(packageJSON)
	if err != nil {
		return nil
	}

	var pkg struct {
		Dependencies map[string]string `json:"dependencies"`
	}

	if err := json.Unmarshal(data, &pkg); err != nil {
		return nil
	}

	// Look for common external service dependencies
	externalDeps := []string{}
	serviceDeps := map[string]string{
		"pg":           "postgresql",
		"mysql":        "mysql",
		"mysql2":       "mysql",
		"mongodb":      "mongodb",
		"mongoose":     "mongodb",
		"redis":        "redis",
		"ioredis":      "redis",
		"kafkajs":      "kafka",
		"amqplib":      "rabbitmq",
		"elasticsearch": "elasticsearch",
	}

	for dep, service := range serviceDeps {
		if _, ok := pkg.Dependencies[dep]; ok {
			externalDeps = append(externalDeps, service)
		}
	}

	return externalDeps
}

// detectPythonFramework detects Python framework
func detectPythonFramework(path string) string {
	reqPath := filepath.Join(path, "requirements.txt")
	data, err := os.ReadFile(reqPath)
	if err != nil {
		return ""
	}

	content := strings.ToLower(string(data))
	frameworks := map[string]string{
		"fastapi":  "fastapi",
		"flask":    "flask",
		"django":   "django",
		"starlette": "starlette",
		"tornado":  "tornado",
		"aiohttp":  "aiohttp",
	}

	for dep, framework := range frameworks {
		if strings.Contains(content, dep) {
			return framework
		}
	}

	return ""
}

// extractPythonDependencies extracts external service dependencies
func extractPythonDependencies(path string) []string {
	reqPath := filepath.Join(path, "requirements.txt")
	data, err := os.ReadFile(reqPath)
	if err != nil {
		return nil
	}

	content := strings.ToLower(string(data))
	externalDeps := []string{}
	serviceDeps := map[string]string{
		"psycopg2":     "postgresql",
		"asyncpg":      "postgresql",
		"pymysql":      "mysql",
		"pymongo":      "mongodb",
		"redis":        "redis",
		"kafka-python": "kafka",
		"pika":         "rabbitmq",
		"elasticsearch": "elasticsearch",
		"celery":       "redis", // Celery typically uses Redis
	}

	for dep, service := range serviceDeps {
		if strings.Contains(content, dep) {
			externalDeps = append(externalDeps, service)
		}
	}

	return externalDeps
}

// detectGoFramework detects Go web framework from go.mod
func detectGoFramework(goMod string) string {
	data, err := os.ReadFile(goMod)
	if err != nil {
		return ""
	}

	content := string(data)
	frameworks := map[string]string{
		"github.com/gin-gonic/gin":    "gin",
		"github.com/labstack/echo":    "echo",
		"github.com/gofiber/fiber":    "fiber",
		"github.com/gorilla/mux":      "gorilla",
		"github.com/go-chi/chi":       "chi",
		"github.com/beego/beego":      "beego",
	}

	for dep, framework := range frameworks {
		if strings.Contains(content, dep) {
			return framework
		}
	}

	return ""
}

// extractGoDependencies extracts external service dependencies from go.mod
func extractGoDependencies(goMod string) []string {
	data, err := os.ReadFile(goMod)
	if err != nil {
		return nil
	}

	content := string(data)
	externalDeps := []string{}
	serviceDeps := map[string]string{
		"github.com/lib/pq":         "postgresql",
		"github.com/jackc/pgx":      "postgresql",
		"github.com/go-sql-driver/mysql": "mysql",
		"go.mongodb.org/mongo-driver": "mongodb",
		"github.com/go-redis/redis": "redis",
		"github.com/segmentio/kafka-go": "kafka",
		"github.com/streadway/amqp": "rabbitmq",
	}

	for dep, service := range serviceDeps {
		if strings.Contains(content, dep) {
			externalDeps = append(externalDeps, service)
		}
	}

	return externalDeps
}

// detectRubyFramework detects Ruby framework from Gemfile
func detectRubyFramework(gemfile string) string {
	data, err := os.ReadFile(gemfile)
	if err != nil {
		return ""
	}

	content := string(data)
	if strings.Contains(content, "rails") {
		return "rails"
	}
	if strings.Contains(content, "sinatra") {
		return "sinatra"
	}
	if strings.Contains(content, "hanami") {
		return "hanami"
	}

	return ""
}

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
	filepath.Walk(path, func(filePath string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}

		// Skip node_modules, vendor, etc.
		if strings.Contains(filePath, "node_modules") ||
			strings.Contains(filePath, "vendor") ||
			strings.Contains(filePath, ".git") {
			return filepath.SkipDir
		}

		// Only check relevant file types
		ext := filepath.Ext(filePath)
		relevantExts := map[string]bool{
			".js": true, ".ts": true, ".py": true, ".go": true,
			".rb": true, ".java": true, ".rs": true,
		}
		if !relevantExts[ext] {
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
	filepath.Walk(path, func(filePath string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}

		if strings.Contains(filePath, "node_modules") ||
			strings.Contains(filePath, "vendor") {
			return filepath.SkipDir
		}

		ext := filepath.Ext(filePath)
		relevantExts := map[string]bool{
			".js": true, ".ts": true, ".py": true, ".go": true,
		}
		if !relevantExts[ext] {
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
