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

// OllamaClient implements the Client interface for local Ollama
type OllamaClient struct {
	host   string
	model  string
	client *http.Client
}

// NewOllamaClient creates a new Ollama client
func NewOllamaClient(host string) *OllamaClient {
	return &OllamaClient{
		host:   host,
		model:  "llama2",                                 // Default model, can be configured
		client: &http.Client{Timeout: 120 * time.Second}, // Longer timeout for local inference
	}
}

// ollamaRequest represents a request to the Ollama API
type ollamaRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	System string `json:"system,omitempty"`
	Stream bool   `json:"stream"`
	Format string `json:"format,omitempty"` // "json" for JSON output
}

// ollamaResponse represents a response from the Ollama API
type ollamaResponse struct {
	Response string `json:"response"`
	Done     bool   `json:"done"`
	Error    string `json:"error,omitempty"`
}

// AnalyzeApp uses Ollama to analyze an application
func (c *OllamaClient) AnalyzeApp(analysis *types.AppAnalysis) (*types.AppAnalysis, error) {
	prompt := buildAnalysisPrompt(analysis)

	response, err := c.complete(
		"You are an expert DevOps engineer analyzing containerized applications. Respond only with valid JSON.",
		prompt,
		true, // JSON format
	)
	if err != nil {
		return nil, err
	}

	// Extract JSON from response
	jsonStr := extractJSON(response)

	var result types.AppAnalysis
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		return nil, fmt.Errorf("failed to parse LLM response: %w (response: %s)", err, response)
	}

	return &result, nil
}

// GeneratePersona generates an application persona document
func (c *OllamaClient) GeneratePersona(analysis *types.AppAnalysis) (string, error) {
	prompt := buildPersonaPrompt(analysis)

	return c.complete(
		"You are a technical writer creating documentation for platform engineers.",
		prompt,
		false, // Markdown output
	)
}

// Complete sends a generic prompt and returns the completion
func (c *OllamaClient) Complete(ctx context.Context, prompt string) (string, error) {
	return c.complete("", prompt, false)
}

func (c *OllamaClient) complete(system, prompt string, jsonFormat bool) (string, error) {
	reqBody := ollamaRequest{
		Model:  c.model,
		System: system,
		Prompt: prompt,
		Stream: false,
	}

	if jsonFormat {
		reqBody.Format = "json"
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	url := fmt.Sprintf("%s/api/generate", c.host)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonBody))
	if err != nil {
		return "", err
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("Ollama API request failed (is Ollama running at %s?): %w", c.host, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Ollama API error (status %d): %s", resp.StatusCode, string(body))
	}

	var ollamaResp ollamaResponse
	if err := json.Unmarshal(body, &ollamaResp); err != nil {
		return "", fmt.Errorf("failed to parse Ollama response: %w", err)
	}

	if ollamaResp.Error != "" {
		return "", fmt.Errorf("Ollama error: %s", ollamaResp.Error)
	}

	return ollamaResp.Response, nil
}
