package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/dorgu-ai/dorgu/internal/types"
)

// AnthropicClient implements the Client interface for Anthropic Claude
type AnthropicClient struct {
	apiKey string
	model  string
	client *http.Client
}

// NewAnthropicClient creates a new Anthropic client
func NewAnthropicClient(apiKey string) *AnthropicClient {
	return &AnthropicClient{
		apiKey: apiKey,
		model:  "claude-3-sonnet-20240229",
		client: &http.Client{Timeout: 60 * time.Second},
	}
}

// anthropicRequest represents a request to the Anthropic API
type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    string             `json:"system,omitempty"`
	Messages  []anthropicMessage `json:"messages"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// anthropicResponse represents a response from the Anthropic API
type anthropicResponse struct {
	Content []struct {
		Text string `json:"text"`
	} `json:"content"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// AnalyzeApp uses Claude to analyze an application
func (c *AnthropicClient) AnalyzeApp(analysis *types.AppAnalysis) (*types.AppAnalysis, error) {
	prompt := buildAnalysisPrompt(analysis)

	response, err := c.complete(
		"You are an expert DevOps engineer analyzing containerized applications. Respond only with valid JSON, no markdown formatting.",
		prompt,
	)
	if err != nil {
		return nil, err
	}

	// Extract JSON from response (Claude might wrap it)
	jsonStr := extractJSON(response)

	var result types.AppAnalysis
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		return nil, fmt.Errorf("failed to parse LLM response: %w", err)
	}

	return &result, nil
}

// GeneratePersona generates an application persona document
func (c *AnthropicClient) GeneratePersona(analysis *types.AppAnalysis) (string, error) {
	prompt := buildPersonaPrompt(analysis)

	return c.complete(
		"You are a technical writer creating documentation for platform engineers.",
		prompt,
	)
}

// Complete sends a generic prompt and returns the completion
func (c *AnthropicClient) Complete(ctx context.Context, prompt string) (string, error) {
	return c.complete("", prompt)
}

func (c *AnthropicClient) complete(system, prompt string) (string, error) {
	reqBody := anthropicRequest{
		Model:     c.model,
		MaxTokens: 4096,
		System:    system,
		Messages: []anthropicMessage{
			{Role: "user", Content: prompt},
		},
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequest("POST", "https://api.anthropic.com/v1/messages", bytes.NewBuffer(jsonBody))
	if err != nil {
		return "", err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("Anthropic API request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Anthropic API error (status %d): %s", resp.StatusCode, string(body))
	}

	var anthropicResp anthropicResponse
	if err := json.Unmarshal(body, &anthropicResp); err != nil {
		return "", fmt.Errorf("failed to parse Anthropic response: %w", err)
	}

	if anthropicResp.Error != nil {
		return "", fmt.Errorf("Anthropic API error: %s", anthropicResp.Error.Message)
	}

	if len(anthropicResp.Content) == 0 {
		return "", fmt.Errorf("no content in Anthropic response")
	}

	return anthropicResp.Content[0].Text, nil
}

// extractJSON tries to extract JSON from a potentially markdown-wrapped response
func extractJSON(s string) string {
	// Look for JSON object
	start := -1
	end := -1
	depth := 0

	for i, c := range s {
		if c == '{' {
			if depth == 0 {
				start = i
			}
			depth++
		} else if c == '}' {
			depth--
			if depth == 0 && start != -1 {
				end = i + 1
				break
			}
		}
	}

	if start != -1 && end != -1 {
		return s[start:end]
	}

	return s
}
