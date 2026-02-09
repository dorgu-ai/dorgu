# Contributing to Dorgu

Thank you for considering contributing to Dorgu. This document explains how to report issues, propose changes, and submit pull requests.

---

## Table of contents

- [Code of conduct](#code-of-conduct)
- [How to raise an issue](#how-to-raise-an-issue)
- [How to contribute code](#how-to-contribute-code)
- [Development setup](#development-setup)
- [Code standards](#code-standards)
- [Pull request process](#pull-request-process)

---

## Code of conduct

Be respectful and constructive. We aim to keep the community inclusive and focused on building a useful tool for Kubernetes users.

---

## How to raise an issue

### Before opening an issue

- **Search** [existing issues](https://github.com/dorgu-ai/dorgu/issues) to see if the bug or idea is already reported.
- For **bugs**, try to reproduce with the latest release or `main` and note your environment (OS, Go version, dorgu version).
- For **feature requests**, describe the use case and why it would help.

### Opening an issue

1. Go to [github.com/dorgu-ai/dorgu/issues](https://github.com/dorgu-ai/dorgu/issues).
2. Click **New issue** and choose the template that fits (Bug report, Feature request, or use “Open a blank issue”).
3. Fill in the template:
   - **Title:** Short, clear summary (e.g. “Generate fails when Dockerfile has no EXPOSE”).
   - **Description:** What happened vs what you expected, steps to reproduce (for bugs), or rationale (for features).
   - **Environment:** OS, Go version, `dorgu version` (if applicable).
   - **Logs/screenshots:** Paste relevant output or errors (redact secrets).

We’ll use the issue to discuss and, when ready, link a pull request.

---

## How to contribute code

### What we welcome

- **Bug fixes** — Fixes for issues tagged `bug` or confirmed by maintainers.
- **Documentation** — Fixes and improvements to README, CONTRIBUTING, comments, and docs.
- **Features** — For larger features, open an issue first so we can align on scope and design.

### What to do

1. **Fork** the repo: [github.com/dorgu-ai/dorgu](https://github.com/dorgu-ai/dorgu) → **Fork**.
2. **Clone** your fork and add upstream:
   ```bash
   git clone https://github.com/YOUR_USERNAME/dorgu.git
   cd dorgu
   git remote add upstream https://github.com/dorgu-ai/dorgu.git
   ```
3. **Create a branch** from `main`:
   ```bash
   git fetch upstream
   git checkout -b fix/short-description upstream/main
   ```
   Use a descriptive prefix: `fix/`, `docs/`, `feat/`, `chore/`.
4. **Make your changes** (see [Development setup](#development-setup) and [Code standards](#code-standards)).
5. **Commit** with a clear message:
   ```bash
   git add .
   git commit -m "fix: handle missing EXPOSE in Dockerfile"
   ```
   Prefer present tense and a short first line (e.g. `fix: ...`, `docs: ...`, `feat: ...`).
6. **Push** and open a **Pull request**:
   ```bash
   git push origin fix/short-description
   ```
   Open the PR against `dorgu-ai/dorgu` `main`. Fill in the PR template and reference any issue (e.g. `Fixes #123`).

---

## Development setup

**Requirements:** Go 1.21+

```bash
# Clone (or use your fork)
git clone https://github.com/dorgu-ai/dorgu.git
cd dorgu

# Build
make build

# Run tests
make test

# Run the CLI locally
./dorgu generate ./testapps/sample_app_java_spring   # if you have a sample app
./dorgu version
```

Optional: set `OPENAI_API_KEY` or `GEMINI_API_KEY` (or use `dorgu config set llm.api_key`) to test LLM-backed analysis.

---

## Code standards

- **Format:** Run `make fmt` (or `gofmt -s -w .`) before committing.
- **Lint:** Run `make lint` and fix reported issues.
- **Tests:** Run `make test`. New code should include or extend tests where appropriate (e.g. parsers, config, validation).
- **Style:** Follow existing patterns in the codebase; keep functions focused and names clear.

---

## Pull request process

1. **Target branch:** Open the PR against `main` of `dorgu-ai/dorgu`.
2. **Description:** Use the PR template: what changed, why, and how to verify. Link the issue if one exists (e.g. `Fixes #42`).
3. **CI:** The PR must pass CI (build, tests, lint). We’ll re-run if needed.
4. **Review:** A maintainer will review. Address feedback by pushing new commits to the same branch.
5. **Merge:** Once approved, a maintainer will merge. Your contribution will be included in the next release.

---

## Releasing (maintainers)

Releases are **tag-driven**. Pushing a tag matching `v*` (e.g. `v0.2.0`) triggers the Release workflow, which runs tests and [GoReleaser](https://goreleaser.com) to build binaries and publish a [GitHub Release](https://github.com/dorgu-ai/dorgu/releases) with assets.

**To cut a release:**

1. Ensure `main` has all changes you want in the release.
2. Create an annotated tag:  
   `git tag -a v0.2.0 -m "Release v0.2.0"`
3. Push the tag:  
   `git push origin v0.2.0`
4. The [Release workflow](.github/workflows/release.yaml) runs automatically; when it finishes, the release and download assets appear on the repo’s Releases page. No separate “update” of the tag is needed—the tag is the release.

`go install github.com/dorgu-ai/dorgu/cmd/dorgu@latest` will then point at the latest release.

---

## Questions

If something is unclear, open a [Discussion](https://github.com/dorgu-ai/dorgu/discussions) or add a comment on the relevant issue or PR.

Thank you for contributing to Dorgu.
