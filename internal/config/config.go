package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
	if err != nil {
		return false
	}
	return info.Size() > 0
}

// GetInstructionsContext returns a formatted string of app context for LLM
func (c *AppConfig) GetInstructionsContext() string {
	if c == nil {
		return ""
	}

	var b strings.Builder

	if c.App.Name != "" {
		fmt.Fprintf(&b, "Application Name: %s\n", c.App.Name)
	}
	if c.App.Description != "" {
		fmt.Fprintf(&b, "Description: %s\n", c.App.Description)
	}
	if c.App.Team != "" {
		fmt.Fprintf(&b, "Team: %s\n", c.App.Team)
	}
	if c.App.Type != "" {
		fmt.Fprintf(&b, "Application Type: %s\n", c.App.Type)
	}
	if c.Environment != "" {
		fmt.Fprintf(&b, "Environment: %s\n", c.Environment)
	}

	if len(c.Dependencies) > 0 {
		b.WriteString("\nKnown Dependencies:\n")
		for _, dep := range c.Dependencies {
			required := ""
			if dep.Required {
				required = " (required)"
			}
			fmt.Fprintf(&b, "- %s (%s)%s\n", dep.Name, dep.Type, required)
		}
	}

	if c.App.Instructions != "" {
		fmt.Fprintf(&b, "\nApplication-Specific Context:\n%s\n", c.App.Instructions)
	}

	return b.String()
}
