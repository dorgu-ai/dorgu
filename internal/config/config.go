package config

import (
	"os"
	"path/filepath"

	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

// Config represents the dorgu configuration
type Config struct {
	Version string `mapstructure:"version"`

	// Organization info
	Org OrgConfig `mapstructure:"org"`

	// Naming conventions
	Naming NamingConfig `mapstructure:"naming"`

	// Resource defaults
	Resources ResourceConfig `mapstructure:"resources"`

	// Labels
	Labels LabelConfig `mapstructure:"labels"`

	// Annotations
	Annotations AnnotationConfig `mapstructure:"annotations"`

	// Security policies
	Security SecurityConfig `mapstructure:"security"`

	// Ingress configuration
	Ingress IngressConfig `mapstructure:"ingress"`

	// ArgoCD configuration
	ArgoCD ArgoCDConfig `mapstructure:"argocd"`

	// CI/CD configuration
	CI CIConfig `mapstructure:"ci"`

	// LLM configuration
	LLM LLMConfig `mapstructure:"llm"`
}

// OrgConfig contains organization information
type OrgConfig struct {
	Name string `mapstructure:"name"`
}

// NamingConfig contains naming conventions
type NamingConfig struct {
	Pattern string `mapstructure:"pattern"`
	DNSSafe bool   `mapstructure:"dns_safe"`
}

// ResourceConfig contains resource defaults
type ResourceConfig struct {
	Defaults ResourceSpec            `mapstructure:"defaults"`
	Profiles map[string]ResourceSpec `mapstructure:"profiles"`
}

// ResourceSpec contains resource requests and limits
type ResourceSpec struct {
	Requests ResourceValues `mapstructure:"requests"`
	Limits   ResourceValues `mapstructure:"limits"`
}

// ResourceValues contains CPU and memory values
type ResourceValues struct {
	CPU    string `mapstructure:"cpu"`
	Memory string `mapstructure:"memory"`
}

// LabelConfig contains label configuration
type LabelConfig struct {
	Required []string          `mapstructure:"required"`
	Custom   map[string]string `mapstructure:"custom"`
}

// AnnotationConfig contains annotation configuration
type AnnotationConfig struct {
	Custom map[string]string `mapstructure:"custom"`
}

// SecurityConfig contains security policies
type SecurityConfig struct {
	PodSecurityContext       PodSecurityContext       `mapstructure:"pod_security_context"`
	ContainerSecurityContext ContainerSecurityContext `mapstructure:"container_security_context"`
}

// PodSecurityContext contains pod-level security settings
type PodSecurityContext struct {
	RunAsNonRoot   bool            `mapstructure:"run_as_non_root"`
	SeccompProfile *SeccompProfile `mapstructure:"seccomp_profile"`
}

// SeccompProfile contains seccomp profile settings
type SeccompProfile struct {
	Type string `mapstructure:"type"`
}

// ContainerSecurityContext contains container-level security settings
type ContainerSecurityContext struct {
	AllowPrivilegeEscalation bool         `mapstructure:"allow_privilege_escalation"`
	ReadOnlyRootFilesystem   bool         `mapstructure:"read_only_root_filesystem"`
	Capabilities             Capabilities `mapstructure:"capabilities"`
}

// Capabilities contains Linux capabilities
type Capabilities struct {
	Drop []string `mapstructure:"drop"`
	Add  []string `mapstructure:"add"`
}

// IngressConfig contains ingress settings
type IngressConfig struct {
	Class        string    `mapstructure:"class"`
	DomainSuffix string    `mapstructure:"domain_suffix"`
	TLS          TLSConfig `mapstructure:"tls"`
}

// TLSConfig contains TLS settings
type TLSConfig struct {
	Enabled       bool   `mapstructure:"enabled"`
	ClusterIssuer string `mapstructure:"cluster_issuer"`
}

// ArgoCDConfig contains ArgoCD settings
type ArgoCDConfig struct {
	Project     string            `mapstructure:"project"`
	Destination DestinationConfig `mapstructure:"destination"`
	SyncPolicy  SyncPolicyConfig  `mapstructure:"sync_policy"`
}

// DestinationConfig contains ArgoCD destination settings
type DestinationConfig struct {
	Server    string `mapstructure:"server"`
	Namespace string `mapstructure:"namespace"`
}

// SyncPolicyConfig contains ArgoCD sync policy settings
type SyncPolicyConfig struct {
	Automated AutomatedConfig `mapstructure:"automated"`
}

// AutomatedConfig contains ArgoCD automated sync settings
type AutomatedConfig struct {
	Prune    bool `mapstructure:"prune"`
	SelfHeal bool `mapstructure:"self_heal"`
}

// CIConfig contains CI/CD settings
type CIConfig struct {
	Provider string `mapstructure:"provider"`
	Registry string `mapstructure:"registry"`
}

// LLMConfig contains LLM settings
type LLMConfig struct {
	Provider string `mapstructure:"provider"`
	Model    string `mapstructure:"model"`
}

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

// applyDefaults fills in default values for missing config
func applyDefaults(cfg *Config) {
	if cfg.Version == "" {
		cfg.Version = "1"
	}

	if cfg.Naming.Pattern == "" {
		cfg.Naming.Pattern = "{app}"
	}
	cfg.Naming.DNSSafe = true

	if cfg.Resources.Defaults.Requests.CPU == "" {
		cfg.Resources.Defaults.Requests.CPU = "100m"
	}
	if cfg.Resources.Defaults.Requests.Memory == "" {
		cfg.Resources.Defaults.Requests.Memory = "128Mi"
	}
	if cfg.Resources.Defaults.Limits.CPU == "" {
		cfg.Resources.Defaults.Limits.CPU = "500m"
	}
	if cfg.Resources.Defaults.Limits.Memory == "" {
		cfg.Resources.Defaults.Limits.Memory = "512Mi"
	}

	// Default resource profiles
	if cfg.Resources.Profiles == nil {
		cfg.Resources.Profiles = map[string]ResourceSpec{
			"api": {
				Requests: ResourceValues{CPU: "100m", Memory: "256Mi"},
				Limits:   ResourceValues{CPU: "1000m", Memory: "1Gi"},
			},
			"worker": {
				Requests: ResourceValues{CPU: "500m", Memory: "512Mi"},
				Limits:   ResourceValues{CPU: "2000m", Memory: "2Gi"},
			},
			"web": {
				Requests: ResourceValues{CPU: "50m", Memory: "128Mi"},
				Limits:   ResourceValues{CPU: "500m", Memory: "512Mi"},
			},
		}
	}

	if cfg.Ingress.Class == "" {
		cfg.Ingress.Class = "nginx"
	}
	if cfg.Ingress.DomainSuffix == "" {
		cfg.Ingress.DomainSuffix = ".local"
	}

	if cfg.ArgoCD.Project == "" {
		cfg.ArgoCD.Project = "default"
	}
	if cfg.ArgoCD.Destination.Server == "" {
		cfg.ArgoCD.Destination.Server = "https://kubernetes.default.svc"
	}

	if cfg.CI.Provider == "" {
		cfg.CI.Provider = "github-actions"
	}

	if cfg.LLM.Provider == "" {
		cfg.LLM.Provider = "openai"
	}
	if cfg.LLM.Model == "" {
		cfg.LLM.Model = "gpt-4"
	}
}

// GetResourcesForProfile returns resource spec for a given profile
func (c *Config) GetResourcesForProfile(profile string) ResourceSpec {
	if spec, ok := c.Resources.Profiles[profile]; ok {
		return spec
	}
	return c.Resources.Defaults
}

// AppConfig represents application-specific configuration from .dorgu.yaml in app directory
type AppConfig struct {
	Version string `yaml:"version"`

	// Application metadata
	App AppMetadata `yaml:"app"`

	// Environment (production, staging, development)
	Environment string `yaml:"environment"`

	// Resource overrides for this specific app
	Resources *AppResources `yaml:"resources"`

	// Scaling configuration
	Scaling *AppScaling `yaml:"scaling"`

	// Custom labels for this app
	Labels map[string]string `yaml:"labels"`

	// Custom annotations for this app
	Annotations map[string]string `yaml:"annotations"`

	// Ingress configuration for this app
	Ingress *AppIngress `yaml:"ingress"`

	// Health check configuration
	Health *AppHealth `yaml:"health"`

	// Dependencies for documentation
	Dependencies []AppDependency `yaml:"dependencies"`

	// Operational notes
	Operations *AppOperations `yaml:"operations"`
}

// AppMetadata contains application metadata
type AppMetadata struct {
	Name         string `yaml:"name"`
	Description  string `yaml:"description"`
	Team         string `yaml:"team"`
	Owner        string `yaml:"owner"`
	Repository   string `yaml:"repository"`
	Type         string `yaml:"type"`         // api, web, worker, cron, daemon
	Instructions string `yaml:"instructions"` // Custom instructions for AI analysis
}

// AppResources contains app-specific resource configuration
type AppResources struct {
	Requests ResourceValues `yaml:"requests"`
	Limits   ResourceValues `yaml:"limits"`
}

// AppScaling contains app-specific scaling configuration
type AppScaling struct {
	MinReplicas  int `yaml:"min_replicas"`
	MaxReplicas  int `yaml:"max_replicas"`
	TargetCPU    int `yaml:"target_cpu"`
	TargetMemory int `yaml:"target_memory"`
}

// AppIngress contains app-specific ingress configuration
type AppIngress struct {
	Enabled bool          `yaml:"enabled"`
	Host    string        `yaml:"host"`
	Paths   []IngressPath `yaml:"paths"`
	TLS     *AppTLS       `yaml:"tls"`
}

// IngressPath defines an ingress path
type IngressPath struct {
	Path     string `yaml:"path"`
	PathType string `yaml:"path_type"`
}

// AppTLS contains TLS configuration for ingress
type AppTLS struct {
	Enabled    bool   `yaml:"enabled"`
	SecretName string `yaml:"secret_name"`
}

// AppHealth contains health check configuration
type AppHealth struct {
	Liveness  *HealthProbe `yaml:"liveness"`
	Readiness *HealthProbe `yaml:"readiness"`
}

// HealthProbe defines a health check probe
type HealthProbe struct {
	Path         string `yaml:"path"`
	Port         int    `yaml:"port"`
	InitialDelay int    `yaml:"initial_delay"`
	Period       int    `yaml:"period"`
}

// AppDependency describes an application dependency
type AppDependency struct {
	Name     string `yaml:"name"`
	Type     string `yaml:"type"` // database, cache, service, external
	Required bool   `yaml:"required"`
}

// AppOperations contains operational information
type AppOperations struct {
	Runbook           string   `yaml:"runbook"`
	Alerts            []string `yaml:"alerts"`
	MaintenanceWindow string   `yaml:"maintenance_window"`
	OnCall            string   `yaml:"on_call"`
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
