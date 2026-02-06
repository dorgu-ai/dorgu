package generator

import (
	"github.com/dorgu-ai/dorgu/internal/config"
	"github.com/dorgu-ai/dorgu/internal/types"
)

// HPAManifest represents a Kubernetes HorizontalPodAutoscaler
type HPAManifest struct {
	APIVersion string   `json:"apiVersion"`
	Kind       string   `json:"kind"`
	Metadata   Metadata `json:"metadata"`
	Spec       HPASpec  `json:"spec"`
}

// HPASpec represents an HPA spec
type HPASpec struct {
	ScaleTargetRef ScaleTargetRef `json:"scaleTargetRef"`
	MinReplicas    int            `json:"minReplicas"`
	MaxReplicas    int            `json:"maxReplicas"`
	Metrics        []MetricSpec   `json:"metrics"`
}

// ScaleTargetRef represents the target to scale
type ScaleTargetRef struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
}

// MetricSpec represents a metric for scaling
type MetricSpec struct {
	Type     string          `json:"type"`
	Resource *ResourceMetric `json:"resource,omitempty"`
}

// ResourceMetric represents a resource-based metric
type ResourceMetric struct {
	Name   string       `json:"name"`
	Target MetricTarget `json:"target"`
}

// MetricTarget represents the target value
type MetricTarget struct {
	Type               string `json:"type"`
	AverageUtilization int    `json:"averageUtilization"`
}

// GenerateHPA generates a Kubernetes HorizontalPodAutoscaler manifest
func GenerateHPA(analysis *types.AppAnalysis, namespace string, cfg *config.Config) (string, error) {
	labels := buildLabelsWithAppConfig(analysis, cfg)

	minReplicas := 2
	maxReplicas := 10
	targetCPU := 70
	targetMemory := 0

	// Use app config scaling if available (already merged into analysis.Scaling by analyzer)
	if analysis.AppConfig != nil && analysis.AppConfig.Scaling != nil {
		scaling := analysis.AppConfig.Scaling
		if scaling.MinReplicas > 0 {
			minReplicas = scaling.MinReplicas
		}
		if scaling.MaxReplicas > 0 {
			maxReplicas = scaling.MaxReplicas
		}
		if scaling.TargetCPU > 0 {
			targetCPU = scaling.TargetCPU
		}
		if scaling.TargetMemory > 0 {
			targetMemory = scaling.TargetMemory
		}
	} else if analysis.Scaling != nil {
		if analysis.Scaling.MinReplicas > 0 {
			minReplicas = analysis.Scaling.MinReplicas
		}
		if analysis.Scaling.MaxReplicas > 0 {
			maxReplicas = analysis.Scaling.MaxReplicas
		}
		if analysis.Scaling.TargetCPU > 0 {
			targetCPU = analysis.Scaling.TargetCPU
		}
		if analysis.Scaling.TargetMemory > 0 {
			targetMemory = analysis.Scaling.TargetMemory
		}
	}

	metrics := []MetricSpec{
		{
			Type: "Resource",
			Resource: &ResourceMetric{
				Name: "cpu",
				Target: MetricTarget{
					Type:               "Utilization",
					AverageUtilization: targetCPU,
				},
			},
		},
	}

	// Add memory metric if specified
	if targetMemory > 0 {
		metrics = append(metrics, MetricSpec{
			Type: "Resource",
			Resource: &ResourceMetric{
				Name: "memory",
				Target: MetricTarget{
					Type:               "Utilization",
					AverageUtilization: targetMemory,
				},
			},
		})
	}

	hpa := HPAManifest{
		APIVersion: "autoscaling/v2",
		Kind:       "HorizontalPodAutoscaler",
		Metadata: Metadata{
			Name:      analysis.Name,
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: HPASpec{
			ScaleTargetRef: ScaleTargetRef{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
				Name:       analysis.Name,
			},
			MinReplicas: minReplicas,
			MaxReplicas: maxReplicas,
			Metrics:     metrics,
		},
	}

	return toYAML(hpa)
}
