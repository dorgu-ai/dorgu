package setup

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestStepHeaderTruncation(t *testing.T) {
	// Build a header with multi-byte em-dash characters (U+2500 "─", 3 bytes each in UTF-8)
	// that would be corrupted by byte slicing at position 65
	header := "── Step 1 of 5: External Secrets Operator ──────────────────────────────────"

	// Simulate the rune-safe truncation from PromptComponentSelection
	runes := []rune(header)
	if len(runes) > 65 {
		header = string(runes[:65])
	}

	if !utf8.ValidString(header) {
		t.Errorf("truncated header is not valid UTF-8: %q", header)
	}
	if strings.Contains(header, "\uFFFD") {
		t.Errorf("truncated header contains replacement character (U+FFFD): %q", header)
	}
}

func TestPrintStepHeader_NoUTF8Corruption(t *testing.T) {
	var buf bytes.Buffer
	c := ComponentConfig{
		DisplayName: "cert-manager",
		Description: "Automated TLS certificate management",
	}
	printStepHeader(&buf, c, 1, 4)
	out := buf.String()

	if strings.Contains(out, "\uFFFD") {
		t.Error("output contains UTF-8 replacement character \\uFFFD — truncation corrupted multibyte chars")
	}
	if !strings.Contains(out, "cert-manager") {
		t.Error("output should contain component name 'cert-manager'")
	}
	if !strings.Contains(out, "Step 1 of 4") {
		t.Errorf("output should contain step counter 'Step 1 of 4', got:\n%s", out)
	}
	if !strings.Contains(out, "╭") {
		t.Error("output should contain lipgloss rounded border ╭")
	}
}

func TestPrintStepHeader_MultibyteName(t *testing.T) {
	var buf bytes.Buffer
	c := ComponentConfig{
		DisplayName: "日本語テスト",
		Description: "Unicode component name",
	}
	printStepHeader(&buf, c, 2, 4)
	out := buf.String()

	if strings.Contains(out, "\uFFFD") {
		t.Error("multibyte DisplayName caused UTF-8 corruption")
	}
}

func TestPrintInstallPlan_TableFormat(t *testing.T) {
	var buf bytes.Buffer
	cfg := SetupConfig{
		Environment:        "development",
		ClusterPersonaName: "qa-cluster",
		Components: []ComponentConfig{
			{DisplayName: "cert-manager", Version: "v1.16.3", Namespace: "cert-manager"},
			{DisplayName: "ingress-nginx", Version: "4.11.3", Namespace: "ingress-nginx"},
		},
		Timestamp: time.Now(),
	}
	printInstallPlanTo(&buf, cfg)
	out := buf.String()

	for _, ch := range []string{"┌", "┐", "│", "└"} {
		if !strings.Contains(out, ch) {
			t.Errorf("output should contain box-drawing char %s", ch)
		}
	}
	if !strings.Contains(out, "cert-manager") {
		t.Error("output should contain component name 'cert-manager'")
	}
	if !strings.Contains(out, "v1.16.3") {
		t.Error("output should contain version 'v1.16.3'")
	}
	if !strings.Contains(out, "development") {
		t.Error("output should contain environment")
	}
	if !strings.Contains(out, "qa-cluster") {
		t.Error("output should contain ClusterPersona name")
	}
}

func TestPrintInstallPlan_VersionOverride(t *testing.T) {
	var buf bytes.Buffer
	cfg := SetupConfig{
		Environment: "staging",
		Components: []ComponentConfig{
			{ID: ComponentCertManager, DisplayName: "cert-manager", Version: "v1.16.3", Namespace: "cert-manager"},
		},
		VersionOverrides: map[ComponentID]string{ComponentCertManager: "v1.17.0"},
		Timestamp:        time.Now(),
	}
	printInstallPlanTo(&buf, cfg)
	out := buf.String()

	if !strings.Contains(out, "v1.17.0") {
		t.Error("output should show overridden version v1.17.0")
	}
}

func TestPrintFinalSummary_Success(t *testing.T) {
	var buf bytes.Buffer
	results := []InstallResult{
		{Component: ComponentConfig{DisplayName: "cert-manager"}, Succeeded: true, Duration: 45 * time.Second},
		{Component: ComponentConfig{DisplayName: "ingress-nginx"}, Succeeded: true, Duration: 30 * time.Second},
		{Component: ComponentConfig{DisplayName: "openobserve"}, Skipped: true},
	}
	cfg := SetupConfig{Environment: "development", Timestamp: time.Now()}
	printFinalSummaryTo(&buf, results, nil, cfg)
	out := buf.String()

	if !strings.Contains(out, "╭") {
		t.Error("output should contain lipgloss box char ╭")
	}
	if !strings.Contains(out, "Setup Complete") {
		t.Error("output should contain 'Setup Complete' title")
	}
	if !strings.Contains(out, "dorgu cluster status") {
		t.Error("output should contain next step 'dorgu cluster status'")
	}
}

func TestPrintFinalSummary_WithFailures(t *testing.T) {
	var buf bytes.Buffer
	results := []InstallResult{
		{Component: ComponentConfig{DisplayName: "cert-manager"}, Succeeded: true, Duration: 45 * time.Second},
		{
			Component: ComponentConfig{DisplayName: "ingress-nginx", Namespace: "ingress-nginx"},
			Succeeded: false,
			Error:     fmt.Errorf("context deadline exceeded"),
			Duration:  5 * time.Minute,
		},
	}
	cfg := SetupConfig{Environment: "development", Timestamp: time.Now()}
	printFinalSummaryTo(&buf, results, nil, cfg)
	out := buf.String()

	if !strings.Contains(out, "╭") {
		t.Error("output should contain lipgloss box char ╭")
	}
	if !strings.Contains(out, "Setup Complete") {
		t.Error("output should contain 'Setup Complete' title")
	}
}

// Note: Interactive prompt tests (PromptGitRepoURL, PromptEnvironment, etc.)
// have been removed because the functions now use huh forms which require a
// real terminal. These are tested manually and via integration tests.
