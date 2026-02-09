package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// GlobalConfig represents the user's global dorgu configuration.
// Stored at ~/.config/dorgu/config.yaml
type GlobalConfig struct {
	Version string `yaml:"version"`

	// LLM settings
	LLM GlobalLLMConfig `yaml:"llm"`

	// Default values for generation
	Defaults GlobalDefaults `yaml:"defaults"`
}

// GlobalLLMConfig contains LLM provider settings
type GlobalLLMConfig struct {
	Provider string `yaml:"provider"` // openai, anthropic, gemini, ollama
	APIKey   string `yaml:"api_key"` // stored here; env var takes precedence
	Model    string `yaml:"model"`   // optional model override
}

// GlobalDefaults contains default generation settings
type GlobalDefaults struct {
	Namespace string `yaml:"namespace"` // default k8s namespace
	Registry  string `yaml:"registry"`  // default container registry
	OrgName   string `yaml:"org_name"`   // organization name
}

// GlobalConfigDir returns the path to the dorgu config directory
func GlobalConfigDir() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "dorgu")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", ".dorgu")
	}
	return filepath.Join(home, ".config", "dorgu")
}

// GlobalConfigPath returns the full path to the global config file
func GlobalConfigPath() string {
	return filepath.Join(GlobalConfigDir(), "config.yaml")
}

// LoadGlobalConfig loads the global config from disk
func LoadGlobalConfig() (*GlobalConfig, error) {
	path := GlobalConfigPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultGlobalConfig(), nil
		}
		return nil, fmt.Errorf("failed to read global config: %w", err)
	}
	if len(data) == 0 {
		return DefaultGlobalConfig(), nil
	}
	var cfg GlobalConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse global config: %w", err)
	}
	return &cfg, nil
}

// SaveGlobalConfig writes the global config to disk
func SaveGlobalConfig(cfg *GlobalConfig) error {
	dir := GlobalConfigDir()
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("failed to serialize config: %w", err)
	}
	header := "# Dorgu Global Configuration\n# Location: " + GlobalConfigPath() + "\n# Edit with: dorgu config set <key> <value>\n\n"
	if err := os.WriteFile(GlobalConfigPath(), []byte(header+string(data)), 0600); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}
	return nil
}

// DefaultGlobalConfig returns a default global config
func DefaultGlobalConfig() *GlobalConfig {
	return &GlobalConfig{
		Version: "1",
		LLM: GlobalLLMConfig{
			Provider: "",
			APIKey:   "",
			Model:    "",
		},
		Defaults: GlobalDefaults{
			Namespace: "default",
			Registry:  "",
			OrgName:   "",
		},
	}
}

// Set sets a config value by dot-separated key path
func (c *GlobalConfig) Set(key, value string) error {
	switch key {
	case "llm.provider":
		valid := map[string]bool{"openai": true, "anthropic": true, "gemini": true, "ollama": true, "": true}
		if !valid[value] {
			return fmt.Errorf("invalid LLM provider: %s (valid: openai, anthropic, gemini, ollama)", value)
		}
		c.LLM.Provider = value
	case "llm.api_key":
		c.LLM.APIKey = value
	case "llm.model":
		c.LLM.Model = value
	case "defaults.namespace":
		c.Defaults.Namespace = value
	case "defaults.registry":
		c.Defaults.Registry = value
	case "defaults.org_name":
		c.Defaults.OrgName = value
	default:
		return fmt.Errorf("unknown config key: %s\n\nValid keys:\n  llm.provider\n  llm.api_key\n  llm.model\n  defaults.namespace\n  defaults.registry\n  defaults.org_name", key)
	}
	return nil
}

// Get returns a config value by dot-separated key path
func (c *GlobalConfig) Get(key string) (string, error) {
	switch key {
	case "llm.provider":
		return c.LLM.Provider, nil
	case "llm.api_key":
		if c.LLM.APIKey != "" {
			return maskKey(c.LLM.APIKey), nil
		}
		return "", nil
	case "llm.model":
		return c.LLM.Model, nil
	case "defaults.namespace":
		return c.Defaults.Namespace, nil
	case "defaults.registry":
		return c.Defaults.Registry, nil
	case "defaults.org_name":
		return c.Defaults.OrgName, nil
	default:
		return "", fmt.Errorf("unknown config key: %s", key)
	}
}

// GetAPIKey returns the effective API key for the configured provider.
// Priority: env var > global config
func (c *GlobalConfig) GetAPIKey(provider string) string {
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
	return c.LLM.APIKey
}

// GetEffectiveProvider returns the LLM provider to use (flag > global > empty)
func (c *GlobalConfig) GetEffectiveProvider(flagValue string) string {
	if flagValue != "" {
		return flagValue
	}
	return c.LLM.Provider
}

// ConfigEntry represents a single config key-value with its source
type ConfigEntry struct {
	Key    string
	Value  string
	Source string
}

// ListAll returns all config values for display
func (c *GlobalConfig) ListAll() []ConfigEntry {
	entries := []ConfigEntry{
		{Key: "llm.provider", Value: c.LLM.Provider, Source: "global"},
		{Key: "llm.api_key", Value: maskKey(c.LLM.APIKey), Source: "global"},
		{Key: "llm.model", Value: c.LLM.Model, Source: "global"},
		{Key: "defaults.namespace", Value: c.Defaults.Namespace, Source: "global"},
		{Key: "defaults.registry", Value: c.Defaults.Registry, Source: "global"},
		{Key: "defaults.org_name", Value: c.Defaults.OrgName, Source: "global"},
	}
	for i := range entries {
		if entries[i].Key == "llm.api_key" {
			envKey := envKeyForProvider(c.LLM.Provider)
			if envKey != "" && os.Getenv(envKey) != "" {
				entries[i].Value = maskKey(os.Getenv(envKey))
				entries[i].Source = "env:" + envKey
			}
		}
	}
	return entries
}

func maskKey(key string) string {
	if key == "" {
		return "(not set)"
	}
	if len(key) <= 8 {
		return strings.Repeat("*", len(key))
	}
	return key[:4] + strings.Repeat("*", len(key)-8) + key[len(key)-4:]
}

func envKeyForProvider(provider string) string {
	switch provider {
	case "openai":
		return "OPENAI_API_KEY"
	case "anthropic":
		return "ANTHROPIC_API_KEY"
	case "gemini":
		return "GEMINI_API_KEY"
	default:
		return ""
	}
}
