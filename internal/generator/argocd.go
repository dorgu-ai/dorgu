package generator

import (
	"github.com/dorgu-ai/dorgu/internal/config"
	"github.com/dorgu-ai/dorgu/internal/types"
)

// ArgoCDApplication represents an ArgoCD Application
type ArgoCDApplication struct {
	APIVersion string        `json:"apiVersion"`
	Kind       string        `json:"kind"`
	Metadata   Metadata      `json:"metadata"`
	Spec       ArgoCDAppSpec `json:"spec"`
}

// ArgoCDAppSpec represents the ArgoCD Application spec
type ArgoCDAppSpec struct {
	Project     string            `json:"project"`
	Source      ArgoCDSource      `json:"source"`
	Destination ArgoCDDest        `json:"destination"`
	SyncPolicy  *ArgoCDSyncPolicy `json:"syncPolicy,omitempty"`
}

// ArgoCDSource represents the source configuration
type ArgoCDSource struct {
	RepoURL        string `json:"repoURL"`
	Path           string `json:"path"`
	TargetRevision string `json:"targetRevision"`
}

// ArgoCDDest represents the destination configuration
type ArgoCDDest struct {
	Server    string `json:"server"`
	Namespace string `json:"namespace"`
}

// ArgoCDSyncPolicy represents sync policy configuration
type ArgoCDSyncPolicy struct {
	Automated   *ArgoCDAutomated `json:"automated,omitempty"`
	SyncOptions []string         `json:"syncOptions,omitempty"`
}

// ArgoCDAutomated represents automated sync settings
type ArgoCDAutomated struct {
	Prune    bool `json:"prune"`
	SelfHeal bool `json:"selfHeal"`
}

// GenerateArgoCD generates an ArgoCD Application manifest
func GenerateArgoCD(analysis *types.AppAnalysis, namespace string, cfg *config.Config) (string, error) {
	labels := buildLabelsWithAppConfig(analysis, cfg)

	// Get repository URL from app config, or generate default
	repoURL := "https://github.com/YOUR_ORG/" + analysis.Name + ".git"
	if analysis.Repository != "" {
		repoURL = analysis.Repository
	} else if analysis.AppConfig != nil && analysis.AppConfig.Repository != "" {
		repoURL = analysis.AppConfig.Repository
	}

	app := ArgoCDApplication{
		APIVersion: "argoproj.io/v1alpha1",
		Kind:       "Application",
		Metadata: Metadata{
			Name:      analysis.Name,
			Namespace: "argocd", // ArgoCD apps typically live in argocd namespace
			Labels:    labels,
		},
		Spec: ArgoCDAppSpec{
			Project: cfg.ArgoCD.Project,
			Source: ArgoCDSource{
				RepoURL:        repoURL,
				Path:           "k8s",
				TargetRevision: "HEAD",
			},
			Destination: ArgoCDDest{
				Server:    cfg.ArgoCD.Destination.Server,
				Namespace: namespace,
			},
			SyncPolicy: &ArgoCDSyncPolicy{
				Automated: &ArgoCDAutomated{
					Prune:    cfg.ArgoCD.SyncPolicy.Automated.Prune,
					SelfHeal: cfg.ArgoCD.SyncPolicy.Automated.SelfHeal,
				},
				SyncOptions: []string{
					"CreateNamespace=true",
				},
			},
		},
	}

	return toYAML(app)
}
