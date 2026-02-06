package analyzer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseDockerfile(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		wantBase string
		wantPort int
		wantErr  bool
	}{
		{
			name: "simple node dockerfile",
			content: `FROM node:18-alpine
WORKDIR /app
COPY package*.json ./
RUN npm install
COPY . .
EXPOSE 3000
CMD ["npm", "start"]`,
			wantBase: "node:18-alpine",
			wantPort: 3000,
			wantErr:  false,
		},
		{
			name: "python flask dockerfile",
			content: `FROM python:3.11-slim
WORKDIR /app
COPY requirements.txt .
RUN pip install -r requirements.txt
COPY . .
EXPOSE 5000
CMD ["python", "app.py"]`,
			wantBase: "python:3.11-slim",
			wantPort: 5000,
			wantErr:  false,
		},
		{
			name: "go dockerfile with multi-stage",
			content: `FROM golang:1.21 AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o server .

FROM alpine:3.18
COPY --from=builder /app/server /server
EXPOSE 8080
CMD ["/server"]`,
			wantBase: "alpine:3.18",
			wantPort: 8080,
			wantErr:  false,
		},
		{
			name: "dockerfile with multiple ports",
			content: `FROM nginx:alpine
EXPOSE 80
EXPOSE 443
CMD ["nginx", "-g", "daemon off;"]`,
			wantBase: "nginx:alpine",
			wantPort: 80, // First port
			wantErr:  false,
		},
		{
			name: "dockerfile with user",
			content: `FROM node:18
USER node
EXPOSE 3000
CMD ["node", "server.js"]`,
			wantBase: "node:18",
			wantPort: 3000,
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temp file
			tmpDir := t.TempDir()
			dockerfilePath := filepath.Join(tmpDir, "Dockerfile")
			if err := os.WriteFile(dockerfilePath, []byte(tt.content), 0644); err != nil {
				t.Fatalf("Failed to write temp Dockerfile: %v", err)
			}

			// Parse
			result, err := ParseDockerfile(dockerfilePath)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseDockerfile() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if err != nil {
				return
			}

			// Check base image
			if result.BaseImage != tt.wantBase {
				t.Errorf("BaseImage = %q, want %q", result.BaseImage, tt.wantBase)
			}

			// Check port
			if len(result.Ports) == 0 {
				t.Errorf("Expected at least one port, got none")
			} else if result.Ports[0] != tt.wantPort {
				t.Errorf("First port = %d, want %d", result.Ports[0], tt.wantPort)
			}
		})
	}
}

func TestParseDockerfileEnvVars(t *testing.T) {
	content := `FROM node:18
ENV NODE_ENV=production
ENV PORT=3000
ENV DATABASE_URL=
EXPOSE 3000
CMD ["node", "server.js"]`

	tmpDir := t.TempDir()
	dockerfilePath := filepath.Join(tmpDir, "Dockerfile")
	if err := os.WriteFile(dockerfilePath, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write temp Dockerfile: %v", err)
	}

	result, err := ParseDockerfile(dockerfilePath)
	if err != nil {
		t.Fatalf("ParseDockerfile() error = %v", err)
	}

	// Check env vars
	if len(result.EnvVars) < 2 {
		t.Errorf("Expected at least 2 env vars, got %d", len(result.EnvVars))
	}

	// Check for NODE_ENV
	found := false
	for _, ev := range result.EnvVars {
		if ev.Name == "NODE_ENV" && ev.Value == "production" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected to find NODE_ENV=production")
	}
}

func TestParseDockerfileLabels(t *testing.T) {
	content := `FROM node:18
LABEL maintainer="team@example.com"
LABEL version="1.0"
EXPOSE 3000
CMD ["node", "server.js"]`

	tmpDir := t.TempDir()
	dockerfilePath := filepath.Join(tmpDir, "Dockerfile")
	if err := os.WriteFile(dockerfilePath, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write temp Dockerfile: %v", err)
	}

	result, err := ParseDockerfile(dockerfilePath)
	if err != nil {
		t.Fatalf("ParseDockerfile() error = %v", err)
	}

	// Check labels
	if result.Labels == nil {
		t.Error("Expected labels map to be non-nil")
		return
	}

	if result.Labels["maintainer"] != "team@example.com" {
		t.Errorf("Label maintainer = %q, want %q", result.Labels["maintainer"], "team@example.com")
	}
}

func TestParseDockerfileWorkdir(t *testing.T) {
	content := `FROM node:18
WORKDIR /app
COPY . .
EXPOSE 3000
CMD ["node", "server.js"]`

	tmpDir := t.TempDir()
	dockerfilePath := filepath.Join(tmpDir, "Dockerfile")
	if err := os.WriteFile(dockerfilePath, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write temp Dockerfile: %v", err)
	}

	result, err := ParseDockerfile(dockerfilePath)
	if err != nil {
		t.Fatalf("ParseDockerfile() error = %v", err)
	}

	if result.WorkDir != "/app" {
		t.Errorf("WorkDir = %q, want %q", result.WorkDir, "/app")
	}
}

func TestParseDockerfileNotFound(t *testing.T) {
	_, err := ParseDockerfile("/nonexistent/path/Dockerfile")
	if err == nil {
		t.Error("Expected error for non-existent file, got nil")
	}
}
