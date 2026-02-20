# Dorgu

**AI-powered Kubernetes manifest generator.** Analyze Dockerfile, Compose, and source code; get production-ready manifests, ArgoCD config, CI/CD workflows, and application personas. Optionally pair with the [Dorgu Operator](https://github.com/dorgu-ai/dorgu-operator) for cluster-side validation and insights.

[![Go](https://img.shields.io/badge/Go-1.21+-00ADD8?style=flat&logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)

---

## Table of Contents

- [Installation](#installation)
- [Quick Start](#quick-start)
- [Commands](#commands)
- [Dorgu Operator](#dorgu-operator)
- [Persona Commands](#persona-commands)
- [Cluster Commands](#cluster-commands)
- [Complete Workflow](#complete-workflow)
- [Configuration](#configuration)
- [Optional Features (Operator)](#optional-features-operator)
- [Development](#development)
- [Vision & AI Assistants](#vision--ai-assistants)

---

## Installation

### CLI Installation

```bash
# Install latest release (recommended)
go install github.com/dorgu-ai/dorgu/cmd/dorgu@latest

# Install a specific version
go install github.com/dorgu-ai/dorgu/cmd/dorgu@v0.2.0

# Or download a binary from GitHub Releases (Linux, macOS, Windows)
# https://github.com/dorgu-ai/dorgu/releases
```

Ensure `$GOPATH/bin` (or `$HOME/go/bin`) is in your `PATH`.

### Operator Installation (Optional)

The Dorgu Operator provides cluster-side validation and monitoring. Install via Helm:

```bash
helm install dorgu-operator oci://ghcr.io/dorgu-ai/dorgu-operator-charts/dorgu-operator \
  --version 0.2.0 \
  --namespace dorgu-system \
  --create-namespace
```

Verify installation:

```bash
kubectl get pods -n dorgu-system
kubectl get crd applicationpersonas.dorgu.io
kubectl get crd clusterpersonas.dorgu.io
```

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

### Core Commands

| Command | Description |
|---------|-------------|
| `dorgu generate [path]` | Analyze app and generate K8s manifests, ArgoCD, CI/CD, and PERSONA.md |
| `dorgu init [path]` | Create app-level `.dorgu.yaml`; use `--global` for global config |
| `dorgu config list` | Show global config (provider, API key mask, defaults) |
| `dorgu config set <key> <value>` | Set a global config value |
| `dorgu config get <key>` | Get a single config value |
| `dorgu version` | Show version |

### Persona Commands (Requires Operator)

| Command | Description |
|---------|-------------|
| `dorgu persona generate [path]` | Generate ApplicationPersona YAML from app analysis |
| `dorgu persona apply [path]` | Apply persona to Kubernetes cluster |
| `dorgu persona status <name>` | Check persona status and validation results |

### Cluster Commands (Requires Operator)

| Command | Description |
|---------|-------------|
| `dorgu cluster init` | Initialize ClusterPersona for cluster-wide identity |
| `dorgu cluster status` | Show cluster status and discovered information |

### Real-time Commands (Requires Operator with WebSocket)

| Command | Description |
|---------|-------------|
| `dorgu watch personas` | Watch persona events in real-time |
| `dorgu watch cluster` | Watch cluster events in real-time |
| `dorgu sync status` | Sync status from operator |
| `dorgu sync pull` | Pull latest persona data |

### Generate Flags

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

## Dorgu Operator

The Dorgu Operator is the cluster-side component that validates deployments and provides cluster insights.

### What It Does

- **ApplicationPersona validation** — Checks resource limits, replica counts, health probes, and security context against persona constraints
- **ClusterPersona discovery** — Automatically discovers cluster state including nodes, add-ons (ArgoCD, Prometheus, cert-manager), and resource usage
- **ArgoCD integration** — Watches ArgoCD Applications and updates persona status with sync status
- **Prometheus baseline learning** — Queries Prometheus for resource usage metrics to establish baselines
- **Status reporting** — Updates persona status with validation results, health information, and recommendations
- **Non-invasive** — The operator reads and validates only; it does not modify workloads

### CRDs

**ApplicationPersona** — Represents the identity and requirements of an application:
- Resource constraints (CPU, memory limits)
- Scaling parameters (min/max replicas)
- Health probe configuration
- Security policies
- Ownership and team information

**ClusterPersona** — Represents the identity and state of a Kubernetes cluster:
- Cluster policies and conventions
- Node information and resource capacity
- Discovered add-ons (ArgoCD, Prometheus, etc.)
- Application count and namespace summary

### Installation Options

**Basic installation:**

```bash
helm install dorgu-operator oci://ghcr.io/dorgu-ai/dorgu-operator-charts/dorgu-operator \
  --version 0.2.0 \
  --namespace dorgu-system \
  --create-namespace
```

**With all features enabled:**

```bash
helm install dorgu-operator oci://ghcr.io/dorgu-ai/dorgu-operator-charts/dorgu-operator \
  --version 0.2.0 \
  --namespace dorgu-system \
  --create-namespace \
  --set webhook.enabled=true \
  --set webhook.mode=advisory \
  --set prometheus.enabled=true \
  --set prometheus.url=http://prometheus-server.monitoring:9090 \
  --set websocket.enabled=true
```

### Configuration Options

| Parameter | Description | Default |
|-----------|-------------|---------|
| `webhook.enabled` | Enable deployment validation webhook | `false` |
| `webhook.mode` | Webhook mode: `advisory` or `enforcing` | `advisory` |
| `argocd.enabled` | Enable ArgoCD Application watching | `true` |
| `prometheus.enabled` | Enable Prometheus metrics integration | `false` |
| `prometheus.url` | Prometheus server URL | `""` |
| `websocket.enabled` | Enable WebSocket server for CLI | `false` |
| `websocket.port` | WebSocket server port | `9090` |

---

## Persona Commands

### Generate Persona

Generate an ApplicationPersona YAML from your application:

```bash
# Preview persona
dorgu persona generate ./my-app --dry-run

# Generate to file
dorgu persona generate ./my-app -o persona.yaml
```

### Apply Persona

Apply the persona to your Kubernetes cluster:

```bash
# Apply to default namespace
dorgu persona apply ./my-app

# Apply to specific namespace
dorgu persona apply ./my-app --namespace production
```

### Check Status

Check the status of an applied persona:

```bash
dorgu persona status my-app -n production
```

Or with kubectl:

```bash
kubectl get applicationpersona my-app -n production -o yaml
```

**Status fields:**
- `.status.phase` — Current phase: Pending, Active, Degraded, Unknown
- `.status.validation.passed` — Whether validation passed (true/false)
- `.status.validation.issues` — List of validation issues
- `.status.health.status` — Health status: Healthy, Degraded, Unknown
- `.status.health.replicas` — Current and desired replica counts

---

## Cluster Commands

### Initialize Cluster

Create a ClusterPersona to establish cluster identity:

```bash
dorgu cluster init --name production-cluster --environment production
```

Options:
- `--name` — Cluster name (required)
- `--environment` — Environment: development, staging, production

### Check Cluster Status

View cluster status and discovered information:

```bash
dorgu cluster status
```

Or with kubectl:

```bash
kubectl get clusterpersona -o yaml
```

**Discovered information:**
- Nodes and resource capacity
- Kubernetes version
- Platform (Kind, Minikube, EKS, GKE, etc.)
- Installed add-ons (ArgoCD, Prometheus, cert-manager)
- Namespace summary
- ApplicationPersona count

---

## Complete Workflow

1. **Generate** — `dorgu init` then `dorgu generate .` (see [Quick Start](#quick-start)).
2. **Install operator** (optional) — See [Installation](#operator-installation-optional) above.
3. **Apply persona** — `dorgu persona apply ./my-app --namespace production`.
4. **Deploy** — `kubectl apply -f k8s/deployment.yaml -f k8s/service.yaml -n production`.
5. **Verify** — `dorgu persona status my-app -n production` (or `kubectl get applicationpersona`). Expect `phase: Active` and `validation.passed: true` after reconciliation (~60s).
6. **Cluster** (optional) — `dorgu cluster init --name my-cluster --environment production`.

---

## Configuration

### App-level (`.dorgu.yaml` in your app directory)

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

### Global Config

Set once with `dorgu init --global` or `dorgu config set`:

| Key | Description |
|-----|-------------|
| `llm.provider` | LLM provider: openai, anthropic, gemini, ollama |
| `llm.api_key` | API key for LLM provider |
| `llm.model` | Model name (e.g., gpt-4, claude-3) |
| `defaults.namespace` | Default Kubernetes namespace |
| `defaults.registry` | Default container registry |
| `defaults.org_name` | Organization name for labels |

---

## Optional Features (Operator)

When using the [Dorgu Operator](https://github.com/dorgu-ai/dorgu-operator), you can enable:

| Feature | Helm flag | What you get |
|---------|-----------|--------------|
| **Webhook** | `--set webhook.enabled=true` | Validate deployments at apply time; `webhook.mode=advisory` (warn) or `enforcing` (reject). |
| **WebSocket** | `--set websocket.enabled=true` | Real-time `dorgu watch personas` / `dorgu watch cluster` and `dorgu sync status`. |
| **ArgoCD** | Enabled by default | Persona status includes sync/health, last sync time, revision. |
| **Prometheus** | `--set prometheus.enabled=true --set prometheus.url=...` | Resource baseline learning in persona status. |

Use `helm upgrade dorgu-operator ... --namespace dorgu-system` with the flags above; chart and options are documented in the [operator README](https://github.com/dorgu-ai/dorgu-operator#configuration-options).

---

## Features

- **Application analysis** — Dockerfile (ports, env, base image), docker-compose, and source (language, framework, health path)
- **LLM-enhanced analysis** — Optional deeper understanding via OpenAI, Anthropic, Gemini, or Ollama
- **Layered config** — Global, workspace, and app-level configuration with CLI flag overrides
- **Post-generation validation** — Resource bounds, ports, health probes, HPA; optional kubectl dry-run
- **Git integration** — Repository URL auto-detected from `git remote`
- **Persona validation** — Operator validates deployments against persona constraints
- **Cluster discovery** — Automatic discovery of nodes, add-ons, and resource usage

---

## Output Layout

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

## Contributing & Support

| Need | Action |
|------|--------|
| Bugs / features | [Open an issue](https://github.com/dorgu-ai/dorgu/issues) (search first) |
| Security | [SECURITY.md](SECURITY.md) — report privately |
| Code / docs | [CONTRIBUTING.md](CONTRIBUTING.md) — fork, branch, test, PR |

Contributions are welcome.

---

## Development

```bash
git clone https://github.com/dorgu-ai/dorgu.git
cd dorgu
make build    # build binary
make test     # run tests
make fmt      # format code
make check    # run same checks as CI (gofmt, vet, test) — run before pushing
make lint     # run linter
```

Before pushing, run `make check` to catch formatting and test failures locally.

---

## Vision & AI Assistants

Dorgu is the first step toward an **agentic Kubernetes platform** — from manifest generation today to validation and cluster insights with the operator, with a path to deeper automation. For Cursor, Claude, and other AI assistants, see [.cursor/agents/dorgu.md](.cursor/agents/dorgu.md) for workflow guidance.

---

## License

Apache 2.0 — see [LICENSE](LICENSE).
