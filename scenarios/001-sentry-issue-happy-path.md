# Scenario 001: Sentry Issue Happy Path (per-alert)

**Purpose**: prove the agent works end-to-end on a single Sentry alert. Run before any deploy; run again after any non-trivial change.

**Architecture**: the sentry-watcher (separate component) creates one vault task per new Sentry alert; this agent is triggered per task and analyzes THAT single alert. This scenario simulates the watcher by creating one task manually.

**Setup**:
- Agent `bborbe/agent-sentry-issue-analyzer` deployed to dev
- Config CRD applied: `kubectlquant -n dev get config.agent.benjamin-borbe.de sentry-issue-analyzer` shows it
- Sentry MCP configured with the required `mcp__sentry__*` tools in ALLOWED_TOOLS (fail-fast preflight enforces this)
- Operator creates a synthetic Sentry-issue task manually (v0: sentry-watcher not built yet)

**Acceptance**: agent processes the task end-to-end (planning → execution → done) within 15 minutes; planning writes `## Analysis` (root cause + implicated `file.go:line`), execution writes `## Verdict` YAML with one of the 6 verdicts.

## Steps

1. **Create the task** (manual operator path — v0, simulating the watcher):

   ```bash
   vault-cli task create "Analyze Sentry issue SENTRY-TEST-001" \
     --vault Personal \
     --assignee sentry-issue-analyzer \
     --task-type sentry-issue-analyzer
   ```

   Then edit the task body to include the alert payload:
   ```
   sentry_link: https://seibert-group.sentry.io/issues/SENTRY-TEST-001
   ## Stack Trace
   panic: nil pointer dereference at pkg/foo/bar.go:42
   goroutine 1 [running]:
   github.com/bborbe/example.Foo(0x0)
       /go/src/github.com/bborbe/example/pkg/foo/bar.go:42 +0x1a
   ```

2. **Observe controller pickup**:
   ```bash
   kubectlquant -n dev logs agent-task-controller-personal-0 --tail=100 | grep "SENTRY-TEST-001"
   ```
   Expected: `published CreateTaskCommand` for this task within 1 poll cycle (~5min).

3. **Observe executor spawn**:
   ```bash
   kubectlquant -n dev get pods | grep "agent-sentry-issue-analyzer"
   ```
   Expected: a Job pod appears within 30s of the controller publish.

4. **Watch the agent run**:
   ```bash
   kubectlquant -n dev logs <POD_NAME> --tail=200 -f
   ```
   Expected phases: planning → execution → done.

5. **Verify task file updated**:
   ```bash
   cat "~/Documents/Obsidian/Personal/24 Tasks/Analyze Sentry issue SENTRY-TEST-001.md"
   ```
   Expected:
   - Frontmatter: `phase: done`, `status: completed`
   - Body has `## Analysis` section (root cause + `file.go:line` + certainty) and `## Verdict` section with a fenced YAML block (`verdict`, `confidence`, `root_cause`, `recommended_fix`)

## Pass criteria

- [ ] Task transitions all configured phases without `## Failure` section
- [ ] Final phase = `done`, status = `completed`
- [ ] `## Analysis` section identifies the nil-pointer root cause + `pkg/foo/bar.go:42`
- [ ] `## Verdict` YAML block has a valid verdict (this synthetic case is trivially `real bug`)
- [ ] No agent-pipeline alerts fired in the 10 min after task completion

## Fail recovery

If the task fails (phase: human_review with `## Failure`):
1. Read the `## Failure` JSON for the error class
2. Common classes: transient infra (retry should help), missing dependency (preflight gap — see [[Fail-Fast Preflight for Tool-Dependent LLM Agents]]), rate limit (wait for window reset), semantic error (task input is wrong shape), missing Sentry MCP tool (ALLOWED_TOOLS not configured)
3. Fix the root cause, then re-trigger the task via `vault-cli task set "Analyze Sentry issue SENTRY-TEST-001" status next` + `vault-cli task set ... assignee sentry-issue-analyzer`

## Cleanup

After scenario passes:
- Leave the test task in the vault as a known-good reference (don't delete)
- If repeated runs accumulate test tasks, archive periodically: `vault-cli task complete --reason "scenario 001 regression baseline"`

## Related

- [[Sentry Issue Analyzer Agent]] — agent knowledge page
- [[Agent Hub]] — catalog
- [[Agent Pipeline Debug Guide]] — what to check when a scenario fails for non-obvious reasons
