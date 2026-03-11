package cli

import (
	"bytes"
	"strings"
	"testing"

	k8syaml "sigs.k8s.io/yaml"
)

const fixtureClusterPersonaYAML = `
apiVersion: dorgu.io/v1
kind: ClusterPersona
metadata:
  name: qa-cluster
  creationTimestamp: "2026-01-15T10:00:00Z"
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
      ready: true
      role: control-plane
    - name: kind-worker
      ready: true
      role: worker
    - name: kind-worker2
      ready: false
      role: worker
  addons:
    - name: cert-manager
      installed: true
      healthy: true
      version: v1.16.3
      namespace: cert-manager
    - name: ingress-nginx
      installed: true
      healthy: true
      version: 4.11.3
      namespace: ingress-nginx
    - name: openobserve
      installed: true
      healthy: true
      version: "0.60.0"
      namespace: openobserve
    - name: external-secrets
      installed: false
      healthy: false
  resourceSummary:
    totalCPU: "8"
    totalMemory: "16Gi"
    allocatableCPU: "4"
    allocatableMemory: "6.2Gi"
    runningPods: 42
`

func TestParseClusterPersonaYAML(t *testing.T) {
	var cp clusterPersonaYAML
	if err := k8syaml.Unmarshal([]byte(fixtureClusterPersonaYAML), &cp); err != nil {
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

func TestDisplayClusterPersonaStatus_Formatted(t *testing.T) {
	var buf bytes.Buffer
	displayClusterPersonaStatusTo(&buf, "qa-cluster", fixtureClusterPersonaYAML)
	out := buf.String()

	// Header box chars
	if !strings.Contains(out, "╭") {
		t.Error("output should contain header box char ╭")
	}
	if !strings.Contains(out, "╰") {
		t.Error("output should contain header box char ╰")
	}

	// Cluster name in header
	if !strings.Contains(out, "qa-cluster") {
		t.Error("output should contain cluster name")
	}

	// Phase dot
	if !strings.Contains(out, "●") {
		t.Error("output should contain phase dot ●")
	}

	// Node count (3 total, 2 ready)
	if !strings.Contains(out, "3 (2 ready)") {
		t.Errorf("output should contain node count '3 (2 ready)', got:\n%s", out)
	}

	// Add-ons section
	if !strings.Contains(out, "cert-manager") {
		t.Error("output should contain addon cert-manager")
	}
	if !strings.Contains(out, "✓") {
		t.Error("output should contain ✓ for installed addons")
	}
	if !strings.Contains(out, "✗") {
		t.Error("output should contain ✗ for uninstalled addons")
	}
	if !strings.Contains(out, "not installed") {
		t.Error("output should contain 'not installed' for external-secrets")
	}

	// Next Steps section
	if !strings.Contains(out, "Next Steps") {
		t.Error("output should contain Next Steps section")
	}
	// external-secrets not installed → should suggest dorgu cluster setup
	if !strings.Contains(out, "dorgu cluster setup") {
		t.Error("output should suggest dorgu cluster setup when addons are missing")
	}
}

func TestFormatAge(t *testing.T) {
	tests := []struct {
		ts      string
		wantLen bool // just verify non-empty
	}{
		{"", false},
		{"not-a-date", false},
	}
	for _, tt := range tests {
		result := formatAge(tt.ts)
		if tt.wantLen && result == "" {
			t.Errorf("formatAge(%q) = empty, want non-empty", tt.ts)
		}
		if !tt.wantLen && result != "?" {
			t.Errorf("formatAge(%q) = %q, want '?'", tt.ts, result)
		}
	}
}
