package config

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

	// Deployment strategy
	DeploymentPolicy *AppDeploymentPolicy `yaml:"deployment_policy"`
}

// AppMetadata contains application metadata
type AppMetadata struct {
	Name         string `yaml:"name"`
	Description  string `yaml:"description"`
	Team         string `yaml:"team"`
	Owner        string `yaml:"owner"`
	Repository   string `yaml:"repository"`
	Type         string `yaml:"type"`         // api, web, worker, cron, daemon
	Tier         string `yaml:"tier"`         // critical, standard, best-effort
	Instructions string `yaml:"instructions"` // Custom instructions for AI analysis
}

// AppResources contains app-specific resource configuration
type AppResources struct {
	Requests ResourceValues `yaml:"requests"`
	Limits   ResourceValues `yaml:"limits"`
}

// AppScaling contains app-specific scaling configuration
type AppScaling struct {
	MinReplicas  int    `yaml:"min_replicas"`
	MaxReplicas  int    `yaml:"max_replicas"`
	TargetCPU    int    `yaml:"target_cpu"`
	TargetMemory int    `yaml:"target_memory"`
	Behavior     string `yaml:"behavior"` // conservative, balanced, aggressive
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
	Liveness           *HealthProbe `yaml:"liveness"`
	Readiness          *HealthProbe `yaml:"readiness"`
	StartupGracePeriod string       `yaml:"startup_grace_period"` // e.g., "30s", "60s"
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
	Name        string `yaml:"name"`
	Type        string `yaml:"type"` // database, cache, service, external
	Required    bool   `yaml:"required"`
	HealthCheck string `yaml:"health_check"` // e.g., "SELECT 1" for DB deps
}

// AppOperations contains operational information
type AppOperations struct {
	Runbook           string   `yaml:"runbook"`
	Alerts            []string `yaml:"alerts"`
	MaintenanceWindow string   `yaml:"maintenance_window"`
	OnCall            string   `yaml:"on_call"`
	AutoRestart       bool     `yaml:"auto_restart"`
}

// AppDeploymentPolicy contains deployment strategy configuration
type AppDeploymentPolicy struct {
	Strategy       string `yaml:"strategy"`        // RollingUpdate, Recreate, BlueGreen, Canary
	MaxSurge       string `yaml:"max_surge"`       // e.g., "25%"
	MaxUnavailable string `yaml:"max_unavailable"` // e.g., "25%"
}
