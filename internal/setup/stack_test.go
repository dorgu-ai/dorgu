package setup

import (
	"testing"
	"time"
)

func TestBlessedStackOrder(t *testing.T) {
	components := blessedComponents()
	if len(components) != 6 {
		t.Fatalf("expected 6 components, got %d", len(components))
	}
	if components[0].ID != ComponentCertManager {
		t.Errorf("index 0: expected %q, got %q", ComponentCertManager, components[0].ID)
	}
	if components[1].ID != ComponentIngressNginx {
		t.Errorf("index 1: expected %q, got %q", ComponentIngressNginx, components[1].ID)
	}
	if components[2].ID != ComponentCNPG {
		t.Errorf("index 2: expected %q, got %q", ComponentCNPG, components[2].ID)
	}
	if components[3].ID != ComponentOpenObserve {
		t.Errorf("index 3: expected %q, got %q", ComponentOpenObserve, components[3].ID)
	}
	if components[4].ID != ComponentArgoCd {
		t.Errorf("index 4: expected %q, got %q", ComponentArgoCd, components[4].ID)
	}
	if components[5].ID != ComponentExternalSecrets {
		t.Errorf("index 5: expected %q, got %q", ComponentExternalSecrets, components[5].ID)
	}
}

func TestBlessedStackCertManagerFirst(t *testing.T) {
	components := DefaultStack().Components()
	if len(components) == 0 {
		t.Fatal("expected non-empty component list")
	}
	if components[0].ID != ComponentCertManager {
		t.Errorf("Components()[0].ID = %q, want %q", components[0].ID, ComponentCertManager)
	}
}

func TestDefaultStackIsNotNil(t *testing.T) {
	s := DefaultStack()
	if s == nil {
		t.Fatal("DefaultStack() returned nil")
	}
}

func TestVersionOverrideApplied(t *testing.T) {
	ex := &DryRunExecutor{}
	comp := blessedComponents()[0] // cert-manager, Version = "v1.16.3"

	cfg := SetupConfig{
		VersionOverrides: map[ComponentID]string{
			ComponentCertManager: "v1.17.0",
		},
		Timestamp: time.Now(),
	}

	result := InstallComponent(ex, comp, cfg)
	// DryRunExecutor never fails
	if !result.Succeeded {
		t.Fatalf("InstallComponent unexpectedly failed: %v", result.Error)
	}
	// Check that the override version appears in the logged command
	found := false
	for _, cmd := range ex.Log {
		if contains(cmd, "v1.17.0") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("version override v1.17.0 not found in helm commands: %v", ex.Log)
	}
}

func TestAnnotationStack(t *testing.T) {
	cfg := SetupConfig{
		Components: []ComponentConfig{
			{ID: ComponentCertManager},
			{ID: ComponentIngressNginx},
			{ID: ComponentOpenObserve},
		},
	}
	got := cfg.AnnotationStack()
	want := "cert-manager,ingress-nginx,openobserve"
	if got != want {
		t.Errorf("AnnotationStack() = %q, want %q", got, want)
	}
}

func TestAnnotationStackFromResults(t *testing.T) {
	results := []InstallResult{
		{Component: ComponentConfig{ID: ComponentCertManager}, Succeeded: true},
		{Component: ComponentConfig{ID: ComponentIngressNginx}, Succeeded: true},
		{Component: ComponentConfig{ID: ComponentOpenObserve}, Succeeded: false, Skipped: true},
		{Component: ComponentConfig{ID: ComponentArgoCd}, Succeeded: true},
	}
	got := AnnotationStackFromResults(results)
	want := "cert-manager,ingress-nginx,argocd"
	if got != want {
		t.Errorf("AnnotationStackFromResults() = %q, want %q", got, want)
	}
}

func TestAnnotationSkippedFromResults(t *testing.T) {
	results := []InstallResult{
		{Component: ComponentConfig{ID: ComponentCertManager}, Succeeded: true},
		{Component: ComponentConfig{ID: ComponentOpenObserve}, Succeeded: false, Skipped: true},
		{Component: ComponentConfig{ID: ComponentExternalSecrets}, Succeeded: false},
	}
	got := AnnotationSkippedFromResults(results)
	want := "openobserve,external-secrets"
	if got != want {
		t.Errorf("AnnotationSkippedFromResults() = %q, want %q", got, want)
	}
}

func TestAnnotationSkippedFromResults_AllSucceeded(t *testing.T) {
	results := []InstallResult{
		{Component: ComponentConfig{ID: ComponentCertManager}, Succeeded: true},
	}
	got := AnnotationSkippedFromResults(results)
	if got != "" {
		t.Errorf("AnnotationSkippedFromResults() = %q, want empty", got)
	}
}

func TestOpenObserveEnvironmentOverrides(t *testing.T) {
	components := blessedComponents()
	var oo *ComponentConfig
	for i := range components {
		if components[i].ID == ComponentOpenObserve {
			oo = &components[i]
			break
		}
	}
	if oo == nil {
		t.Fatal("openobserve not found")
	}
	if oo.EnvironmentOverrides == nil {
		t.Fatal("openobserve should have EnvironmentOverrides")
	}
	devOverrides, ok := oo.EnvironmentOverrides["development"]
	if !ok || len(devOverrides) == 0 {
		t.Fatal("openobserve should have development overrides")
	}
	foundLocalMode := false
	for _, v := range devOverrides {
		if v == "config.ZO_LOCAL_MODE=true" {
			foundLocalMode = true
		}
	}
	if !foundLocalMode {
		t.Error("development overrides should include config.ZO_LOCAL_MODE=true")
	}
	sandboxOverrides, ok := oo.EnvironmentOverrides["sandbox"]
	if !ok || len(sandboxOverrides) == 0 {
		t.Error("openobserve should have sandbox overrides")
	}
	// staging/production should NOT have local mode overrides
	if _, ok := oo.EnvironmentOverrides["staging"]; ok {
		t.Error("staging should not have local mode overrides")
	}
	if _, ok := oo.EnvironmentOverrides["production"]; ok {
		t.Error("production should not have local mode overrides")
	}
}

func TestExternalSecretsOptional(t *testing.T) {
	components := blessedComponents()
	var es *ComponentConfig
	for i := range components {
		if components[i].ID == ComponentExternalSecrets {
			es = &components[i]
			break
		}
	}
	if es == nil {
		t.Fatal("external-secrets not found in blessedComponents()")
	}
	if es.Required {
		t.Error("external-secrets should not be Required")
	}
	if es.DefaultEnabled {
		t.Error("external-secrets should not be DefaultEnabled")
	}
}

func TestArgoCdRequired(t *testing.T) {
	components := DefaultStack().Components()
	var argocd *ComponentConfig
	for i := range components {
		if components[i].ID == ComponentArgoCd {
			argocd = &components[i]
			break
		}
	}
	if argocd == nil {
		t.Fatal("argocd not found in DefaultStack().Components()")
	}
	if !argocd.Required {
		t.Error("argocd should be Required")
	}
}

func TestCNPGComponent(t *testing.T) {
	components := DefaultStack().Components()
	var cnpg *ComponentConfig
	for i := range components {
		if components[i].ID == ComponentCNPG {
			cnpg = &components[i]
			break
		}
	}
	if cnpg == nil {
		t.Fatal("cnpg not found in DefaultStack().Components()")
	}
	if !cnpg.Required {
		t.Error("cnpg should be Required")
	}
	if cnpg.Namespace != "cnpg-system" {
		t.Errorf("cnpg namespace = %q, want %q", cnpg.Namespace, "cnpg-system")
	}
}

func TestOpenObserveDependsOnCNPG(t *testing.T) {
	components := DefaultStack().Components()
	var oo *ComponentConfig
	for i := range components {
		if components[i].ID == ComponentOpenObserve {
			oo = &components[i]
			break
		}
	}
	if oo == nil {
		t.Fatal("openobserve not found in DefaultStack().Components()")
	}
	found := false
	for _, dep := range oo.DependsOn {
		if dep == ComponentCNPG {
			found = true
			break
		}
	}
	if !found {
		t.Error("openobserve should DependsOn cnpg")
	}
}

func TestOpenObserveTimeoutIsSet(t *testing.T) {
	components := blessedComponents()
	for _, c := range components {
		if c.ID == ComponentOpenObserve {
			if c.Timeout == "" {
				t.Error("OpenObserve must have an explicit Timeout (deploys ~14 pods)")
			}
			d, err := time.ParseDuration(c.Timeout)
			if err != nil {
				t.Errorf("OpenObserve Timeout %q is not a valid duration: %v", c.Timeout, err)
			}
			if d < 10*time.Minute {
				t.Errorf("OpenObserve Timeout %v is too short (minimum 10m for ~14 pods)", d)
			}
			return
		}
	}
	t.Error("OpenObserve component not found in blessed stack")
}

func TestAllComplexChartsHaveAdequateTimeout(t *testing.T) {
	components := blessedComponents()
	for _, c := range components {
		if len(c.DependsOn) > 0 || c.ID == ComponentOpenObserve {
			if c.Timeout == "" {
				t.Errorf("Component %s has dependencies or is known-heavy but no explicit Timeout", c.ID)
			}
			d, err := time.ParseDuration(c.Timeout)
			if err != nil {
				t.Errorf("Component %s Timeout %q is not a valid duration: %v", c.ID, c.Timeout, err)
				continue
			}
			if d <= 5*time.Minute {
				t.Errorf("Component %s Timeout %v should be > 5m (has dependencies or is complex)", c.ID, d)
			}
		}
	}
}

// contains is a helper for substring search in tests.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr(s, substr))
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
