package generator

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"

	"github.com/dorgu-ai/dorgu/internal/config"
	"github.com/dorgu-ai/dorgu/internal/types"
)

// GenerateDeployment generates a Kubernetes Deployment manifest
func GenerateDeployment(analysis *types.AppAnalysis, namespace string, resources config.ResourceSpec, cfg *config.Config) (string, error) {
	name := ToDNSSubdomain(analysis.Name)
	// Build labels - merge org config and app config labels (use DNS-safe name for operator matching)
	labels := buildLabelsWithAppConfig(analysis, cfg)

	// Build annotations from app config
	annotations := buildAnnotationsWithAppConfig(analysis, cfg)

	// Build container ports
	var containerPorts []ContainerPort
	for i, p := range analysis.Ports {
		containerPorts = append(containerPorts, ContainerPort{
			Name:          fmt.Sprintf("port-%d", i),
			ContainerPort: p.Port,
			Protocol:      "TCP",
		})
	}

	// Build environment variables
	var envVars []EnvVar
	for _, e := range analysis.EnvVars {
		ev := EnvVar{Name: e.Name}
		if e.Secret {
			// Reference from secret (use DNS-safe name for secret name)
			ev.ValueFrom = &EnvVarSource{
				SecretKeyRef: &SecretKeySelector{
					Name: name + "-secrets",
					Key:  strings.ToLower(e.Name),
				},
			}
		} else if e.Value != "" {
			ev.Value = e.Value
		}
		envVars = append(envVars, ev)
	}

	// Override resources from app config if present
	finalResources := resources
	if analysis.AppConfig != nil && analysis.AppConfig.Resources != nil {
		res := analysis.AppConfig.Resources
		if res.RequestsCPU != "" {
			finalResources.Requests.CPU = res.RequestsCPU
		}
		if res.RequestsMemory != "" {
			finalResources.Requests.Memory = res.RequestsMemory
		}
		if res.LimitsCPU != "" {
			finalResources.Limits.CPU = res.LimitsCPU
		}
		if res.LimitsMemory != "" {
			finalResources.Limits.Memory = res.LimitsMemory
		}
	}

	// Build probes - use app config health settings if available
	var livenessProbe, readinessProbe *Probe
	if analysis.AppConfig != nil && analysis.AppConfig.Health != nil {
		health := analysis.AppConfig.Health
		if health.LivenessPath != "" {
			livenessProbe = &Probe{
				HTTPGet: &HTTPGetAction{
					Path: health.LivenessPath,
					Port: health.LivenessPort,
				},
				InitialDelaySeconds: health.InitialDelay,
				PeriodSeconds:       health.Period,
				TimeoutSeconds:      5,
				FailureThreshold:    3,
			}
			if livenessProbe.InitialDelaySeconds == 0 {
				livenessProbe.InitialDelaySeconds = 10
			}
			if livenessProbe.PeriodSeconds == 0 {
				livenessProbe.PeriodSeconds = 10
			}
		}
		if health.ReadinessPath != "" {
			readinessProbe = &Probe{
				HTTPGet: &HTTPGetAction{
					Path: health.ReadinessPath,
					Port: health.ReadinessPort,
				},
				InitialDelaySeconds: health.InitialDelay,
				PeriodSeconds:       health.Period,
				TimeoutSeconds:      5,
				FailureThreshold:    3,
			}
			if readinessProbe.InitialDelaySeconds == 0 {
				readinessProbe.InitialDelaySeconds = 5
			}
			if readinessProbe.PeriodSeconds == 0 {
				readinessProbe.PeriodSeconds = 5
			}
		}
	}

	// Fallback to analysis health check if app config didn't specify
	if livenessProbe == nil && analysis.HealthCheck != nil {
		probe := &Probe{
			HTTPGet: &HTTPGetAction{
				Path: analysis.HealthCheck.Path,
				Port: analysis.HealthCheck.Port,
			},
			InitialDelaySeconds: 10,
			PeriodSeconds:       10,
			TimeoutSeconds:      5,
			FailureThreshold:    3,
		}
		if analysis.HealthCheck.InitialDelay > 0 {
			probe.InitialDelaySeconds = analysis.HealthCheck.InitialDelay
		}
		if analysis.HealthCheck.Period > 0 {
			probe.PeriodSeconds = analysis.HealthCheck.Period
		}
		livenessProbe = probe
		readinessProbe = probe
	}

	// Build security contexts
	trueVal := true
	falseVal := false

	podSecurityContext := &PodSecurityContext{
		RunAsNonRoot: &trueVal,
		SeccompProfile: &SeccompProfile{
			Type: "RuntimeDefault",
		},
	}

	containerSecurityContext := &ContainerSecurityContext{
		AllowPrivilegeEscalation: &falseVal,
		ReadOnlyRootFilesystem:   &trueVal,
		Capabilities: &Capabilities{
			Drop: []corev1.Capability{"ALL"},
		},
	}
	if ImageRunsAsRoot(analysis) {
		uid := DefaultNonRootUID
		containerSecurityContext.RunAsUser = &uid
	}

	// Determine image name and pull policy (Never for local/Kind when no registry)
	imageName := fmt.Sprintf("%s/%s:latest", cfg.CI.Registry, analysis.Name)
	imagePullPolicy := ""
	if cfg.CI.Registry == "" {
		imageName = analysis.Name + ":latest"
		imagePullPolicy = "Never"
	}

	// Determine replicas - prefer app config scaling
	replicas := 2
	if analysis.AppConfig != nil && analysis.AppConfig.Scaling != nil && analysis.AppConfig.Scaling.MinReplicas > 0 {
		replicas = analysis.AppConfig.Scaling.MinReplicas
	} else if analysis.Scaling != nil && analysis.Scaling.MinReplicas > 0 {
		replicas = analysis.Scaling.MinReplicas
	}

	deployment := DeploymentManifest{
		APIVersion: "apps/v1",
		Kind:       "Deployment",
		Metadata: Metadata{
			Name:        name,
			Namespace:   namespace,
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: DeploymentSpec{
			Replicas: replicas,
			Selector: LabelSelector{
				MatchLabels: map[string]string{
					"app.kubernetes.io/name": name,
				},
			},
			Template: PodTemplateSpec{
				Metadata: Metadata{
					Labels:      labels,
					Annotations: annotations,
				},
				Spec: PodSpec{
					SecurityContext: podSecurityContext,
					Containers: []Container{
						{
							Name:            name,
							Image:           imageName,
							ImagePullPolicy: imagePullPolicy,
							Ports:           containerPorts,
							Env:             envVars,
							Resources: ResourceRequirements{
								Requests: map[string]string{
									"cpu":    finalResources.Requests.CPU,
									"memory": finalResources.Requests.Memory,
								},
								Limits: map[string]string{
									"cpu":    finalResources.Limits.CPU,
									"memory": finalResources.Limits.Memory,
								},
							},
							LivenessProbe:   livenessProbe,
							ReadinessProbe:  readinessProbe,
							SecurityContext: containerSecurityContext,
						},
					},
				},
			},
		},
	}

	return toYAML(deployment)
}
