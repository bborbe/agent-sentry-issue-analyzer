---
status: prompted
tags:
    - dark-factory
    - spec
approved: "2026-09-01T12:15:25Z"
generating: "2026-09-01T12:15:57Z"
prompted: "2026-09-01T12:27:32Z"
branch: dark-factory/sentry-analyzer-repo-map-and-lineno-less-frames
---

## Summary

- The analyzer's planning prompts tell it to derive the implicated repo from the stack trace, but for the nuke-backlog issues the trace frames are external library code (e.g. `kafka/coordinator/consumer.py`) with no repo path. The agent then guesses a project-named repo and fails. This spec adds a project→repo mapping note: Sentry projects `nuke-dev` / `nuke-prod` map to source repo `bborbe/nuke`, and that canonical repo is tried before any project-named guess.
- `scripts/sentry-read.sh` drops every frame that lacks an integer `lineno` and then reports `stack_trace unavailable (no exception entry)` — even when an exception entry with frames exists. Real nuke events have exception entries whose frames all lack `lineno` (and `abs_path`), so the agent is actively misled into believing no stack trace exists.
- Fix: when frames exist but none has a usable line number, emit them as `filename in function` lines (no `:line`) under `stack_trace=<N> frames`; reserve the `no exception entry` marker for events that genuinely lack an exception entry; add a distinct `no frames` marker for an exception entry with zero frames.
- The deterministic mock-API test (`scripts/test-sentry-read.sh`) is extended with a lineno-less-frames fixture, and the prompt tests gain a mapping assertion. A `CHANGELOG.md` `## Unreleased` entry is added.

## Problem

Two defects surfaced in the live E2E (2026-09-01, fresh analyzer job with the `:dev` image + prod GitHub App creds on task NUKE-DEV-3A / issue 5192501045, payload probes on issues 6727724202, 6827871815, 6946325125).

First, repo identification: the planning prompt derives the repo from the stack trace, but the nuke-backlog traces are external-library frames with no bborbe repo path. The fresh job ran `repo-clone.sh clone bborbe/nuke-dev`, then `repo-clone.sh clone seibert-group/nuke-dev` — both wrong; the real source repo for projects `nuke-dev` and `nuke-prod` is `bborbe/nuke`, which clones fine with prod GitHub App creds. Without guidance the agent cannot identify the repo for the most common issue class.

Second, frame emission: `sentry-read.sh` (shipped in the prior SC1 change) drops every frame without an integer `lineno` and then emits the misleading marker `stack_trace unavailable (no exception entry)`. The real events have exception entries with frames that all lack `lineno` (e.g. issue 6727724202: frame0 `{'filename': 'kafka/coordinator/consumer.py', 'abs_path': None, 'lineno': None, 'function': '_maybe_auto_commit_offsets_sync'}`). The agent then concludes no stack trace is available at all, corrupting every downstream root-cause decision.

## Goal

After this work: given a nuke-cluster Sentry issue whose stack trace has no repo path, the analyzer clones the canonical `bborbe/nuke` repo (never a guessed project-named variant). And `sentry-read.sh` output truthfully represents the latest event's trace: frames are emitted whenever an exception entry carries frames — including when none have a line number — and the `no exception entry` marker is emitted only for events that genuinely lack an exception entry. Both behaviors are locked by deterministic tests (mock-API script test + Go prompt tests).

## Non-goals

- Do NOT add a per-project table of every Sentry project → repo mapping — the mapping guidance is a short note covering `nuke-dev`/`nuke-prod` only.
- Do NOT change `scripts/repo-clone.sh` — clone semantics, validation, and read-only behavior stay as-is.
- Do NOT change the 7-key metadata output contract or the frozen key order of `sentry-read.sh`.
- Do NOT touch execution / deep-execution prompts, verdict logic, or the output-format contract.
- Do NOT emit context lines, raw payload, or any source code from the event body (no-PII rule is a hard invariant).
- Do NOT add a new scenario — container-executable mock-API test + operator live probe cover the behavior (see Acceptance Criteria note).

## Acceptance Criteria

- [ ] **AC1 — repo mapping guidance anchored in the clone step and tested:** in both `pkg/prompts/planning.md` and `pkg/prompts/deep-planning.md`, the mapping note (`nuke-dev`/`nuke-prod` → `bborbe/nuke`) appears within 5 lines of the `repo-clone.sh clone` invocation, and `pkg/prompts/prompts_test.go` gains a Go assertion that both embedded planning contents contain `bborbe/nuke`, which passes under `go test ./pkg/prompts/...`. Evidence: `grep -A5 'repo-clone.sh clone' <file> | grep -c 'bborbe/nuke'` returns ≥1 for each file + exit code of the Go test.
- [ ] **AC2 — lineno-less frames emitted truthfully:** on an event whose exception entry has ≥1 frame but none with an integer `lineno`, `scripts/sentry-read.sh` emits exactly one `stack_trace=<N> frames` header (N ≥ 1) followed by `<basename> in <function>` lines (no `:line`), and its stdout contains zero occurrences of `no exception entry`. Evidence: new mock-API fixture in `scripts/test-sentry-read.sh` + grep on captured stdout.
- [ ] **AC3 — no-exception-entry marker preserved:** on an event with genuinely no exception entry, `scripts/sentry-read.sh` emits exactly one `stack_trace unavailable (no exception entry)` marker with the 7 metadata keys intact and the run exits 0. Evidence: existing issue-456 fixture assertion in `scripts/test-sentry-read.sh` (unchanged).
- [ ] **AC4 — no-PII + cap hold for both frame shapes:** for both the `file:line in` and the lineno-less `filename in` outputs, stdout contains no raw JSON keys (`"context"` / `"frames"` / `stacktrace`), no fixture context sentinel, and ≤30 frame lines total. Evidence: negative greps + count assertion in `scripts/test-sentry-read.sh`.
- [ ] **AC5 — suites green:** `bash scripts/test-sentry-read.sh` exits 0 and prints `PASS: all AC1-AC6 assertions passed` (including the new fixtures), `go test ./pkg/prompts/...` exits 0, and `CHANGELOG.md` contains the new `## Unreleased` entries (frame-emission contract + repo-mapping guidance). Evidence: exit codes + `grep '## Unreleased' CHANGELOG.md`.
- [ ] **AC6 — `no frames` marker distinct:** on an event whose exception entry's first value has an empty/missing `stacktrace.frames`, `scripts/sentry-read.sh` emits exactly one `stack_trace unavailable (no frames)` marker and zero occurrences of `no exception entry`, with the 7 metadata keys intact and exit 0. Evidence: empty-frames fixture in `scripts/test-sentry-read.sh` + grep on captured stdout.

**Scenario coverage — NO new scenario.** The frame-emission behavior is fully reachable by the container-executable mock-API test (embedded python3 `http.server` on 127.0.0.1 + real curl, deterministic fixtures) and the prompt behavior by Go tests; the operator live probe is verification, not a scenario. Conditions (a)–(d) of the scenario rule do not hold.

## Verification

### Container-executable (runs inside the YOLO container at prompt time)

```
bash scripts/test-sentry-read.sh          # exits 0, prints PASS: all AC1-AC5 assertions passed
go test ./pkg/prompts/...                 # exits 0
grep -A5 'repo-clone.sh clone' pkg/prompts/planning.md | grep -c 'bborbe/nuke'        # ≥1
grep -A5 'repo-clone.sh clone' pkg/prompts/deep-planning.md | grep -c 'bborbe/nuke'   # ≥1
```

### Operator-executable (runs on the host after the change, real Sentry creds)

```
SENTRY_API_TOKEN=<real token> SENTRY_ORG=bborbe bash scripts/sentry-read.sh 6727724202
# stdout must NOT contain 'stack_trace unavailable (no exception entry)';
# must contain a 'stack_trace=<N> frames' header with ≥1 frame line
# (or, if the event has an exception entry with zero frames, the distinct 'no frames' marker)
```

## Desired Behavior

1. A successful (HTTP 200) latest-event fetch is classified into exactly one output state: (a) emit frames, (b) `stack_trace unavailable (no frames)`, or (c) `stack_trace unavailable (no exception entry)` — the current conflation of (b) and (c) into `no exception entry` is removed.
2. When the exception entry's first value has ≥1 frame and none has an integer `lineno`, emit every frame (capped 30) as `<basename> in <function>` under a `stack_trace=<N> frames` header — the agent sees that a trace exists.
3. When the exception entry exists but its first value's `stacktrace` has zero frames (empty or missing array), emit the distinct marker `stack_trace unavailable (no frames)`. Only an event with no exception entry emits `stack_trace unavailable (no exception entry)`.
4. Frame emission preserves the no-PII + cap contract for both line shapes: basename of `abs_path`/`filename` only, no context lines, no raw payload keys, at most 30 frames, and every emitted frame value is single-line (newline / control characters stripped).
5. `pkg/prompts/planning.md` (Step 3) and `pkg/prompts/deep-planning.md` (Step 2) each gain a short mapping note in the clone step: Sentry projects `nuke-dev` and `nuke-prod` map to source repo `bborbe/nuke`; when the stack trace lacks a repo path, clone the mapped canonical repo before guessing project-named variants. The note is minimal and general — not a table of every project.
6. `pkg/prompts/prompts_test.go` gains assertions that both the triage planning and deep-planning embedded contents contain the mapping (`nuke-dev` and `bborbe/nuke`).
7. `scripts/test-sentry-read.sh` gains a lineno-less-frames fixture (frames all `lineno: None`, `abs_path: None`, matching the real 6727724202 shape) asserting AC2's output, and an empty-frames fixture asserting the `no frames` marker (AC6); all existing AC1-AC5 assertions remain green unchanged.
8. `CHANGELOG.md` gains a fresh `## Unreleased` section at the top with one entry per change (frame-emission contract; repo-mapping guidance).

## Constraints

- The metadata block contract is frozen: exactly the 7 key=value lines (`short_id`, `status`, `live_event_count`, `first_seen`, `last_seen`, `users_impacted`, `title`) in their current order; a metadata fetch failure still exits non-zero.
- The event fetch stays best-effort and non-fatal: it never fails the run and degrades to exactly one `stack_trace unavailable (<reason>)` line (reasons `timeout | no events | auth | fetch failed | no frames | no exception entry`).
- No-PII is a hard invariant for both frame shapes: basename only, no context lines, no raw payload, capped 30 frames.
- Mixed-case frames (some with a usable integer `lineno`, some without) keep current behavior: only the frames with a usable `lineno` are emitted as `file:line in function` — no new regression surface.
- `scripts/repo-clone.sh`, the execution / deep-execution prompts, the verdict logic, and the `<output-format>` contract are untouched.
- The existing `scripts/test-sentry-read.sh` assertions for metadata order, the 30-frame cap on the issue-123 fixture, and the failure stubs (456 no-exception / 789 404 / 999 401) must still pass without weakening.
- The mapping guidance is a note, not a project table — do NOT add a table of every Sentry project (invariant; if a future consumer demands broader mapping, that is a separate spec).
- Prompts stay Go-embedded via `//go:embed`; `pkg/prompts/prompts.go` requires no structural change, only markdown content and test additions.

## Failure Modes

| Trigger | Expected behavior | Recovery |
|---------|-------------------|----------|
| Sentry API down / event fetch timeout (curl rc 28) | `stack_trace unavailable (timeout)`, run exits 0, metadata intact | Rerun the script; operator checks Sentry status |
| Sentry schema drift — `lineno` present but a string (e.g. `"42"`) | Frames treated as lineno-less and emitted as `filename in function` — truthful, never a false `no exception entry` | None needed; log the drift if it persists across events |
| Exception entry with empty `values` or missing `stacktrace.frames` | `stack_trace unavailable (no frames)` — distinct from `no exception entry` | None — classification is now correct |
| Curl interrupted mid-download (partial event body) | Python parse fails → best-effort marker, run exits 0 | Rerun the script |
| Rate limiting (HTTP 429) on `events/latest` | `stack_trace unavailable (fetch failed)`, non-fatal | Rerun later; verify token scope |
| Malicious event payload — frame function/filename contains a newline or `=` | Emitted frame values are single-line (control chars stripped) so the key=value block and frame lines cannot be line-injected | Payload treated as data; no shell interpolation (existing `[0-9]+` id validation unchanged) |
| Deployed image still lacks the mapping note | Operator live probe shows the agent cloning a project-named repo | Re-deploy after merge; verify via `grep -A5 'repo-clone.sh clone'` on the embedded prompt sources |
| Clock skew | Not applicable — this change adds no time-based logic | — |

## Security / Abuse Cases

- Input crosses a trust boundary: Sentry HTTP event payloads are remote data rendered into `sentry-read.sh` stdout. Emitted frame values are derived from `filename`/`abs_path`/`function` fields and must be single-line (strip newline/control characters) so a crafted payload cannot inject spurious lines into the output block.
- No context lines and no raw payload are ever emitted — secrets inside the event body (e.g. exception values) never reach stdout; the Python extraction reads only the first exception value's `stacktrace.frames`.
- Token handling is unchanged: `SENTRY_API_TOKEN` comes from env, never echoed; the issue id is validated to `[0-9]+` before any URL construction.
- The new logic stays inside the existing `python3 -c` block reading a temp file — no `eval`, no shell metacharacter interpolation into a command line.

## Suggested Decomposition

Prompts are generated in this order — each row is a single prompt with a clear scope. The spec touches 2 independent code layers (bash script + Go-embedded markdown), so two prompts, both fully container-testable.

| # | Prompt focus | Covers DBs | Covers ACs | Depends on |
|---|---|---|---|---|
| 1 | `sentry-read.sh` three-state frame contract (lineno-less frames, `no frames` marker, reserved `no exception entry`) + `test-sentry-read.sh` fixtures; creates the `## Unreleased` CHANGELOG entry | 1–3, 7, 8 | 2–5 | — |
| 2 | Repo-mapping guidance in `planning.md` + `deep-planning.md` + `prompts_test.go` mapping assertions; appends its CHANGELOG entry | 4–6, 8 | 1, 5 | — |

Rationale: prompt 1 fixes the more damaging defect first — the misleading `no exception entry` marker actively corrupts every downstream root-cause analysis the moment the new image ships — and is fully locked by the deterministic mock-API test. Prompt 2 is an independent markdown + Go-test change with no dependency on prompt 1. Both are pure container-executable changes; the operator live probe runs once after both land.

## Do-Nothing Option

Without this work the analyzer keeps cloning wrong repos for the entire nuke-backlog class (external-library traces with no repo path are the norm for nuke issues), and `sentry-read.sh` keeps reporting that traces are absent when they exist. Every such issue is either misanalyzed or needs manual intervention, and the false `no exception entry` marker erodes trust in the pipeline's output. The current approach is not acceptable for the E2E follow-up.
