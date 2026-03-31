package cli

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPrintHealthSummary(t *testing.T) {
	summary := &healthSummary{
		Nodes: []healthNode{
			{Name: "node-1", Status: "Ready", Roles: "control-plane", Age: "5d"},
			{Name: "node-2", Status: "Ready", Roles: "worker", Age: "5d"},
			{Name: "node-3", Status: "NotReady", Roles: "worker", Age: "5d"},
		},
		ResourceSaturation: &resourceSaturation{
			CPU: &saturationDetail{
				Percentage:  "62%",
				Used:        "3200m",
				Allocatable: "5200m",
			},
			Memory: &saturationDetail{
				Percentage:  "74%",
				Used:        "8.2Gi",
				Allocatable: "11.1Gi",
			},
		},
		ControlPlane: &controlPlaneStatus{
			Healthy: true,
			Components: []controlPlaneComponent{
				{Name: "API Server", Healthy: true},
				{Name: "Scheduler", Healthy: true},
				{Name: "Controller Manager", Healthy: true},
				{Name: "etcd", Healthy: true},
			},
		},
		ActiveIncidents: &incidentsSummary{
			Count: 2,
			Items: []incidentBrief{
				{
					Severity:  "critical",
					Category:  "resource",
					Signal:    "OOMKilled",
					Persona:   "api-server",
					Namespace: "default",
					Age:       "5m",
				},
				{
					Severity:  "warning",
					Category:  "health",
					Signal:    "CrashLoopBackOff",
					Persona:   "web-frontend",
					Namespace: "default",
					Age:       "1h",
				},
			},
		},
		PendingRemediations: &remediationSummary{Count: 0},
	}

	var buf bytes.Buffer
	printHealthSummary(&buf, summary)
	out := buf.String()

	assert.Contains(t, out, "Cluster Health Summary")
	assert.Contains(t, out, "node-1")
	assert.Contains(t, out, "node-2")
	assert.Contains(t, out, "node-3")
	assert.Contains(t, out, "Ready")
	assert.Contains(t, out, "NotReady")
	assert.Contains(t, out, "Resource Saturation:")
	assert.Contains(t, out, "62%")
	assert.Contains(t, out, "3200m")
	assert.Contains(t, out, "74%")
	assert.Contains(t, out, "Control Plane:")
	assert.Contains(t, out, "API Server")
	assert.Contains(t, out, "Active Incidents: 2")
	assert.Contains(t, out, "OOMKilled")
	assert.Contains(t, out, "CrashLoopBackOff")
	assert.Contains(t, out, "Pending Remediations: 0")
}

func TestPrintHealthSummaryMinimal(t *testing.T) {
	summary := &healthSummary{
		Nodes:               nil,
		ControlPlane:        nil,
		ActiveIncidents:     &incidentsSummary{Count: 0},
		PendingRemediations: &remediationSummary{Count: 0},
	}

	var buf bytes.Buffer
	printHealthSummary(&buf, summary)
	out := buf.String()

	assert.Contains(t, out, "Cluster Health Summary")
	assert.Contains(t, out, "Active Incidents: 0")
	assert.Contains(t, out, "Pending Remediations: 0")
}

func TestPrintHealthSummaryUnhealthyControlPlane(t *testing.T) {
	summary := &healthSummary{
		ControlPlane: &controlPlaneStatus{
			Healthy: false,
			Components: []controlPlaneComponent{
				{Name: "API Server", Healthy: true},
				{Name: "Scheduler", Healthy: false},
			},
		},
		ActiveIncidents:     &incidentsSummary{Count: 0},
		PendingRemediations: &remediationSummary{Count: 0},
	}

	var buf bytes.Buffer
	printHealthSummary(&buf, summary)
	out := buf.String()

	assert.Contains(t, out, "Unhealthy")
	assert.Contains(t, out, "API Server")
	assert.Contains(t, out, "Scheduler")
}

func TestPrintHealthSummaryResourceSaturationPartial(t *testing.T) {
	summary := &healthSummary{
		ResourceSaturation: &resourceSaturation{
			CPU: &saturationDetail{
				Percentage:  "45%",
				Used:        "2000m",
				Allocatable: "4400m",
			},
		},
		ActiveIncidents:     &incidentsSummary{Count: 0},
		PendingRemediations: &remediationSummary{Count: 0},
	}

	var buf bytes.Buffer
	printHealthSummary(&buf, summary)
	out := buf.String()

	assert.Contains(t, out, "CPU:")
	assert.Contains(t, out, "45%")
	assert.NotContains(t, out, "Memory:")
}

func TestNodeRoles(t *testing.T) {
	tests := []struct {
		name     string
		labels   map[string]string
		expected string
	}{
		{
			name:     "control plane",
			labels:   map[string]string{"node-role.kubernetes.io/control-plane": ""},
			expected: "control-plane",
		},
		{
			name:     "worker role",
			labels:   map[string]string{"node-role.kubernetes.io/worker": "true"},
			expected: "worker",
		},
		{
			name:     "no roles",
			labels:   map[string]string{"kubernetes.io/os": "linux"},
			expected: "<none>",
		},
		{
			name:     "nil labels",
			labels:   nil,
			expected: "<none>",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := nodeRoles(tc.labels)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestFriendlyComponentName(t *testing.T) {
	assert.Equal(t, "API Server", friendlyComponentName("kube-apiserver"))
	assert.Equal(t, "Scheduler", friendlyComponentName("kube-scheduler"))
	assert.Equal(t, "Controller Manager", friendlyComponentName("kube-controller-manager"))
	assert.Equal(t, "etcd", friendlyComponentName("etcd"))
	assert.Equal(t, "custom", friendlyComponentName("custom"))
}

func TestNewHealthCmdFlags(t *testing.T) {
	cmd := newHealthCmd()
	assert.Equal(t, "health", cmd.Use)

	ns := cmd.Flags().Lookup("namespace")
	assert.NotNil(t, ns)
	assert.Equal(t, "n", ns.Shorthand)

	kc := cmd.Flags().Lookup("kubeconfig")
	assert.NotNil(t, kc)
}
