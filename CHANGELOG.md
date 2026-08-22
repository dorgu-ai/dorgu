# Changelog

All notable changes to the Dorgu CLI are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.10.0] - 2026-08-23

### Upgrade notes

> **1. New exit code `4` (`ExitDeclined`) separates "declined by design" from "broke".** When Dorgu refuses to patch a workload something else owns, the command ran, the plan was understood, and the decision was not to write. That is not a failure, and a script should be able to tell the difference. **Any wrapper that treats every non-zero exit as breakage will now report a successful refusal as an error and needs updating.** The full set is `0` success, `1` failure, `4` declined. To record the decision without a workload patch, use `dorgu remediation approve <name> -n <ns> --no-heal`, which exits `0`.
>
> **2. `--workload` can no longer aim the patch at a different Deployment.** Ownership is a fact about one specific object, so a flag that redirects the patch to a Deployment the operator never observed is the guard with a hole in it: Dorgu would have cleared `frontend` as unmanaged and then written to `frontend-canary`, which Helm owns. The flag still resolves the workload, but it must agree with the one recorded in `spec.workloadRef`, and a mismatch is refused. **`--container` still overrides freely,** because ownership is per-Deployment, not per-container. When `--container` is omitted, the container the operator actually read is used, so the patch targets the same container whose values were the diff's before-state.
>
> **3. Pair this release with operator v0.9.0 or newer.** The refusal is driven by `spec.workloadRef`, which the operator only started recording in **v0.9.0**. Against an older operator the field is absent, and an absent record is treated as owned on purpose: it means either an operator too old to know or a workload that could not be read, and neither is evidence that patching is safe. The practical effect is that **against operator v0.8.1 or older, any remediation carrying a workload resource change is declined** and `--no-heal` becomes the way through. Advisory-only remediations are unaffected: they have no workload change, so there is nothing to refuse.

### Changed

- **Dorgu will not patch a workload that Helm, ArgoCD, Flux or kustomize owns.** Approving an OOM fix on a Helm-managed app used to patch the Deployment directly and print `✓ Healed apps/frontend-podinfo`. The next `helm upgrade` then hard-failed on a field-manager conflict, because the patch had claimed those fields away from Helm. Clean-room run #2 hit exactly that. `dorgu remediation heal` and `dorgu remediation approve` now read `spec.workloadRef` and decline for every `managedBy` except `unmanaged`. There is no `--force` and no override flag, on purpose.
- **The refusal is a trust moment, not an error message.** It names the owner using the operator's own `managedByDetail` wording, prints the Deployment diff so declining is an informed decision rather than a dead end, hands over the owner-shaped steps the operator already generated (which values to set for a Helm release, which Git manifests for an ArgoCD application), gives one line on what a direct patch would have broken, states plainly that nothing was approved and nothing in the cluster changed, and points at `--no-heal` for recording the decision.
- **Approval is withheld along with the patch.** Approving is what tells the operator to patch the persona and start the verification clock, so approving a change the CLI will not apply leaves the persona at 128Mi, the workload at 32Mi, and a ten-minute verification window running over a fix that was never coming. The gate therefore sits in the preflight, ahead of any write: on an owned workload, `approve` writes nothing at all, neither a Deployment patch nor a status patch.
- **`dorgu remediation diff` compares workload to workload instead of persona to persona.** The old diff read the persona on both sides, so a fix that silently introduced a CPU limit the container never had showed nothing at all. The diff now carries a `Workload:` and `Owner:` header pair and a Deployment change block built from `observedResources`. A key the workload does not set renders as `not set` with an explicit `(adds a key this workload does not set)` marker, never as blank and never as zero. Where the operator observed nothing, the before column reads `unknown`, because "not read" is a different fact from "not set". On an owned workload the diff also stops suggesting `approve`, since that command would be declined, and offers `--no-heal` and `reject` instead.
- **Step commands are printed only where they are runnable.** Anything on an `unmanaged` workload is printed unchanged. On an owned one, only read-only commands survive: `kubectl logs` and `kubectl get events` are kept, because they matter most on exactly the workloads Dorgu will not patch, while a command that writes takes field ownership from Helm or ArgoCD and breaks their next apply. The operator already strips the writing commands, but the command is model-authored, so the CLI establishes read-only-ness itself rather than trusting that stripping. It mirrors the operator's verb sets with the same scan-for-the-first-known-verb rule, so `kubectl -n apps patch ...` cannot hide the verb behind a flag argument. Read-only verbs are listed explicitly and anything unrecognised is refused, including a bare `kubectl rollout` with no subcommand. The existing shell-metacharacter check still runs first, on owned and unmanaged workloads alike.

### Fixed

- The `/release` runbook told the releaser to `git push origin main`. This repo's default branch is `master`, so the push would have failed outright at the last step of a release.
- `k8s.io/apimachinery` was marked `// indirect` in `go.mod` while `internal/importer` imported it directly, so any `go build` rewrote the file and left the tree dirty mid-release. `make check` runs gofmt, vet and tests, none of which look at module hygiene, so nothing caught it. No dependency version changed and `go.sum` is untouched.

## [0.9.0] - 2026-08-16

### Upgrade note

> **`dorgu health` no longer always exits 0.** It used to exit 0 whether the cluster was healthy, on fire, or unreachable, which made it useless as a check and actively misleading in a script. Two changes:
>
> - **Unreachable cluster now exits 1.** Any script that ran `dorgu health` against a cluster it could not reach previously saw exit 0 and an empty-looking summary; it will now see a failure. This is unconditional, with no flag to opt out, because reporting health for a cluster you cannot see is the one thing the command must never do.
> - **`--exit-code` is opt-in** and adds: **2** when critical incidents are active, **3** when health cannot be judged. Without the flag, a reachable cluster still exits 0 regardless of what it finds, so interactive use is unchanged.
>
> If you have a monitoring job that treats any non-zero exit as an alert, it will now alert on an unreachable cluster. That is the intended behaviour, but check the job before upgrading if a transient kubeconfig or VPN failure would page someone. All four codes are documented in `dorgu health --help`.

### Added

- **`dorgu persona import -n <namespace> [--all | --name <deployment>]`**, the onboarding path for a cluster that already has apps running. Dorgu only watches workloads that have an ApplicationPersona, and until now the only way to get one was `persona generate`, which needs local source and a Dockerfile. On a cluster full of existing Deployments, Dorgu simply saw nothing: no personas, so no incidents, so no remediations, for any real user who was not starting from the tutorial app. `persona import` reads the live Deployments and synthesizes a persona for each from what is already in the spec: resources, probes, replicas, ports, image and ownership labels. No local source, no Dockerfile, no relabelling. It prints the YAML by default so you can read it before anything reaches the cluster; `--apply` creates the personas, `-o` writes them to a file. The persona name is chosen so the operator resolves it back to that same Deployment, and where no name can (another Deployment's label outranks it) the CLI says so rather than pointing Dorgu at the wrong workload. Containers with no resource limits get inferred ones, reported explicitly, because the remediation proposer skips personas without limits and a persona that cannot heal is worse than no persona.
- `dorgu health` names the Deployments it cannot see. With three unmonitored Deployments broken, health used to report `Active Incidents: 2` (both from the docs' toy app) and never mention the others: it presented a blind spot as health. There is now an `Unmonitored` section listing every Deployment with no matching ApplicationPersona, one `dorgu persona import` hint per namespace, and an `unmonitored` object in `--json` carrying the count and the full uncapped list. Cluster add-on namespaces are skipped unless `-n` asks for one, the printed list is capped at 10 names and says how many it left out, and a missing ApplicationPersona CRD is reported as a fact about the operator rather than about the apps.
- **Advisory remediation steps print the command that carries them out.** For an ImagePullBackOff the planner would identify `nginx:1.27-alpineX` as a typo, name `nginx:1.27-alpine` as the fix, link Docker Hub, and then leave the reader to write the `kubectl set image` themselves. `dorgu remediation diff` and the heal preamble now print a `Run:` line under any advisory step that carries a command. The command is only ever printed, never executed, and the CLI re-checks it before showing it (single line, must start with `kubectl `, no shell metacharacters) rather than trusting whatever wrote the RemediationAction. Requires operator v0.8.0 or newer to populate the field.
- `dorgu health --exit-code` makes the command usable in a monitoring script: exit **2** when critical incidents are active, **3** when health cannot be judged (for example the incident records cannot be read). Without the flag the command keeps exiting 0 whenever it managed to read the cluster, so interactive use is unchanged. All four codes are documented in `dorgu health --help`.

### Changed

- **`dorgu --help` describes the product we ship.** The root help called Dorgu an "AI-powered Kubernetes application onboarding" tool and gave only `generate` and `init` examples, contradicting dorgu.run, both READMEs and the docs, so the first thing a stranger read was about a manifest generator. It now leads with the self-healing loop (`health`, `incidents`, `remediation diff`/`approve`), states that nothing is applied without your approval, points at `dorgu persona import` for a cluster that already has apps, names the `kubectl` prerequisite, and documents `DORGU_DEBUG`. Manifest generation stays, explicitly as a secondary capability.
- **Binary install instructions that work.** The documented command was `curl -Lo dorgu .../releases/latest/download/dorgu_linux_amd64`. No such asset has ever been published, so it 404'd, `curl` exited 0 anyway, and the next two lines `chmod +x` and `sudo mv`'d a 9-byte file containing `Not Found` into `/usr/local/bin`. Anyone without a Go toolchain got a broken install and no error. The README now carries a real `curl -fLO` plus `tar` recipe with the actual asset names, and GoReleaser publishes **unversioned `dorgu_<os>_<arch>.tar.gz` aliases** alongside the versioned archives, so `https://github.com/dorgu-ai/dorgu/releases/latest/download/dorgu_linux_amd64.tar.gz` is a copy-paste URL that never needs a version bump. **This is the first release that publishes those aliases.** The release notes template now contains a working no-Go recipe too.

### Fixed

- **`dorgu health` no longer reports health for a cluster it cannot reach.** Against an unreachable cluster it printed an empty node table, `Active Incidents: 0`, `"activeIncidents": {"count": 0}` in `--json`, and exited **0**. A monitoring script could not tell "healthy" from "on fire" from "cannot see the cluster". The command now probes the API server first (via `/version`, which any authenticated user can read, so a namespace-scoped role is not mistaken for an unreachable cluster) and fails with `cannot reach cluster; check your kubeconfig/context` plus the kubectl detail and context hints, exiting **1**. No summary is rendered from a failed call.
- **An unreadable incident list reads `unknown`, not `0`.** Any failure listing `IncidentMemory` was flattened to "0 incidents", so an operator that is not installed and a cluster with nothing wrong looked identical. The summary now distinguishes the two, names the reason, and `--json` carries `unavailable` and `reason`.
- **`dorgu remediation diff` stops recommending the command that fails.** For a plan with no auto-applicable step it printed `dorgu remediation approve ...` as its own suggested action; following that instruction marked the remediation `Failed` and put the app into a 30-minute remediation cooldown. An advisory plan now says "This plan is advisory: nothing in it can be applied for you", keeps its manual steps, and offers only `reject`. `approve` on an advisory plan reports "Approval recorded. This plan is advisory, so nothing was applied", matching the operator's new `Acknowledged` phase.
- **Malformed saturation line.** `CPU: n/a requests / allocatable ( / 3860m)` had an empty left operand whenever the cluster reported no used figure. Every missing value now reads `n/a` on both sides, and a genuine `0` on an idle cluster is printed as `0` rather than hidden behind `n/a`. (The operator side of this, computing the figures at all, is a matching operator change.)
- **An unknown subcommand is an error.** `dorgu remediation frobnicate` printed the manual and exited **0**. Cobra returns "print help" for a command that is not runnable before it validates arguments, so every subcommand group silently accepted nonsense; each group now handles its own arguments and reports `unknown command "frobnicate" for "dorgu remediation"`, a suggestion when there is a near match, and a non-zero exit.
- **Runtime errors print the error, not the manual.** A genuine failure dumped roughly 15 lines of usage with the real message on the last line. Usage is now silenced once argument parsing is done, so flag and argument mistakes still show usage while runtime failures show the failure.
- `dorgu remediation approve` resolves the target Deployment **before** it approves. Approval is what tells the operator to patch the persona and start the verification clock, so approving first and failing to find the workload afterwards left the persona at the new limits, the running workload at the old ones, and a 10-minute `Applying`/`Verifying` window over a change that never landed. When the heal cannot be planned, nothing is approved and nothing is changed, and the CLI says which Deployments it did find.
- A failed workload patch is never reported as a heal. The CLI now names the divergence: "the remediation is Approved but `<ns>/<deployment>` was NOT patched; the persona and the workload now disagree."
- Workload discovery no longer requires a label on the Deployment object. Helm, kustomize and most hand-written YAML label the pod template only, so `dorgu remediation heal` failed with `no Deployment found for app "<name>"` while the Deployment sat in the same namespace. Discovery now walks the same ordered chain as the operator: `app.kubernetes.io/name` label, `app` label, `metadata.name`, then `spec.selector.matchLabels`. Zero and ambiguous matches list the Deployments actually present and point at `--workload`.
- `--no-heal` warns that the persona and the running workload will disagree until the change is applied by hand.
- **client-go noise no longer buries the real error.** Every cluster command shells out to kubectl and showed its combined output, so one failed API call surfaced five identical `memcache.go:265 "Unhandled Error"` lines in front of the single line that said what went wrong. klog-formatted lines are now stripped from every kubectl error the CLI surfaces; if filtering would leave nothing, the original text is shown rather than an empty error, and `DORGU_DEBUG=1` keeps the raw output. This also fixes a real hazard: the current kube-context was read the same way, so klog output could have been glued to the front of the context name that the production-heal guard compares against.
- **`dorgu remediation diff` prints one paragraph once.** `Explanation:` and `Plan summary:` rendered the same text under two headings. The operator no longer writes near-identical values into both fields, and the CLI suppresses the duplicate for RemediationActions written by older operators.

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
