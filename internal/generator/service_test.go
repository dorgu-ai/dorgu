package generator

import (
	"strings"
	"testing"

	"github.com/dorgu-ai/dorgu/internal/config"
	"github.com/dorgu-ai/dorgu/internal/types"
)

func TestGenerateService_Basic(t *testing.T) {
	analysis := &types.AppAnalysis{
		Name: "test-service",
		Ports: []types.Port{
			{Port: 8080, Protocol: "TCP"},
		},
	}

	cfg := config.Default()

	yaml, err := GenerateService(analysis, "default", cfg)
	if err != nil {
		t.Fatalf("GenerateService() error = %v", err)
	}

	if !strings.Contains(yaml, "apiVersion: v1") {
		t.Error("GenerateService() missing apiVersion")
	}
	if !strings.Contains(yaml, "kind: Service") {
		t.Error("GenerateService() missing kind")
	}
	if !strings.Contains(yaml, "name: test-service") {
		t.Error("GenerateService() missing name")
	}
	if !strings.Contains(yaml, "namespace: default") {
		t.Error("GenerateService() missing namespace")
	}
	if !strings.Contains(yaml, "type: ClusterIP") {
		t.Error("GenerateService() missing type")
	}
	if !strings.Contains(yaml, "port: 8080") {
		t.Error("GenerateService() missing port")
	}
	if !strings.Contains(yaml, "targetPort: 8080") {
		t.Error("GenerateService() missing targetPort")
	}
}

func TestGenerateService_MultiplePorts(t *testing.T) {
	analysis := &types.AppAnalysis{
		Name: "multi-port-service",
		Ports: []types.Port{
			{Port: 8080, Protocol: "TCP", Purpose: "HTTP"},
			{Port: 9090, Protocol: "TCP", Purpose: "metrics"},
			{Port: 50051, Protocol: "TCP", Purpose: "gRPC"},
		},
	}

	cfg := config.Default()

	yaml, err := GenerateService(analysis, "default", cfg)
	if err != nil {
		t.Fatalf("GenerateService() error = %v", err)
	}

	if !strings.Contains(yaml, "port: 8080") {
		t.Error("GenerateService() missing port 8080")
	}
	if !strings.Contains(yaml, "port: 9090") {
		t.Error("GenerateService() missing port 9090")
	}
	if !strings.Contains(yaml, "port: 50051") {
		t.Error("GenerateService() missing port 50051")
	}
	if !strings.Contains(yaml, "name: port-0") {
		t.Error("GenerateService() missing port-0 name")
	}
	if !strings.Contains(yaml, "name: port-1") {
		t.Error("GenerateService() missing port-1 name")
	}
	if !strings.Contains(yaml, "name: port-2") {
		t.Error("GenerateService() missing port-2 name")
	}
}

func TestGenerateService_NoPorts(t *testing.T) {
	analysis := &types.AppAnalysis{
		Name:  "no-ports-service",
		Ports: []types.Port{},
	}

	cfg := config.Default()

	yaml, err := GenerateService(analysis, "default", cfg)
	if err != nil {
		t.Fatalf("GenerateService() error = %v", err)
	}

	if !strings.Contains(yaml, "kind: Service") {
		t.Error("GenerateService() missing kind even with no ports")
	}
	if !strings.Contains(yaml, "ports:") {
		t.Error("GenerateService() should have ports field")
	}
}

func TestGenerateService_WithLabels(t *testing.T) {
	analysis := &types.AppAnalysis{
		Name:        "labeled-service",
		Team:        "platform",
		Environment: "production",
		Ports: []types.Port{
			{Port: 8080, Protocol: "TCP"},
		},
	}

	cfg := config.Default()

	yaml, err := GenerateService(analysis, "default", cfg)
	if err != nil {
		t.Fatalf("GenerateService() error = %v", err)
	}

	if !strings.Contains(yaml, "app.kubernetes.io/name: labeled-service") {
		t.Error("GenerateService() missing app name label")
	}
	if !strings.Contains(yaml, "app.kubernetes.io/managed-by: dorgu") {
		t.Error("GenerateService() missing managed-by label")
	}
	if !strings.Contains(yaml, "app.kubernetes.io/team: platform") {
		t.Error("GenerateService() missing team label")
	}
	if !strings.Contains(yaml, "app.kubernetes.io/environment: production") {
		t.Error("GenerateService() missing environment label")
	}
}

func TestGenerateService_Selector(t *testing.T) {
	analysis := &types.AppAnalysis{
		Name: "selector-test",
		Ports: []types.Port{
			{Port: 8080, Protocol: "TCP"},
		},
	}

	cfg := config.Default()

	yaml, err := GenerateService(analysis, "default", cfg)
	if err != nil {
		t.Fatalf("GenerateService() error = %v", err)
	}

	if !strings.Contains(yaml, "selector:") {
		t.Error("GenerateService() missing selector")
	}
	if !strings.Contains(yaml, "app.kubernetes.io/name: selector-test") {
		t.Error("GenerateService() selector should match app name")
	}
}

func TestGenerateService_DNSSafeName(t *testing.T) {
	analysis := &types.AppAnalysis{
		Name: "My_Service_Name",
		Ports: []types.Port{
			{Port: 8080, Protocol: "TCP"},
		},
	}

	cfg := config.Default()

	yaml, err := GenerateService(analysis, "default", cfg)
	if err != nil {
		t.Fatalf("GenerateService() error = %v", err)
	}

	if !strings.Contains(yaml, "name: my-service-name") {
		t.Error("GenerateService() should convert name to DNS-safe format")
	}
}

func TestGenerateService_CustomNamespace(t *testing.T) {
	analysis := &types.AppAnalysis{
		Name: "namespace-test",
		Ports: []types.Port{
			{Port: 8080, Protocol: "TCP"},
		},
	}

	cfg := config.Default()

	yaml, err := GenerateService(analysis, "production", cfg)
	if err != nil {
		t.Fatalf("GenerateService() error = %v", err)
	}

	if !strings.Contains(yaml, "namespace: production") {
		t.Error("GenerateService() should use custom namespace")
	}
}
