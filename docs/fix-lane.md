# Fix lane

The Sentry resolution pipeline has two halves that meet at one artifact: an
approved spec on a draft pull request. The fix agent produces that artifact;
the dark-factory watcher consumes it. This document describes the seam between
them — the contract the watcher enforces, the flow the fix agent follows, and
the per-repository requirement that decides whether a filed spec produces an
implementation task.

## Why this lane exists

The pipeline is split deliberately:

- The **fix agent** turns a confirmed High/High `real bug` deep verdict into an
  approved `kind: bug` spec and opens a draft pull request carrying it. It does
  not implement the fix.
- The **dark-factory watcher** watches pull requests and turns one that carries
  an approved, unfinished spec into an implementation task.

Neither half consumes the other's output without the artifact in between. The
spec must be in the location and state the watcher accepts, and the pull
request must satisfy the watcher's predicate. This document is the reference
for that seam.

## The watcher's four-condition contract

The watcher evaluates a pull request and keeps it only when **all four**
conditions hold at the pull request's head SHA:

1. **Open and draft.** The pull request is open and is a draft. The watcher
   re-confirms both through the pull-request API at evaluation time; the search
   snapshot that surfaced the candidate is not trusted for this.
2. **Dark-factory configuration present.** `.dark-factory.yaml` exists at the
   head ref. Without it the repository is not a dark-factory target and no
   implementation task can be emitted.
3. **An approved, unfinished spec is in the diff.** At least one spec under
   `specs/in-progress/*.md` is touched by the pull request's diff and is
   approved-but-not-completed: its frontmatter has `approved:` set and
   `completed:` absent or empty.
4. **No prompts in flight.** No `prompts/in-progress/*.md` remain at the head
   ref. This is self-trigger suppression: the watcher must not act on a
   repository whose prompt queue is already being processed.

The third condition is diff-scoped, and that is essential. Specs persist in
`specs/in-progress/` for their whole lifecycle, so an existence-only check
would keep every unrelated draft pull request in the repository. Only a spec
the pull request's own diff touches counts.

When the watcher drops a candidate, it records why. The skip reasons are:

| Skip reason | Meaning |
|---|---|
| `not_draft` | The pull request is not a draft (or not open). |
| `not_open` | The pull request is not open. |
| `no_dark_factory_yaml` | `.dark-factory.yaml` is absent at the head ref. |
| `no_spec_in_diff` | No approved-but-unfinished spec under `specs/in-progress/` is touched by the diff. |
| `prompts_in_flight` | `prompts/in-progress/*.md` remain at the head ref. |

An operator diagnosing a fix run that produced no implementation task reads
these labels from the watcher's metrics or logs.

## The fix lane end to end

1. The deep analyzer classifies an issue as a High/High `real bug` and hands
   the task to the fix agent.
2. The fix agent reads the Sentry trace, resolves the repository behind the
   verdict's `file:line`, and refuses a stale or unverifiable citation. A
   refusal is recorded in `## Fix Result` as `status: stale` or
   `status: skipped`; nothing is filed.
3. The fix agent builds an approved `kind: bug` spec at
   `specs/in-progress/bug-<slug>.md`. The spec's `approved:` value is stamped
   from the run's injected clock.
4. The fix agent creates or reuses the branch `sentry-fix/<slug>`, files the
   spec on it, and opens a draft pull request titled `fix(sentry): <issue-id>`
   from that branch to the repository's default branch. The base is resolved at
   runtime, never hardcoded.
5. The fix agent records `status: filed`, `repo`, `spec_path`, and `pr_url` in
   the task's `## Fix Result`, so the emitted pull request is traceable from
   the task body.
6. The watcher evaluates the pull request. On `Keep: true` it emits a
   `dark-factory-implement` task keyed by a deterministic UUID5 of the
   owner/repo, pull request number, and head SHA.

The lane ends at the emitted `dark-factory-implement` task. It does not
implement, review, or merge the fix.

## Idempotency and recovery

The Sentry issue id is the only idempotency key.

- The spec check and the pull-request check are independent. A spec already on
  the branch does not short-circuit pull-request creation, so a run that
  crashed between the two calls completes on its next attempt.
- A re-run reuses the existing branch, spec, and pull request: no second spec,
  no second branch, no second pull request.
- A concurrent create answered with `422` (a pull request already exists for
  the head branch) is treated as reuse, and the existing pull request's URL is
  returned.
- A failed spec filing returns an error before any pull request is created. A
  pull request pointing at an empty branch would read as done, which is worse
  than no pull request.

## Why the pull request stays a draft

The draft state is the human gate. The fix agent never flips the pull request
to ready-for-review, and the watcher consumes the draft directly. A draft
signals that the spec is machine-filed and awaiting the watcher, not that it
has been reviewed by a person.

## Per-repository requirement

The target repository must carry `.dark-factory.yaml` at the head ref for the
watcher to emit an implementation task.

The fix agent does not pre-check this file. Emission is the agent's contract,
and the watcher's own skip (`no_dark_factory_yaml`) is the filter. For example,
`bborbe/nuke` has no such file, so a spec filed there produces a pull request
the watcher skips.

`REPO_ALLOWLIST` is the operator's control over which repositories the lane may
write into. An empty allowlist means unbound; in that case the GitHub App
installation scope is the hard wall.

## Prerequisites

The GitHub App behind `PEM_KEY` needs `pull_requests: write` in addition to
`contents: write`. Without it the pull-request create returns `403` and the run
fails loudly rather than silently producing a branch with no pull request.
