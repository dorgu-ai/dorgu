package llm

import (
	"context"
	"fmt"
	"os"

	"github.com/dorgu-ai/dorgu/internal/types"
)

// Client is the interface for LLM providers
type Client interface {
	// AnalyzeApp uses LLM to provide deeper analysis of an application
	AnalyzeApp(analysis *types.AppAnalysis) (*types.AppAnalysis, error)

	// GeneratePersona generates an application persona document
	GeneratePersona(analysis *types.AppAnalysis) (string, error)

	// Complete sends a prompt and returns the completion
	Complete(ctx context.Context, prompt string) (string, error)
}

// NewClient creates a new LLM client based on the provider name
func NewClient(provider string) (Client, error) {
	switch provider {
	case "openai":
		apiKey := os.Getenv("OPENAI_API_KEY")
		if apiKey == "" {
			return nil, fmt.Errorf("OPENAI_API_KEY environment variable not set")
		}
		return NewOpenAIClient(apiKey), nil

	case "anthropic":
		apiKey := os.Getenv("ANTHROPIC_API_KEY")
		if apiKey == "" {
			return nil, fmt.Errorf("ANTHROPIC_API_KEY environment variable not set")
		}
		return NewAnthropicClient(apiKey), nil

	case "gemini":
		apiKey := os.Getenv("GEMINI_API_KEY")
		if apiKey == "" {
			// Also check for GOOGLE_API_KEY as an alternative
			apiKey = os.Getenv("GOOGLE_API_KEY")
		}
		if apiKey == "" {
			return nil, fmt.Errorf("GEMINI_API_KEY (or GOOGLE_API_KEY) environment variable not set")
		}
		return NewGeminiClient(apiKey), nil

	case "ollama":
		host := os.Getenv("OLLAMA_HOST")
		if host == "" {
			host = "http://localhost:11434"
		}
		return NewOllamaClient(host), nil

	default:
		return nil, fmt.Errorf("unknown LLM provider: %s (supported: openai, anthropic, gemini, ollama)", provider)
	}
}
