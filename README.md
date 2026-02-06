# Dorgu

> AI-powered Kubernetes manifest generator that understands your applications

[![Go Version](https://img.shields.io/badge/Go-1.21+-00ADD8?style=flat&logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)

Dorgu analyzes your containerized applications and generates production-ready Kubernetes manifests. It reads your Dockerfile, docker-compose, and source code to understand what your application needs and creates:

- **Kubernetes Manifests** - Deployment, Service, Ingress, HPA
- **GitOps Configuration** - ArgoCD Application manifests
- **CI/CD Pipelines** - GitHub Actions workflows
- **Application Personas** - Living documentation for your apps

## Quick Start

### Installation

```bash
# Install via go (requires Go 1.21+)
go install github.com/dorgu-ai/dorgu/cmd/dorgu@latest

# Or download a binary from GitHub Releases
# https://github.com/dorgu-ai/dorgu/releases

# Or build from source
git clone https://github.com/dorgu-ai/dorgu.git
cd dorgu
make build
```

### Generate Manifests

```bash
# Basic usage
dorgu generate ./my-app

# Preview without writing files
dorgu generate ./my-app --dry-run

# Custom output directory
dorgu generate ./my-app --output ./k8s

# With LLM-enhanced analysis
dorgu generate ./my-app --llm-provider openai
```

## Features

### Smart Application Analysis

Dorgu automatically detects:
- **Language & Framework** - Node.js/Express, Java/Spring, Python/Flask, Go/Gin, and more
- **Health Endpoints** - `/health`, `/healthz`, `/actuator/health`
- **External Dependencies** - PostgreSQL, Redis, MongoDB, RabbitMQ
- **Resource Requirements** - Based on application type (API, worker, web)

### LLM-Enhanced Analysis

Optionally use AI for deeper application understanding:

```bash
# OpenAI
OPENAI_API_KEY=sk-xxx dorgu generate ./my-app --llm-provider openai

# Google Gemini
GEMINI_API_KEY=xxx dorgu generate ./my-app --llm-provider gemini

# Anthropic Claude
ANTHROPIC_API_KEY=xxx dorgu generate ./my-app --llm-provider anthropic

# Local Ollama
dorgu generate ./my-app --llm-provider ollama
```

### Application Configuration

Create a `.dorgu.yaml` in your application directory to customize generation:

```yaml
app:
  name: "order-service"
  description: "Order processing API for e-commerce platform"
  team: "commerce-backend"
  owner: "orders-team@company.com"
  
  instructions: |
    High-traffic service handling ~5000 orders/minute.
    Requires MySQL for persistence and Redis for caching.

resources:
  requests:
    cpu: "500m"
    memory: "1Gi"
  limits:
    cpu: "2000m"
    memory: "2Gi"

scaling:
  min_replicas: 5
  max_replicas: 50
  target_cpu: 65

health:
  liveness:
    path: "/actuator/health/liveness"
    port: 8000
  readiness:
    path: "/actuator/health/readiness"
    port: 8000

dependencies:
  - name: mysql
    type: database
    required: true
  - name: redis
    type: cache
    required: true
```

## Output Structure

```
my-app/
├── k8s/
│   ├── deployment.yaml      # Kubernetes Deployment
│   ├── service.yaml         # Kubernetes Service
│   ├── ingress.yaml         # Kubernetes Ingress
│   ├── hpa.yaml             # HorizontalPodAutoscaler
│   └── argocd/
│       └── application.yaml # ArgoCD Application
├── .github/
│   └── workflows/
│       └── deploy.yaml      # GitHub Actions CI/CD
└── PERSONA.md               # Application documentation
```

## Example Output

### Deployment (excerpt)
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: order-service
  labels:
    app.kubernetes.io/name: order-service
    app.kubernetes.io/managed-by: dorgu
    app.kubernetes.io/team: commerce-backend
spec:
  replicas: 5
  template:
    spec:
      containers:
      - name: order-service
        resources:
          requests:
            cpu: 500m
            memory: 1Gi
          limits:
            cpu: 2000m
            memory: 2Gi
        livenessProbe:
          httpGet:
            path: /actuator/health/liveness
            port: 8000
```

### Application Persona (excerpt)
```markdown
# order-service

## Overview
Order processing and management API for the e-commerce platform.

## Technical Stack
- **Language:** java
- **Framework:** spring
- **Type:** api

## External Dependencies
- **mysql** (database) - required
- **redis** (cache) - required

## Ownership
- **Team:** commerce-backend
- **Contact:** orders-team@company.com
```

## Commands

| Command | Description |
|---------|-------------|
| `dorgu generate <path>` | Generate manifests for an application |
| `dorgu version` | Show version information |
| `dorgu help` | Show help |

### Generate Flags

| Flag | Description | Default |
|------|-------------|---------|
| `--output, -o` | Output directory | `./k8s` |
| `--namespace, -n` | Kubernetes namespace | `default` |
| `--dry-run` | Preview without writing | `false` |
| `--llm-provider` | LLM provider (openai, anthropic, gemini, ollama) | none |
| `--skip-argocd` | Skip ArgoCD generation | `false` |
| `--skip-ci` | Skip GitHub Actions generation | `false` |

## Development

```bash
# Build
make build

# Run tests
make test

# Format code
make fmt

# Lint
make lint

# Hot reload during development
make dev
```

## Project Structure

```
dorgu/
├── cmd/dorgu/          # CLI entrypoint
├── internal/
│   ├── analyzer/       # Application analysis
│   ├── cli/            # CLI commands
│   ├── config/         # Configuration handling
│   ├── generator/      # Manifest generation
│   ├── llm/            # LLM providers
│   ├── output/         # Output formatting
│   └── types/          # Shared types
├── docs/               # Documentation
└── .cursor/agents/     # AI assistant prompts
```

## Vision

Dorgu is the first step toward an **agentic Kubernetes platform**:

1. **Phase 1** (Current) - CLI for manifest generation
2. **Phase 2** - ApplicationPersona CRD + cluster operator
3. **Phase 3** - Monitoring agents with pattern learning
4. **Phase 4** - OpenWorld Gateway for external AI agents

## Contributing

Contributions are welcome! Please see our contributing guidelines.

## License

Apache 2.0 - See [LICENSE](LICENSE) for details.

---

**Built with** by the Dorgu team
