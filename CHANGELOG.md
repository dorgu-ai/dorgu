# Changelog

All notable changes to the Dorgu CLI are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Community standards: [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md) (Contributor Covenant 2.1), [SECURITY.md](SECURITY.md), and [CHANGELOG.md](CHANGELOG.md).

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
