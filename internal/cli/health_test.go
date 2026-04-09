package cli

import (
	"bytes"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dorgu-ai/dorgu/internal/ws"
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

	watch := cmd.Flags().Lookup("watch")
	assert.NotNil(t, watch)
	assert.Equal(t, "w", watch.Shorthand)
	assert.Equal(t, "false", watch.DefValue)

	operatorURL := cmd.Flags().Lookup("operator-url")
	assert.NotNil(t, operatorURL)
	assert.Equal(t, "ws://localhost:9090/ws", operatorURL.DefValue)
}

func TestPrintIncidentEvent(t *testing.T) {
	ts := time.Date(2026, 4, 2, 10, 15, 2, 0, time.UTC)
	event := ws.IncidentEvent{
		EventType:   "created",
		Name:        "im-default-api-oom-abc123",
		Namespace:   "production",
		Severity:    "critical",
		Category:    "resource",
		Signal:      "OOMKilled",
		Phase:       "Detected",
		PersonaName: "api-server",
		PersonaKind: "ApplicationPersona",
		Summary:     "Pod OOMKilled due to memory pressure",
	}

	// Capture stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	printIncidentEvent(ts, event)

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	out := buf.String()

	assert.Contains(t, out, "10:15:02")
	assert.Contains(t, out, "INCIDENT")
	assert.Contains(t, out, "OOMKilled")
	assert.Contains(t, out, "production/api-server")
	assert.Contains(t, out, "Detected")
}

func TestPrintIncidentEvent_Resolved(t *testing.T) {
	ts := time.Date(2026, 4, 2, 10, 25, 3, 0, time.UTC)
	event := ws.IncidentEvent{
		EventType:   "resolved",
		Name:        "im-default-api-oom-abc123",
		Namespace:   "production",
		Severity:    "critical",
		Signal:      "OOMKilled",
		PersonaName: "api-server",
		PersonaKind: "ApplicationPersona",
	}

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	printIncidentEvent(ts, event)

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	out := buf.String()

	assert.Contains(t, out, "10:25:03")
	assert.Contains(t, out, "INCIDENT")
	assert.Contains(t, out, "Resolved")
}

func TestPrintRemediationEvent(t *testing.T) {
	ts := time.Date(2026, 4, 2, 10, 15, 5, 0, time.UTC)
	event := ws.RemediationEvent{
		EventType:   "created",
		Name:        "ra-fix-oom-api",
		Namespace:   "production",
		Phase:       "Pending",
		ActionType:  "persona-update",
		Confidence:  "0.85",
		PersonaName: "api-server",
	}

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	printRemediationEvent(ts, event)

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	out := buf.String()

	assert.Contains(t, out, "10:15:05")
	assert.Contains(t, out, "REMEDY")
	assert.Contains(t, out, "persona-update")
	assert.Contains(t, out, "production/api-server")
	assert.Contains(t, out, "Pending")
}

func TestPrintRemediationEvent_Completed(t *testing.T) {
	ts := time.Date(2026, 4, 2, 10, 25, 2, 0, time.UTC)
	event := ws.RemediationEvent{
		EventType:   "completed",
		Name:        "ra-fix-oom-api",
		Namespace:   "production",
		ActionType:  "persona-update",
		PersonaName: "api-server",
	}

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	printRemediationEvent(ts, event)

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	out := buf.String()

	assert.Contains(t, out, "Completed")
}

func TestPrintHealthUpdateEvent(t *testing.T) {
	ts := time.Date(2026, 4, 2, 10, 16, 0, 0, time.UTC)
	event := ws.HealthUpdateEvent{
		EventType:       "health-update",
		ActiveIncidents: 2,
		PendingRemedies: 1,
		NodeCount:       3,
		HealthyNodes:    2,
		CPUUtilization:  "65%",
		MemUtilization:  "78%",
	}

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	printHealthUpdateEvent(ts, event)

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	out := buf.String()

	assert.Contains(t, out, "10:16:00")
	assert.Contains(t, out, "HEALTH")
	assert.Contains(t, out, "incidents=2")
	assert.Contains(t, out, "pending-remedies=1")
	assert.Contains(t, out, "nodes=2/3")
	assert.Contains(t, out, "cpu=65%")
	assert.Contains(t, out, "mem=78%")
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

func TestParseResourceSaturation_EmptyUsed_ShowsNA(t *testing.T) {
	out := []byte(`{"items":[{"status":{"resourceSummary":{"allocatableCPU":"4000m","allocatableMemory":"8Gi","usedCPU":"","usedMemory":"","cpuUtilization":"","memoryUtilization":""}}}]}`)
	sat, err := parseResourceSaturation(out)
	require.NoError(t, err)
	require.NotNil(t, sat)
	require.NotNil(t, sat.CPU)
	assert.Equal(t, "n/a", sat.CPU.Percentage)
	require.NotNil(t, sat.Memory)
	assert.Equal(t, "n/a", sat.Memory.Percentage)
}

func TestParseResourceSaturation_WithValues(t *testing.T) {
	out := []byte(`{"items":[{"status":{"resourceSummary":{"allocatableCPU":"4000m","allocatableMemory":"8Gi","usedCPU":"2000m","usedMemory":"4Gi","cpuUtilization":"50%","memoryUtilization":"50%"}}}]}`)
	sat, err := parseResourceSaturation(out)
	require.NoError(t, err)
	require.NotNil(t, sat)
	require.NotNil(t, sat.CPU)
	assert.Equal(t, "50%", sat.CPU.Percentage)
	assert.Equal(t, "2000m", sat.CPU.Used)
	require.NotNil(t, sat.Memory)
	assert.Equal(t, "50%", sat.Memory.Percentage)
}

func TestPrintHealthUpdateEvent_Healthy(t *testing.T) {
	ts := time.Date(2026, 4, 2, 10, 16, 0, 0, time.UTC)
	event := ws.HealthUpdateEvent{
		EventType:       "health-update",
		ActiveIncidents: 0,
		PendingRemedies: 0,
	}

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	printHealthUpdateEvent(ts, event)

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	out := buf.String()

	assert.Contains(t, out, "incidents=0")
	assert.Contains(t, out, "pending-remedies=0")
	assert.NotContains(t, out, "nodes=")
	assert.NotContains(t, out, "cpu=")
}
