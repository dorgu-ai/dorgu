package generator

import (
	"sigs.k8s.io/yaml"

	"github.com/dorgu-ai/dorgu/internal/config"
	"github.com/dorgu-ai/dorgu/internal/types"
)

// buildLabelsWithAppConfig creates labels merging org config and app config.
// Uses DNS-safe name for app.kubernetes.io/name so operator matching works.
func buildLabelsWithAppConfig(analysis *types.AppAnalysis, cfg *config.Config) map[string]string {
	name := ToDNSSubdomain(analysis.Name)
	labels := map[string]string{
		"app.kubernetes.io/name":       name,
		"app.kubernetes.io/managed-by": "dorgu",
	}

	// Add team label if available from app config
	if analysis.Team != "" {
		labels["app.kubernetes.io/team"] = analysis.Team
	}

	// Add environment label if available
	if analysis.Environment != "" {
		labels["app.kubernetes.io/environment"] = analysis.Environment
	}

	// Add custom labels from org config
	for k, v := range cfg.Labels.Custom {
		labels[k] = v
	}

	// Add custom labels from app config (these override org config)
	if analysis.AppConfig != nil {
		for k, v := range analysis.AppConfig.Labels {
			labels[k] = v
		}
	}

	return labels
}

// buildAnnotationsWithAppConfig creates annotations from org and app config
func buildAnnotationsWithAppConfig(analysis *types.AppAnalysis, cfg *config.Config) map[string]string {
	annotations := make(map[string]string)

	// Add custom annotations from org config
	for k, v := range cfg.Annotations.Custom {
		annotations[k] = v
	}

	// Add custom annotations from app config (these override org config)
	if analysis.AppConfig != nil {
		for k, v := range analysis.AppConfig.Annotations {
			annotations[k] = v
		}
	}

	// Return nil if no annotations to avoid empty map in YAML
	if len(annotations) == 0 {
		return nil
	}

	return annotations
}

// toYAML converts a struct to YAML string
func toYAML(obj interface{}) (string, error) {
	data, err := yaml.Marshal(obj)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
