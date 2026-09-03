---
status: completed
spec: [001-bug-deep-reassign-targets-deleted-config]
summary: 'Fixed real-bug reassign to stamp assignee: sentry-analyzer-agent via a new assigneeSentryAnalyzerAgent constant while keeping task_type: sentry-deep-analyzer, corrected stale routing comments, added a factory-level Ginkgo regression lock with mutation evidence, and updated CHANGELOG.md under ## Unreleased'
execution_id: agent-sentry-deep-assignee-exec-001-spec-001-factory-reassign-assignee-lock
dark-factory-version: v0.193.0
created: "2026-09-03T19:29:44Z"
queued: "2026-09-03T19:44:06Z"
started: "2026-09-03T19:44:43Z"
completed: "2026-09-03T19:55:25Z"
---

<summary>
- A `real bug` triage verdict now hands the task to an agent that actually exists, so deep analysis can run instead of stalling forever.
- The routing target (which agent gets the work) and the task type (what kind of work it is) stop being the same value — they were accidentally collapsed into one constant.
- The correct agent name is pinned as its own named declaration in the factory, so a future edit cannot silently merge the two back together.
- A new behavioral test drives the real triage agent end-to-end with a `real bug` verdict and asserts on the frontmatter the agent actually writes.
- That test is proven to fail when the bug is reintroduced — not merely to pass when it is absent — with both runs captured as evidence.
- Stale comments that documented the pre-consolidation routing as intentional are corrected so the repo stops describing both sides of the contradiction.
- The deep task type itself is unchanged: only who receives the task changes, never what kind of task it is.
- Nothing about the verdict schema, the deep prompts, or the reassign step's signature changes.
- A CHANGELOG entry records the fix under `## Unreleased`.
</summary>

<objective>
Fix `pkg/factory/factory.go` so the real-bug reassign step stamps `assignee: sentry-analyzer-agent` (the live Config CR) while keeping `task_type: sentry-deep-analyzer`, and add a factory-level Ginkgo regression lock that fails if the two values are ever conflated again.
</objective>

<context>
This repo has no `CLAUDE.md`. Read these before making changes:

- `specs/in-progress/001-bug-deep-reassign-targets-deleted-config.md` — the full spec, including Constraints and Acceptance Criteria.
- `pkg/factory/factory.go` — the defect is in `CreateAgentFromRunner`; the same constant is passed into both the `deepAssignee` and `deepTaskType` parameters.
- `pkg/factory/factory_suite_test.go` — the Ginkgo suite entry point. `TestSuite` is the ONLY Go test function in `pkg/factory`; every spec is a Ginkgo `It`.
- `pkg/factory/factory_test.go` — existing style for `package factory_test` specs.
- `pkg/steps/reassign.go` — `NewReassignExecutionStep(execution agentlib.Step, deepAssignee string, deepTaskType string) agentlib.Step`. Its `Run` sets `md.Frontmatter["assignee"] = s.deepAssignee`, `md.Frontmatter["phase"] = "planning"`, `md.Frontmatter["task_type"] = s.deepTaskType` and returns `AgentStatusInProgress`.
- `pkg/steps/reassign_e2e_test.go` — the closest existing precedent: it drives an `agentlib.Agent` and reads the delivered `AgentResultInfo.Output`. It builds the step DIRECTLY, which is exactly why it cannot catch a factory re-conflation — the new test must go through `factory.CreateAgentFromRunner`.
- `cmd/create-tasks/main.go` (the `Assignee` field, `default:"sentry-analyzer-agent"`) and `cmd/create-tasks/main_test.go` (`Describe("application defaults")`) — the sibling prior art for pinning this literal.

Reference guides (read the ones relevant to what you touch):

- `/home/node/.claude/plugins/marketplaces/coding/docs/go-testing-guide.md` — Ginkgo/Gomega conventions.
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-mocking-guide.md` — counterfeiter fake usage (`...Returns`, `...ArgsForCall(i)`, `...CallCount()`).
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-factory-pattern.md` — factory conventions (zero business logic, `Create*` prefix).
- `/home/node/.claude/plugins/marketplaces/coding/docs/changelog-guide.md` — CHANGELOG entry format.

Verified library facts (do NOT re-derive these from memory — they were read from `github.com/bborbe/agent@v0.84.1`):

```go
// agent_agent.go
func (a *Agent) Run(
    ctx context.Context,
    phaseName domain.TaskPhase,
    taskContent string,
    deliverer ResultDeliverer,
) (*Result, error)
```

`Agent.Run` parses the markdown internally and NEVER returns the `*Markdown`. The only way to observe the mutated frontmatter is the delivered output.

```go
// agent_status.go
type AgentResultInfo struct {
    Status         AgentStatus
    Output         string // the re-marshaled task markdown
    Message        string
    NextPhase      string
    ContinueToNext bool
}

// agent_markdown.go
type Markdown struct {
    Frontmatter TaskFrontmatter
    ...
}
func ParseMarkdown(_ context.Context, content string) (*Markdown, error)

// agent_task-frontmatter.go
type TaskFrontmatter map[string]interface{}
```

Counterfeiter fakes, both in package `mocks` at import path `github.com/bborbe/agent/mocks`:

```go
type ClaudeRunner struct{ ... }
func (fake *ClaudeRunner) RunReturns(result1 *claude.ClaudeResult, result2 error)

type AgentResultDeliverer struct{ ... }
func (fake *AgentResultDeliverer) DeliverResultCallCount() int
func (fake *AgentResultDeliverer) DeliverResultArgsForCall(i int) (context.Context, lib.AgentResultInfo)
```

`domain.TaskPhaseExecution` is `"execution"` (`github.com/bborbe/vault-cli/pkg/domain`), which is the phase name `CreateAgentFromRunner` registers.
</context>

<requirements>

1. In `pkg/factory/factory.go`, add a new declaration immediately after the `taskTypeSentryDeepAnalyzer` var block (currently around line 38), before `taskTypeSentryCollector`:

   ```go
   // assigneeSentryAnalyzerAgent is the `assignee` of the live agent Config CR
   // that handles deep analysis. It is a PLAIN string, not an agentlib.TaskType:
   // assignee and task_type are different namespaces and must never share one
   // constant. The dedicated sentry-deep-analyzer Config CR was deleted on
   // 2026-08-26 when the sentry pipeline consolidated from 4 Config CRs to 2;
   // the surviving sentry-analyzer-agent CR lists sentry-deep-analyzer in its
   // taskTypes. agent-task-executor resolves an agent by exact assignee string
   // and silently drops unknown names (skipped_unknown_assignee), so a wrong
   // value here strands the task with no error anywhere. Keep this literal in
   // sync with cmd/create-tasks/main.go's Assignee default.
   const assigneeSentryAnalyzerAgent = "sentry-analyzer-agent"
   ```

   Use `const`, not `var`, and use exactly the identifier `assigneeSentryAnalyzerAgent`. Do NOT wrap the value in `agentlib.TaskType(...)` — `NewReassignExecutionStep`'s `deepAssignee` parameter is a plain `string`, and the spec's acceptance grep requires a bare quoted literal on the declaration line.

   Do NOT write the identifier `NewReassignExecutionStep` anywhere in this comment (or in any other comment in the file). Acceptance criterion 1 greps for `NewReassignExecutionStep` with 4 lines of trailing context and counts `taskTypeSentryDeepAnalyzer` occurrences; an extra comment mention shifts that count and fails the check.

2. In `pkg/factory/factory.go`, in `CreateAgentFromRunner`, change the `deepAssignee` argument (the FIRST of the two identical arguments, currently line 137) from the task-type cast to the new constant. Leave the second argument — `deepTaskType` — exactly as it is:

   Old:
   ```go
   execution := steps.NewReassignExecutionStep(
       steps.NewExecutionStep(runner, prompts.BuildExecutionInstructions(), envContext),
       string(taskTypeSentryDeepAnalyzer),
       string(taskTypeSentryDeepAnalyzer),
   )
   ```

   New:
   ```go
   execution := steps.NewReassignExecutionStep(
       steps.NewExecutionStep(runner, prompts.BuildExecutionInstructions(), envContext),
       assigneeSentryAnalyzerAgent,
       string(taskTypeSentryDeepAnalyzer),
   )
   ```

   Keep the call multi-line exactly as shown — one argument per line. Do not add inline `// deepAssignee` / `// deepTaskType` trailing comments; keep the argument lines bare.

   Do NOT change `NewReassignExecutionStep`'s signature, and do NOT change the third argument. Pass the IDENTIFIER `assigneeSentryAnalyzerAgent`, never a re-typed bare `"sentry-analyzer-agent"` literal at the call site — verification 4b greps for the identifier inside the call's 4-line window.

3. Correct the two stale comments in `pkg/factory/factory.go` that document the deleted Config CR as the routing target. Change only comment text, no code:

   - The `taskTypeSentryDeepAnalyzer` doc comment currently ends with `...and the executor routes it to the sentry-deep-analyzer Config CR.` Replace that clause so it reads that the executor routes the task by its `assignee` to the live `sentry-analyzer-agent` Config CR, whose `taskTypes` list includes `sentry-deep-analyzer`. The literal `sentry-deep-analyzer` MUST remain somewhere in this file (it is the task type and an acceptance criterion greps for it).
   - The `CreateAgentFromRunner` doc comment currently says the execution step `reassigns the SAME task to the deep analyzer (sentry-deep-analyzer) instead of closing it`. Rewrite that sentence to say it reassigns the SAME task to `sentry-analyzer-agent` with `task_type: sentry-deep-analyzer` instead of closing it. Again: do not introduce the identifier `NewReassignExecutionStep` into this comment.

4. Correct the equivalent stale comment in `pkg/steps/reassign.go`. The `reassignExecutionStep` type doc comment says `...and the executor re-routes the task to the sentry-deep-analyzer Config CR.` Rewrite that clause so it names no hard-coded CR: the executor re-routes the task to the Config CR named by `deepAssignee`. This is a comment-only edit — do not change any code in `pkg/steps/`, and do not change `pkg/steps/reassign_test.go` or `pkg/steps/reassign_e2e_test.go` (those exercise the step's own parameters and are correct as written).

5. Create a new test file `pkg/factory/reassign_wiring_test.go` in `package factory_test`. Start it with the repo's standard license header (copy the 3-line header verbatim from the top of `pkg/factory/factory_test.go`).

   The outermost Ginkgo container description MUST contain the exact substring `reassign wiring` so the focus filter `--ginkgo.focus='reassign wiring'` selects it.

   The test must:
   - build the agent through the exported `factory.CreateAgentFromRunner(runner, nil)` — NOT by constructing `steps.NewReassignExecutionStep` directly, which is what makes it a wiring lock rather than a step test;
   - drive `agent.Run(ctx, domain.TaskPhaseExecution, taskContent, deliverer)` with a `verdict: real bug`;
   - capture the marshaled task from the deliverer fake and re-parse it.

   Write it as:

   ```go
   var _ = Describe("CreateAgentFromRunner reassign wiring", func() {
       var (
           ctx        context.Context
           runner     *agentmocks.ClaudeRunner
           deliverer  *agentmocks.AgentResultDeliverer
           reassigned *agentlib.Markdown
       )

       BeforeEach(func() {
           ctx = context.Background()

           runner = &agentmocks.ClaudeRunner{}
           runner.RunReturns(&claudelib.ClaudeResult{
               Result: "```yaml\nsentry_issue_id: OCTOPUS-PROD-1J\nverdict: real bug\nconfidence: high\nreason: clear defect\n```",
           }, nil)

           deliverer = &agentmocks.AgentResultDeliverer{}

           agent := factory.CreateAgentFromRunner(runner, nil)
           _, err := agent.Run(
               ctx,
               domain.TaskPhaseExecution,
               "---\nstatus: in_progress\nphase: execution\nassignee: sentry-issue-analyzer\ntask_type: sentry-issue-analyzer\n---\n\n## Analysis\n\nroot cause\n",
               deliverer,
           )
           Expect(err).NotTo(HaveOccurred())

           // Agent.Run parses the markdown internally and never returns the
           // *Markdown, so read the re-marshaled task out of the deliverer.
           Expect(deliverer.DeliverResultCallCount()).To(Equal(1))
           _, info := deliverer.DeliverResultArgsForCall(0)
           Expect(info.Status).To(Equal(agentlib.AgentStatusInProgress))

           reassigned, err = agentlib.ParseMarkdown(ctx, info.Output)
           Expect(err).NotTo(HaveOccurred())
       })

       It("stamps the live sentry-analyzer-agent Config CR as the assignee", func() {
           Expect(reassigned.Frontmatter["assignee"]).To(Equal("sentry-analyzer-agent"))
       })

       It("keeps sentry-deep-analyzer as the task type", func() {
           Expect(reassigned.Frontmatter["task_type"]).To(Equal("sentry-deep-analyzer"))
       })

       It("keeps assignee and task_type as distinct values", func() {
           Expect(
               reassigned.Frontmatter["assignee"],
           ).NotTo(Equal(reassigned.Frontmatter["task_type"]))
       })

       It("hands the task back at phase planning for the deep run", func() {
           Expect(reassigned.Frontmatter["phase"]).To(Equal("planning"))
       })
   })
   ```

   Imports (goimports-reviser groups these; `make precommit` will regroup them, so just get the paths right):

   ```go
   import (
       "context"

       agentlib "github.com/bborbe/agent"
       claudelib "github.com/bborbe/agent/claude"
       agentmocks "github.com/bborbe/agent/mocks"
       "github.com/bborbe/vault-cli/pkg/domain"
       . "github.com/onsi/ginkgo/v2"
       . "github.com/onsi/gomega"

       "github.com/bborbe/agent-sentry-issue-analyzer/pkg/factory"
   )
   ```

   The literal `"sentry-analyzer-agent"` MUST appear inside a Gomega assertion of the exact form `Equal("sentry-analyzer-agent")` — an acceptance criterion greps for `Equal\("sentry-analyzer-agent"\)`. Putting the literal only in an `It(...)` description or a comment does not satisfy it.

   Error path: if `agent.Run` returns an error, or `DeliverResultCallCount()` is not exactly 1, the `BeforeEach` fails the spec — that is the intended behavior, do not add fallbacks or `if err != nil { return }` guards.

6. Run the focused lock and confirm it actually selects specs:

   ```
   go test ./pkg/factory/... -run TestSuite -v -args --ginkgo.focus='reassign wiring'
   ```

   It must exit 0 AND stdout must match `Ran [1-9][0-9]* of` (e.g. `Ran 4 of 24 Specs`). `Ran 0 of ...` means the focus string does not match your container description — fix the description, do not weaken the focus. Do NOT assert on `0 Passed` / `0 Failed` substrings: `10 Passed` contains `0 Passed`.

7. Produce mutation evidence that the lock is behavioral, not decorative. Execute these four steps in order and paste the verbatim stdout of both test runs into your final output:

   a. Temporarily reintroduce the bug: change the `deepAssignee` argument in `CreateAgentFromRunner` back to `string(taskTypeSentryDeepAnalyzer)`.
   b. Re-run the focused command from step 6. It MUST exit non-zero and its summary line must show a NON-ZERO `Failed` count — expect `2 Passed | 2 Failed` exactly, because two of the four specs (assignee equality, and assignee-vs-task_type distinctness) assert on the reverted value while the task_type and phase specs still pass. Do NOT grep for the literal `1 Failed`; match `[1-9][0-9]* Failed` instead. Capture the output.
   c. Immediately restore the fix: change that argument back to `assigneeSentryAnalyzerAgent`.
   d. Re-run the focused command. It MUST exit 0. Capture the output.

   Do not use `make precommit` for the mutation runs — use `go test` directly so the mutation window stays as short as possible. After step (d), confirm the restoration mechanically before moving on:

   ```
   grep -n 'NewReassignExecutionStep' -A 4 pkg/factory/factory.go | grep -c 'taskTypeSentryDeepAnalyzer'
   ```

   must print `1`. If it prints `2`, the fix was not restored — restore it and re-run.

8. Append a new bullet to the EXISTING `## Unreleased` section of `CHANGELOG.md`, directly below the current `fix: bump golang.org/x/crypto ...` bullet. Do not create a second `## Unreleased` heading, do not rename any released `## vX.Y.Z` heading, and do not edit any existing bullet. The new bullet must begin with `fix:` and name the reassign target change, for example:

   ```
   - fix: the real-bug reassign now targets the live `sentry-analyzer-agent` Config CR instead of the deleted `sentry-deep-analyzer` one — `pkg/factory` passed a single task-type constant into both the `deepAssignee` and `deepTaskType` parameters of `NewReassignExecutionStep`, so every `verdict: real bug` since the 2026-08-26 4-CR→2-CR consolidation stamped an assignee that `agent-task-executor` cannot resolve, silently dropping the task (`skipped_unknown_assignee`) with no Job, no error, and no escalation. The assignee is now its own `assigneeSentryAnalyzerAgent` constant, `task_type` stays `sentry-deep-analyzer`, and a focused factory-level Ginkgo spec drives `CreateAgentFromRunner` end-to-end with a real-bug verdict so the two values cannot be re-conflated unnoticed (third occurrence of this bug class in this repo; the `create-tasks` sibling path was fixed twice).
   ```

9. Run `make precommit`. It reformats (`goimports-reviser`, `golines --max-len=100`, `gofmt`) and applies license headers. AFTER it completes, re-run the two mechanical checks from the verification block below to confirm the reformatting did not collapse the multi-line call site.

</requirements>

<constraints>
- The assignee value MUST be the literal `sentry-analyzer-agent`, matching the live Config CR. `cmd/create-tasks/main.go` already uses this literal as its `default:` tag — do not invent a new spelling. The in-repo manifests are stale and must NOT be used as the source: `k8s/agent-sentry-issue-analyzer-config.yaml` declares `assignee: sentry-issue-analyzer` and `k8s/agent-sentry-issue-analyzer.yaml` declares `assignee: claude-agent`; neither is correct.
- The regression lock MUST live in `pkg/factory`, not `pkg/steps`. `NewReassignExecutionStep` takes the routing values as parameters, so a `pkg/steps` test asserts only its own input and is structurally incapable of catching a factory re-conflation.
- Declare the assignee as a plain `string`, NOT `agentlib.TaskType(...)`. The `deepAssignee` parameter is a plain `string`, and the acceptance grep pins a bare quoted literal.
- `NewReassignExecutionStep`'s signature is already `(execution, deepAssignee, deepTaskType)` — do not change it.
- The literal `sentry-deep-analyzer` must remain the task type — the live `sentry-analyzer-agent` CR matches on it. Do not remove it from `pkg/factory/factory.go`.
- Do not restore or re-create the `sentry-deep-analyzer` Config CR; the consolidation to two agents is intentional and verified against both live clusters.
- Do not change the triage verdict schema, `pkg/verdict`, or `pkg/deepverdict`.
- Do not change the deep prompts (`prompts/deep-planning.md` / `prompts/deep-execution.md`) or any other prompt file.
- Do not modify `k8s/agent-sentry-issue-analyzer-config.yaml` or any other file under `k8s/` — manifest changes belong to a separate prompt.
- Reassign stays per-task and idempotent — no batching, no change to the `InProgress` return contract, no change to `md.Frontmatter["phase"] = "planning"`.
- Do not add any new config field, flag, threshold, or metric. The spec asks for one constant, one argument change, one test file, comment corrections, and a CHANGELOG bullet — nothing else.
- Do NOT run any `git` command. This repo runs with `hideGit=true` and its `.git` resolves outside the container mount; a `git` invocation fails on tooling, not on behavior.
- Do NOT commit — dark-factory handles git.
- Existing tests must still pass. `pkg/steps` tests hardcode `"sentry-deep-analyzer"` for both step parameters and must keep passing untouched.
</constraints>

<verification>
Run each of these. All must hold.

1. `make precommit` — must exit 0.

2. `go test ./pkg/factory/... -run TestSuite -v -args --ginkgo.focus='reassign wiring'` — must exit 0 and stdout must match `Ran [1-9][0-9]* of`. `Ran 0 of` is a failure.

3. `grep -n 'NewReassignExecutionStep' -A 4 pkg/factory/factory.go | grep -c 'taskTypeSentryDeepAnalyzer'` — must print exactly `1` (the unmodified repo prints `2`).

4. `grep -nE '^[[:space:]]*(const|var)?[[:space:]]*[A-Za-z0-9_]*[Aa]ssignee[A-Za-z0-9_]*[[:space:]]*(string)?[[:space:]]*=[[:space:]]*"sentry-analyzer-agent"' pkg/factory/factory.go` — must print at least one line, and that line must be the `const assigneeSentryAnalyzerAgent` declaration. A commented-out line does not satisfy this.

4b. `grep -n 'NewReassignExecutionStep' -A 4 pkg/factory/factory.go | grep -c 'assigneeSentryAnalyzerAgent'` — must print exactly `1`. This is the other half of spec AC2: the declared IDENTIFIER, not a re-typed bare literal, must appear at the call site. Declaring the constant and then hardcoding `"sentry-analyzer-agent"` in the argument passes checks 3, 4 and the behavioural test but violates Desired Behavior 2.

5. `grep -c 'sentry-deep-analyzer' pkg/factory/factory.go` — must print `1` or more (the deep task type survives).

6. `grep -c 'CreateAgentFromRunner' pkg/factory/reassign_wiring_test.go` — must print `1` or more.

7. `grep -cE 'Equal\("sentry-analyzer-agent"\)' pkg/factory/reassign_wiring_test.go` — must print `1` or more (the literal must be inside an assertion, not just a description).

8. `grep -n '^## Unreleased' -A 12 CHANGELOG.md | grep -c 'fix: the real-bug reassign'` — must print `1`. The `-A 12` window is deliberate: the sibling prompt for this spec appends a bullet under the same `## Unreleased`, so your entry may sit several lines below. Additionally `grep -c '^## Unreleased' CHANGELOG.md` must print `1`.

9. Mutation evidence — paste the verbatim stdout of BOTH runs from requirement 7 into your final output: the bug-reintroduced run (non-zero exit; summary matches `[1-9][0-9]* Failed`, expected `2 Passed | 2 Failed`) and the restored run (exit 0). A lock that only passes is not evidence; a lock proven to fail on the reintroduced bug is.
</verification>
