package types

// AppAnalysis represents the complete analysis of an application
type AppAnalysis struct {
	// Basic info
	Name        string `json:"name"`
	Type        string `json:"type"` // api, web, worker, cron
	Language    string `json:"language"`
	Framework   string `json:"framework"`
	Description string `json:"description"`

	// Deployment characteristics
	Ports       []Port       `json:"ports"`
	HealthCheck *HealthCheck `json:"health_check,omitempty"`
	EnvVars     []EnvVar     `json:"env_vars"`

	// Dependencies and resources
	Dependencies    []string       `json:"dependencies"`
	ResourceProfile string         `json:"resource_profile"` // api, worker, web
	Scaling         *ScalingConfig `json:"scaling,omitempty"`

	// Source analysis
	Dockerfile *DockerfileAnalysis `json:"dockerfile,omitempty"`
	Compose    *ComposeAnalysis    `json:"compose,omitempty"`
	Code       *CodeAnalysis       `json:"code,omitempty"`

	// App config from .dorgu.yaml (optional)
	AppConfig *AppConfigContext `json:"app_config,omitempty"`

	// Ownership information (from app config or placeholders)
	Team       string `json:"team,omitempty"`
	Owner      string `json:"owner,omitempty"`
	Repository string `json:"repository,omitempty"`

	// Environment
	Environment string `json:"environment,omitempty"`
}

// AppConfigContext contains relevant app config for analysis and generation
type AppConfigContext struct {
	// App metadata
	Name         string `json:"name,omitempty"`
	Description  string `json:"description,omitempty"`
	Team         string `json:"team,omitempty"`
	Owner        string `json:"owner,omitempty"`
	Repository   string `json:"repository,omitempty"`
	Type         string `json:"type,omitempty"`
	Instructions string `json:"instructions,omitempty"`

	// Environment
	Environment string `json:"environment,omitempty"`

	// Resource overrides
	Resources *ResourceOverrides `json:"resources,omitempty"`

	// Scaling overrides
	Scaling *ScalingConfig `json:"scaling,omitempty"`

	// Custom labels
	Labels map[string]string `json:"labels,omitempty"`

	// Custom annotations
	Annotations map[string]string `json:"annotations,omitempty"`

	// Ingress config
	Ingress *IngressContext `json:"ingress,omitempty"`

	// Health overrides
	Health *HealthContext `json:"health,omitempty"`

	// Dependencies from config
	Dependencies []DependencyContext `json:"dependencies,omitempty"`

	// Operations
	Operations *OperationsContext `json:"operations,omitempty"`
}

// ResourceOverrides contains resource configuration overrides
type ResourceOverrides struct {
	RequestsCPU    string `json:"requests_cpu,omitempty"`
	RequestsMemory string `json:"requests_memory,omitempty"`
	LimitsCPU      string `json:"limits_cpu,omitempty"`
	LimitsMemory   string `json:"limits_memory,omitempty"`
}

// IngressContext contains ingress configuration from app config
type IngressContext struct {
	Enabled    bool             `json:"enabled"`
	Host       string           `json:"host,omitempty"`
	Paths      []IngressPathDef `json:"paths,omitempty"`
	TLSEnabled bool             `json:"tls_enabled"`
	TLSSecret  string           `json:"tls_secret,omitempty"`
}

// IngressPathDef defines an ingress path
type IngressPathDef struct {
	Path     string `json:"path"`
	PathType string `json:"path_type"`
}

// HealthContext contains health check configuration from app config
type HealthContext struct {
	LivenessPath  string `json:"liveness_path,omitempty"`
	LivenessPort  int    `json:"liveness_port,omitempty"`
	ReadinessPath string `json:"readiness_path,omitempty"`
	ReadinessPort int    `json:"readiness_port,omitempty"`
	InitialDelay  int    `json:"initial_delay,omitempty"`
	Period        int    `json:"period,omitempty"`
}

// DependencyContext describes a dependency from app config
type DependencyContext struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Required bool   `json:"required"`
}

// OperationsContext contains operational information
type OperationsContext struct {
	Runbook           string   `json:"runbook,omitempty"`
	Alerts            []string `json:"alerts,omitempty"`
	MaintenanceWindow string   `json:"maintenance_window,omitempty"`
	OnCall            string   `json:"on_call,omitempty"`
}

// Port represents an exposed port
type Port struct {
	Port     int    `json:"port"`
	Protocol string `json:"protocol"` // TCP, UDP
	Purpose  string `json:"purpose"`  // HTTP API, gRPC, metrics, etc.
}

// HealthCheck represents health check configuration
type HealthCheck struct {
	Path             string `json:"path"`
	Port             int    `json:"port"`
	InitialDelay     int    `json:"initial_delay_seconds,omitempty"`
	Period           int    `json:"period_seconds,omitempty"`
	Timeout          int    `json:"timeout_seconds,omitempty"`
	SuccessThreshold int    `json:"success_threshold,omitempty"`
	FailureThreshold int    `json:"failure_threshold,omitempty"`
}

// EnvVar represents an environment variable
type EnvVar struct {
	Name        string `json:"name"`
	Value       string `json:"value,omitempty"`
	Required    bool   `json:"required"`
	Description string `json:"description,omitempty"`
	Secret      bool   `json:"secret,omitempty"`
}

// ScalingConfig represents HPA configuration
type ScalingConfig struct {
	MinReplicas  int `json:"min_replicas"`
	MaxReplicas  int `json:"max_replicas"`
	TargetCPU    int `json:"target_cpu_percent,omitempty"`
	TargetMemory int `json:"target_memory_percent,omitempty"`
}

// DockerfileAnalysis contains parsed Dockerfile information
type DockerfileAnalysis struct {
	BaseImage   string            `json:"base_image"`
	Ports       []int             `json:"ports"`
	EnvVars     []EnvVar          `json:"env_vars"`
	WorkDir     string            `json:"workdir"`
	Entrypoint  []string          `json:"entrypoint"`
	Cmd         []string          `json:"cmd"`
	User        string            `json:"user"`
	Labels      map[string]string `json:"labels"`
	BuildStages []string          `json:"build_stages"`
}

// ComposeAnalysis contains parsed docker-compose information
type ComposeAnalysis struct {
	Services []ComposeService `json:"services"`
}

// ComposeService represents a service in docker-compose
type ComposeService struct {
	Name        string        `json:"name"`
	Image       string        `json:"image"`
	Build       string        `json:"build"`
	Ports       []PortMapping `json:"ports"`
	Environment []EnvVar      `json:"environment"`
	Volumes     []string      `json:"volumes"`
	DependsOn   []string      `json:"depends_on"`
	HealthCheck *HealthCheck  `json:"healthcheck,omitempty"`
}

// PortMapping represents a port mapping in docker-compose
type PortMapping struct {
	Host      int    `json:"host"`
	Container int    `json:"container"`
	Protocol  string `json:"protocol"`
}

// CodeAnalysis contains source code analysis results
type CodeAnalysis struct {
	Language     string   `json:"language"`
	Framework    string   `json:"framework"`
	Dependencies []string `json:"dependencies"`
	HealthPath   string   `json:"health_path"`
	MetricsPath  string   `json:"metrics_path"`
	Routes       []string `json:"routes"`
}
