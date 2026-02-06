package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/sashabaranov/go-openai"

	"github.com/dorgu-ai/dorgu/internal/types"
)

// OpenAIClient implements the Client interface for OpenAI
type OpenAIClient struct {
	client *openai.Client
	model  string
}

// NewOpenAIClient creates a new OpenAI client
func NewOpenAIClient(apiKey string) *OpenAIClient {
	return &OpenAIClient{
		client: openai.NewClient(apiKey),
		model:  openai.GPT4TurboPreview, // Use GPT-4 Turbo for better JSON handling
	}
}

// AnalyzeApp uses GPT to analyze an application
func (c *OpenAIClient) AnalyzeApp(analysis *types.AppAnalysis) (*types.AppAnalysis, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	prompt := buildAnalysisPrompt(analysis)

	resp, err := c.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model: c.model,
		Messages: []openai.ChatCompletionMessage{
			{
				Role:    openai.ChatMessageRoleSystem,
				Content: "You are an expert DevOps engineer analyzing containerized applications to generate Kubernetes deployment configurations. Always respond with valid JSON.",
			},
			{
				Role:    openai.ChatMessageRoleUser,
				Content: prompt,
			},
		},
		ResponseFormat: &openai.ChatCompletionResponseFormat{
			Type: openai.ChatCompletionResponseFormatTypeJSONObject,
		},
		Temperature: 0.3, // Lower temperature for more consistent output
	})

	if err != nil {
		return nil, fmt.Errorf("OpenAI API error: %w", err)
	}

	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("no response from OpenAI")
	}

	// Parse the response
	var result types.AppAnalysis
	if err := json.Unmarshal([]byte(resp.Choices[0].Message.Content), &result); err != nil {
		return nil, fmt.Errorf("failed to parse LLM response: %w", err)
	}

	return &result, nil
}

// GeneratePersona generates an application persona document
func (c *OpenAIClient) GeneratePersona(analysis *types.AppAnalysis) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	prompt := buildPersonaPrompt(analysis)

	resp, err := c.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model: c.model,
		Messages: []openai.ChatCompletionMessage{
			{
				Role:    openai.ChatMessageRoleSystem,
				Content: "You are a technical writer creating documentation for platform engineers. Write clear, concise documentation that helps engineers understand applications quickly during incidents.",
			},
			{
				Role:    openai.ChatMessageRoleUser,
				Content: prompt,
			},
		},
		Temperature: 0.5,
	})

	if err != nil {
		return "", fmt.Errorf("OpenAI API error: %w", err)
	}

	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("no response from OpenAI")
	}

	return resp.Choices[0].Message.Content, nil
}

// Complete sends a generic prompt and returns the completion
func (c *OpenAIClient) Complete(ctx context.Context, prompt string) (string, error) {
	resp, err := c.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model: c.model,
		Messages: []openai.ChatCompletionMessage{
			{
				Role:    openai.ChatMessageRoleUser,
				Content: prompt,
			},
		},
	})

	if err != nil {
		return "", fmt.Errorf("OpenAI API error: %w", err)
	}

	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("no response from OpenAI")
	}

	return resp.Choices[0].Message.Content, nil
}

// buildAnalysisPrompt creates the prompt for application analysis
func buildAnalysisPrompt(analysis *types.AppAnalysis) string {
	// Build context from existing analysis
	var dockerInfo, composeInfo, codeInfo, appConfigInfo string

	if analysis.Dockerfile != nil {
		dockerInfo = fmt.Sprintf(`
Dockerfile Analysis:
- Base Image: %s
- Exposed Ports: %v
- Environment Variables: %d defined
- Working Directory: %s
- Entrypoint: %v
- CMD: %v
- User: %s
`,
			analysis.Dockerfile.BaseImage,
			analysis.Dockerfile.Ports,
			len(analysis.Dockerfile.EnvVars),
			analysis.Dockerfile.WorkDir,
			analysis.Dockerfile.Entrypoint,
			analysis.Dockerfile.Cmd,
			analysis.Dockerfile.User,
		)
	}

	if analysis.Compose != nil && len(analysis.Compose.Services) > 0 {
		composeInfo = "\nDocker Compose Services:\n"
		for _, svc := range analysis.Compose.Services {
			composeInfo += fmt.Sprintf("- %s: ports=%v, depends_on=%v\n",
				svc.Name, svc.Ports, svc.DependsOn)
		}
	}

	if analysis.Code != nil {
		codeInfo = fmt.Sprintf(`
Code Analysis:
- Language: %s
- Framework: %s
- External Dependencies: %v
- Health Endpoint: %s
- Metrics Endpoint: %s
`,
			analysis.Code.Language,
			analysis.Code.Framework,
			analysis.Code.Dependencies,
			analysis.Code.HealthPath,
			analysis.Code.MetricsPath,
		)
	}

	// Include app config context if available
	if analysis.AppConfig != nil {
		appConfigInfo = "\nApplication Configuration (.dorgu.yaml):\n"
		if analysis.AppConfig.Name != "" {
			appConfigInfo += fmt.Sprintf("- Name: %s\n", analysis.AppConfig.Name)
		}
		if analysis.AppConfig.Description != "" {
			appConfigInfo += fmt.Sprintf("- Description: %s\n", analysis.AppConfig.Description)
		}
		if analysis.AppConfig.Team != "" {
			appConfigInfo += fmt.Sprintf("- Team: %s\n", analysis.AppConfig.Team)
		}
		if analysis.AppConfig.Type != "" {
			appConfigInfo += fmt.Sprintf("- Type: %s\n", analysis.AppConfig.Type)
		}
		if analysis.AppConfig.Environment != "" {
			appConfigInfo += fmt.Sprintf("- Environment: %s\n", analysis.AppConfig.Environment)
		}
		if len(analysis.AppConfig.Dependencies) > 0 {
			appConfigInfo += "- Known Dependencies:\n"
			for _, dep := range analysis.AppConfig.Dependencies {
				required := ""
				if dep.Required {
					required = " (required)"
				}
				appConfigInfo += fmt.Sprintf("  - %s (%s)%s\n", dep.Name, dep.Type, required)
			}
		}
		if analysis.AppConfig.Instructions != "" {
			appConfigInfo += fmt.Sprintf("\nApplication Context from Owner:\n%s\n", analysis.AppConfig.Instructions)
		}
	}

	return fmt.Sprintf(`Analyze this containerized application and provide deployment recommendations.

Application Name: %s
%s%s%s%s

Based on this information, provide a JSON response with:
{
  "name": "application name (lowercase, dns-safe)",
  "type": "api|web|worker|cron (what type of workload is this)",
  "language": "primary programming language",
  "framework": "detected framework",
  "description": "one paragraph description of what this application likely does based on the analysis",
  "ports": [{"port": 8080, "protocol": "TCP", "purpose": "HTTP API"}],
  "health_check": {"path": "/health", "port": 8080, "initial_delay_seconds": 10, "period_seconds": 10},
  "dependencies": ["postgresql", "redis"],
  "resource_profile": "api|worker|web (for resource sizing)",
  "scaling": {"min_replicas": 2, "max_replicas": 10, "target_cpu_percent": 70}
}

Ensure all values are appropriate for a production Kubernetes deployment.`,
		analysis.Name,
		dockerInfo,
		composeInfo,
		codeInfo,
		appConfigInfo,
	)
}

// buildPersonaPrompt creates the prompt for persona generation
func buildPersonaPrompt(analysis *types.AppAnalysis) string {
	analysisJSON, _ := json.MarshalIndent(analysis, "", "  ")

	// Build ownership section based on app config
	ownershipSection := `## Ownership
- Team: [PLACEHOLDER]
- Contact: [PLACEHOLDER]
- Repository: [PLACEHOLDER]`

	if analysis.AppConfig != nil {
		team := analysis.Team
		if team == "" {
			team = "[PLACEHOLDER]"
		}
		owner := analysis.Owner
		if owner == "" {
			owner = "[PLACEHOLDER]"
		}
		repo := analysis.Repository
		if repo == "" {
			repo = "[PLACEHOLDER]"
		}

		ownershipSection = fmt.Sprintf(`## Ownership
- Team: %s
- Contact: %s
- Repository: %s`, team, owner, repo)

		// Add operations info if available
		if analysis.AppConfig.Operations != nil {
			ops := analysis.AppConfig.Operations
			if ops.Runbook != "" {
				ownershipSection += fmt.Sprintf("\n- Runbook: %s", ops.Runbook)
			}
			if ops.OnCall != "" {
				ownershipSection += fmt.Sprintf("\n- On-Call: %s", ops.OnCall)
			}
			if ops.MaintenanceWindow != "" {
				ownershipSection += fmt.Sprintf("\n- Maintenance Window: %s", ops.MaintenanceWindow)
			}
		}
	}

	// Build alerts section if available
	alertsSection := ""
	if analysis.AppConfig != nil && analysis.AppConfig.Operations != nil && len(analysis.AppConfig.Operations.Alerts) > 0 {
		alertsSection = "\n\n## Alerts\nConfigured alerts for this application:\n"
		for _, alert := range analysis.AppConfig.Operations.Alerts {
			alertsSection += fmt.Sprintf("- %s\n", alert)
		}
	}

	// Build custom instructions context
	customContext := ""
	if analysis.AppConfig != nil && analysis.AppConfig.Instructions != "" {
		customContext = fmt.Sprintf(`

Additional Context from Application Owner:
%s
`, analysis.AppConfig.Instructions)
	}

	return fmt.Sprintf(`Based on the following application analysis, generate a comprehensive application persona document in Markdown format.

Application Analysis:
%s%s

Generate a persona document with the following sections:

# [Application Name]

## Overview
What this application does in plain English (2-3 sentences). Use any provided context from the application owner to make this description accurate and specific.

## Technical Stack
- Language, framework, runtime version
- Key dependencies

## API/Interfaces
- Exposed ports and their purposes (make sure to list the actual port numbers from the analysis)
- Known endpoints

## External Dependencies
- Databases, caches, message queues this app needs
- Other services it communicates with

## Resource Profile
- Expected CPU/memory usage
- Scaling characteristics

## Health & Monitoring
- Health check endpoints
- Key metrics to watch
- Common alert conditions

%s%s

## Operational Notes
- Startup time and dependencies
- Graceful shutdown behavior
- Common issues and troubleshooting

Write this for a platform engineer who needs to understand this application quickly during an incident.
Make sure to include specific port numbers in the API/Interfaces section.`,
		string(analysisJSON),
		customContext,
		ownershipSection,
		alertsSection,
	)
}
