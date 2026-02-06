package generator

import (
	"github.com/dorgu-ai/dorgu/internal/config"
	"github.com/dorgu-ai/dorgu/internal/types"
)

// IngressManifest represents a Kubernetes Ingress
type IngressManifest struct {
	APIVersion string      `json:"apiVersion"`
	Kind       string      `json:"kind"`
	Metadata   Metadata    `json:"metadata"`
	Spec       IngressSpec `json:"spec"`
}

// IngressSpec represents an Ingress spec
type IngressSpec struct {
	IngressClassName *string       `json:"ingressClassName,omitempty"`
	TLS              []IngressTLS  `json:"tls,omitempty"`
	Rules            []IngressRule `json:"rules"`
}

// IngressTLS represents TLS configuration
type IngressTLS struct {
	Hosts      []string `json:"hosts"`
	SecretName string   `json:"secretName"`
}

// IngressRule represents an ingress rule
type IngressRule struct {
	Host string          `json:"host"`
	HTTP IngressRuleHTTP `json:"http"`
}

// IngressRuleHTTP represents HTTP rules
type IngressRuleHTTP struct {
	Paths []IngressPath `json:"paths"`
}

// IngressPath represents a path rule
type IngressPath struct {
	Path     string         `json:"path"`
	PathType string         `json:"pathType"`
	Backend  IngressBackend `json:"backend"`
}

// IngressBackend represents the backend service
type IngressBackend struct {
	Service IngressServiceBackend `json:"service"`
}

// IngressServiceBackend represents the service reference
type IngressServiceBackend struct {
	Name string             `json:"name"`
	Port ServiceBackendPort `json:"port"`
}

// ServiceBackendPort represents the port reference
type ServiceBackendPort struct {
	Number int `json:"number"`
}

// GenerateIngress generates a Kubernetes Ingress manifest
func GenerateIngress(analysis *types.AppAnalysis, namespace string, cfg *config.Config) (string, error) {
	labels := buildLabelsWithAppConfig(analysis, cfg)
	annotations := buildAnnotationsWithAppConfig(analysis, cfg)
	if annotations == nil {
		annotations = make(map[string]string)
	}

	// Determine TLS settings from app config or org config
	tlsEnabled := cfg.Ingress.TLS.Enabled
	tlsSecret := analysis.Name + "-tls"
	if analysis.AppConfig != nil && analysis.AppConfig.Ingress != nil {
		if analysis.AppConfig.Ingress.TLSEnabled {
			tlsEnabled = true
		}
		if analysis.AppConfig.Ingress.TLSSecret != "" {
			tlsSecret = analysis.AppConfig.Ingress.TLSSecret
		}
	}

	// Add TLS annotations if enabled
	if tlsEnabled && cfg.Ingress.TLS.ClusterIssuer != "" {
		annotations["cert-manager.io/cluster-issuer"] = cfg.Ingress.TLS.ClusterIssuer
	}

	// Determine host from app config or generate from org config
	host := analysis.Name + cfg.Ingress.DomainSuffix
	if analysis.AppConfig != nil && analysis.AppConfig.Ingress != nil && analysis.AppConfig.Ingress.Host != "" {
		host = analysis.AppConfig.Ingress.Host
	}

	// Find the HTTP port
	httpPort := 80
	for _, p := range analysis.Ports {
		if p.Port == 80 || p.Port == 8080 || p.Port == 3000 || p.Port == 5000 || p.Port == 8000 {
			httpPort = p.Port
			break
		}
	}
	if len(analysis.Ports) > 0 && httpPort == 80 {
		httpPort = analysis.Ports[0].Port
	}

	ingressClassName := cfg.Ingress.Class

	// Build paths from app config or default to "/"
	var ingressPaths []IngressPath
	if analysis.AppConfig != nil && analysis.AppConfig.Ingress != nil && len(analysis.AppConfig.Ingress.Paths) > 0 {
		for _, p := range analysis.AppConfig.Ingress.Paths {
			pathType := p.PathType
			if pathType == "" {
				pathType = "Prefix"
			}
			ingressPaths = append(ingressPaths, IngressPath{
				Path:     p.Path,
				PathType: pathType,
				Backend: IngressBackend{
					Service: IngressServiceBackend{
						Name: analysis.Name,
						Port: ServiceBackendPort{
							Number: httpPort,
						},
					},
				},
			})
		}
	} else {
		// Default path
		ingressPaths = []IngressPath{
			{
				Path:     "/",
				PathType: "Prefix",
				Backend: IngressBackend{
					Service: IngressServiceBackend{
						Name: analysis.Name,
						Port: ServiceBackendPort{
							Number: httpPort,
						},
					},
				},
			},
		}
	}

	ingress := IngressManifest{
		APIVersion: "networking.k8s.io/v1",
		Kind:       "Ingress",
		Metadata: Metadata{
			Name:        analysis.Name,
			Namespace:   namespace,
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: IngressSpec{
			IngressClassName: &ingressClassName,
			Rules: []IngressRule{
				{
					Host: host,
					HTTP: IngressRuleHTTP{
						Paths: ingressPaths,
					},
				},
			},
		},
	}

	// Add TLS configuration
	if tlsEnabled {
		ingress.Spec.TLS = []IngressTLS{
			{
				Hosts:      []string{host},
				SecretName: tlsSecret,
			},
		}
	}

	return toYAML(ingress)
}
