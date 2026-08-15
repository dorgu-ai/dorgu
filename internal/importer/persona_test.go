package importer

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/yaml"
)

// --- fixtures modelled on ~/dorgu-cleanroom/2026-08-09/brownfield.yaml ---

type containerSpec struct {
	name           string
	image          string
	requestsCPU    string
	requestsMemory string
	limitsCPU      string
	limitsMemory   string
	port           int32
	livenessPath   string
	readinessPath  string
	probePort      int32
}

func buildDeployment(name string, replicas int32, specs ...containerSpec) *appsv1.Deployment {
	containers := make([]corev1.Container, 0, len(specs))
	for _, s := range specs {
		c := corev1.Container{Name: s.name, Image: s.image}

		requests := corev1.ResourceList{}
		if s.requestsCPU != "" {
			requests[corev1.ResourceCPU] = resource.MustParse(s.requestsCPU)
		}
		if s.requestsMemory != "" {
			requests[corev1.ResourceMemory] = resource.MustParse(s.requestsMemory)
		}
		limits := corev1.ResourceList{}
		if s.limitsCPU != "" {
			limits[corev1.ResourceCPU] = resource.MustParse(s.limitsCPU)
		}
		if s.limitsMemory != "" {
			limits[corev1.ResourceMemory] = resource.MustParse(s.limitsMemory)
		}
		if len(requests) > 0 {
			c.Resources.Requests = requests
		}
		if len(limits) > 0 {
			c.Resources.Limits = limits
		}

		if s.port > 0 {
			c.Ports = []corev1.ContainerPort{{ContainerPort: s.port}}
		}
		if s.livenessPath != "" {
			c.LivenessProbe = httpGetProbe(s.livenessPath, s.probePort)
		}
		if s.readinessPath != "" {
			c.ReadinessProbe = httpGetProbe(s.readinessPath, s.probePort)
		}
		containers = append(containers, c)
	}

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "apps"},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": name}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": name}},
				Spec:       corev1.PodSpec{Containers: containers},
			},
		},
	}
}

func httpGetProbe(path string, port int32) *corev1.Probe {
	return &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{Path: path, Port: intstr.FromInt32(port)},
		},
	}
}

// webDeployment is the clean-room `web` Deployment: full requests and limits.
func webDeployment() *appsv1.Deployment {
	return buildDeployment("web", 2, containerSpec{
		name: "nginx", image: "nginx:1.27-alpine",
		requestsCPU: "25m", requestsMemory: "32Mi",
		limitsCPU: "200m", limitsMemory: "96Mi",
		port: 80,
	})
}

// --- schema mirror ---

// personaDoc mirrors the ApplicationPersona CRD closely enough to catch drift:
// UnmarshalStrict rejects any field the importer emits that the CRD does not
// have, and the assertions below cover the required fields and enums.
type personaDoc struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Metadata   struct {
		Name        string            `json:"name"`
		Namespace   string            `json:"namespace"`
		Labels      map[string]string `json:"labels"`
		Annotations map[string]string `json:"annotations"`
	} `json:"metadata"`
	Spec struct {
		Name      string `json:"name"`
		Version   string `json:"version"`
		Type      string `json:"type"`
		Tier      string `json:"tier"`
		Technical *struct {
			Language    string `json:"language"`
			Framework   string `json:"framework"`
			Description string `json:"description"`
		} `json:"technical"`
		Resources *struct {
			Requests *struct {
				CPU    string `json:"cpu"`
				Memory string `json:"memory"`
			} `json:"requests"`
			Limits *struct {
				CPU    string `json:"cpu"`
				Memory string `json:"memory"`
			} `json:"limits"`
			Profile string `json:"profile"`
		} `json:"resources"`
		Scaling *struct {
			MinReplicas  *int32 `json:"minReplicas"`
			MaxReplicas  *int32 `json:"maxReplicas"`
			TargetCPU    *int32 `json:"targetCPU"`
			TargetMemory *int32 `json:"targetMemory"`
			Behavior     string `json:"behavior"`
		} `json:"scaling"`
		Health *struct {
			LivenessPath       string `json:"livenessPath"`
			ReadinessPath      string `json:"readinessPath"`
			Port               *int32 `json:"port"`
			StartupGracePeriod string `json:"startupGracePeriod"`
		} `json:"health"`
		Networking *struct {
			Ports []struct {
				Port     int32  `json:"port"`
				Protocol string `json:"protocol"`
				Purpose  string `json:"purpose"`
			} `json:"ports"`
		} `json:"networking"`
		Ownership *struct {
			Team       string `json:"team"`
			Owner      string `json:"owner"`
			Repository string `json:"repository"`
			OnCall     string `json:"oncall"`
			Runbook    string `json:"runbook"`
		} `json:"ownership"`
	} `json:"spec"`
}

var (
	validTypes     = map[string]bool{"api": true, "web": true, "worker": true, "cron": true, "daemon": true}
	validTiers     = map[string]bool{"critical": true, "standard": true, "best-effort": true}
	validBehaviors = map[string]bool{"conservative": true, "balanced": true, "aggressive": true}
)

// decodePersona round-trips the rendered YAML through the schema mirror and
// asserts the CRD's required fields and enum constraints.
func decodePersona(t *testing.T, y string) personaDoc {
	t.Helper()

	var doc personaDoc
	require.NoError(t, yaml.UnmarshalStrict([]byte(y), &doc),
		"rendered persona must round-trip with no unknown fields")

	assert.Equal(t, "dorgu.io/v1", doc.APIVersion)
	assert.Equal(t, "ApplicationPersona", doc.Kind)
	assert.NotEmpty(t, doc.Metadata.Name, "metadata.name is required")
	assert.NotEmpty(t, doc.Spec.Name, "spec.name is required")
	assert.True(t, validTypes[doc.Spec.Type], "spec.type %q is not in the CRD enum", doc.Spec.Type)
	assert.True(t, validTiers[doc.Spec.Tier], "spec.tier %q is not in the CRD enum", doc.Spec.Tier)
	if doc.Spec.Scaling != nil {
		assert.True(t, validBehaviors[doc.Spec.Scaling.Behavior],
			"scaling.behavior %q is not in the CRD enum", doc.Spec.Scaling.Behavior)
		require.NotNil(t, doc.Spec.Scaling.MinReplicas)
		require.NotNil(t, doc.Spec.Scaling.MaxReplicas)
		assert.GreaterOrEqual(t, *doc.Spec.Scaling.MinReplicas, int32(0), "minReplicas has a CRD minimum of 0")
		assert.GreaterOrEqual(t, *doc.Spec.Scaling.MaxReplicas, int32(1), "maxReplicas has a CRD minimum of 1")
	}
	return doc
}

// --- tests ---

func TestFromDeployment_FullSpec(t *testing.T) {
	result := FromDeployment(webDeployment(), "web", "apps")

	assert.False(t, result.LimitsInvented)
	assert.Equal(t, "web", result.Name)
	assert.Equal(t, "web", result.AppName)
	assert.Equal(t, "apps", result.Namespace)

	doc := decodePersona(t, result.YAML)
	assert.Equal(t, "web", doc.Spec.Name)
	assert.Equal(t, "apps", doc.Metadata.Namespace)
	assert.Equal(t, "api", doc.Spec.Type, "a Deployment with a port serves traffic")

	require.NotNil(t, doc.Spec.Resources)
	require.NotNil(t, doc.Spec.Resources.Requests)
	require.NotNil(t, doc.Spec.Resources.Limits)
	assert.Equal(t, "25m", doc.Spec.Resources.Requests.CPU)
	assert.Equal(t, "32Mi", doc.Spec.Resources.Requests.Memory)
	assert.Equal(t, "200m", doc.Spec.Resources.Limits.CPU)
	assert.Equal(t, "96Mi", doc.Spec.Resources.Limits.Memory)

	require.NotNil(t, doc.Spec.Scaling)
	assert.Equal(t, int32(2), *doc.Spec.Scaling.MinReplicas)
	assert.Equal(t, int32(2), *doc.Spec.Scaling.MaxReplicas)

	require.NotNil(t, doc.Spec.Networking)
	require.Len(t, doc.Spec.Networking.Ports, 1)
	assert.Equal(t, int32(80), doc.Spec.Networking.Ports[0].Port)
	assert.Equal(t, "TCP", doc.Spec.Networking.Ports[0].Protocol)

	assert.Equal(t, "nginx:1.27-alpine", doc.Metadata.Annotations["dorgu.io/imported-image"])
	assert.Equal(t, "Deployment/web", doc.Metadata.Annotations["dorgu.io/imported-from"])
	assert.Equal(t, "web", doc.Metadata.Labels["app.kubernetes.io/name"])
}

func TestFromDeployment_Probes(t *testing.T) {
	deploy := buildDeployment("checkout-api", 1, containerSpec{
		name: "api", image: "hashicorp/http-echo:1.0",
		requestsCPU: "25m", requestsMemory: "24Mi",
		limitsCPU: "200m", limitsMemory: "64Mi",
		port: 8080, livenessPath: "/healthz", readinessPath: "/ready", probePort: 8080,
	})

	result := FromDeployment(deploy, "checkout-api", "apps")

	doc := decodePersona(t, result.YAML)
	require.NotNil(t, doc.Spec.Health)
	assert.Equal(t, "/healthz", doc.Spec.Health.LivenessPath)
	assert.Equal(t, "/ready", doc.Spec.Health.ReadinessPath)
	require.NotNil(t, doc.Spec.Health.Port)
	assert.Equal(t, int32(8080), *doc.Spec.Health.Port)
	assert.Equal(t, "30s", doc.Spec.Health.StartupGracePeriod)
}

func TestFromDeployment_WorkerHasNoPorts(t *testing.T) {
	deploy := buildDeployment("report-worker", 1, containerSpec{
		name: "worker", image: "busybox:1.36",
		requestsCPU: "25m", requestsMemory: "16Mi",
		limitsCPU: "100m", limitsMemory: "48Mi",
	})

	result := FromDeployment(deploy, "report-worker", "apps")

	doc := decodePersona(t, result.YAML)
	assert.Equal(t, "worker", doc.Spec.Type)
	assert.Nil(t, doc.Spec.Networking)
	assert.Nil(t, doc.Spec.Health)
	assert.Contains(t, result.Warnings[0], "no HTTP liveness or readiness probe")
}

// The proposer skips personas with no resource limits, so an import that
// quietly produced one would create a persona that can never heal.
func TestFromDeployment_NoLimitsDerivesFromRequestsAndWarns(t *testing.T) {
	deploy := buildDeployment("billing", 1, containerSpec{
		name: "billing", image: "billing:1.0",
		requestsCPU: "25m", requestsMemory: "32Mi",
	})

	result := FromDeployment(deploy, "billing", "apps")

	assert.True(t, result.LimitsInvented)
	require.NotEmpty(t, result.Warnings)
	assert.Contains(t, result.Warnings[0], "sets no cpu limit")
	assert.Contains(t, result.Warnings[1], "sets no memory limit")

	doc := decodePersona(t, result.YAML)
	require.NotNil(t, doc.Spec.Resources.Limits)
	assert.Equal(t, "50m", doc.Spec.Resources.Limits.CPU, "2x the 25m request")
	assert.Equal(t, "64Mi", doc.Spec.Resources.Limits.Memory, "2x the 32Mi request")
}

func TestFromDeployment_NoRequestsOrLimitsUsesDefaultsAndWarns(t *testing.T) {
	deploy := buildDeployment("bare", 1, containerSpec{name: "bare", image: "bare:1.0"})

	result := FromDeployment(deploy, "bare", "apps")

	assert.True(t, result.LimitsInvented)
	assert.Contains(t, result.Warnings[0], "Dorgu default")

	doc := decodePersona(t, result.YAML)
	require.NotNil(t, doc.Spec.Resources.Limits)
	assert.Equal(t, DefaultCPULimit, doc.Spec.Resources.Limits.CPU)
	assert.Equal(t, DefaultMemoryLimit, doc.Spec.Resources.Limits.Memory)
	assert.Nil(t, doc.Spec.Resources.Requests, "requests are not invented")
}

func TestFromDeployment_MixedLimitsFillsOnlyTheMissingOne(t *testing.T) {
	deploy := buildDeployment("halfway", 1, containerSpec{
		name: "halfway", image: "halfway:1.0",
		requestsCPU: "100m", requestsMemory: "64Mi", limitsMemory: "128Mi",
	})

	result := FromDeployment(deploy, "halfway", "apps")

	assert.True(t, result.LimitsInvented)
	doc := decodePersona(t, result.YAML)
	assert.Equal(t, "200m", doc.Spec.Resources.Limits.CPU, "derived from the request")
	assert.Equal(t, "128Mi", doc.Spec.Resources.Limits.Memory, "the declared limit is kept as is")
}

func TestFromDeployment_ScaledToZeroStillSatisfiesTheSchema(t *testing.T) {
	deploy := buildDeployment("paused", 0, containerSpec{
		name: "paused", image: "paused:1.0", limitsCPU: "100m", limitsMemory: "64Mi",
	})

	result := FromDeployment(deploy, "paused", "apps")

	doc := decodePersona(t, result.YAML)
	assert.Equal(t, int32(0), *doc.Spec.Scaling.MinReplicas)
	assert.Equal(t, int32(1), *doc.Spec.Scaling.MaxReplicas, "the CRD requires maxReplicas >= 1")
}

func TestFromDeployment_PrefersTheContainerNamedAfterTheDeployment(t *testing.T) {
	deploy := buildDeployment("api", 1,
		containerSpec{name: "istio-proxy", image: "proxy:1.0", limitsMemory: "1Gi"},
		containerSpec{name: "api", image: "api:2.0", limitsCPU: "200m", limitsMemory: "96Mi"},
	)

	result := FromDeployment(deploy, "api", "apps")

	doc := decodePersona(t, result.YAML)
	assert.Equal(t, "96Mi", doc.Spec.Resources.Limits.Memory)
	assert.Equal(t, "api:2.0", doc.Metadata.Annotations["dorgu.io/imported-image"])
}

func TestFromDeployment_ReadsOwnershipFromLabelsAndAnnotations(t *testing.T) {
	deploy := webDeployment()
	deploy.Labels = map[string]string{"app.kubernetes.io/part-of": "storefront"}
	deploy.Annotations = map[string]string{
		"dorgu.io/owner":        "payments@example.com",
		"dorgu.io/repository":   "github.com/example/web",
		"unrelated.io/whatever": "ignored",
	}

	result := FromDeployment(deploy, "web", "apps")

	doc := decodePersona(t, result.YAML)
	require.NotNil(t, doc.Spec.Ownership)
	assert.Equal(t, "storefront", doc.Spec.Ownership.Team)
	assert.Equal(t, "payments@example.com", doc.Spec.Ownership.Owner)
	assert.Equal(t, "github.com/example/web", doc.Spec.Ownership.Repository)
	assert.Equal(t, "storefront", doc.Metadata.Labels["dorgu.io/team"])
}

func TestFromDeployment_OwnershipIsOmittedWhenUnknown(t *testing.T) {
	result := FromDeployment(webDeployment(), "web", "apps")

	doc := decodePersona(t, result.YAML)
	assert.Nil(t, doc.Spec.Ownership, "ownership must not be invented")
}

func TestFromDeployment_NoContainers(t *testing.T) {
	deploy := buildDeployment("empty", 1)

	result := FromDeployment(deploy, "empty", "apps")

	assert.True(t, result.LimitsInvented)
	assert.Contains(t, result.Warnings[0], "no containers")
	doc := decodePersona(t, result.YAML)
	assert.Equal(t, DefaultMemoryLimit, doc.Spec.Resources.Limits.Memory)
}

func TestFromDeployment_FallsBackToTheDeploymentNamespaceAndName(t *testing.T) {
	result := FromDeployment(webDeployment(), "", "")

	assert.Equal(t, "web", result.AppName)
	assert.Equal(t, "apps", result.Namespace)
}

func TestJoinYAMLAndSortResults(t *testing.T) {
	results := []Result{
		FromDeployment(buildDeployment("web", 1, containerSpec{name: "web", limitsMemory: "96Mi"}), "web", "apps"),
		FromDeployment(buildDeployment("checkout-api", 1, containerSpec{name: "checkout-api", limitsMemory: "64Mi"}), "checkout-api", "apps"),
	}

	sorted := SortResults(results)
	assert.Equal(t, "checkout-api", sorted[0].Deployment)
	assert.Equal(t, "web", sorted[1].Deployment)
	assert.Equal(t, "web", results[0].Deployment, "SortResults must not mutate its input")

	joined := JoinYAML(sorted)
	assert.Equal(t, 2, len(splitDocs(joined)))
	for _, doc := range splitDocs(joined) {
		decodePersona(t, doc)
	}
}

// splitDocs splits a multi-document YAML file into its documents.
func splitDocs(y string) []string {
	var docs []string
	for _, part := range strings.Split(y, "\n---\n") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			docs = append(docs, trimmed)
		}
	}
	return docs
}

func TestMultiplyQuantity(t *testing.T) {
	tests := []struct {
		value string
		want  string
		ok    bool
	}{
		{"25m", "50m", true},
		{"32Mi", "64Mi", true},
		{"1", "2", true},
		{"1Gi", "2Gi", true},
		{"", "", false},
		{"not-a-quantity", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			got, ok := multiplyQuantity(tt.value, 2)
			assert.Equal(t, tt.ok, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}
