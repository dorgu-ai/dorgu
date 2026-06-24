# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

**Dorgu** is an AI-powered CLI tool that analyzes containerized applications (Dockerfile, docker-compose, source code) and generates production-ready Kubernetes manifests, ArgoCD configs, CI/CD workflows, and ApplicationPersona CRDs. It pairs with the **Dorgu Operator** (separate repo: `dorgu-ai/dorgu-operator`) for cluster-side validation, cluster discovery, and real-time CLI↔operator communication via WebSocket.

**Current status (June 2026):** Phases 1 → 1.7, Phase 2a (detection), and Phase 2b (remediation loop) are **shipped**. Current releases: **CLI v0.7.6, Operator v0.6.1**. The full cluster self-healing loop is implemented end-to-end (detect → diagnose → propose → approve → apply → verify → learn). Phase 2c is **partially done**: operator auto-creates a ClusterPersona on startup (shipped v0.6.1), but the remaining 2c scope — app auto-discovery + skeleton personas, the two-state `managed/unmanaged` persona model, `dorgu persona discover`, the REST Query API (`:8088`), and the MCP server (`:8090/mcp`) — is **approved but NOT built**. Work paused ~April 2026 and restarted June 2026. See `docs-internal/PROJECT_SUMMARY.md` for the authoritative state.

Go module: `github.com/dorgu-ai/dorgu`

## Commands

```bash
make build           # Build binary to ./build/dorgu
make test            # Run all tests with verbose output
make test-coverage   # Run tests with coverage; generates coverage.html
make fmt             # Format with gofmt -s -w
make check           # Run gofmt + go vet + tests (same as CI — run before pushing)
make lint            # Run golangci-lint (installs if missing)
make tidy            # go mod tidy
make install         # Install to $GOPATH/bin
make example-generate # Build and run dorgu generate on ./testdata/node-app --dry-run
```

Run a single test package:
```bash
go test -v ./internal/generator/...
go test -v -run TestFunctionName ./internal/analyzer/...
```

Build and run directly:
```bash
./build/dorgu generate ./testdata/node-app --dry-run
./build/dorgu generate ./testdata/node-app --llm-provider openai --dry-run
```

## Architecture

### CLI Pipeline

```
Input (Dockerfile / docker-compose.yml / source code / .dorgu.yaml / ~/.config/dorgu/config.yaml)
  └─► internal/analyzer/ — parse and produce AppAnalysis
       └─► internal/llm/ (optional) — LLM-enhance AppAnalysis
            └─► internal/generator/ — generate manifests + persona YAML
                 └─► internal/output/ — write files or print (dry-run)
                      └─► internal/generator/validate.go — post-generation validation report
```

The central struct is `types.AppAnalysis` (`internal/types/analysis.go`). Everything — analyzers, generators, LLM clients, validators — reads from or writes to this struct.

### Configuration Layering (highest priority first)

1. CLI flags
2. App `.dorgu.yaml` (in the app directory)
3. Workspace `.dorgu.yaml` (cwd)
4. Global `~/.config/dorgu/config.yaml` (set via `dorgu init --global` or `dorgu config set`)
5. Environment variables (`OPENAI_API_KEY`, `ANTHROPIC_API_KEY`, `GEMINI_API_KEY`, `OLLAMA_HOST`)
6. Built-in defaults

Config loading: `internal/config/config.go` (app/workspace) and `internal/config/global.go` (global).

### CLI → Operator Communication

For `dorgu watch` and `dorgu sync` commands, the CLI connects to the operator's WebSocket server (`ws://operator:9090/ws`). The WebSocket client is in `internal/ws/client.go`. The operator's WebSocket server is in the `dorgu-operator` repo at `internal/websocket/`.

Message flow: CLI sends `{type: "subscribe", topic: "personas"}` → operator pushes `PersonaEvent` on changes. Also supports request/response (`{type: "request", requestId: "..."}`) for commands like `ListPersonas`.

### Trust Model

Dorgu implements progressive trust levels:
- **Level 1 (RECOMMEND):** CLI generates manifests — current default
- **Level 2 (PROPOSE):** Operator validates and proposes changes via persona status
- **Level 3+ (DEPLOY-DEV → AUTONOMOUS):** Planned in Phase 3–5

**Non-negotiable invariant:** The Dorgu Operator **must not** create or modify Deployments, Services, or other workload resources. It may only: read cluster state, validate against Persona constraints, recommend changes, and update Persona CRD status/learned fields. ArgoCD, Flux, and kubectl remain responsible for deployment.

### CRDs (API group `dorgu.io/v1`)

- **ApplicationPersona** (namespaced) — living identity of an app: resources, scaling, health, security policies, ownership. Status includes validation results, health, ArgoCD sync, and Prometheus baselines.
- **ClusterPersona** (cluster-scoped, singleton) — cluster identity: nodes, add-ons, resource capacity, platform type. Auto-discovered and kept up-to-date by the ClusterPersona controller (requeues every 5 minutes); auto-created on operator startup if absent (`dorgu-cluster`).
- **IncidentMemory** (namespaced) — detected incidents with signal, root-cause diagnosis, confidence, correlation, and resolution/outcome. Organizational learning across incidents.
- **RemediationAction** (namespaced) — proposed fix as a JSON merge patch to a Persona spec (never to workloads), with explanation, pre-patch snapshot, approval workflow, trust level, and rollback policy. Lifecycle: Pending → Approved → Applying → Verifying → Completed / RolledBack / Failed / Rejected.
- **DorguEvent** — classified/correlated K8s event surface used by the event pipeline.

Source-of-truth hierarchy: CLI/GitOps owns `spec` (desired intent); Operator owns `status` (observed reality, learned patterns).

## Key Internal Packages

| Package | Role |
|---------|------|
| `internal/analyzer/` | Dockerfile, Compose, code (language/framework detection), git repo auto-detect |
| `internal/cli/` | Cobra commands: generate, init, config, persona (generate/apply/status/list), cluster (init/status/setup/info), platform serve, health, incidents (list/describe), remediation (list/diff/approve/reject), watch (personas/cluster/events/incidents/remediations), sync, version |
| `internal/config/` | Config loading and merging (app/workspace + global) |
| `internal/generator/` | Manifest generators + `validate.go` (post-generation checks) + `security.go` + `persona_yaml.go` |
| `internal/llm/` | LLM provider interface + OpenAI, Anthropic, Gemini, Ollama implementations |
| `internal/output/` | File writer and terminal formatter |
| `internal/types/` | Shared `AppAnalysis` type (separate from `internal/analyzer/types.go`) |
| `internal/ws/` | WebSocket client for CLI↔Operator real-time communication |

## Extending the Codebase

### Adding a New LLM Provider

1. Create `internal/llm/newprovider.go`
2. Implement the `LLMClient` interface (`internal/llm/client.go`):
   ```go
   type LLMClient interface {
       AnalyzeApp(analysis *types.AppAnalysis) (*types.AppAnalysis, error)
       GeneratePersona(analysis *types.AppAnalysis) (string, error)
   }
   ```
3. Register in the factory in `internal/llm/client.go`

### Adding a New Manifest Generator

1. Create `internal/generator/newresource.go`
2. Implement with signature:
   ```go
   func GenerateNewResource(analysis *types.AppAnalysis, namespace string, cfg *config.Config) (string, error)
   ```
3. Call from `internal/generator/generator.go` and add to file output in `internal/output/writer.go`

### Adding a New CLI Command

1. Create `internal/cli/newcmd.go` with a Cobra command
2. Add to root in `internal/cli/root.go`

### Adding Framework Detection

Edit `internal/analyzer/code.go`, add detection patterns in `AnalyzeCode()`.

## Testing Approach

Tests live alongside source files (`*_test.go`). Key test files:
- `internal/analyzer/dockerfile_test.go`, `compose_test.go`, `code_test.go`, `git_test.go`
- `internal/generator/deployment_test.go`, `service_test.go`, `validate_test.go`, `persona_yaml_test.go`, `security_test.go`, `names_test.go`
- `internal/config/config_test.go`, `global_test.go`
- `internal/ws/client_test.go`

For end-to-end testing against a real cluster, see `docs-internal/QA_TESTING_GUIDE.md`. Use `testapps/` for sample applications and `testdata/` for fixtures.

## Environment Variables

| Variable | Purpose |
|----------|---------|
| `OPENAI_API_KEY` | OpenAI access (overrides global config) |
| `ANTHROPIC_API_KEY` | Anthropic Claude access |
| `GEMINI_API_KEY` | Google Gemini access |
| `OLLAMA_HOST` | Custom Ollama endpoint |

## Code Style

- Go 1.21+
- `gofmt -s` — enforced by CI (`make check`)
- Return errors over panicking
- `internal/types/analysis.go` for types shared across packages; `internal/analyzer/types.go` for analyzer-internal types

## Related Repositories

- **Operator:** `dorgu-ai/dorgu-operator` — controller-runtime based, watches ApplicationPersona and ClusterPersona CRDs, optional validating webhook, optional WebSocket server, optional ArgoCD and Prometheus integrations
- **Helm chart:** published to `oci://ghcr.io/dorgu-ai/dorgu-operator-charts/dorgu-operator`

## Claude Code Agents and Skills

### Agents (`.claude/agents/`)

Subagents that Claude Code can launch automatically or on request:

- **`phase-planner`** — strategic intelligence and planning. Reads all four vision/roadmap docs and uses web search for live market data. Covers: problem research and validation, competitive landscape (open/closed source alternatives), market opportunity sizing, customer and segment identification, tailored value propositions, phase planning, and proposed updates to `RECALIBRATION_NEXT_STEPS.md` when the evidence warrants it.

### Skills / Slash Commands (`.claude/commands/`)

Invoke with `/skill-name` in Claude Code:

| Command | Purpose |
|---------|---------|
| `/qa` | Run the release QA checklist against a local Kind cluster. Imports from `docs-internal/QA_TESTING_GUIDE.md` and version-specific checklists in `docs-internal/testing/`. |
| `/release` | Cut a release: runs `make check`, updates `CHANGELOG.md`, tags, and pushes to trigger GoReleaser. |
| `/changelog` | Update `CHANGELOG.md` from commits since the last tag using Keep a Changelog + Conventional Commits conventions. |

### Cursor Agents (`.cursor/agents/`)

End-user-facing prompts for Cursor IDE:
- `dorgu.md` — end-user assistant guide (how to use Dorgu commands)
- `generate-manifests.md` — manifest generation workflow
- `create-persona.md` — persona generation/apply/status
- `review-manifests.md` — K8s manifest review
- `analyze-application.md` — deep application analysis

## Internal Documentation

| File | Purpose |
|------|---------|
| `docs-internal/PROJECT_SUMMARY.md` | Current status, all phases, vision alignment, next steps |
| `docs-internal/RECALIBRATION_NEXT_STEPS.md` | 90-day open-source launch plan (Feb 2026) |
| `docs-internal/VISION_ROADMAP.md` | Full vision: Cluster Soul, Agent Fleet, Phases 1–5 |
| `docs-internal/architecture/ARCHITECTURE_*.md` | CLI pipeline, operator controllers, data flow diagrams, CRD schema |
| `docs-internal/QA_TESTING_GUIDE.md` | Release QA checklist (90 test cases across CLI, operator, persona, webhook, integrations) |
