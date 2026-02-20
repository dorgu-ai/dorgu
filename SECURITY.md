# Security Policy

## Supported Versions

We provide security updates for the following versions of Dorgu CLI:

| Version | Supported          |
| ------- | ------------------ |
| 0.2.x   | :white_check_mark: |
| < 0.2   | :x:                |

During the 0.x release line, we focus on the latest minor version (e.g. 0.2.x). When we release a new major or minor line, we will update this table and may end support for older lines with notice.

## How to Report a Vulnerability

We take security seriously. If you believe you have found a security vulnerability, please report it privately so we can address it before public disclosure.

**Preferred method:** Open a **private** security advisory on GitHub:

1. Go to [github.com/dorgu-ai/dorgu](https://github.com/dorgu-ai/dorgu).
2. Click **Security** → **Advisories** → **New draft security advisory**.
3. Describe the vulnerability, steps to reproduce, and impact. Do not disclose it in public issues or PRs.

**What to expect:**

- We will acknowledge your report within **5 business days**.
- We will work with you to understand and validate the issue.
- We will not disclose the vulnerability publicly before a fix is available, and we ask that you do the same during the process.
- We will credit you in the advisory (unless you prefer to remain anonymous) when we publish the fix.

## Scope

**In scope:**

- The Dorgu CLI binary and its behavior (manifest generation, config loading, file I/O).
- Dependencies shipped with or required by the CLI (e.g. Go modules).
- Security impact of generated manifests (e.g. insecure defaults we emit).

**Out of scope:**

- The Kubernetes cluster or operator you run; report operator issues in the [dorgu-operator](https://github.com/dorgu-ai/dorgu-operator) repository.
- Third-party tools (ArgoCD, Prometheus, etc.) or your own application code.
- General usage questions; use [Discussions](https://github.com/dorgu-ai/dorgu/discussions) or [Issues](https://github.com/dorgu-ai/dorgu/issues) for those.

Thank you for helping keep Dorgu and its users safe.
