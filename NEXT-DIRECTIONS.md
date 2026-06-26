# NEXT-DIRECTIONS — agent-sentry-issue-analyzer

Deferred-not-cut work captured during `/launch-agent` proof-of-life scaffold on 2026-06-26. Each entry has a clear mechanism so future-you (or a sub-agent) can pick it up without re-deriving the context.

## v0 (shipped — initial scaffold)

- Scaffolded from `bborbe/agent-claude` template
- Config CRD targets `dev` namespace
- Phases: planning → ai_review → done
- Smoke test: scenario 001 (happy path with synthetic stack trace)
- Pinned to `github.com/bborbe/agent` master (will pin to v0.71.0 once tagged)

## v1 (next iteration — domain logic)

- **Implement `pkg/factory/factory.go` + `pkg/prompts/{planning,ai_review,done}.md`**
  - **Why deferred**: v0 scaffold is the runtime shell only — Claude template's generic dispatcher passes the task body to the LLM, but the prompts need domain-specific instructions (Sentry stack-trace parsing, severity rubric, fixability heuristics)
  - **How**: write per-phase prompts to `pkg/prompts/<phase>.md`; expand scenario 001 to cover a real Sentry issue (not synthetic)
  - **Effort**: 1-2 days

- **Build `sentry-watcher` as the upstream producer**
  - **Why deferred**: v0 uses manual task creation; the watcher needs Sentry API integration + poll loop
  - **How**: separate repo `bborbe/sentry-watcher` (future `/launch-watcher` plugin); emit `sentry-task` on new Sentry issue
  - **Effort**: 1-2 days (after `/launch-watcher` plugin exists)

## v2 (medium-term — quality / robustness improvements)

- **Add `## Rubric` body section + ai_review wiring**
  - **Why deferred**: agent ships with phase prompts hard-coded; rubric-as-data lets operators tune severity classification without code release
  - **How**: see [[Claude Managed Agents - Ideas for bborbe Framework#6 Outcome rubric as separate primitive from agent config]]
  - **Effort**: half-day

- **Add Prometheus metrics for token burn + per-task cost**
  - **Why deferred**: cost estimate from interview was rough ($0.10–0.50/task); production metrics needed to validate
  - **How**: implement `pkg/metrics/metrics.go` (counterfeiter mock for tests) + Prometheus scrape via `agent-task-executor`'s shared `/metrics` endpoint
  - **Effort**: half-day

- **More scenarios**
  - **Why deferred**: scenario 001 is happy path only; edge cases pending (incomplete stack trace, multi-repo issue, transient Sentry API failure)
  - **How**: write `scenarios/002-incomplete-stack-trace.md`, `003-multi-repo-issue.md`, ... per real failures seen in dev
  - **Effort**: half-day per scenario

## v3 (long-term — platform-level upgrades that benefit this agent)

- **Add persistent memory store for Sentry-issue dedup history**
  - **Why deferred**: requires platform change ([[Claude Managed Agents - Ideas for bborbe Framework#4]]); not agent-specific
  - **How**: when the platform ships `spec.memoryClaim` on Config CRD, add it to this agent's Config and use to track "already-analyzed Sentry issues" across pod restarts
  - **Effort**: half-day once platform is ready

- **Wire downstream `fix-task` emission**
  - **Why deferred**: downstream `bug-fix-agent` doesn't exist yet — emitting tasks for a missing consumer is wasted state
  - **How**: once `bug-fix-agent` exists, add `pkg/emit/fix_task.go` to publish a `CreateTaskCommand` with `task_type: fix-task` and `assignee: bug-fix-agent` when ai_review's `fixable=true`
  - **Effort**: half-day

## Notes

- Updating this file: as v1 work lands, move entries to v0 with the shipped commit SHA; as new ideas surface, add to v2/v3
- Cross-link to vault tasks where the work is tracked: `[[Implement Sentry Issue Analyzer Agent Domain Logic]]`
- This file is the source of truth for "what's next for this agent" — vault tasks reference back here, not the other way
