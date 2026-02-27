package setup

import (
	"fmt"
	"strings"
	"testing"
)

// mockExecutor is a simple Executor for tests that returns preconfigured output.
type mockExecutor struct {
	output string
	err    error
}

func (m *mockExecutor) Run(_ string, _ ...string) (string, error) {
	return m.output, m.err
}

func TestCheckPodsRunning_NoOutput(t *testing.T) {
	ex := &mockExecutor{output: ""}
	ok, msg := CheckPodsRunning(ex, "cert-manager", "cert-manager")
	if ok {
		t.Errorf("expected ok=false for empty output, got true")
	}
	if !strings.Contains(msg, "no pods found") {
		t.Errorf("expected 'no pods found' in message, got %q", msg)
	}
}

func TestCheckPodsRunning_RunningPod(t *testing.T) {
	// Simulate 2 running pods (2 non-empty lines)
	podOutput := "cert-manager-abc123   1/1   Running   0   5m\ncert-manager-webhook-def456   1/1   Running   0   5m"
	ex := &mockExecutor{output: podOutput}
	ok, msg := CheckPodsRunning(ex, "cert-manager", "cert-manager")
	if !ok {
		t.Errorf("expected ok=true for running pods, got false; msg: %q", msg)
	}
	if !strings.Contains(msg, "2 pod(s) running") {
		t.Errorf("expected '2 pod(s) running' in message, got %q", msg)
	}
}

func TestValidateAll_SkipFlag(t *testing.T) {
	ex := &mockExecutor{}
	results := []InstallResult{
		{Component: blessedComponents()[0], Succeeded: true},
		{Component: blessedComponents()[1], Succeeded: true},
	}

	vrs := ValidateAll(ex, results, true)
	if len(vrs) != len(results) {
		t.Fatalf("expected %d ValidationResults, got %d", len(results), len(vrs))
	}
	for _, vr := range vrs {
		if !vr.Healthy {
			t.Errorf("component %q: expected Healthy=true when skip=true, got false", vr.ComponentID)
		}
		if vr.Message != "skipped" {
			t.Errorf("component %q: expected Message='skipped', got %q", vr.ComponentID, vr.Message)
		}
	}
}

// Ensure mockExecutor satisfies Executor interface at compile time.
var _ Executor = (*mockExecutor)(nil)

// suppress unused import error
var _ = fmt.Sprintf
