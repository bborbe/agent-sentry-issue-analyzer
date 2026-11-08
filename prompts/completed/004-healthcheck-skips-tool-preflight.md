---
status: completed
summary: Added an unconditional preflight skip for the healthcheck/oauth-probe liveness task types, documented it, covered both halves with tests, and recorded a changelog bullet.
execution_id: agent-sentry-issue-analyzer-healthcheck-preflight-exec-004-healthcheck-skips-tool-preflight
dark-factory-version: v0.198.17
created: "2026-11-08T18:10:00Z"
queued: "2026-11-08T18:11:04Z"
started: "2026-11-08T18:19:52Z"
completed: "2026-11-08T18:28:30Z"
---

# Skip tool/token preflight for healthcheck and oauth-probe runs

<summary>
- Liveness runs (`healthcheck`, and its deprecated alias `oauth-probe`) no longer fail on a lane that does not grant the Sentry read script or the repo-clone script, or that only half-configures the opt-in Jira read.
- Those runs never use any of those tools — the liveness step only asks Claude to reply "ok" — so the startup check was refusing them for a capability they never touch.
- Today every healthcheck Job on the sentry-fix and sentry-collector lanes exits within about a second and the executor records a pod crash with no output; after this change those Jobs reach the liveness step.
- Every other task type keeps exactly the startup checks it has today: triage/deep still require the Sentry and repo-clone pieces (plus the Jira pieces on a lane that opts into the Jira read), the fix agent still requires repo-clone, the collector still requires its own scripts.
- New tests pin both halves: liveness types pass with no tools and no tokens, and the triage, fix and collector types still fail with no tools.
- The changelog records the fix under the unreleased section.
</summary>

<objective>
Make `validatePreflight` in `main.go` return nil for the liveness task types `agentlib.TaskTypeHealthcheck` and `agentlib.TaskTypeOAuthProbe`, so a healthcheck Job on a lane whose `ALLOWED_TOOLS` lacks `Bash(scripts/sentry-read.sh:*)` stops dying at startup with `sentry preflight failed`. Behaviour for every other task type must stay byte-for-byte the same.
</objective>

<context>
Read CLAUDE.md (if present) and `docs/dod.md` for project conventions.

Background (reproduced 2026-11-08 on dev, both lanes, first try): `application.Run` in `main.go` calls `a.validatePreflight(ctx, allowedTools)` for every task type, before `a.buildProvider(...)` / `provider.Get(ctx, agentlib.TaskType(a.TaskType))` selects the agent. `validatePreflight` special-cases only `taskTypeSentryCollector` and `taskTypeSentryFix`; every other task type falls through to `preflight.ValidateSentryTools`, `preflight.ValidateRepoCloneTools` and `preflight.ValidateJiraTools`. On the sentry-fix and sentry-collector lanes `ALLOWED_TOOLS` does not grant `Bash(scripts/sentry-read.sh:*)`, so every `healthcheck` Job exits 1 with:

```
application failed: sentry preflight: sentry preflight failed: missing required piece(s) for token-based Sentry access: Bash(scripts/sentry-read.sh:*)
```

and the executor records `pod_crash_no_stdout (Error/1)`.

Files to read before changing anything:
- `main.go` — the `validatePreflight` method (receiver `*application`, signature `func (a *application) validatePreflight(ctx context.Context, allowedTools claudelib.AllowedTools) error`), its doc comment, the package-level constants `taskTypeSentryCollector` / `taskTypeSentryFix` (plain `string` consts), the `application` struct field `TaskType string` (env `TASK_TYPE`, default `"unknown"`), and the call site in `Run` right after `claudelib.ParseAllowedTools(a.AllowedToolsRaw)`. `main.go` already imports `agentlib "github.com/bborbe/agent"` and `claudelib "github.com/bborbe/agent/claude"`.
- `pkg/factory/factory.go` — `CreateAgentProvider`: `livenessAgent := healthcheck.NewAgent(healthcheck.NewClaudeStep(runner))` is registered under BOTH `agentlib.TaskTypeHealthcheck` and `agentlib.TaskTypeOAuthProbe`. Use those same two library constants.
- `pkg/preflight/preflight.go` — `ValidateSentryTools`, `ValidateCollectorTools`, `ValidateRepoCloneTools`, `ValidateJiraTools` and their error texts (`sentry preflight failed: ...`, `sentry-collector preflight failed: ...`, `repo-clone preflight failed: ...`). Do not change this file.
- `main_test.go` — Ginkgo/Gomega style for `application` methods: see the `validateSentryConfig` Describe (builds `app := &application{...}`, `ctx = context.Background()` in `BeforeEach`, asserts with `Succeed()` / `HaveOccurred()` / `ContainSubstring`). `validatePreflight` currently has no tests.
- `CHANGELOG.md` — already has exactly one `## Unreleased` section above `## v0.23.0`, holding one `- fix:` bullet.

Library facts (verified in `github.com/bborbe/agent@v0.138.0`, file `agent_task-type.go`):
- `agentlib.TaskTypeHealthcheck TaskType = "healthcheck"`
- `agentlib.TaskTypeOAuthProbe TaskType = "oauth-probe"` (marked Deprecated in favour of `TaskTypeHealthcheck`, but still routed by the factory — must be covered)
- `agentlib.TaskType` is a named string type; `a.TaskType` is a plain `string`, so compare via `agentlib.TaskType(a.TaskType)` — the same conversion `Run` already uses for `provider.Get`.
- `claudelib.AllowedTools` is `[]string`; an empty set is `claudelib.AllowedTools{}`.

Coding guides (in-container paths):
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-testing-guide.md`
- `/home/node/.claude/plugins/marketplaces/coding/docs/changelog-guide.md`
</context>

<requirements>
1. In `main.go`, at the very top of `validatePreflight` (before the `taskTypeSentryCollector` branch), add a guard that returns `nil` when `agentlib.TaskType(a.TaskType)` is `agentlib.TaskTypeHealthcheck` or `agentlib.TaskTypeOAuthProbe`. Use the library constants directly; do not add new string literals `"healthcheck"` / `"oauth-probe"` to `main.go`. A `switch` or an `if` with `||` are both fine.

2. Leave the rest of `validatePreflight` unchanged: the collector branch, the sentry-fix branch, and the triage/deep fall-through (Sentry, repo-clone, Jira checks, in that order, with their existing `errors.Wrap` messages `"sentry-collector preflight"`, `"sentry-fix preflight"`, `"sentry preflight"`, `"repo-clone preflight"`, `"jira preflight"`). Do not move the `validatePreflight` call in `Run` and do not change `pkg/preflight`.

3. Update the `validatePreflight` doc comment to add one or two sentences stating that the liveness task types (`healthcheck` and its deprecated alias `oauth-probe`, both bound to the same liveness agent in `factory.CreateAgentProvider`) skip preflight entirely, because the liveness step only asks Claude to reply "ok" and uses none of the Sentry, repo-clone or Jira tools — so a lane that does not grant those tools (the sentry-fix and sentry-collector lanes) must still pass its healthcheck. Keep the existing sentences about the collector and fix branches.

4. In `main_test.go`, add a new top-level `var _ = Describe("validatePreflight", func() { ... })` following the `validateSentryConfig` Describe's style (`ctx` set in `BeforeEach`, `app := &application{...}`). Import `claudelib "github.com/bborbe/agent/claude"` in `main_test.go` (it is not imported there yet). Cover, each with empty allowed tools `claudelib.AllowedTools{}` and every token/URL field left at its zero value:
   - `TaskType: string(agentlib.TaskTypeHealthcheck)` → `Succeed()`.
   - `TaskType: string(agentlib.TaskTypeOAuthProbe)` → `Succeed()`.
   - `TaskType: "sentry-issue-analyzer"` → error whose message contains `"sentry preflight failed"` (proves the guard did not widen to the triage path).
   - `TaskType: "unknown"` (the env default) → error containing `"sentry preflight failed"`.
   - `TaskType: taskTypeSentryFix` → error containing `"sentry-fix preflight"` and `"repo-clone preflight failed"`.
   - `TaskType: taskTypeSentryCollector` → error containing `"sentry-collector preflight failed"`.
   A `DescribeTable` for the two liveness types and/or the four failing types is acceptable if it reads more cleanly.

5. Add one more test proving the liveness guard does not depend on tools being absent: `TaskType: string(agentlib.TaskTypeHealthcheck)` with `claudelib.AllowedTools{"Bash(scripts/repo-clone.sh:*)"}` (a partial grant that would fail the triage path) → `Succeed()`.

6. In `CHANGELOG.md`, append this bullet as the last bullet of the `## Unreleased` section, separated by one blank line from the previous bullet as the file already does (if a release has consumed that section since this prompt was written, create `## Unreleased` directly above the newest `## vX.Y.Z` heading). Do not create a second `## Unreleased` heading and do not touch any released section:

   ```
   - fix: skip the Sentry/repo-clone/Jira preflight for healthcheck and oauth-probe runs, which never use those tools — every healthcheck on the sentry-fix and sentry-collector lanes died with pod_crash_no_stdout
   ```

7. Self-check before finishing: re-run every command in `<verification>` and confirm each passes; then walk the summary bullets against the diff — liveness types skip, all other types unchanged, tests cover both halves, one Unreleased section holding the new bullet.
</requirements>

<constraints>
- Do NOT commit — dark-factory handles git.
- Existing tests must still pass.
- Only `main.go`, `main_test.go` and `CHANGELOG.md` change. Do not edit `pkg/preflight/`, `pkg/factory/`, README or docs (README and `docs/` do not describe preflight; the generic mentions in `NEXT-DIRECTIONS.md` and `scenarios/001-sentry-issue-happy-path.md` are out of scope).
- Leave `cmd/run-task/main.go` unchanged: it runs its own inline Sentry + repo-clone preflight but has no task-type dispatch (it always builds the triage agent via `factory.CreateAgent`), so it has no liveness path.
- Behaviour for every task type other than `healthcheck` / `oauth-probe` must be unchanged — no reordering of checks, no changed error messages.
- Do not add configuration, flags or env vars to toggle the skip; the skip is unconditional for liveness types.
- Use `github.com/bborbe/errors` for any error wrapping (project convention); none should be needed for this change.
- Do not run `go mod vendor` (even though `docs/dod.md` mentions it). Do not change `go.mod` / `go.sum` unless `make precommit` fails on a dependency vulnerability with a known fixed version; then bump only that module with `go get <pkg>@<fixed> && go mod tidy` and add a CHANGELOG bullet for it.
</constraints>

<verification>
Run from the repo root:

```bash
set -e
set -o pipefail
ROOTDIR=/workspace make precommit

# Guard uses the library constants inside main.go.
grep -q 'agentlib.TaskTypeHealthcheck' main.go
grep -q 'agentlib.TaskTypeOAuthProbe' main.go

# validatePreflight is now tested.
grep -q 'Describe("validatePreflight"' main_test.go

# Exactly one Unreleased heading, and the new bullet sits under it.
test "$(grep -c '^## Unreleased' CHANGELOG.md)" = "1"
awk '/^## /{sec=$0} /skip the Sentry\/repo-clone\/Jira preflight for healthcheck and oauth-probe/{print sec}' CHANGELOG.md | grep -qx '## Unreleased'
```
</verification>
