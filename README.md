# Dorgu

**Open-source AI SRE for Kubernetes — for teams without an SRE.** Dorgu detects what's wrong in your cluster, diagnoses the root cause with AI, proposes a reviewable fix, and heals it once you approve.

Pods start OOMKilling at 3am. Nobody on the team knows why, or which limit to bump. Dorgu's operator sees the signal within 60 seconds, works out the root cause, and files a proposed fix with a diff you can read. You run `dorgu remediation approve` — the CLI applies the change with *your* credentials, verifies the workload recovers, and rolls back automatically if it doesn't. Nothing is ever applied without your approval.

[![Go](https://img.shields.io/badge/Go-1.21+-00ADD8?style=flat&logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)

<p align="center">
  <img src="docs/assets/demo.gif" alt="Dorgu detects an OOMKill, diagnoses the root cause, proposes a fix, and heals the workload after approval" width="800">
</p>

<p align="center">
  <a href="https://youtu.be/lB_529ydWw4"><strong>▶ Watch the 3-minute demo</strong></a>
</p>

---

## Why Dorgu

| | |
|---|---|
| **Runs in your cluster** | No SaaS, no agent phoning home, no telemetry. Apache-2.0. |
| **Nothing applied without approval** | The operator only ever patches Persona CRDs. Workload changes happen through the CLI, with your credentials, after you approve. |
| **AI is optional and BYO-key** | Rule-based detection and diagnosis work with **no API key at all**. Add an Anthropic key to get AI-enhanced diagnosis and ordered remediation plans. |
| **No lock-in** | Plain CRDs, plain Helm, plain YAML. ArgoCD, Flux, and `kubectl` stay in charge of deployment. |

Manifest generation (`dorgu generate`) is also part of the CLI — it analyzes your Dockerfile, Compose file, and source, and emits Kubernetes manifests, ArgoCD config, CI workflows, and an ApplicationPersona. That's a secondary capability; the self-healing loop is the point.

---

## Table of Contents

- [Self-Healing Quick Start](#self-healing-quick-start)
- [How the Loop Works](#how-the-loop-works)
- [Installation](#installation)
- [Commands](#commands)
- [Health & Incidents](#health--incidents)
- [Remediation](#remediation)
- [Guardrails](#guardrails)
- [Manifest Generation](#manifest-generation)
- [Persona Commands](#persona-commands)
- [Cluster Commands](#cluster-commands)
- [Dorgu Operator](#dorgu-operator)
- [Configuration](#configuration)
- [Development](#development)
- [License](#license)

---

## Self-Healing Quick Start

**Prerequisites:** a Kubernetes cluster ([Kind](https://kind.sigs.k8s.io/), [vCluster](https://www.vcluster.com/), EKS, or any cluster), `kubectl`, and `helm`. Optional: an Anthropic API key for AI diagnosis and AI-generated plans.

**1. Install the CLI:**

```bash
go install github.com/dorgu-ai/dorgu/cmd/dorgu@latest
```

**2. Create the Anthropic key Secret** (skip this step to run fully rule-based):

```bash
kubectl create namespace dorgu-system
kubectl create secret generic dorgu-llm \
  --from-literal=ANTHROPIC_API_KEY=sk-ant-... \
  -n dorgu-system
```

**3. Install the operator with health detection and AI:**

```bash
helm install dorgu-operator oci://ghcr.io/dorgu-ai/dorgu-operator-charts/dorgu-operator \
  --namespace dorgu-system \
  --create-namespace \
  --set healthCheck.enabled=true \
  --set websocket.enabled=true \
  --set llm.provider=claude \
  --set llm.existingSecret=dorgu-llm \
  --set aiRemediation.enabled=true
```

> Without a key, drop the last three `--set` flags. Detection and diagnosis still work — they're deterministic rules with confidence scoring.

**4. Check cluster health:**

```bash
dorgu health
```

Nodes, CPU/memory saturation, control plane components, active incidents, and pending remediations.

**5. Let something break, then look at the incident:**

```bash
dorgu incidents list
dorgu incidents describe <incident-name> -n <namespace>
```

The operator detects OOMKills, CrashLoopBackOff, ImagePullBackOff, node conditions, resource saturation, and control plane problems within 60 seconds, and writes an IncidentMemory CRD with a root-cause diagnosis and a confidence score.

**6. Review the proposed fix:**

```bash
dorgu remediation list
dorgu remediation diff <remediation-name> -n <namespace>
```

**7. Approve — and heal:**

```bash
dorgu remediation approve <remediation-name> -n <namespace>
```

Healing is **on by default**. Approving patches the Persona spec *and* applies the equivalent change to the running workload, then the operator verifies health and rolls back if the workload regresses. Use `--no-heal` to approve the proposal without touching the workload.

---

## How the Loop Works

```
detect  →  diagnose  →  propose  →  approve  →  heal  →  verify  →  remember
  │           │            │           │          │        │           │
operator    operator     operator     you       CLI     operator   IncidentMemory
(signals)  (rules +     (Remediation  (dorgu   (your     (health    (root cause +
           optional      Action with   remedi-  creds,    check,     outcome, for
           Anthropic)    ordered       ation    patches   auto-      next time)
                         Steps[])      approve) workload) rollback)
```

The split matters: **the operator never creates or modifies Deployments, Services, or any other workload resource.** It reads cluster state, writes Persona CRD spec/status, and proposes. The actual workload change is made by the CLI running as *you*. That boundary is a non-negotiable invariant of the project, not a configuration option.

Only `persona-update` steps are auto-executable. Every other step type (`restart`, `scale`, `config-change`, `manual`, `notification`, `git-pr`) is surfaced as an ordered advisory instruction for you to run — Dorgu prints it, it doesn't do it.

---

## Installation

### CLI

```bash
# Latest release (recommended)
go install github.com/dorgu-ai/dorgu/cmd/dorgu@latest

# Pin a specific version
go install github.com/dorgu-ai/dorgu/cmd/dorgu@v0.8.0

# Or download a binary from GitHub Releases (Linux, macOS, Windows)
# https://github.com/dorgu-ai/dorgu/releases
```

Ensure `$GOPATH/bin` (or `$HOME/go/bin`) is on your `PATH`.

### Operator

The operator is required for `dorgu health`, `dorgu incidents`, `dorgu remediation`, `dorgu watch`, and `dorgu sync`.

```bash
# Latest chart
helm install dorgu-operator oci://ghcr.io/dorgu-ai/dorgu-operator-charts/dorgu-operator \
  --namespace dorgu-system \
  --create-namespace \
  --set healthCheck.enabled=true

# Pin a specific chart version
helm install dorgu-operator oci://ghcr.io/dorgu-ai/dorgu-operator-charts/dorgu-operator \
  --version 0.7.2 \
  --namespace dorgu-system \
  --create-namespace \
  --set healthCheck.enabled=true
```

> Check [operator releases](https://github.com/dorgu-ai/dorgu-operator/releases) for the newest chart version.

Verify:

```bash
kubectl get pods -n dorgu-system
kubectl get crd | grep dorgu.io
```

You should see five CRDs: `applicationpersonas`, `clusterpersonas`, `incidentmemories`, `remediationactions`, `dorguevents`.

---

## Commands

### Self-Healing

| Command | Description |
|---------|-------------|
| `dorgu health` | Cluster health summary: nodes, resource saturation, control plane, active incidents, pending remediations |
| `dorgu incidents list` | List incidents with severity, category, affected persona, and phase |
| `dorgu incidents describe <name>` | Incident detail: timeline, root cause, confidence score, contributing signals |
| `dorgu remediation list` | List remediation proposals |
| `dorgu remediation diff <name>` | Show a proposal's explanation, ordered plan, and the patch it would apply |
| `dorgu remediation approve [name]` | Approve a proposal and (by default) heal the workload |
| `dorgu remediation reject <name>` | Reject a proposal |
| `dorgu remediation heal <name>` | Apply an already-approved proposal's change to the workload |

`dorgu incidents` has **`list` and `describe`** only.

### Personas & Cluster

| Command | Description |
|---------|-------------|
| `dorgu persona generate [path]` | Generate ApplicationPersona YAML from app analysis |
| `dorgu persona apply [path]` | Apply a persona to the cluster |
| `dorgu persona status <name>` | Persona status and validation results |
| `dorgu persona list` | List ApplicationPersonas across namespaces |
| `dorgu cluster init` | Create a ClusterPersona for cluster-wide identity |
| `dorgu cluster status` | Cluster status and discovered information |
| `dorgu cluster setup` | Install a production-ready Kubernetes stack via interactive wizard |
| `dorgu cluster info [name]` | Access URLs, port-forward commands, and credentials for components installed by `cluster setup` |

### Generation & Config

| Command | Description |
|---------|-------------|
| `dorgu generate [path]` | Analyze an app and generate K8s manifests, ArgoCD, CI/CD, and PERSONA.md |
| `dorgu init [path]` | Create app-level `.dorgu.yaml`; `--global` for global config |
| `dorgu config list` | Show global config (provider, masked API key, defaults) |
| `dorgu config get <key>` | Get a single config value |
| `dorgu config set <key> <value>` | Set a global config value |
| `dorgu config path` | Print the global config file path |
| `dorgu config reset` | Reset global config |
| `dorgu version` | Show version |

### Real-Time (requires operator with `websocket.enabled=true`)

| Command | Description |
|---------|-------------|
| `dorgu watch personas` | Watch ApplicationPersona updates in real time |
| `dorgu watch cluster` | Watch ClusterPersona updates in real time |
| `dorgu watch events` | Watch validation events in real time |
| `dorgu watch incidents` | Watch IncidentMemory updates in real time |
| `dorgu watch remediations` | Watch RemediationAction updates in real time |
| `dorgu sync status` | Sync status from the operator |
| `dorgu sync pull` | Pull latest persona data |
| `dorgu health --watch` | Stream health updates over WebSocket |

All `watch` subcommands accept `--operator-url` (default `ws://localhost:9090/ws`). `watch personas`, `watch events`, `watch incidents`, and `watch remediations` accept `-n, --namespace`; `watch personas` also accepts `--name`.

### Platform

| Command | Description |
|---------|-------------|
| `dorgu platform serve` | Start the ClusterPersona visualization web UI |

Flags: `--port, -p` (default `8080`), `--kubeconfig`, `--context`, `--verbose, -v`.

### Global Flags

| Flag | Description | Default |
|------|-------------|---------|
| `--json` | JSON output, for scripting, CI, and AI agents | `false` |
| `--no-color` | Disable colored output | `false` |
| `--config` | Path to config file | auto-detected |

The CLI detects whether stdout is a terminal and suppresses colors and spinners when piped. Use `--json` for structured output.

---

## Health & Incidents

### Cluster health

```bash
dorgu health                      # summary
dorgu health -n production        # scope incidents to a namespace
dorgu health --json               # for scripting
dorgu health --watch              # stream updates (needs websocket.enabled=true)
```

| Flag | Description | Default |
|------|-------------|---------|
| `-n, --namespace` | Filter incidents by namespace | all |
| `-w, --watch` | Stream health updates in real time via WebSocket | `false` |
| `--operator-url` | WebSocket URL for `--watch` | `ws://localhost:9090/ws` |
| `--kubeconfig` | Path to kubeconfig | `~/.kube/config` |

### Incidents

```bash
dorgu incidents list                            # active incidents
dorgu incidents list --severity critical
dorgu incidents list --category resource
dorgu incidents list --phase Detected
dorgu incidents list -n production
dorgu incidents list --all                      # include resolved
dorgu incidents list --limit 100
dorgu incidents list --json

dorgu incidents describe im-default-api-oom-a3f2 -n default
```

| Flag | Description | Available on |
|------|-------------|--------------|
| `-n, --namespace` | Filter by namespace / namespace of the incident | list, describe |
| `--severity` | `info`, `warning`, `critical` | list |
| `--category` | Filter by category | list |
| `--phase` | `Detected`, `Investigating`, `Resolved`, `Recurring` | list |
| `--all` | Include resolved incidents | list |
| `--limit` | Max incidents to show (default `50`) | list |
| `--kubeconfig` | Path to kubeconfig | list, describe |

`describe` shows the detection timeline (first/last seen), root cause with confidence score, contributing signals, affected resources, related DorguEvents, and occurrence count.

### How detection works

With `--set healthCheck.enabled=true`, the operator's health check reconciler runs every 60 seconds (`healthCheck.interval`) and:

1. **Detects** signals — node conditions, pod failures (OOMKilled, CrashLoopBackOff, ImagePullBackOff), resource saturation, control plane health, and optionally metrics-server usage data.
2. **Diagnoses** root causes with deterministic rules and a confidence score (0.0–1.0); with an Anthropic key configured, AI enhances the diagnosis.
3. **Creates** IncidentMemory CRDs with full context.
4. **Emits** Kubernetes Events, visible via `kubectl describe`.
5. **Proposes** a RemediationAction where a fix is derivable.
6. **Auto-resolves** the incident when the triggering signal clears.

---

## Remediation

A **RemediationAction** is a proposal: an explanation, a confidence score, an ordered `Steps[]` plan, and a JSON merge patch against a Persona spec. It starts in `Pending` and stays there until you act.

### List proposals

```bash
dorgu remediation list
dorgu remediation list -n production
dorgu remediation list --phase Pending
dorgu remediation list --all          # include completed/rejected/expired
dorgu remediation list --limit 100
dorgu remediation list --json
```

| Flag | Description | Default |
|------|-------------|---------|
| `-n, --namespace` | Filter by namespace | all |
| `--phase` | `Pending`, `Approved`, `Applying`, `Verifying`, `Completed`, … | all active |
| `--all` | Include completed, rejected, and expired | `false` |
| `--limit` | Max remediations to show | `50` |
| `--kubeconfig` | Path to kubeconfig | `~/.kube/config` |

### Review the diff

```bash
dorgu remediation diff ra-default-api-oom-b71c -n default
dorgu remediation diff ra-default-api-oom-b71c -n default --json
```

Shows the explanation, plan source (rule-based or AI), plan summary, the ordered steps, and the exact patch — before/after — so you can read the change rather than trust it.

### Approve (and heal)

```bash
# Approve a specific proposal and heal the workload (default)
dorgu remediation approve ra-default-api-oom-b71c -n default

# Approve the top pending proposal
dorgu remediation approve --next

# Approve the Persona patch only; leave the workload alone
dorgu remediation approve ra-default-api-oom-b71c -n default --no-heal

# Non-interactive (CI)
dorgu remediation approve ra-default-api-oom-b71c -n default --yes --json
```

| Flag | Description | Default |
|------|-------------|---------|
| `-n, --namespace` | Namespace of the remediation | — |
| `--next` | Approve the highest-severity pending remediation | `false` |
| `--reason` | Optional approval reason, recorded on the action | `""` |
| `--heal` | After approval, apply the resource change to the workload | `true` |
| `--no-heal` | Skip the workload heal; only patch the RemediationAction status | `false` |
| `--workload` | Explicit Deployment name (overrides label discovery) | auto |
| `--container` | Explicit container name to patch | auto |
| `--yes` | Skip the heal confirmation prompt | `false` |
| `--kubeconfig` | Path to kubeconfig | `~/.kube/config` |

**Why heal is a CLI step.** The operator patches `ApplicationPersona.spec` — the desired state — and never writes workloads. Without the heal step the persona would say 128Mi while the running pod still had 64Mi and kept OOMing. So the CLI translates the approved `persona-update` into the equivalent Deployment change and applies it with your credentials. v0.8.0 auto-applies the resource limits/requests case (memory and CPU — the OOM and saturation paths). Any other step in the plan is printed as an ordered manual instruction.

### Reject

```bash
dorgu remediation reject ra-default-api-oom-b71c -n default --reason "handled by the platform team"
```

### Heal separately

If you approved with `--no-heal` and later want the workload change:

```bash
dorgu remediation heal ra-default-api-oom-b71c -n default
```

Flags: `-n, --namespace`, `--workload`, `--container`, `--yes`, `--kubeconfig`.

---

## Guardrails

Self-healing is only safe if it's bounded. Dorgu enforces:

| Guardrail | Behavior |
|-----------|----------|
| **Approval required by default** | Every RemediationAction starts `Pending`. Nothing is applied until you approve. |
| **2× blast-radius cap** | A resource increase larger than 2× the current value is rejected as a safety violation. |
| **5 remediations per persona per hour** | Rate limit, configurable via `ClusterPersona.spec.policies.selfHealing.maxRemediationsPerHour`. |
| **`kube-system` deny-listed** | `kube-system` and the operator's own namespace are never remediated. Add more via `excludeNamespaces`. |
| **One remediation per incident** | Dedup: a proposal is skipped if an active RemediationAction already covers the same incident and target. |
| **Auto-rollback on regression** | After applying, the operator verifies health. If it degrades, the pre-patch snapshot is reapplied and the action moves to `RolledBack`. |
| **Operator never writes workloads** | Structural, not configurable. Persona CRDs only. |

---

## Manifest Generation

```bash
# One-time global config (optional, for LLM and defaults)
dorgu init --global

# Per-app config
cd my-app && dorgu init

# Generate
dorgu generate .

# Preview without writing files
dorgu generate ./my-app --dry-run
```

Output: `k8s/deployment.yaml`, `service.yaml`, `ingress.yaml`, `hpa.yaml`, `argocd/application.yaml`, `.github/workflows/deploy.yaml`, and `PERSONA.md`. Post-generation validation runs automatically.

| Flag | Description | Default |
|------|-------------|---------|
| `--output, -o` | Output directory | `./k8s` |
| `--name, -n` | Override application name | from config/dir |
| `--namespace` | Kubernetes namespace | global config or `default` |
| `--dry-run` | Print manifests to stdout, write nothing | `false` |
| `--llm-provider` | `openai`, `anthropic`, `gemini`, `ollama` | from config |
| `--skip-argocd` | Do not generate the ArgoCD Application | `false` |
| `--skip-ci` | Do not generate the GitHub Actions workflow | `false` |
| `--skip-persona` | Do not generate `PERSONA.md` | `false` |
| `--skip-validation` | Skip post-generation and `kubectl` dry-run checks | `false` |

Analysis covers Dockerfile (ports, env, base image), docker-compose, and source (language, framework, health path). LLM enhancement is optional; git remote is used to auto-detect the repository URL.

### Output layout

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

## Persona Commands

```bash
# Preview
dorgu persona generate ./my-app --dry-run

# Write to a file
dorgu persona generate ./my-app -o persona.yaml

# Apply
dorgu persona apply ./my-app --namespace production

# Status
dorgu persona status my-app -n production
dorgu persona list -n production
```

**Status fields:**

- `.status.phase` — `Pending`, `Active`, `Degraded`, `Unknown`
- `.status.validation.passed` — whether validation passed
- `.status.validation.issues` — list of validation issues
- `.status.health.status` — `Healthy`, `Degraded`, `Unknown`
- `.status.health.replicas` — current and desired replica counts

---

## Cluster Commands

```bash
# Create a ClusterPersona
dorgu cluster init --name production-cluster --environment production

# Status and discovered information
dorgu cluster status

# Install a production-ready stack (interactive)
dorgu cluster setup
dorgu cluster setup --dry-run

# Access details for what setup installed
dorgu cluster info
```

`cluster init` options: `--name` (required), `--environment` (`development`, `staging`, `production`).

Discovered by the operator: nodes and capacity, Kubernetes version, platform (Kind, Minikube, EKS, GKE, …), installed add-ons (ArgoCD, Prometheus, cert-manager), namespace summary, and ApplicationPersona count. The operator auto-creates a ClusterPersona named `dorgu-cluster` on startup if none exists.

### Cluster options for trying Dorgu

| Option | Best for | Command |
|--------|----------|---------|
| Kind | Quick local testing | `kind create cluster --name dorgu-dev` |
| vCluster | Isolated testing on an existing cluster | `vcluster create dorgu-dev -n dorgu-vcluster && vcluster connect dorgu-dev` |
| EKS / cloud | Full integration testing | Point `kubectl` at your cluster |

> **Tip:** vCluster inherits the host cluster's image pull capabilities, avoiding TLS certificate issues common with Kind behind corporate proxies.

---

## Dorgu Operator

The [operator](https://github.com/dorgu-ai/dorgu-operator) is the cluster-side half: detection, diagnosis, proposal, verification, and learning. It manages five CRDs:

| CRD | Purpose |
|-----|---------|
| **ApplicationPersona** | App identity: resources, scaling, health probes, security, ownership |
| **ClusterPersona** | Cluster identity: nodes, add-ons, capacity, self-healing policy |
| **IncidentMemory** | Detected incidents: signal, root cause, confidence, resolution |
| **RemediationAction** | Proposed fix as a Persona-spec patch, with approval workflow and rollback |
| **DorguEvent** | Classified, correlated cluster events |

### Chart options

| Parameter | Description | Default |
|-----------|-------------|---------|
| `healthCheck.enabled` | Health detection, diagnosis, incident tracking, and remediation | `false` |
| `healthCheck.interval` | Reconciliation interval | `60s` |
| `metricsServer.enabled` | metrics-server integration for container CPU/memory detection | `true` |
| `llm.provider` | `claude` enables Anthropic AI; empty disables it | `""` |
| `llm.existingSecret` | Name of a pre-created Secret holding the API key (**preferred**) | `""` |
| `llm.existingSecretKey` | Key within that Secret | `ANTHROPIC_API_KEY` |
| `llm.model` | Model override (empty = provider default) | `""` |
| `aiRemediation.enabled` | AI-generated ordered remediation plans (needs `llm.provider=claude`) | `false` |
| `websocket.enabled` | WebSocket server for `dorgu watch` / `dorgu sync` / `dorgu health --watch` | `false` |
| `websocket.port` | WebSocket port | `9090` |
| `webhook.enabled` | Validating webhook for Deployments | `false` |
| `webhook.mode` | `advisory` (warn) or `enforcing` (reject) | `advisory` |
| `argocd.enabled` | ArgoCD Application watching | `true` |
| `prometheus.enabled` | Prometheus integration for baseline learning | `false` |
| `prometheus.url` | Prometheus server URL | `""` |
| `operator.autoCreateClusterPersona` | Auto-create the `dorgu-cluster` ClusterPersona on startup | `true` |

Full options and the AI setup walkthrough are in the [operator README](https://github.com/dorgu-ai/dorgu-operator#configuration-options).

**Graceful degradation.** No Anthropic key → rule-based detection and diagnosis, unchanged. No metrics-server → saturation signals degrade, OOM detection is unaffected.

**With all optional features:**

```bash
helm install dorgu-operator oci://ghcr.io/dorgu-ai/dorgu-operator-charts/dorgu-operator \
  --namespace dorgu-system \
  --create-namespace \
  --set healthCheck.enabled=true \
  --set websocket.enabled=true \
  --set webhook.enabled=true \
  --set webhook.mode=advisory \
  --set prometheus.enabled=true \
  --set prometheus.url=http://prometheus-server.monitoring:9090
```

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

### Global config

Set once with `dorgu init --global` or `dorgu config set`:

| Key | Description |
|-----|-------------|
| `llm.provider` | `openai`, `anthropic`, `gemini`, `ollama` |
| `llm.api_key` | API key for the LLM provider |
| `llm.model` | Model name |
| `defaults.namespace` | Default Kubernetes namespace |
| `defaults.registry` | Default container registry |
| `defaults.org_name` | Organization name for labels |

Stored in `~/.config/dorgu/config.yaml`. This is the CLI's *own* LLM config, used for manifest generation — it is separate from the operator's Anthropic key, which is cluster-side and Secret-based.

Precedence, highest first: CLI flags → app `.dorgu.yaml` → workspace `.dorgu.yaml` → global config → environment variables (`OPENAI_API_KEY`, `ANTHROPIC_API_KEY`, `GEMINI_API_KEY`, `OLLAMA_HOST`) → built-in defaults.

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
make check    # same checks as CI (gofmt, vet, test) — run before pushing
make lint     # run linter
```

Run `make check` before pushing.

For Cursor, Claude Code, and other AI assistants, see [.cursor/agents/dorgu.md](.cursor/agents/dorgu.md).

---

## License

Apache 2.0 — see [LICENSE](LICENSE).
