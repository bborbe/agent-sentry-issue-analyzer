---
status: approved
spec: [001-sentry-analyzer-repo-map-and-lineno-less-frames]
created: "2026-09-01T13:10:00Z"
queued: "2026-09-01T12:53:01Z"
branch: dark-factory/sentry-analyzer-repo-map-and-lineno-less-frames
---

<summary>
- The triage planning prompt now maps Sentry projects `nuke-dev` and `nuke-prod` to the source repo `bborbe/nuke`
- The deep planning prompt carries the same mapping note in its clone step
- The canonical repo is tried before any project-named guess when the stack trace lacks a repo path
- The mapping stays a short, general note — not a table of every Sentry project
- Go prompt tests assert both embedded planning prompts contain the mapping, so a regression fails `go test ./pkg/prompts/...`
- The changelog gains an "Unreleased" entry documenting the mapping guidance
</summary>

<objective>
Anchor the nuke project→repo mapping (`nuke-dev` / `nuke-prod` → `bborbe/nuke`) in the clone step of both analyzer planning prompts so the agent clones the canonical repo before guessing project-named variants when the stack trace lacks a repo path, and lock it with Go prompt-test assertions.
</objective>

<context>
Read CLAUDE.md for project conventions.

Read `pkg/prompts/planning.md` in full — the note goes in Step 3, immediately after the `Bash(scripts/repo-clone.sh clone <repo>)` line, before the blank line / the paragraph starting "where `<repo>` is the owner/name".

Read `pkg/prompts/deep-planning.md` in full — the note goes in Step 2, immediately after the `Bash(scripts/repo-clone.sh clone <repo>)` line, before the blank line / the paragraph starting "where `<repo>` is the owner/name".

Read `pkg/prompts/prompts.go` — the `//go:embed` variables and the `BuildPlanningInstructions` / `BuildDeepPlanningInstructions` functions (do NOT modify this file; it needs no structural change).

Read `pkg/prompts/prompts_test.go` — the Ginkgo/Gomega assertion pattern. Add new `It` blocks inside the existing `Describe("BuildPlanningInstructions (triage)"` and `Describe("BuildDeepPlanningInstructions"` blocks, following the existing `It("planning prompt contains the read-only repo clone invocation", ...)` pattern (which asserts on `instrs[0].Content`).

Read `/home/node/.claude/plugins/marketplaces/coding/docs/changelog-guide.md` and `/home/node/.claude/plugins/marketplaces/coding/docs/go-testing-guide.md`.
</context>

<requirements>
1. In `pkg/prompts/planning.md`, Step 3, insert this note as a single line directly after the `Bash(scripts/repo-clone.sh clone <repo>)` line (before the blank line and the "where `<repo>` is the owner/name" paragraph). The `bborbe/nuke` text must fall within 5 lines after the clone-invocation line (AC1 evidence: `grep -A5 'repo-clone.sh clone' pkg/prompts/planning.md | grep -c 'bborbe/nuke'` ≥ 1):

   ```
   Sentry projects `nuke-dev` and `nuke-prod` map to source repo `bborbe/nuke`; when the stack trace lacks a repo path (frames are external library code with no bborbe repo path), clone the mapped canonical repo `bborbe/nuke` before guessing a project-named variant like `nuke-dev`.
   ```

   The note must contain `nuke-dev`, `nuke-prod`, and `bborbe/nuke`, and must stay a minimal general note — NOT a table of every Sentry project (spec invariant).

2. In `pkg/prompts/deep-planning.md`, Step 2, insert the same note (identical wording) as a single line directly after the `Bash(scripts/repo-clone.sh clone <repo>)` line, before the blank line and the "where `<repo>` is the owner/name" paragraph (AC1 evidence: `grep -A5 'repo-clone.sh clone' pkg/prompts/deep-planning.md | grep -c 'bborbe/nuke'` ≥ 1).

3. In `pkg/prompts/prompts_test.go`, add two `It` blocks following the existing Ginkgo/Gomega pattern (which calls `prompts.BuildPlanningInstructions()` / `prompts.BuildDeepPlanningInstructions()` and asserts on `instrs[0].Content`):
   - Inside `Describe("BuildPlanningInstructions (triage)", ...)`: `It("planning prompt contains the nuke repo-mapping guidance", ...)` asserting the planning content `ContainSubstring("nuke-dev")` and `ContainSubstring("bborbe/nuke")`.
   - Inside `Describe("BuildDeepPlanningInstructions", ...)`: `It("deep planning prompt contains the nuke repo-mapping guidance", ...)` asserting the deep-planning content `ContainSubstring("nuke-dev")` and `ContainSubstring("bborbe/nuke")`.

   These assertions traverse the `//go:embed` boundary — they read the embedded markdown through the same `Build*Instructions` accessors the runtime uses, so an embedded-prompt edit that drops the mapping fails the test.

4. Append the mapping-guidance entry to the `## Unreleased` section of `CHANGELOG.md`: if `## Unreleased` exists (created by the frame-contract prompt), append a new `feat:` bullet to it; if it is absent (this prompt may run first), create `## Unreleased` at the top of the file above `## v0.8.0` and add the bullet. The entry: the analyzer planning prompts (`planning.md`, `deep-planning.md`) now map Sentry projects `nuke-dev` / `nuke-prod` to the source repo `bborbe/nuke` and instruct the agent to clone the canonical repo before guessing project-named variants when the stack trace lacks a repo path. Follow the format in `/home/node/.claude/plugins/marketplaces/coding/docs/changelog-guide.md`.

5. Before finishing, re-run `<verification>` and confirm it passes; walk acceptance criterion AC1 against your change.
</requirements>

<constraints>
- Do NOT commit — dark-factory handles git.
- The mapping guidance is a note, not a project table — do NOT add a table of every Sentry project (spec invariant).
- Do NOT change `scripts/repo-clone.sh` or anything else under `scripts/`.
- Prompts stay Go-embedded via `//go:embed`; `pkg/prompts/prompts.go` requires no structural change — only markdown content in `pkg/prompts/planning.md` / `pkg/prompts/deep-planning.md` and test additions to `pkg/prompts/prompts_test.go`.
- Do NOT touch the execution / deep-execution prompts, the collector-planning prompt, the verdict logic, the `<output-format>` contract, or `scripts/sentry-read.sh`.
- Existing tests must still pass.
- Do NOT create a new scenario file — container-executable tests cover the behavior (spec non-goal).
- No new config knobs, flags, or thresholds — the mapping note is static markdown.
</constraints>

<verification>
- `go test -mod=mod ./pkg/prompts/...` — must exit 0
- `grep -A5 'repo-clone.sh clone' pkg/prompts/planning.md | grep -c 'bborbe/nuke'` — must be ≥ 1
- `grep -A5 'repo-clone.sh clone' pkg/prompts/deep-planning.md | grep -c 'bborbe/nuke'` — must be ≥ 1
- `grep '## Unreleased' CHANGELOG.md` — must match
- `make precommit` — must pass (this prompt changes Go test code)
</verification>
