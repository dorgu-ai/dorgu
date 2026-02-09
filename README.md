# Dorgu

**AI-powered Kubernetes manifest generator.** Dorgu analyzes your containerized apps (Dockerfile, docker-compose, source code) and generates production-ready Kubernetes manifests, ArgoCD config, CI/CD workflows, and application documentation.

[![Go Version](https://img.shields.io/badge/Go-1.21+-00ADD8?style=flat&logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)

---

## Installation

```bash
# Install latest release (recommended)
go install github.com/dorgu-ai/dorgu/cmd/dorgu@latest

# Install a specific version
go install github.com/dorgu-ai/dorgu/cmd/dorgu@v0.1.0

# Or download a binary from GitHub Releases (Linux, macOS, Windows)
# https://github.com/dorgu-ai/dorgu/releases
```

Ensure `$GOPATH/bin` (or `$HOME/go/bin`) is in your `PATH`.

---

## Quick Start

**1. Set up global config (optional, for LLM and defaults):**

```bash
dorgu init --global
# Prompts for LLM provider, API key, default namespace, registry, org name
# Stored in ~/.config/dorgu/config.yaml
```

**2. Initialize your application config:**

```bash
cd my-app
dorgu init
# Creates .dorgu.yaml with app name, team, repo (auto-detected from git), etc.
```

**3. Generate manifests:**

```bash
dorgu generate .
# Output: k8s/deployment.yaml, service.yaml, ingress.yaml, hpa.yaml,
#         argocd/application.yaml, .github/workflows/deploy.yaml, PERSONA.md
# Post-generation validation runs automatically (use --skip-validation to skip)
```

**Preview without writing files:**

```bash
dorgu generate ./my-app --dry-run
```

---

## Commands

| Command | Description |
|---------|-------------|
| `dorgu generate [path]` | Analyze app and generate K8s manifests, ArgoCD, CI/CD, and PERSONA.md |
| `dorgu init [path]` | Create app-level `.dorgu.yaml`; use `--global` for global config |
| `dorgu config list` | Show global config (provider, API key mask, defaults) |
| `dorgu config set <key> <value>` | Set a global config value (e.g. `llm.provider`, `defaults.registry`) |
| `dorgu config get <key>` | Get a single config value |
| `dorgu version` | Show version |

### Generate flags

| Flag | Description | Default |
|------|-------------|---------|
| `--output, -o` | Output directory | `./k8s` |
| `--name, -n` | Override application name | from config/dir |
| `--namespace` | Kubernetes namespace | from global config or `default` |
| `--dry-run` | Print manifests to stdout, do not write files | `false` |
| `--llm-provider` | LLM: openai, anthropic, gemini, ollama | from config |
| `--skip-argocd` | Do not generate ArgoCD Application | `false` |
| `--skip-ci` | Do not generate GitHub Actions workflow | `false` |
| `--skip-persona` | Do not generate PERSONA.md | `false` |
| `--skip-validation` | Skip post-generation and kubectl dry-run checks | `false` |

---

## Features

- **Application analysis** — Dockerfile (ports, env, base image), docker-compose, and source (language, framework, health path)
- **LLM-enhanced analysis** — Optional deeper understanding via OpenAI, Anthropic, Gemini, or Ollama (API key from env or `dorgu config set llm.api_key`)
- **Layered config** — Global (`~/.config/dorgu/config.yaml`), workspace `.dorgu.yaml`, app `.dorgu.yaml`; CLI flags override
- **Post-generation validation** — Resource bounds, ports, health probes, HPA; optional `kubectl apply --dry-run=client` when kubectl is installed
- **Git integration** — Repository URL auto-detected from `git remote` in `dorgu init` and `dorgu generate`

---

## Configuration

**App-level (`.dorgu.yaml` in your app directory):**

```yaml
version: "1"
app:
  name: "order-service"
  description: "Order processing API"
  team: "commerce-backend"
  owner: "orders@company.com"
  repository: "https://github.com/company/order-service"  # or leave empty for git auto-detect
  type: "api"
  instructions: |
    High-traffic service; requires MySQL and Redis.

environment: "production"
resources:
  requests: { cpu: "500m", memory: "1Gi" }
  limits:   { cpu: "2000m", memory: "2Gi" }
scaling:
  min_replicas: 5
  max_replicas: 50
  target_cpu: 65
health:
  liveness:  { path: "/health", port: 8080 }
  readiness: { path: "/ready", port: 8080 }
dependencies:
  - name: mysql
    type: database
    required: true
  - name: redis
    type: cache
    required: true
```

**Global config** — Set once with `dorgu init --global` or `dorgu config set`. Keys: `llm.provider`, `llm.api_key`, `llm.model`, `defaults.namespace`, `defaults.registry`, `defaults.org_name`.

---

## Output layout

```
my-app/
├── k8s/
│   ├── deployment.yaml
│   ├── service.yaml
│   ├── ingress.yaml
│   ├── hpa.yaml
│   └── argocd/
│       └── application.yaml
├── .github/workflows/
│   └── deploy.yaml
└── PERSONA.md
```

---

## Raising issues and contributing

- **Bugs and feature requests:** Open an [issue](https://github.com/dorgu-ai/dorgu/issues). Check existing issues first.
- **Contributing code or docs:** See **[CONTRIBUTING.md](CONTRIBUTING.md)** for how to fork, branch, run tests, and open a pull request.

We welcome contributions: bug reports, documentation improvements, and code changes. Please read CONTRIBUTING.md for guidelines.

---

## Development

```bash
git clone https://github.com/dorgu-ai/dorgu.git
cd dorgu
make build    # build binary
make test     # run tests
make fmt      # format code
make lint     # run linter
```

---

## Vision

Dorgu is the first step toward an **agentic Kubernetes platform**:

- **Phase 1** (current) — CLI for manifest generation and validation
- **Phase 1.5** — ApplicationPersona CRD and cluster operator for validation
- **Phase 2+** — Deeper cluster integration, monitoring, and agent fleet

For the full vision and phased roadmap (Phase 1.5 and beyond), see the repository documentation.

---

## License

Apache 2.0 — see [LICENSE](LICENSE).
