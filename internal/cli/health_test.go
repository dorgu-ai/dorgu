package cli

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrintHealthSummary(t *testing.T) {
	summary := &healthSummary{
		Nodes: []healthNode{
			{Name: "node-1", Status: "Ready", Roles: "control-plane", Age: "5d"},
			{Name: "node-2", Status: "Ready", Roles: "worker", Age: "5d"},
			{Name: "node-3", Status: "NotReady", Roles: "worker", Age: "5d"},
		},
		ResourceSaturation: &resourceSaturation{
			Nodes:         3,
			ScheduledPods: 24,
			CPU: &saturationDetail{
				Allocatable:      "5200m",
				Requested:        "3200m",
				RequestedPercent: "62%",
				Used:             "410m",
				UsedPercent:      "8%",
			},
			Memory: &saturationDetail{
				Allocatable:      "11.1Gi",
				Requested:        "8.2Gi",
				RequestedPercent: "74%",
				Used:             "2.1Gi",
				UsedPercent:      "19%",
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
	assert.Contains(t, out, "3200m / 5200m (62%)")
	assert.Contains(t, out, "410m / 5200m (8%)")
	assert.Contains(t, out, "8.2Gi / 11.1Gi (74%)")
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
			Nodes:         1,
			ScheduledPods: 6,
			CPU: &saturationDetail{
				Allocatable:      "4400m",
				Requested:        "2000m",
				RequestedPercent: "45%",
			},
		},
		ActiveIncidents:     &incidentsSummary{Count: 0},
		PendingRemediations: &remediationSummary{Count: 0},
	}

	var buf bytes.Buffer
	printHealthSummary(&buf, summary)
	out := buf.String()

	assert.Contains(t, out, "CPU")
	assert.Contains(t, out, "2000m / 4400m (45%)")
	assert.NotContains(t, out, "Memory")
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

func TestValidateKubeconfig(t *testing.T) {
	// Empty path returns empty.
	path, err := validateKubeconfig("")
	assert.NoError(t, err)
	assert.Equal(t, "", path)

	// Non-existent file returns error.
	_, err = validateKubeconfig("/nonexistent/path/kubeconfig")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "kubeconfig file not found")

	// Path traversal is cleaned.
	_, err = validateKubeconfig("/tmp/../nonexistent/path")
	assert.Error(t, err)
}

func TestNewHealthCmdFlags(t *testing.T) {
	cmd := newHealthCmd()
	assert.Equal(t, "health", cmd.Use)

	ns := cmd.Flags().Lookup("namespace")
	assert.NotNil(t, ns)
	assert.Equal(t, "n", ns.Shorthand)

	kc := cmd.Flags().Lookup("kubeconfig")
	assert.NotNil(t, kc)

	// --watch streamed over the operator WebSocket and was removed in v0.13.0.
	assert.Nil(t, cmd.Flags().Lookup("watch"))
	assert.Nil(t, cmd.Flags().Lookup("operator-url"))
}

func TestParseControlPlanePods_NoPodsExternalInferred(t *testing.T) {
	out := []byte(`{"items":[]}`)
	cp, err := parseControlPlanePods(out)
	require.NoError(t, err)
	assert.True(t, cp.Healthy)
	assert.True(t, cp.External)
	assert.Len(t, cp.Components, 4)
	for _, c := range cp.Components {
		assert.True(t, c.Healthy)
		assert.Equal(t, "external", c.Status)
	}
}

func TestParseControlPlanePods_SelfHostedHealthy(t *testing.T) {
	out := []byte(`{"items":[
		{"metadata":{"labels":{"component":"kube-apiserver"}},"status":{"phase":"Running","containerStatuses":[{"ready":true}]}},
		{"metadata":{"labels":{"component":"kube-scheduler"}},"status":{"phase":"Running","containerStatuses":[{"ready":true}]}},
		{"metadata":{"labels":{"component":"kube-controller-manager"}},"status":{"phase":"Running","containerStatuses":[{"ready":true}]}},
		{"metadata":{"labels":{"component":"etcd"}},"status":{"phase":"Running","containerStatuses":[{"ready":true}]}}
	]}`)
	cp, err := parseControlPlanePods(out)
	require.NoError(t, err)
	assert.True(t, cp.Healthy)
	assert.False(t, cp.External)
	assert.Len(t, cp.Components, 4)
	for _, c := range cp.Components {
		assert.True(t, c.Healthy)
		assert.Empty(t, c.Status)
	}
}

func TestParseControlPlanePods_ParseError(t *testing.T) {
	_, err := parseControlPlanePods([]byte(`invalid json`))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse control plane pods")
}

func TestPrintHealthSummaryExternalControlPlane(t *testing.T) {
	summary := &healthSummary{
		ControlPlane: &controlPlaneStatus{
			Healthy:  true,
			External: true,
			Components: []controlPlaneComponent{
				{Name: "API Server", Healthy: true, Status: "external"},
				{Name: "Scheduler", Healthy: true, Status: "external"},
				{Name: "Controller Manager", Healthy: true, Status: "external"},
				{Name: "etcd", Healthy: true, Status: "external"},
			},
		},
		ActiveIncidents:     &incidentsSummary{Count: 0},
		PendingRemediations: &remediationSummary{Count: 0},
	}

	var buf bytes.Buffer
	printHealthSummary(&buf, summary)
	out := buf.String()

	assert.Contains(t, out, "external/managed")
	assert.Contains(t, out, "(inferred)")
}

// Saturation is computed from nodes and pods now, not read from ClusterPersona
// (CR-03). The computation and its rendering are covered in
// health_saturation_test.go; this pins the wiring: parseNodes and the saturation
// figures read the same node list, so the two sections cannot disagree about the
// cluster they describe.
func TestParseNodesAndCapacityShareOneNodeList(t *testing.T) {
	nodes, err := parseNodes([]byte(twoNodeListFixture))
	require.NoError(t, err)
	require.Len(t, nodes, 2)
	assert.Equal(t, "ip-10-0-1-10", nodes[0].Name)

	capacity, err := parseNodeCapacity([]byte(twoNodeListFixture))
	require.NoError(t, err)
	assert.Equal(t, len(nodes), capacity.Nodes)
}

func TestParseNodesRejectsGarbage(t *testing.T) {
	_, err := parseNodes([]byte(`not json`))
	assert.Error(t, err)
}
