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
| CLI-01 | A RemediationAction claims `workloadRef.managedBy: unmanaged` for a Deployment that Helm owns | Actuator reads ownership from the live object and refuses | CP3 (stopgap P1) | Open |
| CLI-02 | `workloadRef.name` empty with `managedBy: unmanaged`, so `--workload` or the fallback chain picks any Deployment | Refused: no workload's ownership was checked | CP3 (stopgap P1) | Open |
| CLI-03 | `dorgu remediation heal` on a RemediationAction with an empty `status.phase` | Refused: no ticket | CP3 | Open |
| CLI-04 | The production kube context is renamed so it no longer matches `DORGU_PROTECTED_CONTEXTS` | Safety does not depend on context names (ticket + identity) | CP3 | Open |
| CLI-05 | A kubectl flag value equal to a verb name (`kubectl -n logs delete deployment/foo`) | Classified as a write; never printed as runnable on an owned workload | CP0 (P0 task 0.11) | **Blocked** |
| CLI-06 | The workload was unmanaged at proposal time and Argo CD adopted it before the heal | Actuator re-checks ownership live and refuses | CP3 | Open |

Current: **1/6 blocked**.

Why most are open: the CLI is the actuator, and every check it runs is advisory to anything that does not go through
it (see `ARCHITECTURE_CLI.md` §11 in the docs repo). The harness moves the decision into the operator, which writes
an apply ticket bound to the workload UID, the patch and the approver; the CLI then executes only a valid ticket.
That is phase P3. P1 adds stopgaps for CLI-01 and CLI-02 (live ownership re-detection, refusing an unnamed workload).
