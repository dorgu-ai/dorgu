package generator

import (
	"strings"
	"testing"

	"github.com/dorgu-ai/dorgu/internal/config"
	"github.com/dorgu-ai/dorgu/internal/types"
)

func TestGenerateIngress_Basic(t *testing.T) {
	analysis := &types.AppAnalysis{
		Name: "my-app",
		Ports: []types.Port{
			{Port: 8080, Protocol: "TCP"},
		},
	}
	cfg := config.Default()
	cfg.Ingress.DomainSuffix = ".example.com"

	yaml, err := GenerateIngress(analysis, "default", cfg)
	if err != nil {
		t.Fatalf("GenerateIngress() error = %v", err)
	}

	if !strings.Contains(yaml, "kind: Ingress") {
		t.Error("missing kind: Ingress")
	}
	if !strings.Contains(yaml, "host: my-app.example.com") {
		t.Error("missing expected host")
	}
}

func TestGenerateIngress_UsesDNSCompliantHost(t *testing.T) {
	tests := []struct {
		name         string
		appName      string
		wantContains string
		wantExclude  string
	}{
		{
			name:         "underscores replaced with hyphens",
			appName:      "sample_app_go_net_http",
			wantContains: "host: sample-app-go-net-http.example.com",
			wantExclude:  "sample_app",
		},
		{
			name:         "uppercase lowered",
			appName:      "MyApp",
			wantContains: "host: myapp.example.com",
		},
		{
			name:         "already compliant unchanged",
			appName:      "my-app",
			wantContains: "host: my-app.example.com",
		},
		{
			name:         "dots and spaces cleaned",
			appName:      "my app.v2",
			wantContains: "host: myapp.v2.example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			analysis := &types.AppAnalysis{
				Name: tt.appName,
				Ports: []types.Port{
					{Port: 8080, Protocol: "TCP"},
				},
			}
			cfg := config.Default()
			cfg.Ingress.DomainSuffix = ".example.com"

			yaml, err := GenerateIngress(analysis, "default", cfg)
			if err != nil {
				t.Fatalf("GenerateIngress() error = %v", err)
			}
			if !strings.Contains(yaml, tt.wantContains) {
				t.Errorf("expected %q in output, got:\n%s", tt.wantContains, yaml)
			}
			if tt.wantExclude != "" && strings.Contains(yaml, tt.wantExclude) {
				t.Errorf("unexpected %q in output", tt.wantExclude)
			}
		})
	}
}

func TestGenerateIngress_AppConfigHostOverride(t *testing.T) {
	analysis := &types.AppAnalysis{
		Name: "sample_app",
		Ports: []types.Port{
			{Port: 8080, Protocol: "TCP"},
		},
		AppConfig: &types.AppConfigContext{
			Ingress: &types.IngressContext{
				Host: "custom.example.com",
			},
		},
	}
	cfg := config.Default()
	cfg.Ingress.DomainSuffix = ".example.com"

	yaml, err := GenerateIngress(analysis, "default", cfg)
	if err != nil {
		t.Fatalf("GenerateIngress() error = %v", err)
	}
	if !strings.Contains(yaml, "host: custom.example.com") {
		t.Error("AppConfig host override not applied")
	}
}
