package generator

import (
	corev1 "k8s.io/api/core/v1"
)

// DeploymentManifest represents a Kubernetes Deployment
type DeploymentManifest struct {
	APIVersion string         `json:"apiVersion"`
	Kind       string         `json:"kind"`
	Metadata   Metadata       `json:"metadata"`
	Spec       DeploymentSpec `json:"spec"`
}

// Metadata represents Kubernetes object metadata
type Metadata struct {
	Name        string            `json:"name"`
	Namespace   string            `json:"namespace,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
}

// DeploymentSpec represents a Deployment spec
type DeploymentSpec struct {
	Replicas int             `json:"replicas"`
	Selector LabelSelector   `json:"selector"`
	Template PodTemplateSpec `json:"template"`
}

// LabelSelector represents a label selector
type LabelSelector struct {
	MatchLabels map[string]string `json:"matchLabels"`
}

// PodTemplateSpec represents a pod template
type PodTemplateSpec struct {
	Metadata Metadata `json:"metadata"`
	Spec     PodSpec  `json:"spec"`
}

// PodSpec represents a pod spec
type PodSpec struct {
	Containers         []Container         `json:"containers"`
	SecurityContext    *PodSecurityContext `json:"securityContext,omitempty"`
	ServiceAccountName string              `json:"serviceAccountName,omitempty"`
}

// PodSecurityContext represents pod security context
type PodSecurityContext struct {
	RunAsNonRoot   *bool           `json:"runAsNonRoot,omitempty"`
	SeccompProfile *SeccompProfile `json:"seccompProfile,omitempty"`
}

// SeccompProfile represents seccomp profile
type SeccompProfile struct {
	Type string `json:"type"`
}

// Container represents a container spec
type Container struct {
	Name            string                    `json:"name"`
	Image           string                    `json:"image"`
	ImagePullPolicy string                    `json:"imagePullPolicy,omitempty"`
	Ports           []ContainerPort           `json:"ports,omitempty"`
	Env             []EnvVar                  `json:"env,omitempty"`
	Resources       ResourceRequirements      `json:"resources,omitempty"`
	LivenessProbe   *Probe                    `json:"livenessProbe,omitempty"`
	ReadinessProbe  *Probe                    `json:"readinessProbe,omitempty"`
	SecurityContext *ContainerSecurityContext `json:"securityContext,omitempty"`
}

// ContainerPort represents a container port
type ContainerPort struct {
	Name          string `json:"name,omitempty"`
	ContainerPort int    `json:"containerPort"`
	Protocol      string `json:"protocol,omitempty"`
}

// EnvVar represents an environment variable
type EnvVar struct {
	Name      string        `json:"name"`
	Value     string        `json:"value,omitempty"`
	ValueFrom *EnvVarSource `json:"valueFrom,omitempty"`
}

// EnvVarSource represents the source of an env var
type EnvVarSource struct {
	SecretKeyRef    *SecretKeySelector    `json:"secretKeyRef,omitempty"`
	ConfigMapKeyRef *ConfigMapKeySelector `json:"configMapKeyRef,omitempty"`
}

// SecretKeySelector selects a key from a secret
type SecretKeySelector struct {
	Name string `json:"name"`
	Key  string `json:"key"`
}

// ConfigMapKeySelector selects a key from a configmap
type ConfigMapKeySelector struct {
	Name string `json:"name"`
	Key  string `json:"key"`
}

// ResourceRequirements represents resource requests and limits
type ResourceRequirements struct {
	Requests map[string]string `json:"requests,omitempty"`
	Limits   map[string]string `json:"limits,omitempty"`
}

// Probe represents a liveness or readiness probe
type Probe struct {
	HTTPGet             *HTTPGetAction `json:"httpGet,omitempty"`
	InitialDelaySeconds int            `json:"initialDelaySeconds,omitempty"`
	PeriodSeconds       int            `json:"periodSeconds,omitempty"`
	TimeoutSeconds      int            `json:"timeoutSeconds,omitempty"`
	FailureThreshold    int            `json:"failureThreshold,omitempty"`
}

// HTTPGetAction represents an HTTP GET probe
type HTTPGetAction struct {
	Path   string `json:"path"`
	Port   int    `json:"port"`
	Scheme string `json:"scheme,omitempty"`
}

// ContainerSecurityContext represents container security context
type ContainerSecurityContext struct {
	AllowPrivilegeEscalation *bool         `json:"allowPrivilegeEscalation,omitempty"`
	ReadOnlyRootFilesystem   *bool         `json:"readOnlyRootFilesystem,omitempty"`
	RunAsUser                *int64        `json:"runAsUser,omitempty"`
	Capabilities             *Capabilities `json:"capabilities,omitempty"`
}

// Capabilities represents Linux capabilities
type Capabilities struct {
	Drop []corev1.Capability `json:"drop,omitempty"`
	Add  []corev1.Capability `json:"add,omitempty"`
}
