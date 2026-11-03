# Migrating to v0.13.0

v0.13.0 removes every CLI surface that is not part of the self-healing harness. Dorgu is now one thing: the
operator detects, diagnoses and proposes; this CLI is how a human reads a proposal and approves, rejects or heals
it. Code that did something else was a second product with its own maintenance cost and, in one case, its own
data path.

The last commit with everything below is tagged **`pre-harness`**. If you depend on one of these commands, build
from that tag:

```bash
go install github.com/dorgu-ai/dorgu/cmd/dorgu@pre-harness
```

## Removed commands

| Removed | Why | Use instead |
|---|---|---|
| `dorgu generate` | Manifest generation from a Dockerfile, Compose file and source. Not part of detect, diagnose, heal. | Your existing Helm chart, kustomize overlay or manifests |
| `dorgu init`, `dorgu config` | Only configured `generate` and its LLM provider | Nothing to configure: cluster commands read your kubeconfig |
| `dorgu persona generate`, `dorgu persona apply` | Built a persona from local source | `dorgu persona import -n <namespace> --all`, which reads the Deployments already running |
| `dorgu cluster setup`, `dorgu cluster info` | Installed and described a curated add-on stack. Not Dorgu's job | Install add-ons with their own charts |
| `dorgu cluster init` | Hand-created a ClusterPersona | The operator creates `dorgu-cluster` on startup |
| `dorgu platform serve` | ClusterPersona visualisation UI | A redesigned `dorgu dashboard` is planned |
| `dorgu watch ...`, `dorgu sync ...`, `dorgu health --watch` | Streamed over the operator WebSocket | `dorgu health`, `dorgu incidents list`, `dorgu remediation list` (or `kubectl get -w` on the CRDs) |
| global `--config` flag | Pointed at the `.dorgu.yaml` that only `generate` read | None |

## Removed data path

The CLI's own LLM integration (`internal/llm`: OpenAI, Anthropic, Gemini, Ollama) is gone. `dorgu generate` and
`dorgu persona generate|apply` defaulted to OpenAI and sent the application analysis there whenever a key was
configured or `OPENAI_API_KEY` was set, with no opt-in prompt.
That path no longer exists. AI diagnosis and remediation plans are produced by the operator, in your cluster, with a
key you put in a Secret, and only when you enable it in the chart.

`OPENAI_API_KEY`, `GEMINI_API_KEY`, `ANTHROPIC_API_KEY` and `OLLAMA_HOST` are no longer read by the CLI, and
`~/.config/dorgu/config.yaml` and `.dorgu.yaml` are ignored. You can delete them.

## What did not change

`dorgu health`, `dorgu incidents list|describe`, `dorgu remediation list|diff|approve|reject|heal`,
`dorgu persona import|list|status`, `dorgu cluster status` and `dorgu version` behave as in v0.12.0, with these
additions:

- Error hints now give the `helm install` command for the operator instead of the removed `dorgu cluster setup`.
- The production-context guard on `heal` matches `DORGU_PROTECTED_CONTEXTS` (comma-separated, case-insensitive
  substrings, default `prod`).
