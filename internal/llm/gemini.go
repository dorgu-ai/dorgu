package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/sashabaranov/go-openai"

	"github.com/dorgu-ai/dorgu/internal/types"
)

// GeminiClient implements the Client interface for Google Gemini
// Uses Google's OpenAI-compatible endpoint
type GeminiClient struct {
	client *openai.Client
	model  string
}

// NewGeminiClient creates a new Gemini client using Google's OpenAI-compatible API
func NewGeminiClient(apiKey string) *GeminiClient {
	// Google's OpenAI-compatible endpoint
	config := openai.DefaultConfig(apiKey)
	config.BaseURL = "https://generativelanguage.googleapis.com/v1beta/openai"

	return &GeminiClient{
		client: openai.NewClientWithConfig(config),
		model:  "gemini-2.5-flash", // Fast and capable model
	}
}

// NewGeminiClientWithModel creates a Gemini client with a specific model
func NewGeminiClientWithModel(apiKey, model string) *GeminiClient {
	config := openai.DefaultConfig(apiKey)
	config.BaseURL = "https://generativelanguage.googleapis.com/v1beta/openai"

	return &GeminiClient{
		client: openai.NewClientWithConfig(config),
		model:  model,
	}
}

// AnalyzeApp uses Gemini to analyze an application
func (c *GeminiClient) AnalyzeApp(analysis *types.AppAnalysis) (*types.AppAnalysis, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	prompt := buildAnalysisPrompt(analysis)

	// Note: Gemini's OpenAI-compatible API may not support ResponseFormat,
	// so we rely on the prompt to request JSON output
	resp, err := c.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model: c.model,
		Messages: []openai.ChatCompletionMessage{
			{
				Role:    openai.ChatMessageRoleSystem,
				Content: "You are an expert DevOps engineer analyzing containerized applications to generate Kubernetes deployment configurations. You MUST respond with valid JSON only. No markdown, no code blocks, no explanations - just the raw JSON object.",
			},
			{
				Role:    openai.ChatMessageRoleUser,
				Content: prompt,
			},
		},
		Temperature: 0.3,
	})

	if err != nil {
		return nil, fmt.Errorf("Gemini API error: %w", err)
	}

	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("no response from Gemini")
	}

	// Parse the response
	var result types.AppAnalysis
	responseContent := resp.Choices[0].Message.Content

	// Try to extract JSON if wrapped in markdown
	jsonStr := extractJSON(responseContent)

	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		return nil, fmt.Errorf("failed to parse Gemini response: %w (response: %s)", err, responseContent)
	}

	return &result, nil
}

// GeneratePersona generates an application persona document
func (c *GeminiClient) GeneratePersona(analysis *types.AppAnalysis) (string, error) {
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
		return "", fmt.Errorf("Gemini API error: %w", err)
	}

	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("no response from Gemini")
	}

	return resp.Choices[0].Message.Content, nil
}

// Complete sends a generic prompt and returns the completion
func (c *GeminiClient) Complete(ctx context.Context, prompt string) (string, error) {
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
		return "", fmt.Errorf("Gemini API error: %w", err)
	}

	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("no response from Gemini")
	}

	return resp.Choices[0].Message.Content, nil
}
