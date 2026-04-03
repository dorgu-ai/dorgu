package setup

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestPrintFinalSummary_SkipValidation(t *testing.T) {
	results := []InstallResult{
		{Component: ComponentConfig{ID: "cert-manager", DisplayName: "cert-manager", Namespace: "cert-manager"}, Succeeded: true, Duration: 30 * time.Second},
		{Component: ComponentConfig{ID: "openobserve", DisplayName: "OpenObserve", Namespace: "openobserve"}, Succeeded: true, Duration: 60 * time.Second},
	}
	vrs := []ValidationResult{
		{ComponentID: "cert-manager", Healthy: true, Message: "skipped"},
		{ComponentID: "openobserve", Healthy: true, Message: "skipped"},
	}
	cfg := SetupConfig{
		SkipValidation: true,
	}

	var buf bytes.Buffer
	printFinalSummaryTo(&buf, results, vrs, cfg)
	output := buf.String()

	if !strings.Contains(output, "validation skipped") {
		t.Errorf("expected 'validation skipped' in output when SkipValidation=true, got:\n%s", output)
	}
	if strings.Contains(output, "0/2") {
		t.Error("should not show '0/2' when validation is skipped")
	}
}

func TestPrintFinalSummary_WithValidation(t *testing.T) {
	results := []InstallResult{
		{Component: ComponentConfig{ID: "cert-manager", DisplayName: "cert-manager", Namespace: "cert-manager"}, Succeeded: true, Duration: 30 * time.Second},
		{Component: ComponentConfig{ID: "openobserve", DisplayName: "OpenObserve", Namespace: "openobserve"}, Succeeded: true, Duration: 60 * time.Second},
	}
	vrs := []ValidationResult{
		{ComponentID: "cert-manager", Healthy: true, Message: "all pods running"},
		{ComponentID: "openobserve", Healthy: true, Message: "all pods running"},
	}
	cfg := SetupConfig{
		SkipValidation: false,
	}

	var buf bytes.Buffer
	printFinalSummaryTo(&buf, results, vrs, cfg)
	output := buf.String()

	if !strings.Contains(output, "2/2") {
		t.Errorf("expected 'Pods healthy: 2/2' in output, got:\n%s", output)
	}
	if strings.Contains(output, "validation skipped") {
		t.Error("should not show 'validation skipped' when validation was performed")
	}
}
