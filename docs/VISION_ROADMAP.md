# Dorgu Vision & Roadmap

## Mission

Build an autonomous agentic layer for Kubernetes that **understands** applications, not just manifests.

## Vision

A fleet of AI agents working in harmony to deploy, monitor, and manage applications on Kubernetes - with the cluster developing a "soul" through learned patterns, incident memories, and evolving policies.

---

## Core Concepts

### The Cluster Soul

The cluster maintains memory and context through:

| Component | Storage | Purpose |
|-----------|---------|---------|
| **ApplicationPersona CRD** | Cluster | Living identity of each application |
| **IncidentMemory CRD** | Cluster | Learned lessons from past incidents |
| **PolicyEvolution CRD** | Cluster | Why rules exist and when they were added |
| **DeploymentHistory** | GitOps + CRD | Full audit trail of changes |
| **ResourceBaselines** | Persona Status | Learned normal behavior patterns |

### Agent Fleet

| Agent | Role | Communication |
|-------|------|---------------|
| **CLI Agent** | User interface, manifest generation | Local → WebSocket |
| **Cluster Operator** | Validation, policy enforcement | CRD watches |
| **Monitoring Agent** | Health observation, pattern learning | Prometheus/OTEL → CRD |
| **OpenWorld Gateway** | External agent access, MCP endpoints | WebSocket → sandboxed |

### Trust Model

```
Level 0: OBSERVE     - Read cluster state, no writes
Level 1: RECOMMEND   - Generate suggestions, human applies
Level 2: PROPOSE     - Create PRs/proposals, human approves
Level 3: DEPLOY-DEV  - Deploy to dev/staging, human promotes
Level 4: DEPLOY-PROD - Deploy anywhere with approval gates
Level 5: AUTONOMOUS  - Make decisions without human intervention
```

---

## Phased Roadmap

### Phase 1: CLI Agent Foundation (Current)
**Timeline**: Months 1-3
**Trust Level**: 1 (RECOMMEND)

#### Goals
- Complete manifest generation from Dockerfile/Compose/Code
- Multi-provider LLM integration (OpenAI, Anthropic, Gemini, Ollama)
- Application-level .dorgu.yaml configuration
- Generate Persona documents alongside manifests

#### Deliverables
- [x] Core CLI structure (Cobra/Viper)
- [x] Dockerfile parser
- [x] Docker Compose parser
- [x] Code analyzer (language/framework/dependencies)
- [x] LLM integration for enhanced analysis
- [x] Manifest generators (Deployment, Service, Ingress, HPA)
- [x] ArgoCD Application generation
- [x] GitHub Actions workflow generation
- [x] App-level .dorgu.yaml support
- [ ] Unit tests for parsers
- [ ] Init command for config generation
- [ ] Naming pattern variable substitution

#### Success Metrics
- 100 GitHub stars
- 10 active users generating manifests
- 5 production deployments using generated manifests

---

### Phase 1.5: Persona CRD Bridge
**Timeline**: Months 3-4
**Trust Level**: 1-2 (RECOMMEND → PROPOSE)

#### Goals
- Introduce ApplicationPersona CRD to clusters
- Build validation operator that checks deployments against personas
- Create feedback loop from cluster to CLI

#### Deliverables
- [ ] ApplicationPersona CRD definition
- [ ] `dorgu persona apply` command to create CRD from analysis
- [ ] Dorgu Operator (controller-runtime based)
  - [ ] Watch Persona CRDs
  - [ ] Validating webhook for Deployments
  - [ ] Status updates with validation results
- [ ] `dorgu persona status` command to view cluster state
- [ ] Helm chart for operator installation

#### Architecture

```
┌─────────────────┐     ┌──────────────────────────────────────┐
│   dorgu CLI     │     │           Kubernetes Cluster          │
│                 │     │                                        │
│  dorgu generate │     │  ┌──────────────────────────────────┐ │
│        │        │     │  │     ApplicationPersona CRD       │ │
│        ▼        │     │  │                                  │ │
│  manifests +    │────▶│  │  spec:                           │ │
│  persona.yaml   │     │  │    name: order-service           │ │
│                 │     │  │    resources: ...                │ │
│  dorgu persona  │     │  │    scaling: ...                  │ │
│  apply ─────────│────▶│  │                                  │ │
│                 │     │  │  status:                         │ │
│  dorgu persona  │     │  │    validation: passed            │ │
│  status ◀───────│◀────│  │    recommendations: [...]        │ │
│                 │     │  └──────────────────────────────────┘ │
│                 │     │                 │                      │
│                 │     │                 │ watches              │
│                 │     │                 ▼                      │
│                 │     │  ┌──────────────────────────────────┐ │
│                 │     │  │       Dorgu Operator             │ │
│                 │     │  │                                  │ │
│                 │     │  │  - Validates Deployments         │ │
│                 │     │  │  - Checks resource constraints   │ │
│                 │     │  │  - Updates Persona status        │ │
│                 │     │  │  - Suggests optimizations        │ │
│                 │     │  └──────────────────────────────────┘ │
│                 │     │                 │                      │
│                 │     │                 │ validates            │
│                 │     │                 ▼                      │
│                 │     │  ┌──────────────────────────────────┐ │
│                 │     │  │         Deployment               │ │
│                 │     │  │  (created by ArgoCD/kubectl)     │ │
│                 │     │  └──────────────────────────────────┘ │
└─────────────────┘     └────────────────────────────────────────┘
```

#### Operator Validation Rules

```yaml
# Example validation checks
validations:
  - name: resource-bounds
    description: Deployment resources within persona constraints
    check: deployment.resources <= persona.resources.limits
    severity: error
    
  - name: replica-range
    description: Replicas within scaling bounds
    check: persona.scaling.min <= deployment.replicas <= persona.scaling.max
    severity: warning
    
  - name: health-probes
    description: Health probes match persona configuration
    check: deployment.probes.path == persona.health.*Path
    severity: warning
    
  - name: security-context
    description: Security policies enforced
    check: deployment.securityContext matches persona.policies.security
    severity: error
    
  - name: dependency-ready
    description: Required dependencies are available
    check: all persona.dependencies[required=true] are healthy
    severity: info
```

#### Success Metrics
- 5 clusters with Dorgu Operator installed
- 50 ApplicationPersona CRDs created
- Validation catching 10+ misconfigurations before deployment

---

### Phase 2: Cluster Integration
**Timeline**: Months 4-6
**Trust Level**: 2-3 (PROPOSE → DEPLOY-DEV)

#### Goals
- Deep integration with existing tools (ArgoCD, Prometheus, Grafana)
- Secrets management awareness
- Multi-namespace policy enforcement
- WebSocket communication between CLI and Operator

#### Deliverables
- [ ] ArgoCD integration (sync status, rollback triggers)
- [ ] Prometheus metrics integration (baseline learning)
- [ ] Grafana dashboard generation
- [ ] Secrets inventory (Vault/External Secrets awareness)
- [ ] Namespace-level policies
- [ ] WebSocket server in operator for CLI communication
- [ ] `dorgu sync` command for bidirectional sync

#### Integration Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                     DORGU CLUSTER GATEWAY                       │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│   ┌─────────────┐    WebSocket    ┌────────────────────────┐   │
│   │  CLI Agent  │◀───────────────▶│    Dorgu Operator      │   │
│   └─────────────┘                 └────────────────────────┘   │
│                                              │                  │
│                          ┌───────────────────┼───────────────┐  │
│                          │                   │               │  │
│                          ▼                   ▼               ▼  │
│                    ┌──────────┐       ┌───────────┐   ┌───────┐│
│                    │  ArgoCD  │       │Prometheus │   │ Vault ││
│                    │          │       │           │   │       ││
│                    │ - Sync   │       │ - Metrics │   │ - Sec ││
│                    │ - Status │       │ - Alerts  │   │ - Rot ││
│                    └──────────┘       └───────────┘   └───────┘│
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

#### Success Metrics
- 3 production clusters with full integration
- Automated baseline learning for 20+ applications
- 50% reduction in deployment misconfigurations

---

### Phase 3: Monitoring & Learning
**Timeline**: Months 6-9
**Trust Level**: 3-4 (DEPLOY-DEV → DEPLOY-PROD)

#### Goals
- Proactive anomaly detection
- Incident memory building
- Resource optimization recommendations
- Cost analysis integration

#### Deliverables
- [ ] IncidentMemory CRD
- [ ] Anomaly detection from Prometheus metrics
- [ ] Automatic persona status updates from observed behavior
- [ ] Slack/Teams notifications for recommendations
- [ ] Cost estimation (cloud provider integration)
- [ ] `dorgu incidents` command for history
- [ ] Pattern learning algorithm (simple heuristics → ML later)

#### Soul Evolution

```
┌─────────────────────────────────────────────────────────────────┐
│                     CLUSTER SOUL EVOLUTION                      │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│   Day 1: Persona Created                                        │
│   ┌─────────────────────────────────────────────────────────┐  │
│   │ spec: {...}                                              │  │
│   │ status:                                                  │  │
│   │   learned: {}  # Empty                                   │  │
│   └─────────────────────────────────────────────────────────┘  │
│                                                                 │
│   Week 1: Baselines Established                                 │
│   ┌─────────────────────────────────────────────────────────┐  │
│   │ status:                                                  │  │
│   │   learned:                                               │  │
│   │     resourceBaseline:                                    │  │
│   │       avgCPU: 350m                                       │  │
│   │       avgMemory: 800Mi                                   │  │
│   └─────────────────────────────────────────────────────────┘  │
│                                                                 │
│   Month 1: Patterns Detected                                    │
│   ┌─────────────────────────────────────────────────────────┐  │
│   │ status:                                                  │  │
│   │   learned:                                               │  │
│   │     patterns:                                            │  │
│   │       - type: traffic-spike                              │  │
│   │         description: "3x traffic 12:00-14:00 UTC"        │  │
│   │         confidence: 0.92                                 │  │
│   └─────────────────────────────────────────────────────────┘  │
│                                                                 │
│   After Incident: Memory Added                                  │
│   ┌─────────────────────────────────────────────────────────┐  │
│   │ IncidentMemory:                                          │  │
│   │   rootCause: "Connection pool exhausted"                 │  │
│   │   resolution: "Increased pool size"                      │  │
│   │   preventiveMeasures: [...]                              │  │
│   │                                                          │  │
│   │ Persona Status Updated:                                  │  │
│   │   incidentCount: 1                                       │  │
│   │   recommendations:                                       │  │
│   │     - "Add connection pool monitoring"                   │  │
│   └─────────────────────────────────────────────────────────┘  │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

#### Success Metrics
- Detect 80% of incidents before user notices
- 30% reduction in resource waste through optimization
- 100+ incident memories stored

---

### Phase 4: OpenWorld Gateway
**Timeline**: Months 9-12
**Trust Level**: 4-5 (DEPLOY-PROD → AUTONOMOUS)

#### Goals
- Enable external agents to access cluster capabilities
- MCP endpoint generation for applications
- Sandboxed execution environment
- Cross-cluster coordination

#### Deliverables
- [ ] OpenWorld Gateway service
- [ ] MCP server generation from Persona
- [ ] Sandboxed kubectl proxy for external agents
- [ ] Agent authentication and authorization
- [ ] Usage audit logging
- [ ] Rate limiting and quota management

#### MCP Integration

```
┌─────────────────────────────────────────────────────────────────┐
│                     OPENWORLD GATEWAY                           │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│   External Agent                                                │
│   (Claude, GPT, Custom)                                         │
│          │                                                      │
│          │ MCP Protocol                                         │
│          ▼                                                      │
│   ┌──────────────────────────────────────────────────────────┐ │
│   │                  MCP Gateway                              │ │
│   │                                                           │ │
│   │  Available Tools (auto-generated from Personas):          │ │
│   │  ┌─────────────────────────────────────────────────────┐ │ │
│   │  │ order-service.get_orders(limit, offset)             │ │ │
│   │  │ order-service.get_order_status(order_id)            │ │ │
│   │  │ order-service.get_metrics()                         │ │ │
│   │  │ inventory-service.check_stock(sku)                  │ │ │
│   │  │ cluster.get_pod_logs(app, lines)                    │ │ │
│   │  │ cluster.get_pod_status(app)                         │ │ │
│   │  └─────────────────────────────────────────────────────┘ │ │
│   │                                                           │ │
│   │  Permissions (from Persona + Gateway config):             │ │
│   │  - order-service: read-only                               │ │
│   │  - inventory-service: read-only                           │ │
│   │  - cluster: logs + status only                            │ │
│   │                                                           │ │
│   └──────────────────────────────────────────────────────────┘ │
│                         │                                       │
│                         │ Sandboxed proxy                       │
│                         ▼                                       │
│   ┌──────────────────────────────────────────────────────────┐ │
│   │                  Kubernetes Cluster                       │ │
│   │  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐       │ │
│   │  │order-service│  │inventory-svc│  │ payment-svc │       │ │
│   │  └─────────────┘  └─────────────┘  └─────────────┘       │ │
│   └──────────────────────────────────────────────────────────┘ │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

#### Success Metrics
- 10 external agents connected via MCP
- 1000+ tool invocations per day
- Zero security incidents

---

### Phase 5: Autonomous Operations
**Timeline**: Months 12-18
**Trust Level**: 5 (AUTONOMOUS)

#### Goals
- Self-healing based on learned patterns
- Automatic scaling without human intervention
- Cost optimization execution
- Cross-cluster coordination

#### Deliverables
- [ ] Autonomous decision engine
- [ ] Human approval bypass for known patterns
- [ ] Automatic rollback on anomaly detection
- [ ] Multi-cluster state synchronization
- [ ] Business metric integration (revenue, conversion)

#### Success Metrics
- 50% reduction in ops intervention
- 99.9% availability for managed applications
- 20% cost reduction through optimization

---

## Adoption Strategy

### Don't Replace, Integrate

Dorgu should enhance existing tools, not replace them:

| Tool | Integration | Value Add |
|------|-------------|-----------|
| **ArgoCD** | Sync status, rollback API | AI-driven deployment decisions |
| **Prometheus** | Metrics queries, alerting | Pattern learning, baseline detection |
| **Grafana** | Dashboard generation | Auto-generated dashboards from Persona |
| **Vault** | Secrets awareness | Context-aware secret management |
| **New Relic** | APM integration | Correlation with deployment events |

### Progressive Trust

Users should be able to start minimal and increase trust:

```bash
# Level 0: Observe only
dorgu observe --cluster production --readonly

# Level 1: Recommendations
dorgu recommend order-service --apply-none

# Level 2: Propose PRs
dorgu deploy order-service --create-pr

# Level 3: Deploy to dev
dorgu deploy order-service --env dev --auto-approve

# Level 4: Deploy to prod
dorgu deploy order-service --env prod --approval-required

# Level 5: Full autonomy
dorgu auto order-service --mode autonomous
```

---

## Technical Decisions

### Why Go?
- Native Kubernetes ecosystem (controller-runtime, client-go)
- Single binary distribution
- Strong concurrency model for operators
- Well-supported for CLI tools (Cobra)

### Why CRDs for Soul?
- Native Kubernetes primitives
- Declarative state management
- Built-in RBAC
- Watch mechanism for reactivity
- GitOps compatible

### Why WebSocket for Agent Communication?
- Real-time bidirectional communication
- Streaming support for logs/events
- Proven pattern (OpenClaw)
- Works through tunnels (SSH, Tailscale)

### Why Start with Validation, Not Control?
- Lower trust barrier
- Faster adoption
- Learn user needs before automating
- Fail-safe (validation can only warn, not break)

---

## Risk Mitigation

| Risk | Mitigation |
|------|------------|
| **Security breach** | Progressive trust, audit everything, sandbox by default |
| **Vendor lock-in** | Open source core, standard protocols (MCP, OTEL) |
| **Adoption friction** | Start read-only, integrate with existing tools |
| **Complexity creep** | Clear phase boundaries, ship incrementally |
| **LLM unreliability** | Fallback to rule-based, human approval gates |

---

## Next Steps (Immediate)

1. **Complete Phase 1 CLI** - Unit tests, init command, polish
2. **Design CRD schema** - Finalize ApplicationPersona spec
3. **Scaffold operator** - controller-runtime project structure
4. **Plan Phase 1.5** - Detailed tasks and timeline
