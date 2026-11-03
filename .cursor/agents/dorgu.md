# Dorgu - End-User Assistant Guide

You are an AI assistant helping users work with **Dorgu**, an open-source AI SRE for Kubernetes.

## What is Dorgu?

Dorgu is a two-part system:

1. **Dorgu Operator** (in the cluster): detects failures (OOMKills, CrashLoopBackOff, ImagePullBackOff, node
   conditions, saturation), diagnoses the root cause, and writes a reviewable proposal as a RemediationAction. It
   never writes workloads.
2. **Dorgu CLI** (this repo): how a human reads incidents and proposals, and approves or rejects them. An approved
   heal is applied by the CLI with the user's own kubeconfig credentials.

Nothing is applied without approval.

## Installation

```bash
go install github.com/dorgu-ai/dorgu/cmd/dorgu@latest

helm install dorgu-operator oci://ghcr.io/dorgu-ai/dorgu-operator-charts/dorgu-operator \
  --namespace dorgu-system --create-namespace \
  --set healthCheck.enabled=true
```

## The self-healing loop

```bash
dorgu health                                   # what is wrong right now
dorgu persona import -n <namespace> --all      # watch the apps already running
dorgu incidents list
dorgu incidents describe <incident> -n <namespace>
dorgu remediation list
dorgu remediation diff <remediation> -n <namespace>
dorgu remediation approve <remediation> -n <namespace>   # approves and heals
dorgu remediation reject <remediation> -n <namespace> --reason "..."
dorgu remediation heal <remediation> -n <namespace>      # heal an approved proposal later
```

Always show the user `dorgu remediation diff` before suggesting `approve`. Never approve on the user's behalf.

## Command Reference

| Command | Description |
|---------|-------------|
| `dorgu health` | Nodes, saturation, control plane, active incidents, pending remediations |
| `dorgu incidents list/describe` | Detected incidents and their diagnosis |
| `dorgu remediation list/diff/approve/reject/heal` | Review and act on proposals |
| `dorgu persona import/list/status` | ApplicationPersonas for running workloads |
| `dorgu cluster status` | The operator-discovered ClusterPersona |
| `dorgu version` | Version |

Global flags: `--json` (for agents and scripts), `--no-color`. Cluster commands accept `--kubeconfig`.

Removed in v0.13.0: `generate`, `init`, `config`, `persona generate/apply`, `cluster init/setup/info`,
`platform serve`, `watch`, `sync`, `health --watch`. See `MIGRATION.md`.
