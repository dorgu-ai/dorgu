// Package importer synthesizes an ApplicationPersona from a Deployment that is
// already running.
//
// `dorgu persona generate|apply` read local source (Dockerfile, compose, code),
// which is no help on a cluster that already has apps: there is nothing to
// analyze and no persona, so Dorgu stays completely silent. Everything a persona
// needs is already in the Deployment spec, so read that instead.
package importer

import (
	"fmt"
	"sort"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

// Defaults used when a container declares no resource limits at all. They are a
// starting point, never an observation, and every path that uses them says so.
const (
	DefaultCPULimit    = "500m"
	DefaultMemoryLimit = "512Mi"

	// LimitFromRequestMultiplier derives a limit from a request when the
	// container sets requests but no limits. Headroom without a blank cheque.
	LimitFromRequestMultiplier = 2
)

// Result is one synthesized persona: the YAML to apply, plus everything the
// caller needs to tell the user what was inferred rather than observed.
type Result struct {
	// Name is the persona's metadata.name.
	Name string
	// AppName is spec.name, the value the operator resolves Deployments by.
	AppName string
	// Namespace is the persona's namespace.
	Namespace string
	// Deployment is the source Deployment's name.
	Deployment string
	// YAML is the rendered ApplicationPersona.
	YAML string
	// Warnings describe every value that was invented rather than read.
	Warnings []string
	// LimitsInvented reports whether resource limits had to be made up. The
	// remediation proposer skips personas with no limits, so a persona without
	// them can never heal.
	LimitsInvented bool
}

// persona is the intermediate shape between a Deployment and the rendered YAML.
type persona struct {
	name        string
	appName     string
	namespace   string
	appType     string
	description string
	resources   resources
	minReplicas int32
	maxReplicas int32
	health      *health
	ports       []port
	ownership   ownership
	image       string
}

type resources struct {
	requestsCPU    string
	requestsMemory string
	limitsCPU      string
	limitsMemory   string
}

type health struct {
	livenessPath  string
	readinessPath string
	port          int32
}

type port struct {
	number   int32
	protocol string
}

type ownership struct {
	team       string
	owner      string
	repository string
}

// FromDeployment synthesizes a persona for the given Deployment. appName is the
// value the operator will resolve Deployments by: the caller picks it, because
// only the caller can see the rest of the namespace and check that it resolves
// back to this Deployment and no other.
func FromDeployment(deploy *appsv1.Deployment, appName, namespace string) Result {
	if namespace == "" {
		namespace = deploy.Namespace
	}
	if appName == "" {
		appName = deploy.Name
	}

	container := primaryContainer(deploy)

	p := persona{
		name:        deploy.Name,
		appName:     appName,
		namespace:   namespace,
		description: fmt.Sprintf("Imported from Deployment %s/%s", namespace, deploy.Name),
		ownership:   ownershipFrom(deploy),
	}

	var warnings []string

	res, limitsInvented, resWarnings := resourcesFrom(container)
	p.resources = res
	warnings = append(warnings, resWarnings...)

	p.minReplicas, p.maxReplicas = replicasFrom(deploy)
	p.ports = portsFrom(container)
	p.appType = appTypeFor(p.ports)
	p.health = healthFrom(container)
	if container != nil {
		p.image = container.Image
	}

	if p.health == nil {
		warnings = append(warnings, fmt.Sprintf(
			"%s has no HTTP liveness or readiness probe; health checks were left out of the persona",
			deploy.Name))
	}

	return Result{
		Name:           p.name,
		AppName:        p.appName,
		Namespace:      p.namespace,
		Deployment:     deploy.Name,
		YAML:           p.render(),
		Warnings:       warnings,
		LimitsInvented: limitsInvented,
	}
}

// primaryContainer returns the container the persona describes: the one named
// after the Deployment if there is one, otherwise the first. Returns nil for a
// Deployment with no containers.
func primaryContainer(deploy *appsv1.Deployment) *corev1.Container {
	containers := deploy.Spec.Template.Spec.Containers
	if len(containers) == 0 {
		return nil
	}
	for i := range containers {
		if containers[i].Name == deploy.Name {
			return &containers[i]
		}
	}
	return &containers[0]
}

// resourcesFrom reads requests and limits off the container. Limits matter more
// than the rest: the remediation proposer skips any persona without them, so a
// persona imported with no limits could never heal. When the container declares
// none we derive them, and the caller says so out loud.
func resourcesFrom(container *corev1.Container) (resources, bool, []string) {
	if container == nil {
		return resources{limitsCPU: DefaultCPULimit, limitsMemory: DefaultMemoryLimit}, true,
			[]string{"Deployment has no containers; resource limits are Dorgu defaults, not observed values"}
	}

	res := resources{
		requestsCPU:    quantityString(container.Resources.Requests, corev1.ResourceCPU),
		requestsMemory: quantityString(container.Resources.Requests, corev1.ResourceMemory),
		limitsCPU:      quantityString(container.Resources.Limits, corev1.ResourceCPU),
		limitsMemory:   quantityString(container.Resources.Limits, corev1.ResourceMemory),
	}

	if res.limitsCPU != "" && res.limitsMemory != "" {
		return res, false, nil
	}

	var warnings []string
	invented := false

	fill := func(limit *string, request, fallback string, name string) {
		if *limit != "" {
			return
		}
		invented = true
		if derived, ok := multiplyQuantity(request, LimitFromRequestMultiplier); ok {
			*limit = derived
			warnings = append(warnings, fmt.Sprintf(
				"container %q sets no %s limit; the persona uses %s (%dx its %s request of %s)",
				container.Name, name, derived, LimitFromRequestMultiplier, name, request))
			return
		}
		*limit = fallback
		warnings = append(warnings, fmt.Sprintf(
			"container %q sets no %s request or limit; the persona uses the Dorgu default of %s",
			container.Name, name, fallback))
	}

	fill(&res.limitsCPU, res.requestsCPU, DefaultCPULimit, "cpu")
	fill(&res.limitsMemory, res.requestsMemory, DefaultMemoryLimit, "memory")

	if invented {
		warnings = append(warnings,
			"review these limits before relying on them: remediation resizes from the persona, not from the workload")
	}
	return res, invented, warnings
}

// quantityString renders one resource quantity, or "" when it is unset.
func quantityString(list corev1.ResourceList, name corev1.ResourceName) string {
	q, ok := list[name]
	if !ok || q.IsZero() {
		return ""
	}
	return q.String()
}

// multiplyQuantity scales a resource quantity string by n, preserving its
// format (so 25m stays milli-CPU and 32Mi stays binary SI).
func multiplyQuantity(value string, n int64) (string, bool) {
	if value == "" {
		return "", false
	}
	q, err := resource.ParseQuantity(value)
	if err != nil {
		return "", false
	}
	scaled := q.DeepCopy()
	scaled.Set(0)
	for i := int64(0); i < n; i++ {
		scaled.Add(q)
	}
	scaled.Format = q.Format
	return scaled.String(), true
}

// replicasFrom captures the replica count as it is right now. Both bounds are
// the observed value: describing what is running is honest, inventing headroom
// is not. A Deployment scaled to zero still needs maxReplicas >= 1 to satisfy
// the CRD schema.
func replicasFrom(deploy *appsv1.Deployment) (minReplicas, maxReplicas int32) {
	replicas := int32(1)
	if deploy.Spec.Replicas != nil {
		replicas = *deploy.Spec.Replicas
	}
	maxReplicas = replicas
	if maxReplicas < 1 {
		maxReplicas = 1
	}
	return replicas, maxReplicas
}

// portsFrom reads the container's declared ports.
func portsFrom(container *corev1.Container) []port {
	if container == nil {
		return nil
	}
	ports := make([]port, 0, len(container.Ports))
	for _, p := range container.Ports {
		protocol := string(p.Protocol)
		if protocol == "" {
			protocol = "TCP"
		}
		ports = append(ports, port{number: p.ContainerPort, protocol: protocol})
	}
	return ports
}

// appTypeFor infers the workload type. A Deployment exposing no port is not
// serving traffic, which makes it a worker; anything else is classified as an
// api. The CRD enum has no "unknown", and the field does not change behaviour,
// so the cost of guessing wrong here is cosmetic.
func appTypeFor(ports []port) string {
	if len(ports) == 0 {
		return "worker"
	}
	return "api"
}

// healthFrom reads probe paths off the container. Only HTTP probes map onto the
// persona's health spec; exec and TCP probes have no path to record.
func healthFrom(container *corev1.Container) *health {
	if container == nil {
		return nil
	}

	h := &health{}
	if p := httpProbe(container.LivenessProbe); p != nil {
		h.livenessPath = p.Path
		h.port = p.Port.IntVal
	}
	if p := httpProbe(container.ReadinessProbe); p != nil {
		h.readinessPath = p.Path
		if h.port == 0 {
			h.port = p.Port.IntVal
		}
	}
	if h.livenessPath == "" && h.readinessPath == "" {
		return nil
	}
	return h
}

func httpProbe(probe *corev1.Probe) *corev1.HTTPGetAction {
	if probe == nil {
		return nil
	}
	return probe.HTTPGet
}

// ownershipKeys maps persona ownership fields to the labels and annotations
// they are conventionally recorded in, most specific first.
var (
	teamKeys  = []string{"dorgu.io/team", "app.kubernetes.io/part-of", "team", "owner-team"}
	ownerKeys = []string{
		"dorgu.io/owner", "owner", "app.kubernetes.io/owner",
		"kubernetes.io/created-by",
	}
	repositoryKeys = []string{
		"dorgu.io/repository", "app.kubernetes.io/repository",
		"argocd.argoproj.io/instance-repo-url", "repository",
	}
)

// ownershipFrom reads ownership out of the labels and annotations that are
// already on the Deployment. Nothing is invented: absent keys stay absent.
func ownershipFrom(deploy *appsv1.Deployment) ownership {
	lookup := func(keys []string) string {
		for _, k := range keys {
			if v := deploy.Annotations[k]; v != "" {
				return v
			}
			if v := deploy.Labels[k]; v != "" {
				return v
			}
		}
		return ""
	}
	return ownership{
		team:       lookup(teamKeys),
		owner:      lookup(ownerKeys),
		repository: lookup(repositoryKeys),
	}
}

// SortResults orders results by Deployment name so bulk output is stable.
func SortResults(results []Result) []Result {
	out := append([]Result(nil), results...)
	sort.Slice(out, func(i, j int) bool { return out[i].Deployment < out[j].Deployment })
	return out
}

// JoinYAML concatenates rendered personas into a single multi-document file.
func JoinYAML(results []Result) string {
	docs := make([]string, 0, len(results))
	for _, r := range results {
		docs = append(docs, strings.TrimRight(r.YAML, "\n"))
	}
	return strings.Join(docs, "\n---\n") + "\n"
}
