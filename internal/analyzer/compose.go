package analyzer

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/dorgu-ai/dorgu/internal/types"
	"gopkg.in/yaml.v3"
)

// ComposeFile represents a docker-compose.yml structure
type ComposeFile struct {
	Version  string                    `yaml:"version"`
	Services map[string]ComposeServiceDef `yaml:"services"`
}

// ComposeServiceDef represents a service definition in docker-compose
type ComposeServiceDef struct {
	Image       string            `yaml:"image"`
	Build       interface{}       `yaml:"build"` // Can be string or object
	Ports       []string          `yaml:"ports"`
	Environment interface{}       `yaml:"environment"` // Can be list or map
	Volumes     []string          `yaml:"volumes"`
	DependsOn   interface{}       `yaml:"depends_on"` // Can be list or map
	Healthcheck *ComposeHealthcheck `yaml:"healthcheck"`
	Command     interface{}       `yaml:"command"`
}

// ComposeHealthcheck represents a healthcheck in docker-compose
type ComposeHealthcheck struct {
	Test        interface{} `yaml:"test"` // Can be string or list
	Interval    string      `yaml:"interval"`
	Timeout     string      `yaml:"timeout"`
	Retries     int         `yaml:"retries"`
	StartPeriod string      `yaml:"start_period"`
}

// ParseComposeFile parses a docker-compose.yml file
func ParseComposeFile(path string) (*types.ComposeAnalysis, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var compose ComposeFile
	if err := yaml.Unmarshal(data, &compose); err != nil {
		return nil, fmt.Errorf("failed to parse compose file: %w", err)
	}

	analysis := &types.ComposeAnalysis{
		Services: make([]types.ComposeService, 0, len(compose.Services)),
	}

	for name, svc := range compose.Services {
		service := types.ComposeService{
			Name:    name,
			Image:   svc.Image,
			Volumes: svc.Volumes,
		}

		// Parse build context
		switch b := svc.Build.(type) {
		case string:
			service.Build = b
		case map[string]interface{}:
			if context, ok := b["context"].(string); ok {
				service.Build = context
			}
		}

		// Parse ports
		service.Ports = parsePorts(svc.Ports)

		// Parse environment
		service.Environment = parseEnvironment(svc.Environment)

		// Parse depends_on
		service.DependsOn = parseDependsOn(svc.DependsOn)

		// Parse healthcheck
		if svc.Healthcheck != nil {
			service.HealthCheck = parseHealthcheck(svc.Healthcheck)
		}

		analysis.Services = append(analysis.Services, service)
	}

	return analysis, nil
}

// parsePorts converts compose port strings to PortMapping structs
func parsePorts(ports []string) []types.PortMapping {
	var result []types.PortMapping

	for _, port := range ports {
		// Handle formats: "8080", "8080:80", "8080:80/tcp"
		mapping := types.PortMapping{Protocol: "tcp"}

		// Check for protocol suffix
		if strings.Contains(port, "/") {
			parts := strings.Split(port, "/")
			port = parts[0]
			mapping.Protocol = parts[1]
		}

		// Parse host:container
		if strings.Contains(port, ":") {
			parts := strings.Split(port, ":")
			// Could be "host:container" or "ip:host:container"
			if len(parts) == 2 {
				if h, err := strconv.Atoi(parts[0]); err == nil {
					mapping.Host = h
				}
				if c, err := strconv.Atoi(parts[1]); err == nil {
					mapping.Container = c
				}
			} else if len(parts) == 3 {
				if h, err := strconv.Atoi(parts[1]); err == nil {
					mapping.Host = h
				}
				if c, err := strconv.Atoi(parts[2]); err == nil {
					mapping.Container = c
				}
			}
		} else {
			// Just container port
			if p, err := strconv.Atoi(port); err == nil {
				mapping.Container = p
				mapping.Host = p
			}
		}

		if mapping.Container > 0 {
			result = append(result, mapping)
		}
	}

	return result
}

// parseEnvironment converts compose environment to EnvVar structs
func parseEnvironment(env interface{}) []types.EnvVar {
	var result []types.EnvVar

	switch e := env.(type) {
	case []interface{}:
		// List format: ["KEY=value", "KEY2=value2"]
		for _, item := range e {
			if s, ok := item.(string); ok {
				parts := strings.SplitN(s, "=", 2)
				envVar := types.EnvVar{Name: parts[0]}
				if len(parts) == 2 {
					envVar.Value = parts[1]
				}
				result = append(result, envVar)
			}
		}
	case map[string]interface{}:
		// Map format: {KEY: value, KEY2: value2}
		for key, val := range e {
			envVar := types.EnvVar{Name: key}
			if val != nil {
				envVar.Value = fmt.Sprintf("%v", val)
			}
			result = append(result, envVar)
		}
	}

	return result
}

// parseDependsOn extracts service dependencies
func parseDependsOn(deps interface{}) []string {
	var result []string

	switch d := deps.(type) {
	case []interface{}:
		for _, item := range d {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
	case map[string]interface{}:
		for key := range d {
			result = append(result, key)
		}
	}

	return result
}

// parseHealthcheck converts compose healthcheck to our HealthCheck struct
func parseHealthcheck(hc *ComposeHealthcheck) *types.HealthCheck {
	result := &types.HealthCheck{}

	// Parse test command to try to extract path
	switch t := hc.Test.(type) {
	case string:
		result.Path = extractHealthPath(t)
	case []interface{}:
		for _, item := range t {
			if s, ok := item.(string); ok {
				if path := extractHealthPath(s); path != "" {
					result.Path = path
					break
				}
			}
		}
	}

	return result
}

// extractHealthPath tries to extract a health check path from a command
func extractHealthPath(cmd string) string {
	// Look for curl or wget commands with paths
	if strings.Contains(cmd, "curl") || strings.Contains(cmd, "wget") {
		// Simple extraction of localhost paths
		if idx := strings.Index(cmd, "localhost"); idx != -1 {
			rest := cmd[idx:]
			// Find the path after port
			if portIdx := strings.Index(rest, "/"); portIdx != -1 {
				path := rest[portIdx:]
				// Trim any trailing characters
				if spaceIdx := strings.IndexAny(path, " \"'"); spaceIdx != -1 {
					path = path[:spaceIdx]
				}
				return path
			}
		}
	}
	return ""
}
