---
status: verifying
approved: "2026-09-03T19:23:50Z"
generating: "2026-09-03T19:43:27Z"
prompted: "2026-09-03T19:43:27Z"
verifying: "2026-09-03T20:03:39Z"
branch: dark-factory/bug-deep-reassign-targets-deleted-config
---

## Summary

- On a `real bug` triage verdict, the analyzer reassigns the task to an agent that no longer exists.
- The target Config CR `sentry-deep-analyzer` was deleted on 2026-08-26 when the Sentry pipeline was consolidated from 4 Config CRs to 2.
- `agent-task-executor` resolves agents by exact `assignee` string and silently skips unknown names, so reassigned tasks stop dead with no error anywhere.
- 12 tasks in the Personal vault carry the dead assignee; 9 are still `in_progress` and unreachable.
- This is the third instance of the same bug class in this repo — the `create-tasks` fan-out hit it, was fixed, regressed, and was re-fixed. The reassign path was missed both times.

## Problem

The 2026-08-26 consolidation removed the deep analyzer as a separate *agent* while keeping deep analysis as a *task type* on the surviving `sentry-analyzer-agent`. Two code paths stamp assignees: the `create-tasks` fan-out and the real-bug reassign step. Only the first was updated. Because `agent-task-executor` drops an unknown assignee silently rather than escalating, the failure is invisible — the task looks queued, the queue looks empty, and no metric or log at default verbosity reports the drop. Every real-bug verdict since 2026-08-26 has produced a task that can never run.

## Goal

A `real bug` triage verdict reassigns the task to an agent that exists, so the deep analysis phase actually runs. The deep task type continues to identify the work; only the routing target changes. The repo stops shipping the two deep-analyzer manifests that contradict this and that `make buca` re-applies on every run.

## Reproduction

Environment: `agent-sentry-issue-analyzer` at `69ee7e7` (v0.9.0 + the `x/crypto` v0.56.0 bump required to make `make precommit` pass at all); nuke dev and prod clusters, 2026-09-03.

1. Confirm the reassign target is the task-type constant, used for both parameters — `pkg/factory/factory.go:135-139`:

   ```go
   execution := steps.NewReassignExecutionStep(
       steps.NewExecutionStep(runner, prompts.BuildExecutionInstructions(), envContext),
       string(taskTypeSentryDeepAnalyzer),   // deepAssignee
       string(taskTypeSentryDeepAnalyzer),   // deepTaskType
   )
   ```

2. Confirm no agent answers to that name **on the executor-bearing clusters** (nuke dev / nuke prod — the only clusters running `agent-task-executor`):

   ```
   kubectlnukedev  -n dev  get configs.agent.benjamin-borbe.de sentry-deep-analyzer
   kubectlnukeprod -n prod get configs.agent.benjamin-borbe.de sentry-deep-analyzer
   ```

   Both return verbatim:

   ```
   Error from server (NotFound): configs.agent.benjamin-borbe.de "sentry-deep-analyzer" not found
   ```

   Note: a `sentry-deep-analyzer` CR **does** exist on the **quant** cluster (`kubectlquant -n dev` age 9d, `-n prod` age 7d22h) — created *after* the 2026-08-26 consolidation. Those copies are inert: quant runs 0 `agent-task-executor` deployments and has 0 sentry Jobs. They exist because `Makefile.k8s:7,9` globs `find . -maxdepth 1 -name "*.yaml"` under `k8s/` and applies every manifest via `kubectlquant`, so **every `make buca` re-creates them**. This is the active recreate path that Desired Behavior 3 closes.

3. Confirm the surviving analyzer already accepts the deep task type:

   ```
   kubectlnukedev -n dev get configs.agent.benjamin-borbe.de sentry-analyzer-agent -o jsonpath='{.spec.assignee} {.spec.taskTypes}'
   ```

   Returns verbatim: `sentry-analyzer-agent ["sentry-deep-analyzer","sentry-issue-analyzer","healthcheck"]`

4. Confirm the stranded tasks exist and no deep Jobs run:

   ```
   kubectlnukedev  -n dev  get jobs | grep -c sentry     # 0
   kubectlnukeprod -n prod get jobs | grep -c sentry     # 0
   ```

   while 12 vault tasks under `24 Tasks/` carry `assignee: sentry-deep-analyzer` (9 `in_progress`, 3 `completed` from before the deletion).

## Expected vs Actual

**Expected** — deep analysis is a task type handled by the surviving `sentry-analyzer-agent`. Out-of-repo, `bborbe/nuke` `agent/values-dev.yaml:368` records the consolidation verbatim:

```
# --- sentry pipeline: 4 Config CRs -> 2, 2026-08-26 ---
# Before: agent-sentry-issue-analyzer (Helm, assignee sentry-issue-analyzer-agent) plus
# three hand-applied kubectl CRs (sentry-collector, sentry-deep-analyzer,
# sentry-issue-analyzer) that existed in no manifest anywhere.
```

In-repo, `cmd/create-tasks/main.go:71` already encodes the post-consolidation literal as its default: `default:"sentry-analyzer-agent"`. A real-bug verdict should hand the task to that same agent with `task_type: sentry-deep-analyzer`, and a deep-analysis Job should spawn.

**Actual** — the task is handed to `assignee: sentry-deep-analyzer`, which matches no Config CR on either stage. The executor's exact-match lookup misses, the task is skipped as `skipped_unknown_assignee`, and it remains `in_progress` forever with no Job, no error, and no escalation.

## Why this is a bug

Documented behavior and reality disagree, and the repo documents *both sides*. `cmd/create-tasks/main.go:68-71` states the retired name "no longer resolves"; meanwhile `k8s/sentry-deep-analyzer-config.yaml:1-4` still documents routing to `sentry-deep-analyzer` as deliberate design. A reader of this repo alone would conclude the code is correct.

This bug class has already been fixed twice here, on the sibling code path:

- `CHANGELOG.md:46` — 2026-08-26: pointed the `create-tasks` default at `sentry-analyzer-agent`, because the fan-out "kept stamping the retired `sentry-issue-analyzer` on every per-alert task."
- `CHANGELOG.md:22` — the same default *regressed* via a struct-tag rewrite and was restored, locked this time by a reflection test at `cmd/create-tasks/main_test.go:169-172`.

The reassign path is the third occurrence and has never had a lock. Additionally, `pkg/factory/factory.go` passes one constant into two semantically distinct parameters — assignee and task type are different namespaces, and `NewReassignExecutionStep` already declares them separately.

## Workaround

Until the fix ships, an operator can re-route a stranded task by editing its frontmatter: set `assignee: sentry-analyzer-agent`. `task_type: sentry-deep-analyzer` is already correct and must not be changed. This is per-task and must be repeated for each real-bug verdict.

## Acceptance Criteria

- [ ] The two arguments differ, checked mechanically rather than by eye: `grep -n 'NewReassignExecutionStep' -A 4 pkg/factory/factory.go | grep -c 'taskTypeSentryDeepAnalyzer'` returns **exactly 1** (it returns 2 on the unmodified repo — one per argument).
- [ ] The literal is pinned as the **value of a declaration**, not merely present in the file: `grep -nE '^[[:space:]]*(const|var)?[[:space:]]*[A-Za-z0-9_]*[Aa]ssignee[A-Za-z0-9_]*[[:space:]]*(string)?[[:space:]]*=[[:space:]]*"sentry-analyzer-agent"' pkg/factory/factory.go` returns ≥1 line (the leading `^[[:space:]]*` anchor accepts the grouped `const (...)` form and rejects a commented-out line), and that identifier is what appears as the second argument at the `NewReassignExecutionStep` call site. A comment mentioning the string does NOT satisfy this.
- [ ] A factory-level behavioral regression lock runs and passes. `pkg/factory` is a Ginkgo suite whose only Go test function is `TestSuite`, so the run MUST be focus-filtered:
  `go test ./pkg/factory/... -run TestSuite -v -args --ginkgo.focus='reassign wiring'`
  Evidence: the command **exits 0** and stdout matches `Ran [1-9][0-9]* of` — at least one spec actually ran; the unmodified repo prints `Ran 0 of 20 Specs`. Do not assert on `0 Passed` / `0 Failed` substrings: `10 Passed` contains `0 Passed`, so those clauses fail in both directions. `go test` already exits non-zero on any Ginkgo failure. The spec MUST build the agent through the exported `factory.CreateAgentFromRunner(fakeRunner, nil)`, drive its execution phase with a `verdict: real bug` task (the fake-runner pattern at `pkg/steps/reassign_test.go:31-42` is the starting precedent, but it does NOT transfer directly: `agentlib.Agent.Run` parses the markdown internally and never returns the `*Markdown`, so capture the marshaled task via a second fake — `agentmocks.AgentResultDeliverer` — using `DeliverResultArgsForCall(0)` and re-parse with `agentlib.ParseMarkdown`), and assert on the resulting frontmatter that `assignee == "sentry-analyzer-agent"` AND `task_type == "sentry-deep-analyzer"` AND the two are not equal.
- [ ] **Mutation-proof for the lock.** The test above must be shown to fail when the bug is reintroduced, not merely to pass when it is absent — this is the only evidence shape that distinguishes a behavioral assertion from `Expect(agent).NotTo(BeNil())`. Procedure: revert the call site's second argument to `string(taskTypeSentryDeepAnalyzer)`, re-run the focused command, and observe it **exits non-zero** with a non-zero `Failed` count matching `[1-9][0-9]* Failed` in stdout (do not pin the literal `1 Failed` — reverting the argument fails every spec that asserts on the assignee, so the real count depends on how many such specs the lock contains); then restore the fix and observe exit 0. Both runs pasted verbatim as evidence. Mechanical backstops on the new test file: `grep -c 'CreateAgentFromRunner' <test file>` ≥1 and `grep -cE 'Equal\("sentry-analyzer-agent"\)' <test file>` ≥1 — the literal must appear inside an **assertion**, not merely in an `It(...)` description or a comment, which a bare `grep -c 'sentry-analyzer-agent'` would accept.
- [ ] **Both** contradicting manifests are gone: `ls k8s/ | grep -c sentry-deep-analyzer` returns **0**. Do not use `ls fileA fileB`: `ls` exits 1 when *any* operand is missing, so deleting only the dev manifest would satisfy it while `k8s/sentry-deep-analyzer-config-prod.yaml` stays on disk and `Makefile.k8s:7` keeps re-applying it on every `BRANCH=prod make buca`.
- [ ] `make precommit` exits 0.
- [ ] Negative (container-safe half): the deep task type survives — `grep -c 'sentry-deep-analyzer' pkg/factory/factory.go` returns ≥1.
- [ ] Negative (**operator-executable, post-merge** — see the rung note below): untouched trees, checked against the immutable Reproduction SHA:
  `test -d pkg/verdict && test -d pkg/deepverdict && git diff --exit-code --stat 69ee7e7..HEAD -- pkg/deepverdict/ pkg/verdict/ k8s/agent-sentry-issue-analyzer-config.yaml`
  Three requirements, each load-bearing: the `test -d` guard stops a mistyped pathspec from passing silently (`git diff` on a nonexistent path exits 0 with no output); `--exit-code` makes emptiness mechanical rather than an eyeball (plain `git diff` exits 0 either way); and the baseline MUST be the immutable `69ee7e7`, **not** `origin/master` — `origin/master` advances past the fix on merge, after which the diff is empty for every path and the guarantee silently becomes unconditional again.
- [ ] CHANGELOG has a `## Unreleased` entry beginning `fix:` naming the reassign target change.
- [ ] **Post-Deploy (Rung-2):** the **agent itself** writes the corrected assignee. Drive one triage task to a `verdict: real bug` on dev and read back the frontmatter the agent wrote: `assignee` transitions to `sentry-analyzer-agent` while `task_type` remains `sentry-deep-analyzer`, and a **deep** Job then appears. Discriminate on task type, not Job name: `kubectlnukedev -n dev get jobs -o json | jq '[.items[] | select(.spec.template.spec.containers[].env[]? | select(.name=="TASK_TYPE" and .value=="sentry-deep-analyzer"))] | length'` returns ≥1. A bare `grep -c sentry` will not do: producing a `real bug` verdict requires a triage Job first, which is itself a sentry Job and moves the count 0→1 before any deep Job exists — and after this fix both run under the same `sentry-analyzer-agent`, so no name substring separates them.
  - `deploy_check:` `kubectlnukedev -n dev get configs.agent.benjamin-borbe.de sentry-analyzer-agent -o jsonpath='{.spec.image}'`
  - `deploy_target:` `agent-sentry-issue-analyzer:dev`
  - The discriminating evidence is the **agent-written** `assignee` value: a pre-fix binary writes `sentry-deep-analyzer` there, so the transition cannot be faked by a stale deploy. It also cannot be produced by the Workaround — a hand-edit sets the field directly and produces no verdict-driven write. The `deploy_check` above is weak by construction (`.spec.image` is Helm-managed in `bborbe/nuke` and is not written by this repo's `make buca`); it confirms the observation targets the right image, nothing more. The operator MUST run `BRANCH=dev make buca` from the **repo root** before this AC.

## Verification

### Container-executable (runs inside the YOLO container at prompt time)

- `make precommit` — lint + full test suite clean
- `go test ./pkg/factory/... -run TestSuite -v -args --ginkgo.focus='reassign wiring'` — factory-level lock runs; must exit 0 with stdout matching `Ran [1-9][0-9]* of`, never `Ran 0 of`
- `grep -n 'NewReassignExecutionStep' -A 4 pkg/factory/factory.go` — the two arguments differ
- `grep -nE '^[[:space:]]*(const|var)?[[:space:]]*[A-Za-z0-9_]*[Aa]ssignee[A-Za-z0-9_]*[[:space:]]*(string)?[[:space:]]*=[[:space:]]*"sentry-analyzer-agent"' pkg/factory/factory.go` — literal pinned as a declaration value, not a comment
- (no `git` here by design — see the rung note below)

### Operator-executable (runs on the host after PR merge)

**Why the `git` checks are operator-only:** `.dark-factory.yaml` runs with `hideGit=true`, and this is a linked worktree whose `.git` is a *file* (`gitdir: .../agent-sentry-issue-analyzer/.git/worktrees/agent-sentry-deep-assignee`) resolving outside the container mount. Per `spec-writing.md:351`, any `git` command is operator-executable under either condition; here both hold, so a `git` bullet in the container rung would fail on tooling rather than behavior and would be inherited verbatim into every generated prompt's `<verification>` block.

- `test -d pkg/verdict && test -d pkg/deepverdict && git diff --exit-code --stat 69ee7e7..HEAD -- pkg/deepverdict/ pkg/verdict/ k8s/agent-sentry-issue-analyzer-config.yaml` — the negative-invariant AC

- `BRANCH=dev make buca` — run from the **repo root** (`Makefile.docker:47-48`); from `k8s/` it degrades to apply-only with no image build
- Drive one triage task to a `verdict: real bug` on dev — do NOT hand-edit the assignee, that is the Workaround and would satisfy the AC without the fix
- Read back the task frontmatter: the agent must have written `assignee: sentry-analyzer-agent` with `task_type: sentry-deep-analyzer` unchanged
- `kubectlnukedev -n dev get jobs | grep sentry` — returns ≥1 Job

## Desired Behavior

1. On a `real bug` triage verdict, the reassign step sets `assignee` to the literal `sentry-analyzer-agent`.
2. The assignee value is defined as its own named constant in `pkg/factory`, distinct from the task-type constant, so the two cannot drift back into one.
3. The repo no longer ships `k8s/sentry-deep-analyzer-config.yaml` or `k8s/sentry-deep-analyzer-config-prod.yaml`, whose header comments document the pre-consolidation routing as intentional.

## Constraints

- The assignee value MUST be the literal `sentry-analyzer-agent`, matching the live Config CR. `cmd/create-tasks/main.go:71` already uses this literal as its default — do not invent a new spelling. The other in-repo manifests are stale and must NOT be used as the source: `k8s/agent-sentry-issue-analyzer-config.yaml:10` declares `assignee: sentry-issue-analyzer` and `k8s/agent-sentry-issue-analyzer.yaml:11` declares `assignee: claude-agent`; neither is correct.
- The regression lock MUST live in `pkg/factory`, not `pkg/steps`. `NewReassignExecutionStep` takes the routing values as parameters, so a `pkg/steps` test asserts only its own input and is structurally incapable of catching a factory re-conflation. Follow the precedent at `cmd/create-tasks/main_test.go:169-172`, which pins the sibling default by reflection.
- Declare the assignee as a plain `string`, NOT `agentlib.TaskType(...)`. The `NewReassignExecutionStep` parameter is `string`, and the AC regex pins a bare quoted literal; the file's existing task-type vars use the `agentlib.TaskType("...")` form, so an implementer following local style would write a correct fix that fails the AC.
- `NewReassignExecutionStep`'s signature is already `(execution, deepAssignee, deepTaskType)` — do not change it.
- Do not restore or re-create the `sentry-deep-analyzer` Config CR; the consolidation to two agents is intentional and verified against both live clusters.
- Do not change the triage verdict schema, `pkg/verdict`, or `pkg/deepverdict`.
- Do not change the deep prompts (`deep-planning.md` / `deep-execution.md`).
- The literal `sentry-deep-analyzer` must remain the task type — the live `sentry-analyzer-agent` CR matches on it.
- Reassign stays per-task and idempotent — no batching, no change to the `InProgress` return contract.
- Updating the literals in `pkg/steps/reassign_test.go:38-42` is permitted and harmless, but does not satisfy the regression-lock criteria.
- Deleting the two deep manifests removes this repo's only record of **two** GitHub App pairs, which are NOT equivalent in consequence — verified against the live clusters 2026-09-03, not inferred from the files:
  - `APP_ID 4710983` / `INSTALLATION_ID 156399284` (dev, `k8s/sentry-deep-analyzer-config.yaml:51-52`) — genuinely orphaned by this deletion. No live CR references it.
  - `APP_ID 4710998` / `INSTALLATION_ID 156399409` (prod, `k8s/sentry-deep-analyzer-config-prod.yaml:51-52`) — **still in active use**. `kubectlnukedev -n dev` and `kubectlnukeprod -n prod` both return exactly this pair for `configs sentry-analyzer-agent`, so the surviving agent runs on the same prod App. Deleting the manifest removes the in-repo copy, not the App.
  Cloning is therefore unaffected: the surviving CR is Helm-managed out of `bborbe/nuke` and carries this App plus `Bash(scripts/repo-clone.sh:*)` in `ALLOWED_TOOLS`. Retiring App 4710983 and its TeamVault PEM is out of scope and tracked separately. Do NOT try to verify the surviving CR's App from inside the container — it is not in this repo.
- The other stale manifests are out of scope: `k8s/agent-sentry-issue-analyzer-config.yaml` declares `assignee: sentry-issue-analyzer` and `k8s/sentry-collector-config.yaml` declares `assignee: sentry-collector`, neither of which has a live CR. Do not touch them in this spec.
- Repairing the 12 already-stranded vault tasks is out of scope; it is a data fix tracked separately.

## Suggested Decomposition

| # | Prompt focus | Covers DBs | Covers ACs | Depends on |
|---|---|---|---|---|
| 1 | Factory routing constant + behavioral Ginkgo lock + mutation evidence | 1, 2 | 1, 2, 3, 4, 6, 7, 9 | — |
| 2 | Delete **both** deep-analyzer manifests | 3 | 5, 8 | — |

Rationale: the two prompts touch disjoint layers (Go wiring + test vs `k8s/` manifest deletion) and share no files, so they can run in either order. Splitting keeps the manifest deletion — which closes the `make buca` recreate path — from being buried inside a Go-focused prompt. AC 10 is deliberately assigned to neither prompt: it is operator-executable Rung-2 verification against the live dev cluster, which no container can perform.

## Failure Modes

| Trigger | Expected behavior | Detection | Recovery |
|---|---|---|---|
| Assignee constant typo'd to a name with no Config CR | Same silent-skip class this spec fixes | Factory test asserting the exact literal fails in CI | Fix the constant; the test is the regression lock |
| An operator applies the deleted manifests from an old checkout | The dead CR returns and masks the bug as "working" | `kubectlnukedev -n dev get configs sentry-deep-analyzer` returns a CR instead of `NotFound` | `kubectl delete config sentry-deep-analyzer`; the manifests are removed by this spec so a fresh checkout cannot do it |
| `sentry-analyzer-agent` is renamed again later | Reassigned tasks strand a fourth time | No signal today — the executor drops unknown assignees silently | Out of scope here; observability for `skipped_unknown_assignee` is tracked separately |
| Analyzer CR's `taskTypes` loses `sentry-deep-analyzer` | Task routes to a live agent that rejects the type | `kubectl get configs sentry-analyzer-agent -o jsonpath='{.spec.taskTypes}'` omits the type | Re-add the task type to the Helm values *(out-of-repo, `bborbe/nuke` `agent/values-{dev,prod}.yaml`)* |
| Fix deployed to dev but not prod | Prod continues stranding real-bug tasks | `kubectlnukeprod -n prod get jobs \| grep sentry` stays 0 after a real-bug verdict | Deploy prod with `BRANCH=prod make buca` |
