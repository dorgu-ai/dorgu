# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

**Dorgu** is an open-source AI SRE for Kubernetes. The **Dorgu Operator** (separate repo: `dorgu-ai/dorgu-operator`) detects failures, diagnoses them and writes proposals as RemediationAction CRDs. This CLI is how a human reads incidents and proposals and approves, rejects or heals them; an approved heal is applied by the CLI with the user's own kubeconfig credentials.

**Current status (October 2026):** CLI v0.12.0 / operator v0.11.x shipped. The harness refactor is under way (plan: `dorgu-docs-internal/harness/README.md`). Phase P0 removed every legacy surface (manifest generation, cluster setup, WebSocket watch/sync, the CLI's own LLM clients) in v0.13.0; see `MIGRATION.md`. The last commit with them is the `pre-harness` tag. Adversarial bypass scenarios are tracked in `ADVERSARIAL.md` and `internal/cli/adversarial_test.go`.

Go module: `github.com/dorgu-ai/dorgu`

## Commands

```bash
make build           # Build binary to ./build/dorgu
make test            # Run all tests with verbose output
make test-coverage   # Run tests with coverage; generates coverage.html
make adversarial     # Run the adversarial bypass suite and print blocked/total
make fmt             # Format with gofmt -s -w
make check           # Run gofmt + go vet + tests (same as CI — run before pushing)
make lint            # Run golangci-lint (installs if missing)
make tidy            # go mod tidy
make install         # Install to $GOPATH/bin
```

Run a single test:
```bash
go test -v -run TestFunctionName ./internal/cli/...
```

## Architecture

### Command surface

`health`, `incidents list|describe`, `remediation list|diff|approve|reject|heal`, `persona import|list|status`, `cluster status`, `version`. Every cluster command shells out to `kubectl` through the single `kubectlCmd` helper (`internal/cli/health.go`), which applies `--kubeconfig` and the global `--context`; `TestNoRawKubectlInvocations` fails on any other `exec.Command("kubectl", ...)`.

### Enforcement that lives in the CLI (to move into the operator)

The heal path (`internal/cli/remediation_heal.go`) carries guards that today exist only here: ownership refusal, observed-workload binding, the protected-context check (`DORGU_PROTECTED_CONTEXTS`, default `prod`), phase gates, the cpu/memory-only change shape, and the step-command read-only filter (`readsOnly`, positional kubectl verb parsing in `remediation_workload.go`). See `ARCHITECTURE_CLI.md` §11 in the docs repo. The harness plan moves the decision into the operator (apply ticket, P1–P3).

### Trust Model

**Non-negotiable invariant:** The Dorgu Operator **must not** create or modify Deployments, Services, or other workload resources. It may only read cluster state, propose, and update Persona CRDs. Workload changes are made by the CLI, as the approving user, after approval.

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
| `internal/cli/` | Cobra commands and the heal path |
| `internal/importer/` | Synthesizes an ApplicationPersona from a running Deployment (`persona import`) |
| `internal/output/` | Terminal formatter, tables, diffs, JSON mode |

### Adding a New CLI Command

1. Create `internal/cli/newcmd.go` with a Cobra command
2. Add to root in `internal/cli/root.go`
3. Run kubectl only through `kubectlCmd`

## Testing Approach

Tests live alongside source files (`*_test.go`). The heal path uses fake kubectl/kube seams in `internal/cli/remediation_heal_test.go`. The adversarial suite (`internal/cli/adversarial_test.go`) asserts the *safe* outcome for each bypass scenario; scenarios not yet fixed are listed in its `openScenarios` map and skip with `OPEN`.

## Environment Variables

| Variable | Purpose |
|----------|---------|
| `DORGU_DEBUG` | `1` shows kubectl's raw output in error messages |
| `DORGU_PROTECTED_CONTEXTS` | Comma-separated, case-insensitive substrings of kube context names that `heal` refuses without an explicit confirmation path (default `prod`) |

## Code Style

- Go 1.21+
- `gofmt -s` — enforced by CI (`make check`)
- Return errors over panicking

## Related Repositories

- **Operator:** `dorgu-ai/dorgu-operator` — controller-runtime based; detection, diagnosis, proposals, verification and rollback
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

- `dorgu.md` — end-user assistant guide for the self-healing loop

## Internal Documentation

| File | Purpose |
|------|---------|
| `docs-internal/PROJECT_SUMMARY.md` | Current status, all phases, vision alignment, next steps |
| `docs-internal/RECALIBRATION_NEXT_STEPS.md` | 90-day open-source launch plan (Feb 2026) |
| `docs-internal/VISION_ROADMAP.md` | Full vision: Cluster Soul, Agent Fleet, Phases 1–5 |
| `docs-internal/architecture/ARCHITECTURE_*.md` | CLI pipeline, operator controllers, data flow diagrams, CRD schema |
| `docs-internal/QA_TESTING_GUIDE.md` | Release QA checklist (90 test cases across CLI, operator, persona, webhook, integrations) |
