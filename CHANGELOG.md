# Changelog

All notable changes to the Dorgu CLI are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- `dorgu persona import -n <namespace> [--all | --name <deployment>]` creates ApplicationPersonas from Deployments that are already running, reading resources, probes, replicas, ports, image and ownership labels straight off the spec. No local source, no Dockerfile and no relabelling. Prints the YAML by default; `--apply` creates the personas, `-o` writes them to a file. The persona name is chosen so the operator resolves it back to that same Deployment, and when no name can (another Deployment's label outranks it) the CLI says so instead of pointing Dorgu at the wrong workload. Containers with no resource limits get inferred ones, reported explicitly, because the remediation proposer skips personas without limits and a persona that cannot heal is worse than no persona.

### Fixed

- `dorgu remediation approve` resolves the target Deployment **before** it approves. Approval is what tells the operator to patch the persona and start the verification clock, so approving first and failing to find the workload afterwards left the persona at the new limits, the running workload at the old ones, and a 10-minute `Applying`/`Verifying` window over a change that never landed. When the heal cannot be planned, nothing is approved and nothing is changed, and the CLI says which Deployments it did find.
- A failed workload patch is never reported as a heal. The CLI now names the divergence: "the remediation is Approved but `<ns>/<deployment>` was NOT patched; the persona and the workload now disagree."
- Workload discovery no longer requires a label on the Deployment object. Helm, kustomize and most hand-written YAML label the pod template only, so `dorgu remediation heal` failed with `no Deployment found for app "<name>"` while the Deployment sat in the same namespace. Discovery now walks the same ordered chain as the operator: `app.kubernetes.io/name` label, `app` label, `metadata.name`, then `spec.selector.matchLabels`. Zero and ambiguous matches list the Deployments actually present and point at `--workload`.
- `--no-heal` warns that the persona and the running workload will disagree until the change is applied by hand.

## [0.8.1] - 2026-08-07

### Removed

- **`dorgu remediation list --severity` — a filter that always returned nothing** — it matched `spec.severity` on `RemediationAction`, a field the operator's CRD does not have and never sets, so `--severity critical` silently returned an empty list. The flag, the `SEVERITY` column (always blank), the `Severity:` row in `remediation diff`, and the local `Severity` struct field are removed. Severity lives on the linked `IncidentMemory` — read it with `dorgu incidents describe <incident>`, where `--severity` does work.

### Changed

- **`dorgu remediation approve --next` now picks the oldest pending remediation** — it ranked candidates by the same absent `severity` field, so every one tied and "highest-severity" really meant "whichever the API server listed first." It now orders by creation time, oldest first, with namespace/name as a stable tie-break so repeated runs choose the same action; objects with a missing or unparseable timestamp sort last. The flag itself keeps working as before.
- **README refreshed to lead with the self-healing loop.** The landing copy now opens on the full loop (detect, diagnose, propose, approve, heal) rather than on manifest generation. Adds a recorded demo (`docs/assets/demo.gif`), corrects the sample command output, and drops the `--severity` references.

## [0.8.0] - 2026-07-09

### Added

- `dorgu remediation heal` command — apply an approved remediation's resource change directly to the running workload, translating the RemediationAction's ordered `Steps[]` plan into concrete patches.
- `dorgu remediation approve` now heals the running workload after approval by default. Use `--no-heal` to only patch the RemediationAction status, and `--yes` to skip the heal confirmation prompt.

### Fixed

- `dorgu remediation` now correctly parses operator `RemediationAction` resources and renders their ordered `Steps[]` as readable, sequenced plans in `list` and `diff` output.

## [0.7.6] - 2026-04-18

### Fixed

- `dorgu cluster info --json` now returns a structured JSON error object when no components are installed, instead of printing help text and ignoring the `--json` flag (BUG-10-3).
- `dorgu incidents list --json` and `dorgu remediation list --json` now return `[]` instead of `null` when no items exist, for reliable scripting/agent consumption.

## [0.7.5] - 2026-04-17

### Fixed

- `dorgu cluster setup` no longer fails when no ClusterPersona exists; auto-creates `dorgu-cluster` inline (idempotent with the operator bootstrap). Dry-run mode is unaffected and continues to use a placeholder name.

## [0.7.4] - 2026-04-09

### Added

- `dorgu watch incidents` subcommand — stream real-time IncidentMemory events from the operator WebSocket, with namespace filtering and initial snapshot on connect.
- `dorgu watch remediations` subcommand — stream real-time RemediationAction lifecycle events (created, approved, completed, rolledback, rejected).
- `dorgu watch personas` now shows existing personas immediately on connect instead of waiting for the first change event.
- ArgoCD Application manifests generated by `dorgu cluster setup --driver gitops` now include `ServerSideApply=true` sync option, preventing CRD annotation size failures (e.g., CNPG `poolers` CRD).

### Fixed

- Fix `dorgu health` reporting control plane as "Unhealthy" in vCluster and managed Kubernetes environments (EKS, GKE, AKS, k3s). Control plane is now marked "Healthy (external/managed)" when no `tier=control-plane` pods are found but the API server is responsive.
- Fix `dorgu health --watch --json` producing no output. The command now emits an initial snapshot of active incidents and pending remediations on connect before streaming live events.
- Fix WebSocket connection failures showing raw low-level errors with no guidance. The CLI now prints actionable hints (port-forward command, Helm upgrade flags) based on the error type (DNS failure, connection refused, timeout).
- Fix `dorgu cluster info` showing "no installed components" after GitOps driver install. The GitOps flow now annotates the ClusterPersona at scaffold time, and falls back to ArgoCD Application discovery.
- Fix `dorgu cluster info` displaying raw Kubernetes API `NotFound` errors for services. Operator-only components (CNPG, cert-manager, external-secrets) now show a clean "no user-facing service" note; web-UI components use label-based service discovery as fallback.
- Fix CNPG chart version 0.23.0 causing GitOps sync failures. Bumped to 0.24.0, which has compliant CRD annotation sizes.
- Fix GitOps scaffold README and dry-run instructions including `values/` directory in `kubectl apply` commands. Values files are Helm overrides, not K8s resources; instructions now reference only `apps/` and `argocd/`.
- Fix ArgoCD Helm bootstrap install flooding the interactive wizard with verbose output. Output is now truncated to the last 5 lines by default; use `--verbose` for full logs.

## [0.7.3] - 2026-04-08

### Added

- `dorgu cluster info` command — show access URLs, port-forward commands, and credential retrieval instructions for all installed blessed stack components. Supports `--json` output.
- Post-install "Quick Access" summary printed after `dorgu cluster setup` completes, showing port-forward and credential commands for components with web UIs.
- Next steps section now mentions `dorgu cluster info` for easy discoverability.

## [0.7.2] - 2026-04-07

### Fixed

- Fix generated ingress host using underscores instead of hyphens for app names with underscores. Host is now DNS-sanitized via `ToDNSSubdomain()` to produce RFC1123-compliant values.
- Fix GitOps ArgoCD Application manifests using chart names with repo prefix (e.g., `argo/argo-cd` instead of `argo-cd`), causing all component apps to fail chart fetch.
- Fix GitOps scaffold missing environment-specific overrides for OpenObserve (ZO_LOCAL_MODE) in development environment.
- Fix false-positive kube-context drift detection during `dorgu cluster setup --dry-run`. Drift check is now skipped for dry-run executor.
- Fix `dorgu cluster status --json` returning tabular output instead of JSON.

### Added

- `dorgu persona list` command — list ApplicationPersonas across namespaces with table and `--json` output modes.
- Stream Helm output with dim styling during ArgoCD bootstrap install in GitOps driver (no `--verbose` flag required).

## [0.7.1] - 2026-04-07

### Fixed

- Fix `dorgu generate` failing with `path traversal detected` when CI or persona outputs write files outside the output subdirectory. The path traversal guard now anchors to the project root instead of the output subdir.
- Fix `dorgu cluster setup --dry-run` not validating explicit `--cluster-persona` flag. Preflight now fails fast with a clear error when the specified ClusterPersona does not exist, even in dry-run mode.
- Fix `dorgu cluster init` not setting `selfHealing` defaults on ClusterPersona. Init now populates `selfHealing.mode: observe` and `selfHealing.trustLevel: 2`.

### Added

- Kube-context drift guard for `dorgu cluster setup`. The active kube-context is captured at start and re-checked before each Helm operation. If the context changes mid-run, setup aborts with a clear error to prevent installing to the wrong cluster.

## [0.7.0] - 2026-04-05

### Added

- `dorgu remediation list` command — list pending, active, and completed remediations with severity, category, confidence, and phase. Supports `--phase`, `--severity`, `--namespace`, `--all`, and `--json` flags.
- `dorgu remediation diff <name>` command — terraform-style colored diff showing proposed Persona spec changes with explanation, rollback info, and action hints.
- `dorgu remediation approve <name>` command — approve a pending remediation for execution. Supports `--next` flag to approve the highest-severity pending remediation without specifying a name.
- `dorgu remediation reject <name>` command — reject a pending remediation with optional `--reason`.
- `dorgu health --watch` — real-time streaming of health updates, incidents, and remediations via WebSocket (JSONL in `--json` mode).
- Remediation hint in `dorgu health` output when pending remediations exist.

### Fixed

- Increase OpenObserve Helm install timeout to 15m for fresh installs (was defaulting to 5m, causing consistent timeouts).
- Show "validation skipped" instead of misleading `0/N` pod count when `--skip-validation` is used.
- Add input validation to prevent YAML injection and path traversal in user inputs.
- Propagate context through Ollama and Anthropic HTTP requests for proper timeout handling.
- Add `context.WithTimeout` to kubectl `exec.Command` calls.
- Use rune-based truncation for UTF-8 safety and remove dead code.
- Handle previously ignored errors across LLM, WebSocket, CLI, and generator packages.
- Prevent nil dereference in `HasAppConfig` and fix WebSocket race conditions.

### Changed

- Replace string concatenation with `strings.Builder` for better performance in hot paths.
- Hoist regex and map allocations to package level to reduce GC pressure.
- Replace custom `contains` helpers with `strings.Contains` and fix `Walk` logic.

## [0.6.1] - 2026-03-31

### Fixed

- Fix local git config that caused release commits to show "Test User" instead of the actual author.

## [0.6.0] - 2026-03-29

### Added

- `dorgu health` command — cluster health summary showing node status, resource saturation, control plane health, active incidents, and pending remediations. Works with or without operator (graceful degradation). Supports `--json` and `--namespace` flags.
- `dorgu incidents list` command — list active and recent incidents with severity, category, affected persona, and phase. Supports filtering by `--severity`, `--category`, `--phase`, `--namespace`, and `--all` (include resolved).
- `dorgu incidents describe <name>` command — detailed incident view with timeline, root cause diagnosis, confidence score, contributing signals, related events, and occurrence count.
- Table formatter (`internal/output/table.go`) with aligned columns, colored severity/phase/health rendering, and JSON array output mode.
- Colored diff renderer (`internal/output/diff.go`) for unified YAML diffs (terraform plan style) — foundation for Phase 2b `dorgu remediation diff`.
- `--json` output on all new commands (health, incidents list, incidents describe).

### Fixed

- Address go-reviewer findings on health and incidents command error handling and output formatting.

## [0.5.0] - 2026-03-26

### Added

- Output mode system with automatic TTY detection: human (styled), plain (piped), and JSON modes.
- `--json` global flag for machine-readable output (for scripting, AI agents, and CI pipelines).
- JSON output paths for: `persona status`, `sync status`, `sync pull`, `watch` (JSONL streaming), and `generate --dry-run`.
- Interactive forms using `charmbracelet/huh` for all CLI prompts (`dorgu init`, `dorgu cluster setup`).
- Non-interactive fallback: all prompts detect headless/piped environments and use safe defaults.
- `IsInteractive()`, `IsTTY()`, and `IsJSON()` helpers in the output package.
- GStack reference documentation.

### Changed

- Replace all `bufio.Reader` prompts with `charmbracelet/huh` forms (select menus, confirm dialogs, validated inputs).
- Gate lipgloss colors and spinners on TTY detection — clean output when piped or in `--json` mode.
- Upgrade `charmbracelet/lipgloss` from v0.9.1 to v1.1.0.
- Prompt functions in `internal/setup/prompts.go` no longer require a `*bufio.Reader` parameter.

## [0.4.0] - 2026-03-22

### Added

- `dorgu platform serve` command for platform API server.
- Driver flag (`--driver helm|gitops`) for cluster setup installation modes.
- Interactive skip and smart retry for failed Helm components.
- Verbose mode (`--verbose`) for real-time Helm output streaming during cluster setup.

### Changed

- Extract cli package into focused files: `persona_display.go`, `cluster_display.go`, `cluster_helm.go`, `cluster_gitops.go` (all files under 400 LOC).
- Extract analyzer package: defaults/LLM enhancement, endpoint detection, app config types.
- Extract generator package: K8s type definitions, label/annotation builders.
- Extract config package: type definitions, defaults logic, resource profile helpers.
- Extract setup package: executor types, kubectl operations, prompt/display functions.
- Add YAML tags to `ResourceValues` for dual decode safety.

### Fixed

- Isolate git commands from hook environment variables in analyzer.
- Remove duplicate next steps output in GitOps mode.
- Add sandbox to valid environments in cluster setup prompt.
- Fix missing closing braces in `installer_test.go`.

## [0.3.0] - 2026-03-11

### Added

- `cluster setup` wizard command with StackProvider and Executor abstractions.
- Blessed Stack composition: CNPG dependency, ArgoCD as optional default-on component.
- GitOps mode for cluster setup scaffold with interactive repo URL prompting.
- Kube-context safety guard and operator readiness gate for cluster setup.
- Failed Helm release cleanup and dependency enforcement in install loop.
- Component-specific timeouts and retry for Helm installs.
- Preflight version check and Helm repo update before install.
- Lipgloss-styled setup wizard output and redesigned cluster status visual hierarchy.
- Error hints for persona apply schema mismatch and cluster setup issues.
- `review` command and Go reviewer agent for automated code review.
- QA cluster setup agent and testing paths.
- Unit tests across analyzer, generator, and other packages.
- Clean persona display with new formatting output.
- Community standards: CODE_OF_CONDUCT.md, SECURITY.md, and CHANGELOG.md.
- Cluster quick start guide in README.

### Changed

- Consolidated phase/health color helpers into output package.
- Updated README with latest project information and cluster quick start.
- Structured YAML parsing for cluster status display.
- Rune-safe header truncation in setup wizard UI.
- Updated OpenObserve chart version.

### Fixed

- Validate cluster-persona flag and show real context in dry-run.
- Operator check for `dorgu-operator-system` namespace.
- Improved ClusterPersona not-found error with hints.
- Show enablement guidance when sync WebSocket connection fails.
- Remove duplicate `imageRunsAsRoot` from technical section (BUG-001).

## [0.2.x]

### Added

- Core CLI: `generate`, `init`, `config`, `version` with layered config (global, workspace, app).
- Persona commands: `persona generate`, `persona apply`, `persona status` for ApplicationPersona CRDs.
- Cluster commands: `cluster init`, `cluster status` for ClusterPersona.
- Real-time: `watch personas`, `watch cluster`, `watch events` and `sync status`, `sync pull` (requires operator WebSocket).
- Post-generation validation and optional LLM-enhanced analysis (OpenAI, Anthropic, Gemini, Ollama).
- ArgoCD Application and GitHub Actions workflow generation.

### Changed

- (Specific changes for each release can be listed here as versions are tagged.)

---

For release steps, see [CONTRIBUTING.md](CONTRIBUTING.md#releasing-maintainers).
