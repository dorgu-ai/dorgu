package analyzer

import (
	"bufio"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/dorgu-ai/dorgu/internal/types"
)

// ParseDockerfile parses a Dockerfile and extracts relevant information
func ParseDockerfile(path string) (*types.DockerfileAnalysis, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	analysis := &types.DockerfileAnalysis{
		Labels:      make(map[string]string),
		EnvVars:     []types.EnvVar{},
		Ports:       []int{},
		BuildStages: []string{},
	}

	scanner := bufio.NewScanner(file)
	var currentLine string

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Handle line continuations
		if strings.HasSuffix(line, "\\") {
			currentLine += strings.TrimSuffix(line, "\\") + " "
			continue
		}
		currentLine += line

		// Parse the complete instruction
		parseInstruction(currentLine, analysis)
		currentLine = ""
	}

	// Parse any remaining line
	if currentLine != "" {
		parseInstruction(currentLine, analysis)
	}

	return analysis, scanner.Err()
}

// parseInstruction parses a single Dockerfile instruction
func parseInstruction(line string, analysis *types.DockerfileAnalysis) {
	parts := strings.SplitN(line, " ", 2)
	if len(parts) < 2 {
		return
	}

	instruction := strings.ToUpper(parts[0])
	args := strings.TrimSpace(parts[1])

	switch instruction {
	case "FROM":
		parseFrom(args, analysis)
	case "EXPOSE":
		parseExpose(args, analysis)
	case "ENV":
		parseEnv(args, analysis)
	case "WORKDIR":
		analysis.WorkDir = args
	case "ENTRYPOINT":
		analysis.Entrypoint = parseStringList(args)
	case "CMD":
		analysis.Cmd = parseStringList(args)
	case "USER":
		analysis.User = args
	case "LABEL":
		parseLabel(args, analysis)
	}
}

// parseFrom handles FROM instructions, including multi-stage builds
func parseFrom(args string, analysis *types.DockerfileAnalysis) {
	// Handle "FROM image AS stage"
	parts := strings.Fields(args)
	image := parts[0]

	// Track build stages
	if len(parts) >= 3 && strings.ToUpper(parts[1]) == "AS" {
		analysis.BuildStages = append(analysis.BuildStages, parts[2])
	}

	// Always use the last FROM as the base image (final stage)
	analysis.BaseImage = image
}

// parseExpose handles EXPOSE instructions
func parseExpose(args string, analysis *types.DockerfileAnalysis) {
	// EXPOSE can have multiple ports: EXPOSE 80 443
	portRegex := regexp.MustCompile(`(\d+)(?:/(\w+))?`)
	matches := portRegex.FindAllStringSubmatch(args, -1)

	for _, match := range matches {
		if port, err := strconv.Atoi(match[1]); err == nil {
			// Avoid duplicates
			found := false
			for _, p := range analysis.Ports {
				if p == port {
					found = true
					break
				}
			}
			if !found {
				analysis.Ports = append(analysis.Ports, port)
			}
		}
	}
}

// parseEnv handles ENV instructions
func parseEnv(args string, analysis *types.DockerfileAnalysis) {
	// ENV can be: ENV KEY=value or ENV KEY value
	if strings.Contains(args, "=") {
		// KEY=value format (can have multiple)
		pairs := parseKeyValuePairs(args)
		for key, value := range pairs {
			analysis.EnvVars = append(analysis.EnvVars, types.EnvVar{
				Name:  key,
				Value: value,
			})
		}
	} else {
		// KEY value format
		parts := strings.SplitN(args, " ", 2)
		if len(parts) == 2 {
			analysis.EnvVars = append(analysis.EnvVars, types.EnvVar{
				Name:  parts[0],
				Value: parts[1],
			})
		}
	}
}

// parseLabel handles LABEL instructions
func parseLabel(args string, analysis *types.DockerfileAnalysis) {
	pairs := parseKeyValuePairs(args)
	for key, value := range pairs {
		analysis.Labels[key] = value
	}
}

// parseKeyValuePairs parses KEY=value pairs
func parseKeyValuePairs(args string) map[string]string {
	result := make(map[string]string)

	// Simple regex for KEY=value or KEY="value"
	regex := regexp.MustCompile(`(\w+)=(?:"([^"]*)"|'([^']*)'|(\S+))`)
	matches := regex.FindAllStringSubmatch(args, -1)

	for _, match := range matches {
		key := match[1]
		// Value is in one of the capture groups
		value := match[2]
		if value == "" {
			value = match[3]
		}
		if value == "" {
			value = match[4]
		}
		result[key] = value
	}

	return result
}

// parseStringList parses JSON-style or shell-style command lists
func parseStringList(args string) []string {
	args = strings.TrimSpace(args)

	// JSON format: ["cmd", "arg1"]
	if strings.HasPrefix(args, "[") {
		// Simple JSON array parsing
		args = strings.Trim(args, "[]")
		var result []string
		for _, part := range strings.Split(args, ",") {
			part = strings.TrimSpace(part)
			part = strings.Trim(part, `"'`)
			if part != "" {
				result = append(result, part)
			}
		}
		return result
	}

	// Shell format: cmd arg1 arg2
	return strings.Fields(args)
}
