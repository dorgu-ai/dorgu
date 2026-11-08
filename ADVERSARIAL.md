# Adversarial bypass scenarios: CLI

Each scenario is an attack on the heal path and the *safe* outcome Dorgu must reach. Every one has a test in
`internal/cli/adversarial_test.go` that asserts the safe outcome. A scenario that is not fixed yet is listed in that
file's `openScenarios` map with the checkpoint that should close it: its test skips with `OPEN`, so CI stays green
while the gap stays visible. When a fix lands, the test fails until the scenario is removed from the open list, and
a closed scenario that regresses fails outright.

```bash
make adversarial   # per-scenario status and a blocked/total summary
```

The operator's scenarios (OP-xx, ID-xx) live in the operator repo under `test/adversarial/`. The full catalogue and
checkpoint plan are in the harness plan, phase P0.

| ID | Attack | Safe outcome | Green by | Status |
|---|---|---|---|---|
| CLI-01 | A RemediationAction claims `workloadRef.managedBy: unmanaged` for a Deployment that Helm owns | Actuator reads ownership from the live object and refuses | P1 B5 (stopgap) | **Blocked** |
| CLI-02 | `workloadRef.name` empty with `managedBy: unmanaged`, so `--workload` or the fallback chain picks any Deployment | Refused: no workload's ownership was checked | P1 B5 (stopgap) | **Blocked** |
| CLI-03 | `dorgu remediation heal` on a RemediationAction with an empty `status.phase` | Refused: no ticket | P1 B5 | **Blocked** |
| CLI-04 | The production kube context is renamed so it no longer matches `DORGU_PROTECTED_CONTEXTS` | Safety does not depend on context names (ticket + identity) | P1 B5 (partial, see below) | **Blocked** |
| CLI-05 | A kubectl flag value equal to a verb name (`kubectl -n logs delete deployment/foo`) | Classified as a write; never printed as runnable on an owned workload | CP0 (P0 task 0.11) | **Blocked** |
| CLI-06 | The workload was unmanaged at proposal time and Argo CD adopted it before the heal | Actuator re-checks ownership live and refuses | P1 B5 (stopgap) | **Blocked** |

Current: **6/6 blocked** (was 1/6).

What closed them (P1 B5, task 1.4): `approve` sets Approved, waits up to 30 s for the operator's `status.applyTicket`,
and patches exactly the ticket; `heal` needs an approved remediation and the same ticket, and refuses a Pending one
(CLI-03). Before patching, the CLI re-reads the live Deployment, re-detects ownership from it (Helm, Argo CD, Flux,
kustomize stamps, a controller ownerReference, GitOps field managers) and refuses a Deployment the proposal called
unmanaged (CLI-01, CLI-06), refuses a record that names no workload (CLI-02), and requires the Deployment's UID and
`metadata.generation` to match the ticket (generation, not resourceVersion: status and scale writes move the latter
and change nothing the ticket would patch).

Review fixes (B5 fix round): the CLI ignores any ticket that was on the action before its own approval write, and
accepts one only if it was issued (`status.applyTicket.issuedAt`, operator-set) at or after the `status.approvedAt` the
CLI recorded (its own clock), with at most ten minutes of life, in phase `Ticketed` or the `Applying` the operator moves to in the same
pass. Immediately before it writes, it re-reads the RemediationAction (same ticket, unexpired, still `Ticketed` or
`Applying`), the Deployment (same UID and generation) and the ClusterPersona (not `observe`; unreadable refuses). The
footprint strip removes only the exact `dorgu` Update entry it created, never another manager's.

Known P1 gap, not closed here: a client with `remediationactions/status` can write a whole ticket and its history, and
the CLI cannot tell it from the operator's. The checks above make a forgery harder (it has to be issued after the
approval, short-lived, bound to the live UID and generation, and pass the observe-mode check) but not impossible.
Closing it is P2 task 2.5: an admission policy that lets only the operator ServiceAccount write the ticket and the
phases after approval (maps to the ID-01 scenarios, open until CP2).

CLI-04 is blocked for the scenario as written: an approved action that holds no ticket is not applied. It is not an
identity binding. The protected-context guard is still a name match, and a ticket does not say which context the
user should be writing to, so a renamed production context with a valid ticket still heals. That is phase P3's
ticket-plus-identity work, and this row says "blocked" only for the no-ticket case the test asserts.

The CLI-side ownership re-detection is a stopgap and duplicates a subset of the operator's detection on purpose; the
operator's gate owns the decision, and the CLI no longer trusts the record alone.
