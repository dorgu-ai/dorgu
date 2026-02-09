package llm

import (
	"context"
	"fmt"
	"os"

	"github.com/dorgu-ai/dorgu/internal/config"
	"github.com/dorgu-ai/dorgu/internal/types"
)

// Client is the interface for LLM providers
type Client interface {
	AnalyzeApp(analysis *types.AppAnalysis) (*types.AppAnalysis, error)
	GeneratePersona(analysis *types.AppAnalysis) (string, error)
	Complete(ctx context.Context, prompt string) (string, error)
}

// NewClient creates a new LLM client based on the provider name.
// API key resolution: env var > global config (~/.config/dorgu/config.yaml).
func NewClient(provider string) (Client, error) {
	globalCfg, _ := config.LoadGlobalConfig()
	apiKey := resolveAPIKey(provider, globalCfg)

	switch provider {
	case "openai":
		if apiKey == "" {
			return nil, fmt.Errorf("OpenAI API key not set. Set OPENAI_API_KEY or run: dorgu config set llm.api_key <key>")
		}
		return NewOpenAIClient(apiKey), nil

	case "anthropic":
		if apiKey == "" {
			return nil, fmt.Errorf("Anthropic API key not set. Set ANTHROPIC_API_KEY or run: dorgu config set llm.api_key <key>")
		}
		return NewAnthropicClient(apiKey), nil

	case "gemini":
		if apiKey == "" {
			return nil, fmt.Errorf("Gemini API key not set. Set GEMINI_API_KEY (or GOOGLE_API_KEY) or run: dorgu config set llm.api_key <key>")
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

// resolveAPIKey returns API key: env var takes precedence over global config
func resolveAPIKey(provider string, globalCfg *config.GlobalConfig) string {
	switch provider {
	case "openai":
		if k := os.Getenv("OPENAI_API_KEY"); k != "" {
			return k
		}
	case "anthropic":
		if k := os.Getenv("ANTHROPIC_API_KEY"); k != "" {
			return k
		}
	case "gemini":
		if k := os.Getenv("GEMINI_API_KEY"); k != "" {
			return k
		}
		if k := os.Getenv("GOOGLE_API_KEY"); k != "" {
			return k
		}
	}
	if globalCfg != nil {
		return globalCfg.GetAPIKey(provider)
	}
	return ""
}
