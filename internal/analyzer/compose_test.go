package analyzer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseComposeFile(t *testing.T) {
	tests := []struct {
		name         string
		content      string
		wantServices int
		wantErr      bool
	}{
		{
			name: "simple compose with one service",
			content: `version: '3.8'
services:
  web:
    build: .
    ports:
      - "3000:3000"
`,
			wantServices: 1,
			wantErr:      false,
		},
		{
			name: "compose with multiple services",
			content: `version: '3.8'
services:
  app:
    build: .
    ports:
      - "8080:8080"
  db:
    image: postgres:15
    ports:
      - "5432:5432"
  redis:
    image: redis:7
    ports:
      - "6379:6379"
`,
			wantServices: 3,
			wantErr:      false,
		},
		{
			name: "compose with depends_on",
			content: `version: '3.8'
services:
  app:
    build: .
    ports:
      - "8080:8080"
    depends_on:
      - db
      - redis
  db:
    image: postgres:15
  redis:
    image: redis:7
`,
			wantServices: 3,
			wantErr:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temp file
			tmpDir := t.TempDir()
			composePath := filepath.Join(tmpDir, "docker-compose.yml")
			if err := os.WriteFile(composePath, []byte(tt.content), 0644); err != nil {
				t.Fatalf("Failed to write temp compose file: %v", err)
			}

			// Parse
			result, err := ParseComposeFile(composePath)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseComposeFile() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if err != nil {
				return
			}

			// Check service count
			if len(result.Services) != tt.wantServices {
				t.Errorf("Services count = %d, want %d", len(result.Services), tt.wantServices)
			}
		})
	}
}

func TestParseComposeFilePorts(t *testing.T) {
	content := `version: '3.8'
services:
  app:
    build: .
    ports:
      - "8080:3000"
      - "9090:9090"
`

	tmpDir := t.TempDir()
	composePath := filepath.Join(tmpDir, "docker-compose.yml")
	if err := os.WriteFile(composePath, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write temp compose file: %v", err)
	}

	result, err := ParseComposeFile(composePath)
	if err != nil {
		t.Fatalf("ParseComposeFile() error = %v", err)
	}

	if len(result.Services) != 1 {
		t.Fatalf("Expected 1 service, got %d", len(result.Services))
	}

	svc := result.Services[0]
	if len(svc.Ports) != 2 {
		t.Errorf("Expected 2 ports, got %d", len(svc.Ports))
	}

	// Check first port mapping
	if len(svc.Ports) > 0 {
		if svc.Ports[0].Host != 8080 {
			t.Errorf("First port host = %d, want 8080", svc.Ports[0].Host)
		}
		if svc.Ports[0].Container != 3000 {
			t.Errorf("First port container = %d, want 3000", svc.Ports[0].Container)
		}
	}
}

func TestParseComposeFileEnvironment(t *testing.T) {
	content := `version: '3.8'
services:
  app:
    build: .
    environment:
      - NODE_ENV=production
      - PORT=3000
    ports:
      - "3000:3000"
`

	tmpDir := t.TempDir()
	composePath := filepath.Join(tmpDir, "docker-compose.yml")
	if err := os.WriteFile(composePath, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write temp compose file: %v", err)
	}

	result, err := ParseComposeFile(composePath)
	if err != nil {
		t.Fatalf("ParseComposeFile() error = %v", err)
	}

	if len(result.Services) != 1 {
		t.Fatalf("Expected 1 service, got %d", len(result.Services))
	}

	svc := result.Services[0]
	if len(svc.Environment) < 2 {
		t.Errorf("Expected at least 2 environment variables, got %d", len(svc.Environment))
	}
}

func TestParseComposeFileDependsOn(t *testing.T) {
	content := `version: '3.8'
services:
  app:
    build: .
    depends_on:
      - postgres
      - redis
  postgres:
    image: postgres:15
  redis:
    image: redis:7
`

	tmpDir := t.TempDir()
	composePath := filepath.Join(tmpDir, "docker-compose.yml")
	if err := os.WriteFile(composePath, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write temp compose file: %v", err)
	}

	result, err := ParseComposeFile(composePath)
	if err != nil {
		t.Fatalf("ParseComposeFile() error = %v", err)
	}

	// Find app service
	var appService *ComposeService
	for i := range result.Services {
		if result.Services[i].Name == "app" {
			appService = &result.Services[i]
			break
		}
	}

	if appService == nil {
		t.Fatal("Expected to find 'app' service")
	}

	if len(appService.DependsOn) != 2 {
		t.Errorf("Expected 2 dependencies, got %d", len(appService.DependsOn))
	}
}

func TestParseComposeFileImage(t *testing.T) {
	content := `version: '3.8'
services:
  postgres:
    image: postgres:15-alpine
    ports:
      - "5432:5432"
`

	tmpDir := t.TempDir()
	composePath := filepath.Join(tmpDir, "docker-compose.yml")
	if err := os.WriteFile(composePath, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write temp compose file: %v", err)
	}

	result, err := ParseComposeFile(composePath)
	if err != nil {
		t.Fatalf("ParseComposeFile() error = %v", err)
	}

	if len(result.Services) != 1 {
		t.Fatalf("Expected 1 service, got %d", len(result.Services))
	}

	if result.Services[0].Image != "postgres:15-alpine" {
		t.Errorf("Image = %q, want %q", result.Services[0].Image, "postgres:15-alpine")
	}
}

func TestParseComposeFileNotFound(t *testing.T) {
	_, err := ParseComposeFile("/nonexistent/path/docker-compose.yml")
	if err == nil {
		t.Error("Expected error for non-existent file, got nil")
	}
}
