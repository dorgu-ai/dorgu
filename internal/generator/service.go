package generator

import (
	"fmt"

	"github.com/dorgu-ai/dorgu/internal/config"
	"github.com/dorgu-ai/dorgu/internal/types"
)

// ServiceManifest represents a Kubernetes Service
type ServiceManifest struct {
	APIVersion string      `json:"apiVersion"`
	Kind       string      `json:"kind"`
	Metadata   Metadata    `json:"metadata"`
	Spec       ServiceSpec `json:"spec"`
}

// ServiceSpec represents a Service spec
type ServiceSpec struct {
	Type     string            `json:"type,omitempty"`
	Selector map[string]string `json:"selector"`
	Ports    []ServicePort     `json:"ports"`
}

// ServicePort represents a service port
type ServicePort struct {
	Name       string `json:"name,omitempty"`
	Port       int    `json:"port"`
	TargetPort int    `json:"targetPort"`
	Protocol   string `json:"protocol,omitempty"`
}

// GenerateService generates a Kubernetes Service manifest
func GenerateService(analysis *types.AppAnalysis, namespace string, cfg *config.Config) (string, error) {
	labels := buildLabelsWithAppConfig(analysis, cfg)
	annotations := buildAnnotationsWithAppConfig(analysis, cfg)

	var servicePorts []ServicePort
	for i, p := range analysis.Ports {
		servicePorts = append(servicePorts, ServicePort{
			Name:       fmt.Sprintf("port-%d", i),
			Port:       p.Port,
			TargetPort: p.Port,
			Protocol:   "TCP",
		})
	}

	service := ServiceManifest{
		APIVersion: "v1",
		Kind:       "Service",
		Metadata: Metadata{
			Name:        analysis.Name,
			Namespace:   namespace,
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: ServiceSpec{
			Type: "ClusterIP",
			Selector: map[string]string{
				"app.kubernetes.io/name": analysis.Name,
			},
			Ports: servicePorts,
		},
	}

	return toYAML(service)
}
