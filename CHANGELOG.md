# Changelog

All notable changes to the Dorgu CLI are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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
