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
	ComponentCNPG            ComponentID = "cnpg"
	ComponentOpenObserve     ComponentID = "openobserve"
	ComponentArgoCd          ComponentID = "argocd"
	ComponentExternalSecrets ComponentID = "external-secrets"
)

// ComponentAccess describes how to access a component after installation.
// A nil ComponentAccess means the component has no user-facing UI.
type ComponentAccess struct {
	// WebUIPort is the container port serving the web interface (0 = no web UI).
	WebUIPort int
	// ServiceName is the Kubernetes service name to port-forward to
	// (defaults to HelmReleaseName if empty).
	ServiceName string
	// DefaultCredentials describes how to retrieve initial login credentials.
	DefaultCredentials *CredentialInfo
}

// CredentialInfo describes how to retrieve credentials for a component.
type CredentialInfo struct {
	// SecretName is the Kubernetes secret containing the credentials.
	SecretName string
	// UsernameKey is the data key for username (empty = fixed username in UsernameValue).
	UsernameKey string
	// UsernameValue is a fixed username (used when username is not in a secret).
	UsernameValue string
	// PasswordKey is the data key for the password.
	PasswordKey string
	// Namespace overrides the component namespace for the secret (empty = use component namespace).
	Namespace string
	// Notes is additional guidance (e.g., "Change password after first login").
	Notes string
}

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
	Timeout            string // Helm --timeout value; empty uses default "5m0s"

	// EnvironmentOverrides provides additional --set values per environment.
	// Key is environment name (e.g. "development"), value is a slice of "key=value" strings.
	EnvironmentOverrides map[string][]string

	// Access describes how a user reaches the component after install
	// (web UI port, port-forward target service, credential lookup).
	// nil = no user-facing access (e.g., cert-manager, CNPG).
	Access *ComponentAccess
}

// SetupConfig holds the resolved configuration for a single setup run.
type SetupConfig struct {
	ClusterPersonaName string
	Environment        string
	Components         []ComponentConfig // only components the user selected (or Required)
	Timestamp          time.Time
	VersionOverrides   map[ComponentID]string // e.g. {"cert-manager": "v1.17.0"}; nil = use defaults
	SkipValidation     bool
	LockedContext      string // kube-context captured at setup start; empty = no drift check
}

// AnnotationStack returns a comma-separated list of component IDs for the
// dorgu.io/setup-stack annotation on ClusterPersona.
// Deprecated: use AnnotationStackFromResults for accurate post-install annotations.
func (c SetupConfig) AnnotationStack() string {
	ids := make([]string, len(c.Components))
	for i, comp := range c.Components {
		ids[i] = string(comp.ID)
	}
	return strings.Join(ids, ",")
}

// AnnotationStackFromResults returns comma-separated component IDs for only
// successfully installed components. Skipped and failed components are excluded.
func AnnotationStackFromResults(results []InstallResult) string {
	var ids []string
	for _, r := range results {
		if r.Succeeded {
			ids = append(ids, string(r.Component.ID))
		}
	}
	return strings.Join(ids, ",")
}

// AnnotationSkippedFromResults returns comma-separated component IDs for
// components that were skipped or failed during installation.
func AnnotationSkippedFromResults(results []InstallResult) string {
	var ids []string
	for _, r := range results {
		if !r.Succeeded {
			ids = append(ids, string(r.Component.ID))
		}
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
			Timeout:           "10m0s",
		},
		{
			ID:                ComponentCNPG,
			DisplayName:       "CloudNativePG",
			Description:       "Kubernetes operator for PostgreSQL lifecycle management",
			WhyItMatters:      "CloudNativePG manages PostgreSQL clusters as native Kubernetes resources. It handles replication, failover, backup, and recovery automatically. OpenObserve uses PostgreSQL for metadata storage, so CNPG must be installed before OpenObserve.",
			HelmRepo:          "https://cloudnative-pg.github.io/charts",
			HelmRepoName:      "cnpg",
			HelmChart:         "cnpg/cloudnative-pg",
			HelmReleaseName:   "cnpg",
			Namespace:         "cnpg-system",
			Version:           "0.23.0",
			HelmSetValues:     []string{},
			CreateNamespace:   true,
			Required:          true,
			DefaultEnabled:    true,
			OperatorAddonName: "cnpg",
			Timeout:           "5m0s",
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
			DependsOn:         []ComponentID{ComponentCNPG},
			Timeout:           "15m0s",
			OperatorAddonName: "openobserve",
			EnvironmentOverrides: map[string][]string{
				"development": {
					"config.ZO_LOCAL_MODE=true",
					"config.ZO_LOCAL_MODE_STORAGE=disk",
				},
				"sandbox": {
					"config.ZO_LOCAL_MODE=true",
					"config.ZO_LOCAL_MODE_STORAGE=disk",
				},
			},
			Access: &ComponentAccess{
				WebUIPort:   5080,
				ServiceName: "openobserve",
				DefaultCredentials: &CredentialInfo{
					SecretName:    "openobserve",
					UsernameValue: "root@example.com",
					PasswordKey:   "ZO_ROOT_USER_PASSWORD",
					Notes:         "Default email is root@example.com",
				},
			},
		},
		{
			ID:          ComponentArgoCd,
			DisplayName: "Argo CD",
			Description: "Declarative GitOps continuous delivery for Kubernetes",
			WhyItMatters: `Argo CD watches your Git repository and automatically syncs your Kubernetes
manifests to the cluster. When you run 'dorgu generate', it produces Kubernetes manifests and
ArgoCD Application resources. With Argo CD installed, those Application resources are
automatically picked up — creating a complete GitOps pipeline from source code to running
workloads.

Without Argo CD, you would need to manually 'kubectl apply' every change. With it, pushing
to Git is all it takes to deploy.`,
			HelmRepo:          "https://argoproj.github.io/argo-helm",
			HelmRepoName:      "argo",
			HelmChart:         "argo/argo-cd",
			HelmReleaseName:   "argocd",
			Namespace:         "argocd",
			Version:           "7.8.28",
			HelmSetValues:     []string{},
			CreateNamespace:   true,
			Required:          true,
			DefaultEnabled:    true,
			DependsOn:         []ComponentID{},
			OperatorAddonName: "argocd",
			Access: &ComponentAccess{
				WebUIPort:   443,
				ServiceName: "argocd-server",
				DefaultCredentials: &CredentialInfo{
					SecretName:    "argocd-initial-admin-secret",
					UsernameValue: "admin",
					PasswordKey:   "password",
					Notes:         "Change password after first login: argocd account update-password",
				},
			},
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
