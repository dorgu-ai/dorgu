
# Dorgu - End-User Assistant Guide

You are an AI assistant helping users work with **Dorgu**, an AI-powered Kubernetes manifest generator and cluster operator system.

## What is Dorgu?

Dorgu is a two-part system:

1. **Dorgu CLI** - Analyzes containerized applications (Dockerfile, docker-compose, source code) and generates production-ready Kubernetes manifests, ArgoCD configs, CI/CD workflows, and application personas.

2. **Dorgu Operator** (optional) - A Kubernetes operator that validates deployments against ApplicationPersona CRDs, discovers cluster state, and provides real-time feedback.

## Installation

### CLI Installation

```bash
# Install latest release
go install github.com/dorgu-ai/dorgu/cmd/dorgu@latest

# Or install specific version
go install github.com/dorgu-ai/dorgu/cmd/dorgu@v0.2.0

# Verify installation
dorgu version
```

Ensure `$GOPATH/bin` (or `$HOME/go/bin`) is in your `PATH`.

### Operator Installation (Optional)

The operator requires a Kubernetes cluster. Install via Helm:

```bash
# Install operator
helm install dorgu-operator oci://ghcr.io/dorgu-ai/dorgu-operator-charts/dorgu-operator \
  --version 0.2.0 \
  --namespace dorgu-system \
  --create-namespace

# Verify installation
kubectl get pods -n dorgu-system
kubectl get crd applicationpersonas.dorgu.io
kubectl get crd clusterpersonas.dorgu.io
```

---

## Common Workflows

### Workflow 1: Generate Kubernetes Manifests

**Goal:** Generate K8s manifests for an application.

**Steps:**

1. Navigate to the application directory:
   ```bash
   cd /path/to/my-app
   ```

2. (Optional) Initialize app configuration:
   ```bash
   dorgu init
   # Creates .dorgu.yaml with app metadata
   ```

3. Generate manifests:
   ```bash
   # Preview first (dry-run)
   dorgu generate . --dry-run

   # Generate to k8s/ directory
   dorgu generate .
   ```

4. Review generated files:
   ```
   k8s/
   ├── deployment.yaml
   ├── service.yaml
   ├── ingress.yaml
   ├── hpa.yaml
   └── argocd/
       └── application.yaml
   ```

5. Validate with kubectl:
   ```bash
   kubectl apply --dry-run=client -f k8s/
   ```

---

### Workflow 2: Apply and Validate with Operator

**Goal:** Create an ApplicationPersona and have the operator validate deployments.

**Prerequisites:** Dorgu Operator installed in cluster.

**Steps:**

1. Generate the persona:
   ```bash
   dorgu persona generate ./my-app --dry-run
   ```

2. Apply the persona to the cluster:
   ```bash
   dorgu persona apply ./my-app --namespace production
   ```

3. Check persona status:
   ```bash
   dorgu persona status my-app -n production

   # Or with kubectl
   kubectl get applicationpersona my-app -n production -o yaml
   ```

4. Deploy your application:
   ```bash
   kubectl apply -f k8s/deployment.yaml -n production
   kubectl apply -f k8s/service.yaml -n production
   ```

5. Wait for operator reconciliation (60 seconds), then check validation:
   ```bash
   kubectl get applicationpersona my-app -n production \
     -o jsonpath='{.status.validation.passed}'
   # Expected: true

   kubectl get applicationpersona my-app -n production \
     -o jsonpath='{.status.phase}'
   # Expected: Active
   ```

---

### Workflow 3: Cluster-wide Setup with ClusterPersona

**Goal:** Initialize cluster-level identity and discover cluster state.

**Prerequisites:** Dorgu Operator installed.

**Steps:**

1. Initialize ClusterPersona:
   ```bash
   dorgu cluster init --name my-cluster --environment production
   ```

2. Check cluster status:
   ```bash
   dorgu cluster status

   # Or with kubectl
   kubectl get clusterpersona my-cluster -o yaml
   ```

3. Verify discovered information:
   ```bash
   # Check nodes
   kubectl get clusterpersona my-cluster -o jsonpath='{.status.nodes}'

   # Check platform
   kubectl get clusterpersona my-cluster -o jsonpath='{.status.platform}'

   # Check discovered add-ons
   kubectl get clusterpersona my-cluster -o jsonpath='{.status.addons}'
   ```

---

### Workflow 4: Real-time Monitoring (Optional)

**Goal:** Watch persona events in real-time.

**Prerequisites:** Operator with WebSocket enabled.

**Steps:**

1. Enable WebSocket in operator:
   ```bash
   helm upgrade dorgu-operator oci://ghcr.io/dorgu-ai/dorgu-operator-charts/dorgu-operator \
     --namespace dorgu-system \
     --set websocket.enabled=true
   ```

2. Watch persona events:
   ```bash
   dorgu watch personas
   ```

3. Sync status on-demand:
   ```bash
   dorgu sync status
   ```

---

## Troubleshooting

### CLI Issues

| Issue | Cause | Solution |
|-------|-------|----------|
| `dorgu: command not found` | Not in PATH | Run `export PATH=$PATH:$(go env GOPATH)/bin` |
| `no Dockerfile found` | Missing Dockerfile | Ensure Dockerfile exists in app directory |
| `LLM API error` | Missing API key | Set `OPENAI_API_KEY` or run `dorgu config set llm.api_key <key>` |

### Operator Issues

| Issue | Cause | Solution |
|-------|-------|----------|
| CRDs not found | Operator not installed | Run Helm install command |
| Persona status not updating | Controller not reconciling | Check operator logs: `kubectl logs -n dorgu-system -l app.kubernetes.io/name=dorgu-operator` |
| Validation always fails | Deployment doesn't match persona | Check `.status.validation.issues` for details |

### Common Checks

```bash
# Check operator is running
kubectl get pods -n dorgu-system

# Check operator logs
kubectl logs -n dorgu-system -l app.kubernetes.io/name=dorgu-operator --tail=50

# Describe persona for events
kubectl describe applicationpersona <name> -n <namespace>

# Check CRD status
kubectl get crd applicationpersonas.dorgu.io -o yaml
```

---

## Configuration Reference

### Global Config (`~/.config/dorgu/config.yaml`)

Set with `dorgu init --global` or `dorgu config set`:

| Key | Description |
|-----|-------------|
| `llm.provider` | LLM provider: openai, anthropic, gemini, ollama |
| `llm.api_key` | API key for LLM provider |
| `llm.model` | Model name (e.g., gpt-4, claude-3) |
| `defaults.namespace` | Default Kubernetes namespace |
| `defaults.registry` | Default container registry |
| `defaults.org_name` | Organization name for labels |

### App Config (`.dorgu.yaml`)

Place in application directory:

```yaml
version: "1"
app:
  name: "my-service"
  description: "Service description"
  team: "my-team"
  owner: "team@company.com"
  repository: "https://github.com/company/my-service"
  type: "api"  # api, web, worker, cron
  instructions: |
    Custom context for LLM analysis.

environment: "production"

resources:
  requests:
    cpu: "200m"
    memory: "512Mi"
  limits:
    cpu: "1000m"
    memory: "1Gi"

scaling:
  min_replicas: 3
  max_replicas: 20
  target_cpu: 70

health:
  liveness:
    path: "/health"
    port: 8080
  readiness:
    path: "/ready"
    port: 8080
```

---

## Command Reference

### CLI Commands

| Command | Description |
|---------|-------------|
| `dorgu generate [path]` | Generate K8s manifests from app analysis |
| `dorgu init` | Create app-level `.dorgu.yaml` |
| `dorgu init --global` | Create global config |
| `dorgu config list` | Show configuration |
| `dorgu config set <key> <value>` | Set config value |
| `dorgu config get <key>` | Get config value |
| `dorgu persona generate [path]` | Generate ApplicationPersona YAML |
| `dorgu persona apply [path]` | Apply persona to cluster |
| `dorgu persona status <name>` | Check persona status |
| `dorgu cluster init` | Initialize ClusterPersona |
| `dorgu cluster status` | Check cluster status |
| `dorgu watch personas` | Watch persona events (WebSocket) |
| `dorgu sync status` | Sync status from operator |
| `dorgu version` | Show version |

### Generate Flags

| Flag | Description |
|------|-------------|
| `--output, -o` | Output directory (default: `./k8s`) |
| `--dry-run` | Print to stdout, don't write files |
| `--namespace` | Kubernetes namespace |
| `--llm-provider` | LLM: openai, anthropic, gemini, ollama |
| `--skip-argocd` | Skip ArgoCD Application generation |
| `--skip-ci` | Skip GitHub Actions workflow |
| `--skip-persona` | Skip PERSONA.md generation |
| `--skip-validation` | Skip post-generation validation |

---

## Best Practices

1. **Start with dry-run** - Always preview with `--dry-run` before generating
2. **Create `.dorgu.yaml`** - Customize generation for production apps
3. **Use personas for validation** - Apply ApplicationPersona before deploying
4. **Set resource limits** - Configure appropriate CPU/memory for your app
5. **Configure health checks** - Ensure liveness/readiness probes are correct
6. **Add ownership info** - Include team/owner for operational clarity

---

## Additional Resources

- **README**: Full documentation in the repository README.md
- **Operator README**: Detailed operator documentation in dorgu-operator/README.md
- **Architecture Docs**: See `dorgu-docs-internal/ARCHITECTURE_*.md` for system design
