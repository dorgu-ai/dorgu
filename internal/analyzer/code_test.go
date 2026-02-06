package analyzer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAnalyzeCodeNodeJS(t *testing.T) {
	tmpDir := t.TempDir()

	// Create package.json
	packageJSON := `{
  "name": "my-app",
  "version": "1.0.0",
  "dependencies": {
    "express": "^4.18.0",
    "pg": "^8.11.0",
    "redis": "^4.6.0"
  }
}`
	if err := os.WriteFile(filepath.Join(tmpDir, "package.json"), []byte(packageJSON), 0644); err != nil {
		t.Fatalf("Failed to write package.json: %v", err)
	}

	result, err := AnalyzeCode(tmpDir)
	if err != nil {
		t.Fatalf("AnalyzeCode() error = %v", err)
	}

	// Check language
	if result.Language != "javascript" {
		t.Errorf("Language = %q, want %q", result.Language, "javascript")
	}

	// Check framework
	if result.Framework != "express" {
		t.Errorf("Framework = %q, want %q", result.Framework, "express")
	}

	// Check dependencies
	foundPG := false
	foundRedis := false
	for _, dep := range result.Dependencies {
		if dep == "postgresql" {
			foundPG = true
		}
		if dep == "redis" {
			foundRedis = true
		}
	}

	if !foundPG {
		t.Error("Expected to find postgresql dependency")
	}
	if !foundRedis {
		t.Error("Expected to find redis dependency")
	}
}

func TestAnalyzeCodePython(t *testing.T) {
	tmpDir := t.TempDir()

	// Create requirements.txt
	requirements := `Flask==2.3.0
psycopg2-binary==2.9.6
redis==4.5.0
gunicorn==21.0.0`
	if err := os.WriteFile(filepath.Join(tmpDir, "requirements.txt"), []byte(requirements), 0644); err != nil {
		t.Fatalf("Failed to write requirements.txt: %v", err)
	}

	result, err := AnalyzeCode(tmpDir)
	if err != nil {
		t.Fatalf("AnalyzeCode() error = %v", err)
	}

	// Check language
	if result.Language != "python" {
		t.Errorf("Language = %q, want %q", result.Language, "python")
	}

	// Check framework
	if result.Framework != "flask" {
		t.Errorf("Framework = %q, want %q", result.Framework, "flask")
	}
}

func TestAnalyzeCodeJava(t *testing.T) {
	tmpDir := t.TempDir()

	// Create pom.xml
	pomXML := `<?xml version="1.0" encoding="UTF-8"?>
<project>
  <groupId>com.example</groupId>
  <artifactId>my-app</artifactId>
  <version>1.0.0</version>
  <dependencies>
    <dependency>
      <groupId>org.springframework.boot</groupId>
      <artifactId>spring-boot-starter-web</artifactId>
    </dependency>
    <dependency>
      <groupId>mysql</groupId>
      <artifactId>mysql-connector-java</artifactId>
    </dependency>
  </dependencies>
</project>`
	if err := os.WriteFile(filepath.Join(tmpDir, "pom.xml"), []byte(pomXML), 0644); err != nil {
		t.Fatalf("Failed to write pom.xml: %v", err)
	}

	result, err := AnalyzeCode(tmpDir)
	if err != nil {
		t.Fatalf("AnalyzeCode() error = %v", err)
	}

	// Check language
	if result.Language != "java" {
		t.Errorf("Language = %q, want %q", result.Language, "java")
	}

	// Check framework
	if result.Framework != "spring" {
		t.Errorf("Framework = %q, want %q", result.Framework, "spring")
	}

	// Note: Java pom.xml dependency extraction is not yet implemented
	// Dependencies from compose/LLM are used instead
}

func TestAnalyzeCodeGo(t *testing.T) {
	tmpDir := t.TempDir()

	// Create go.mod
	goMod := `module github.com/example/my-app

go 1.21

require (
	github.com/gin-gonic/gin v1.9.1
	github.com/lib/pq v1.10.9
	github.com/go-redis/redis/v8 v8.11.5
)`
	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(goMod), 0644); err != nil {
		t.Fatalf("Failed to write go.mod: %v", err)
	}

	result, err := AnalyzeCode(tmpDir)
	if err != nil {
		t.Fatalf("AnalyzeCode() error = %v", err)
	}

	// Check language
	if result.Language != "go" {
		t.Errorf("Language = %q, want %q", result.Language, "go")
	}

	// Check framework
	if result.Framework != "gin" {
		t.Errorf("Framework = %q, want %q", result.Framework, "gin")
	}
}

func TestAnalyzeCodeHealthPath(t *testing.T) {
	tmpDir := t.TempDir()

	// Create package.json with health endpoint
	packageJSON := `{
  "name": "my-app",
  "version": "1.0.0",
  "dependencies": {
    "express": "^4.18.0"
  }
}`
	if err := os.WriteFile(filepath.Join(tmpDir, "package.json"), []byte(packageJSON), 0644); err != nil {
		t.Fatalf("Failed to write package.json: %v", err)
	}

	// Create server.js with health endpoint
	serverJS := `const express = require('express');
const app = express();

app.get('/health', (req, res) => {
  res.json({ status: 'ok' });
});

app.get('/api/users', (req, res) => {
  res.json([]);
});

app.listen(3000);`
	if err := os.WriteFile(filepath.Join(tmpDir, "server.js"), []byte(serverJS), 0644); err != nil {
		t.Fatalf("Failed to write server.js: %v", err)
	}

	result, err := AnalyzeCode(tmpDir)
	if err != nil {
		t.Fatalf("AnalyzeCode() error = %v", err)
	}

	// Check health path
	if result.HealthPath != "/health" {
		t.Errorf("HealthPath = %q, want %q", result.HealthPath, "/health")
	}
}

func TestAnalyzeCodeMetricsPath(t *testing.T) {
	tmpDir := t.TempDir()

	// Create package.json
	packageJSON := `{
  "name": "my-app",
  "version": "1.0.0",
  "dependencies": {
    "express": "^4.18.0",
    "prom-client": "^14.0.0"
  }
}`
	if err := os.WriteFile(filepath.Join(tmpDir, "package.json"), []byte(packageJSON), 0644); err != nil {
		t.Fatalf("Failed to write package.json: %v", err)
	}

	// Create server.js with metrics endpoint
	serverJS := `const express = require('express');
const app = express();

app.get('/metrics', (req, res) => {
  res.send('# prometheus metrics');
});

app.listen(3000);`
	if err := os.WriteFile(filepath.Join(tmpDir, "server.js"), []byte(serverJS), 0644); err != nil {
		t.Fatalf("Failed to write server.js: %v", err)
	}

	result, err := AnalyzeCode(tmpDir)
	if err != nil {
		t.Fatalf("AnalyzeCode() error = %v", err)
	}

	// Check metrics path
	if result.MetricsPath != "/metrics" {
		t.Errorf("MetricsPath = %q, want %q", result.MetricsPath, "/metrics")
	}
}

func TestAnalyzeCodeEmptyDir(t *testing.T) {
	tmpDir := t.TempDir()

	result, err := AnalyzeCode(tmpDir)
	if err != nil {
		t.Fatalf("AnalyzeCode() error = %v (should not error on empty dir)", err)
	}

	// Result should be mostly empty but not nil
	if result == nil {
		t.Error("Expected non-nil result for empty directory")
	}
}
