package config

import (
	"os"
	"path/filepath"

	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

// Load loads the configuration from the config file
func Load() (*Config, error) {
	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, err
	}

	// Apply defaults for missing values
	applyDefaults(&cfg)

	return &cfg, nil
}

// Default returns the default configuration
func Default() *Config {
	cfg := &Config{}
	applyDefaults(cfg)
	return cfg
}

// GetResourcesForProfile returns resource spec for a given profile
func (c *Config) GetResourcesForProfile(profile string) ResourceSpec {
	if spec, ok := c.Resources.Profiles[profile]; ok {
		return spec
	}
	return c.Resources.Defaults
}

// LoadAppConfig loads the application-specific .dorgu.yaml from the given path
func LoadAppConfig(appPath string) (*AppConfig, error) {
	configPath := filepath.Join(appPath, ".dorgu.yaml")

	// Check if config file exists
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return nil, nil // No app config is not an error
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}

	// Empty file
	if len(data) == 0 {
		return nil, nil
	}

	var cfg AppConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// HasAppConfig checks if app-level config exists
func HasAppConfig(appPath string) bool {
	configPath := filepath.Join(appPath, ".dorgu.yaml")
	info, err := os.Stat(configPath)
	if os.IsNotExist(err) {
		return false
	}
	return info.Size() > 0
}

// GetInstructionsContext returns a formatted string of app context for LLM
func (c *AppConfig) GetInstructionsContext() string {
	if c == nil {
		return ""
	}

	context := ""

	if c.App.Name != "" {
		context += "Application Name: " + c.App.Name + "\n"
	}
	if c.App.Description != "" {
		context += "Description: " + c.App.Description + "\n"
	}
	if c.App.Team != "" {
		context += "Team: " + c.App.Team + "\n"
	}
	if c.App.Type != "" {
		context += "Application Type: " + c.App.Type + "\n"
	}
	if c.Environment != "" {
		context += "Environment: " + c.Environment + "\n"
	}

	// Add dependencies context
	if len(c.Dependencies) > 0 {
		context += "\nKnown Dependencies:\n"
		for _, dep := range c.Dependencies {
			required := ""
			if dep.Required {
				required = " (required)"
			}
			context += "- " + dep.Name + " (" + dep.Type + ")" + required + "\n"
		}
	}

	// Add custom instructions
	if c.App.Instructions != "" {
		context += "\nApplication-Specific Context:\n" + c.App.Instructions + "\n"
	}

	return context
}
