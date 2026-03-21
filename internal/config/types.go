package config

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
