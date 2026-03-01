package setup

import (
	"testing"
	"time"
)

func TestBlessedStackOrder(t *testing.T) {
	components := blessedComponents()
	if len(components) != 5 {
		t.Fatalf("expected 5 components, got %d", len(components))
	}
	if components[0].ID != ComponentCertManager {
		t.Errorf("index 0: expected %q, got %q", ComponentCertManager, components[0].ID)
	}
	if components[1].ID != ComponentIngressNginx {
		t.Errorf("index 1: expected %q, got %q", ComponentIngressNginx, components[1].ID)
	}
	if components[2].ID != ComponentOpenObserve {
		t.Errorf("index 2: expected %q, got %q", ComponentOpenObserve, components[2].ID)
	}
	if components[3].ID != ComponentArgoCd {
		t.Errorf("index 3: expected %q, got %q", ComponentArgoCd, components[3].ID)
	}
	if components[4].ID != ComponentExternalSecrets {
		t.Errorf("index 4: expected %q, got %q", ComponentExternalSecrets, components[4].ID)
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

func TestArgoCdOptionalDefaultOn(t *testing.T) {
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
	if argocd.Required {
		t.Error("argocd should not be Required")
	}
	if !argocd.DefaultEnabled {
		t.Error("argocd should be DefaultEnabled")
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
