---
status: prompted
approved: "2026-09-03T19:27:29Z"
generating: "2026-09-03T19:29:15Z"
prompted: "2026-09-03T19:39:58Z"
branch: dark-factory/sentry-collector-nil-result
---

## Summary

- `agent.Run` returns `(nil, nil)` when a phase's steps are all skipped (`ShouldRun=false`) or the phase has no registered steps
- Both entry points convert that nil result into a hard error → Job exits 1 → Job-controller retry loop (observed on nukeprod, task `1363bb29-9af1-5c8d-834f-ca9e1bdd87b3`)
- The guard's own comment says "treat as a no-op completion rather than panicking", but the implementation returns an error — the comment/code contradiction is the defect
- Fix: deliver a `Failed` result to the task (terminal state) and exit 0 (no controller retry), mirroring the agent lib's `unsupportedPhase` pattern
- Regression test required in both `main_test.go` and `cmd/run-task/main_test.go`

## Problem

`sentry-collector-agent` Job pods for task `1363bb29-9af1-5c8d-834f-ca9e1bdd87b3` crash-retry looped on nukeprod (`K8sPodStatus` critical alert, 2026-08-27, recurring since 2026-08-26). The loop wastes cluster resources, keeps the alert firing, and the task never reaches a terminal state — so the underlying Sentry issue it was meant to triage never gets resolved.

### Reproduction

Mechanism (verified by reading the code, 2026-09-03):

1. `StepRunner.Run` (`github.com/bborbe/agent`, `agent_runner.go:38-85`) walks a phase's steps; when `ShouldRun` returns `false` (e.g. the phase's output section `## Analysis` already exists from a prior run), the step is skipped. When every step is skipped, `lastResult` stays nil and the runner returns `(nil, nil)`.
2. `Agent.Run` (`agent_agent.go:45-110`) forwards that: `result == nil → return lastResult, nil`.
3. Both entry points in this repo treat the nil result as a hard failure:
   - `main.go:305-314` — records `AgentStatusFailed` + duration, then `return errors.Errorf(ctx, "agent run returned nil result (all steps skipped for phase %s)", a.Phase)`
   - `cmd/run-task/main.go:125-133` — same `errors.Errorf` (no metrics)
4. A returned error makes the process exit 1 → the Job-controller re-creates the Job pod → repeat until the configured backoff limit. Nothing is ever delivered to the task, so it stays `in_progress` and keeps being re-dispatched.

Version context: the incident (2026-08-26/27) ran on the then-deployed build; the identical guard is present in both `v0.9.0` (7486981) and current `origin/master` `v0.9.1` (f6fa861) — reproduced on both. Live check 2026-09-03: no `sentry` Jobs/pods on nukeprod today (the v0.7.3 `NextPhase: "done"` fix cleared the loop), both Config CRDs (`sentry-collector-agent`, `sentry-analyzer-agent`) point at `docker.prod.nuke.benjamin-borbe.de:443/agent-sentry-issue-analyzer:prod`. The 2026-08-27 Job log excerpt showing the exit-1/re-creation is not retrievable in this session; the mechanism is pinned by code evidence below.

**Expected vs Actual**

- Expected (per the guard's own comment at `main.go:307-308`): a stepless phase is a no-op completion — no crash, no retry.
- Actual: `errors.Errorf` → exit 1 → Job-controller retry loop; task never reaches a terminal state.

The agent lib itself shows the canonical semantic-failure pattern at `agent_agent.go:135-155` (`unsupportedPhase`): deliver a `Failed` result via the deliverer, return `(result, nil)` — the process exits 0 and the task is closed out. This repo's guards do the opposite.

## Goal

A `sentry-collector` task whose phase has no registered steps (or all steps skipped) exits the Job cleanly, delivers a `Failed` result so the task reaches a terminal state, and never re-enters the controller retry loop.

## Acceptance Criteria

- [ ] `main.go` nil-result path delivers a `Failed` result via the deliverer whose message names the phase, and returns without error (exit 0) — evidence: `go test` nil-result case asserts `AgentResultInfo.Status == Failed` and `message` contains the phase name
- [ ] `cmd/run-task/main.go` nil-result path behaves identically (deliver `Failed` naming the phase, exit 0) — evidence: `cmd/run-task` nil-result test asserts the same `Status == Failed` + phase-name-in-message
- [ ] Regression tests in `main_test.go` and `cmd/run-task/main_test.go` exercise the nil-result path and assert delivered `Status == Failed`, message contains the phase name, and no error returned — evidence: `go test -run <nil-result case> ./...` exits 0 with those assertions
- [ ] `make precommit` exits 0
- [ ] Behavior documented in CHANGELOG.md under `## Unreleased` — evidence: `grep -n "nil result" CHANGELOG.md` finds the entry above `## v0.9.1`

## Verification

### Container-executable

- `make precommit` exits 0
- New tests: `go test ./...` passes with the added nil-result cases

### Operator-executable

- Deploy dev (`BRANCH=dev make buca`), run a task whose phase would produce nil result, confirm the Job exits 0 (no retry) and the task shows a failed/terminal state
- Deploy prod (`BRANCH=prod make buca`), confirm no new Failed `sentry-collector-agent` Job entries and `K8sPodStatus` stays clear

## Desired Behavior

1. A phase with zero registered steps or all steps skipped produces a delivered `Failed` result, not a crash and not a silent no-op
2. The process exits 0 on this path — no Job-controller retry
3. The delivered message names the phase so the failure is diagnosable in the task body
4. Job metrics are recorded (`AgentStatusFailed` + duration) as today in `main.go`; `cmd/run-task` stays consistent with its current metrics-free shape
5. The pushgateway deferred push (if configured) still fires on this path — it must not be bypassed

## Constraints

- Do not change the agent lib (`github.com/bborbe/agent`) — the fix lives in this repo's two entry points
- Do not re-architect the phase model; the collector's `NextPhase: "done"` behavior (v0.7.3) stays as-is
- Keep the pushgateway push non-fatal (`Warningf`, never fail the Job on push error)

## Failure Modes

| Trigger | Expected | Recovery |
|---|---|---|
| Deliver of the `Failed` result fails (transient network) | Error returned, Job exits non-zero, controller retries (bounded) | `kubectlnukeprod -n prod logs <job pod>` shows the wrapped deliver error (`deliver ... failure`); retry delivers on next attempt |
| A future lib change returns a different nil shape | Guard still routes non-nil results through `PrintResult` unchanged | Regression test on the nil path fails → caught in review |

## Suggested Decomposition

| # | Prompt focus | Covers DBs | Covers ACs | Depends on |
|---|---|---|---|---|
| 1 | Fix `main.go` nil-result branch: deliver `Failed` (phase in message) + exit 0, keep metrics | 1-5 | 1, 4, 5 | — |
| 2 | Fix `cmd/run-task/main.go` nil-result branch identically | 1-5 | 2, 4, 5 | 1 |
| 3 | Regression tests for both entry points (Status == Failed, phase in message, exit 0) | 1, 2, 3 | 3, 4 | 1, 2 |
| 4 | `make precommit` green + CHANGELOG entry under `## Unreleased` | 5 | 4, 5 | 1-3 |
