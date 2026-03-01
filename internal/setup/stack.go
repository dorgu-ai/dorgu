package setup

import (
	"strings"
	"time"
)

// ComponentID is a stable identifier for a stack component.
type ComponentID string

const (
	ComponentCertManager     ComponentID = "cert-manager"
	ComponentIngressNginx    ComponentID = "ingress-nginx"
	ComponentOpenObserve     ComponentID = "openobserve"
	ComponentExternalSecrets ComponentID = "external-secrets"
)

// ComponentConfig describes a single Helm-installable stack component.
type ComponentConfig struct {
	ID                 ComponentID
	DisplayName        string
	Description        string // one-line role
	WhyItMatters       string // multi-line educational paragraph shown before prompt
	HelmRepo           string // URL of the Helm repo
	HelmRepoName       string // name used for 'helm repo add <name> <url>'
	HelmChart          string // "<repoName>/<chartName>"
	HelmReleaseName    string // helm release name (may differ from chart name)
	Namespace          string
	Version            string   // Blessed Stack pinned version
	HelmSetValues      []string // passed as --set k=v --set k2=v2 (ordered, not a map)
	HelmValuesFile     string   // optional --values file path (empty = not used)
	CreateNamespace    bool
	Required           bool // if true, shown as "[Required — will be installed]", no prompt
	DefaultEnabled     bool // for optional components: default when user presses Enter
	DependsOn          []ComponentID
	PostInstallMessage string
	OperatorAddonName  string // matches checkAddon() pod name search in operator
}

// SetupConfig holds the resolved configuration for a single setup run.
type SetupConfig struct {
	ClusterPersonaName string
	Environment        string
	Components         []ComponentConfig // only components the user selected (or Required)
	Timestamp          time.Time
	VersionOverrides   map[ComponentID]string // e.g. {"cert-manager": "v1.17.0"}; nil = use defaults
	SkipValidation     bool
}

// AnnotationStack returns a comma-separated list of component IDs for the
// dorgu.io/setup-stack annotation on ClusterPersona.
func (c SetupConfig) AnnotationStack() string {
	ids := make([]string, len(c.Components))
	for i, comp := range c.Components {
		ids[i] = string(comp.ID)
	}
	return strings.Join(ids, ",")
}

// InstallResult is the outcome of installing a single component.
type InstallResult struct {
	Component  ComponentConfig
	Succeeded  bool
	Skipped    bool // true when user deselected (optional components only)
	Error      error
	Duration   time.Duration
	HelmOutput string
}

// StackProvider is the extension point for future user-defined stacks.
// The BlessedStackProvider is the default implementation.
// Mirror of internal/llm.Client pattern.
type StackProvider interface {
	Name() string
	Description() string
	Components() []ComponentConfig
}

// BlessedStackProvider is the default, curated production-ready stack.
type BlessedStackProvider struct{}

func (b *BlessedStackProvider) Name() string { return "dorgu-blessed-v1" }
func (b *BlessedStackProvider) Description() string {
	return "Curated production-ready Kubernetes stack"
}
func (b *BlessedStackProvider) Components() []ComponentConfig { return blessedComponents() }

// DefaultStack returns the active StackProvider.
// MVP: always returns BlessedStackProvider.
// Future: reads setup.stack from ~/.config/dorgu/config.yaml or .dorgu.yaml.
func DefaultStack() StackProvider { return &BlessedStackProvider{} }

// blessedComponents returns the ordered slice of Blessed Stack components.
// Order matters: cert-manager must install before ingress-nginx.
func blessedComponents() []ComponentConfig {
	return []ComponentConfig{
		{
			ID:                ComponentCertManager,
			DisplayName:       "cert-manager",
			Description:       "Automated TLS certificate management",
			WhyItMatters:      "cert-manager automates TLS certificate lifecycle in your cluster. Without it, you manually create, renew, and rotate certificates — a common source of production outages. Every production cluster needs this.",
			HelmRepo:          "https://charts.jetstack.io",
			HelmRepoName:      "jetstack",
			HelmChart:         "jetstack/cert-manager",
			HelmReleaseName:   "cert-manager",
			Namespace:         "cert-manager",
			Version:           "v1.16.3",
			HelmSetValues:     []string{"installCRDs=true"},
			CreateNamespace:   true,
			Required:          true,
			DefaultEnabled:    true,
			OperatorAddonName: "cert-manager",
		},
		{
			ID:                ComponentIngressNginx,
			DisplayName:       "ingress-nginx",
			Description:       "HTTP/S ingress controller with TLS integration",
			WhyItMatters:      "ingress-nginx routes external HTTP/S traffic to your services and integrates with cert-manager for automatic TLS. Your applications need a stable ingress point before they can receive real traffic.",
			HelmRepo:          "https://kubernetes.github.io/ingress-nginx",
			HelmRepoName:      "ingress-nginx",
			HelmChart:         "ingress-nginx/ingress-nginx",
			HelmReleaseName:   "ingress-nginx",
			Namespace:         "ingress-nginx",
			Version:           "4.11.3",
			HelmSetValues:     []string{},
			CreateNamespace:   true,
			Required:          true,
			DefaultEnabled:    true,
			DependsOn:         []ComponentID{ComponentCertManager},
			OperatorAddonName: "ingress-nginx",
		},
		{
			ID:                ComponentOpenObserve,
			DisplayName:       "OpenObserve",
			Description:       "Unified observability: logs, metrics, traces, dashboards",
			WhyItMatters:      "OpenObserve provides logs, metrics, traces, and dashboards in a single Helm chart. Compared to the LGTM stack (Loki + Grafana + Tempo + Mimir), it requires significantly less memory and has a simpler operational model.\n\nYou cannot improve what you cannot measure.",
			HelmRepo:          "https://charts.openobserve.ai",
			HelmRepoName:      "openobserve",
			HelmChart:         "openobserve/openobserve",
			HelmReleaseName:   "openobserve",
			Namespace:         "openobserve",
			Version:           "0.60.0",
			HelmSetValues:     []string{},
			CreateNamespace:   true,
			Required:          true,
			DefaultEnabled:    true,
			OperatorAddonName: "openobserve",
		},
		{
			ID:                ComponentExternalSecrets,
			DisplayName:       "External Secrets Operator",
			Description:       "Sync secrets from AWS SM, GCP SM, Vault, Azure KV",
			WhyItMatters:      "External Secrets Operator syncs secrets from AWS Secrets Manager, GCP Secret Manager, HashiCorp Vault, or Azure Key Vault into Kubernetes Secrets. Skip if you manage secrets differently or are just getting started.",
			HelmRepo:          "https://charts.external-secrets.io",
			HelmRepoName:      "external-secrets",
			HelmChart:         "external-secrets/external-secrets",
			HelmReleaseName:   "external-secrets",
			Namespace:         "external-secrets",
			Version:           "0.10.7",
			HelmSetValues:     []string{},
			CreateNamespace:   true,
			Required:          false,
			DefaultEnabled:    false,
			OperatorAddonName: "external-secrets",
		},
	}
}
