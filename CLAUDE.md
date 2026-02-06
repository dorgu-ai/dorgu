# Dorgu - AI Context File

> This file provides context for AI assistants (Claude, GPT, Cursor, etc.) working with this codebase.

## Project Overview

**Dorgu** is an AI-powered CLI tool that analyzes containerized applications and generates production-ready Kubernetes manifests. It understands your Dockerfile, docker-compose, and source code to create:

- Kubernetes Deployment, Service, Ingress, HPA manifests
- ArgoCD Application configurations
- GitHub Actions CI/CD workflows
- Application Persona documents (living documentation)

## Quick Start

```bash
# Build
make build

# Run
./build/dorgu generate ./path/to/app

# With LLM enhancement
./build/dorgu generate ./path/to/app --llm-provider openai

# Dry run (preview only)
./build/dorgu generate ./path/to/app --dry-run
```

## Project Structure

```
dorgu/
├── CLAUDE.md                    # This file - AI context
├── cmd/dorgu/main.go           # CLI entrypoint
├── internal/
│   ├── analyzer/               # Application analysis
│   │   ├── analyzer.go         # Main analysis orchestrator
│   │   ├── dockerfile.go       # Dockerfile parser
│   │   ├── compose.go          # Docker Compose parser
│   │   └── code.go             # Source code analyzer
│   ├── cli/                    # CLI commands
│   │   ├── root.go             # Root command (Cobra)
│   │   ├── generate.go         # Generate command
│   │   └── version.go          # Version command
│   ├── config/                 # Configuration
│   │   └── config.go           # Org config + App config
│   ├── generator/              # Manifest generation
│   │   ├── generator.go        # Main generator
│   │   ├── deployment.go       # K8s Deployment
│   │   ├── service.go          # K8s Service
│   │   ├── ingress.go          # K8s Ingress
│   │   ├── hpa.go              # HorizontalPodAutoscaler
│   │   ├── argocd.go           # ArgoCD Application
│   │   └── github_actions.go   # GitHub Actions workflow
│   ├── llm/                    # LLM providers
│   │   ├── client.go           # Client interface + factory
│   │   ├── openai.go           # OpenAI/GPT
│   │   ├── anthropic.go        # Anthropic/Claude
│   │   ├── gemini.go           # Google Gemini
│   │   └── ollama.go           # Local Ollama
│   ├── output/                 # Output formatting
│   │   ├── writer.go           # File writer
│   │   └── formatter.go        # Terminal formatting
│   └── types/                  # Shared types
│       └── analysis.go         # AppAnalysis, Port, etc.
├── docs/                       # Public documentation
├── docs-internal/              # Internal planning docs
├── testdata/                   # Test fixtures
└── testapps/                   # Sample applications
```

## Key Types

### AppAnalysis (`internal/types/analysis.go`)
The central data structure representing an analyzed application:

```go
type AppAnalysis struct {
    Name            string
    Type            string           // api, web, worker, cron
    Language        string
    Framework       string
    Description     string
    Ports           []Port
    HealthCheck     *HealthCheck
    Dependencies    []string
    Scaling         *ScalingConfig
    Dockerfile      *DockerfileAnalysis
    Compose         *ComposeAnalysis
    Code            *CodeAnalysis
    AppConfig       *AppConfigContext  // From .dorgu.yaml
    Team, Owner, Repository string     // Ownership
}
```

### AppConfig (`internal/config/config.go`)
App-level configuration from `.dorgu.yaml`:

```go
type AppConfig struct {
    App          AppMetadata           // name, description, team, instructions
    Environment  string
    Resources    *AppResources         // CPU/memory overrides
    Scaling      *AppScaling           // min/max replicas
    Labels       map[string]string
    Annotations  map[string]string
    Ingress      *AppIngress
    Health       *AppHealth
    Dependencies []AppDependency
    Operations   *AppOperations        // runbook, alerts, on-call
}
```

## Development Workflow

### Adding a New LLM Provider

1. Create `internal/llm/newprovider.go`
2. Implement the `LLMClient` interface:
   ```go
   type LLMClient interface {
       AnalyzeApp(analysis *types.AppAnalysis) (*types.AppAnalysis, error)
       GeneratePersona(analysis *types.AppAnalysis) (string, error)
   }
   ```
3. Add to factory in `internal/llm/client.go`

### Adding a New Manifest Generator

1. Create `internal/generator/newresource.go`
2. Implement generation function with signature:
   ```go
   func GenerateNewResource(analysis *types.AppAnalysis, namespace string, cfg *config.Config) (string, error)
   ```
3. Call from `internal/generator/generator.go`

### Adding a New CLI Command

1. Create `internal/cli/newcmd.go`
2. Create Cobra command and add to root in `internal/cli/root.go`

## Configuration Files

### `.dorgu.yaml` (App-level)
Place in application directory to customize generation:

```yaml
app:
  name: "my-service"
  description: "Service description"
  team: "my-team"
  instructions: |
    Custom context for LLM analysis
resources:
  requests:
    cpu: "200m"
    memory: "512Mi"
scaling:
  min_replicas: 3
  max_replicas: 20
```

### `.dorgu.yaml` (Org-level)
Place in home or workspace root for org-wide defaults.

## Testing

```bash
# Run all tests
make test

# Test with sample app
./build/dorgu generate ./testdata/node-app --dry-run

# Test with LLM (requires API key)
OPENAI_API_KEY=sk-xxx ./build/dorgu generate ./testdata/node-app --llm-provider openai --dry-run
```

## Vision & Roadmap

Dorgu is evolving toward an **agentic Kubernetes platform**:

1. **Phase 1 (Current)**: CLI for manifest generation
2. **Phase 1.5**: ApplicationPersona CRD + validation operator
3. **Phase 2**: Cluster integration (ArgoCD, Prometheus)
4. **Phase 3**: Monitoring agents with pattern learning
5. **Phase 4**: OpenWorld Gateway for MCP/external agents

See `docs/VISION_ROADMAP.md` for full details.

## Agent Files

Specialized agent prompts are available in `.cursor/agents/`:

- `generate-persona.md` - Generate application personas
- `analyze-app.md` - Deep application analysis
- `k8s-review.md` - Review K8s manifests

## Common Tasks

### "Generate manifests for my app"
```bash
dorgu generate ./path/to/app --output ./k8s
```

### "Why did generation fail?"
Check for:
1. Missing Dockerfile or docker-compose.yml
2. Invalid YAML syntax
3. LLM API key not set (for enhanced analysis)

### "How do I customize the output?"
Create `.dorgu.yaml` in your app directory. See `.dorgu.yaml.example` for all options.

### "How do I add support for a new framework?"
Edit `internal/analyzer/code.go`, add detection patterns to `AnalyzeCode()`.

## Environment Variables

| Variable | Purpose |
|----------|---------|
| `OPENAI_API_KEY` | OpenAI GPT access |
| `ANTHROPIC_API_KEY` | Anthropic Claude access |
| `GEMINI_API_KEY` | Google Gemini access |
| `OLLAMA_HOST` | Custom Ollama endpoint |

## Code Style

- Go 1.21+
- Use `gofmt` and `golint`
- Prefer returning errors over panicking
- Use structured logging where applicable
- Keep functions focused and testable
