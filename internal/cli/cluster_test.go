package cli

import (
	"testing"

	k8syaml "sigs.k8s.io/yaml"
)

func TestParseClusterPersonaYAML(t *testing.T) {
	fixture := `
apiVersion: dorgu.io/v1
kind: ClusterPersona
metadata:
  name: qa-cluster
spec:
  name: qa-cluster
  environment: development
status:
  phase: Ready
  kubernetesVersion: v1.31.4
  platform: kind
  applicationCount: 2
  nodes:
    - name: kind-control-plane
    - name: kind-worker
    - name: kind-worker2
  addons:
    - name: cert-manager
      installed: true
      healthy: true
      version: v1.16.3
    - name: ingress-nginx
      installed: true
      healthy: true
      version: 4.11.3
    - name: openobserve
      installed: true
      healthy: true
      version: "0.60.0"
    - name: argocd
      installed: false
      healthy: false
  resourceSummary:
    runningPods: 42
`

	var cp clusterPersonaYAML
	if err := k8syaml.Unmarshal([]byte(fixture), &cp); err != nil {
		t.Fatalf("failed to parse ClusterPersona YAML: %v", err)
	}

	s := cp.Status
	if s.Phase != "Ready" {
		t.Errorf("phase = %q, want %q", s.Phase, "Ready")
	}
	if s.KubernetesVersion != "v1.31.4" {
		t.Errorf("kubernetesVersion = %q, want %q", s.KubernetesVersion, "v1.31.4")
	}
	if s.Platform != "kind" {
		t.Errorf("platform = %q, want %q", s.Platform, "kind")
	}
	if len(s.Nodes) != 3 {
		t.Errorf("node count = %d, want 3", len(s.Nodes))
	}
	if s.ApplicationCount != 2 {
		t.Errorf("applicationCount = %d, want 2", s.ApplicationCount)
	}
	if len(s.Addons) != 4 {
		t.Errorf("addon count = %d, want 4", len(s.Addons))
	}
	if s.Addons[0].Name != "cert-manager" {
		t.Errorf("addons[0].name = %q, want %q", s.Addons[0].Name, "cert-manager")
	}
	if s.ResourceSummary.RunningPods != 42 {
		t.Errorf("runningPods = %d, want 42", s.ResourceSummary.RunningPods)
	}
}
