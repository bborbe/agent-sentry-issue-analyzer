---
status: prompted
tags:
    - dark-factory
    - spec
approved: "2026-08-31T22:30:46Z"
generating: "2026-08-31T22:31:17Z"
prompted: "2026-08-31T22:37:30Z"
branch: dark-factory/sentry-read-event-payload
---

## Summary

- `scripts/sentry-read.sh` currently fetches only the Sentry **issue metadata** endpoint and prints a flat `key=value` block (`short_id`, `status`, `live_event_count`, `first_seen`, `last_seen`, `users_impacted`, `title`). It never fetches the event payload, so the analyzer never sees a stack trace with `file:line` — every escalated task fails with "no stack trace/event payload available via the API".
- This spec extends the script to additionally fetch the **latest event payload** and emit the stack trace frames as `file:line in function` lines, capped at 30 frames, from the first exception value only.
- The metadata block stays the primary output and is **always** emitted; the event fetch is best-effort — a failure emits an explicit `stack_trace unavailable (<reason>)` marker, keeps exit code 0, and never fails the run.
- A small bug is fixed: the script's header claims it accepts a bare numeric issue id, but the id extraction only matches URLs — a bare `5192501045` errors out. Pure numeric ids become accepted.
- Output never contains frame context lines or raw payload bodies (stack traces can carry PII; the data stays on-cluster per the vault KB "Sentry Issue Analyzer Agent" data-privacy note).

## Problem

The analyzer agent's root-cause analysis depends on a stack trace (repo, file, line, function). Today `scripts/sentry-read.sh` — the token-based read path that replaced the Sentry MCP tools — returns only issue metadata. Live verification (2026-09-01) confirmed the metadata endpoint returns no stack trace: the trace lives at `GET /api/0/organizations/{org}/issues/{id}/events/latest/` under `entries[].type=="exception" → data.values[].stacktrace.frames[]` (`abs_path`/`filename`, `lineno`, `function`). Because the script never fetches it, the agent cannot resolve the implicated `file:line`, and every escalated task degrades into a `needs_input` failure. The fix is a read-only, best-effort extension of the existing script — no new tooling, no new auth.

## Goal

After this work, a single `scripts/sentry-read.sh <issue-url-or-id>` invocation returns both the live issue metadata block (unchanged, always present) and, when the issue has an event with an exception stack trace, up to 30 `file:line in function` frames. A missing or failed event fetch degrades to an explicit `stack_trace unavailable (<reason>)` marker while the run still exits 0, so the agent always gets the metadata and never fails because the stack trace is absent. Bare numeric issue ids work exactly like URLs.

## Non-goals

- Do NOT fetch or emit anything beyond the latest event's exception frames — no event list pagination, no context lines, no raw JSON bodies, no other event entries.
- Do NOT change the Go collector/controller/agent code, the Config CRD, preflight, or `ALLOWED_TOOLS` — script-only.
- Do NOT update the agent planning/execution prompt text to consume the new `stack_trace` output — that is a later step of the same vault task, separate from this spec.
- Do NOT add a frame-count cap knob, an opt-out flag for the event fetch, or any retry/backoff logic — the cap is fixed at 30 and the fetch is always attempted.
- Do NOT change metadata failure behavior: a metadata fetch failure still exits non-zero (the prompts treat a failed script as a hard `needs_input` STOP). Only the event fetch is best-effort.

## Acceptance Criteria

**Scenario coverage: none.** The changed behavior is fully reachable by the deterministic mock-API test (container-executable) plus the operator's live check; existing scenario 001 already covers the E2E agent journey. See `docs/rules/scenario-writing.md` "When to Write a Scenario".

- [ ] **AC1 — metadata block unchanged.** Running the script against a live issue prints the metadata block with all 7 keys (`short_id`, `status`, `live_event_count`, `first_seen`, `last_seen`, `users_impacted`, `title`) in the existing order, identical to pre-change output for the same issue. — evidence: `scripts/sentry-read.sh <url>` stdout piped through `grep -cE '^(short_id|status|live_event_count|first_seen|last_seen|users_impacted|title)='` returns 7 (operator live run; also asserted by the mock-API test).
- [ ] **AC2 — stack trace frames emitted.** For an issue with events, the script emits a `stack_trace=<N> frames` header line (N ≥ 1) followed by N frame lines matching `<file>:<line> in <function>`. — evidence: live run against `https://bborbe.sentry.io/issues/5192501045/` stdout has `grep -E '^stack_trace=[1-9]'` returning 1 line and `grep -cE ':[0-9]+ in '` returning ≥ 1; deterministic mock-API test asserts the same on a fixture with a known exception stack trace.
- [ ] **AC3 — bare numeric id works.** `scripts/sentry-read.sh 5192501045` (no URL) exits 0 and emits the same metadata block as the URL form. — evidence: exit code 0, `grep -cE '^short_id='` on stdout returns 1, stderr contains no `could not extract a numeric Sentry issue id`; mock-API test asserts extraction on a bare-id invocation.
- [ ] **AC4 — event-fetch failure is best-effort.** When `events/latest` fails (404 no events, 401/403, timeout, malformed JSON) or the event has no exception entry, the script exits 0, emits the full 7-key metadata block, and emits exactly one `stack_trace unavailable (<reason>)` line in place of the header+frames. — evidence: mock-API test pointing `SENTRY_URL` at a stub that returns 404 for `events/latest` asserts exit code 0, `grep -cE '^(short_id|status|live_event_count|first_seen|last_seen|users_impacted|title)='` == 7, `grep -cE '^stack_trace unavailable'` == 1; operator live run against an issue with no events confirms the same.
- [ ] **AC5 — no PII / raw payload, frames capped at 30.** Stdout never contains frame context lines or raw JSON payload bodies, and at most 30 frame lines are emitted. — evidence: mock-API test with a fixture of 40+ frames each carrying a `context` array asserts `grep -cE '^[^=]+:[0-9]+ in '` == 30 (cap) and `grep -cE '"context"|"frames"|stacktrace'` == 0 on stdout; negative check on the fixture's raw body string returns 0 lines.

## Verification

### Container-executable (runs inside the YOLO container at prompt time)

- `bash -n scripts/sentry-read.sh` — syntax check exits 0.
- `make precommit` — Go suite stays green (unchanged code paths).
- Added mock-API test (the prompt's deterministic test): serves fixture JSON for `/api/0/organizations/test/issues/{id}/` and `/issues/{id}/events/latest/` over localhost HTTP (python3 `http.server` on an ephemeral port), runs the script with `SENTRY_URL=http://127.0.0.1:<port>` + `SENTRY_ORG=test` + a dummy `SENTRY_API_TOKEN`, and asserts the AC1–AC5 output contract (metadata intact, frame format, 30-frame cap, `unavailable` marker on a 404 stub, bare-id acceptance). Expected: exits 0. Concretely: `SENTRY_API_TOKEN=x SENTRY_ORG=test SENTRY_URL=http://127.0.0.1:<port> scripts/sentry-read.sh 123` produces the contract, and `scripts/sentry-read.sh <fixture-url>/issues/123/` the same.

### Operator-executable (runs on the host after merge, needs live `SENTRY_API_TOKEN`)

- `scripts/sentry-read.sh https://bborbe.sentry.io/issues/5192501045/` — grep stdout for `stack_trace` and at least one `:line in ` frame (AC2).
- `scripts/sentry-read.sh 5192501045` — exits 0, same metadata block (AC3).
- `scripts/sentry-read.sh <issue-with-no-events-url>` — exits 0, metadata block + `stack_trace unavailable` marker (AC4).

## Desired Behavior

Numbered observable outcomes — what the script does to make the Acceptance Criteria fire:

1. **Bare numeric id accepted (bug fix).** The argument is accepted unchanged when it matches `^[0-9]+$`; otherwise the existing `/issues/([0-9]+)` URL extraction applies. A non-numeric, non-URL argument still exits 2 with the existing usage error, and the `[0-9]+`-only validation before URL interpolation is preserved. (Covers AC3.)
2. **Latest-event fetch.** After emitting the metadata block, the script GETs `{SENTRY_URL}/api/0/organizations/{SENTRY_ORG}/issues/{id}/events/latest/` with the same Bearer auth and `--max-time 30`, and locates the stack trace as: the first `entries[]` item with `type == "exception"`, its first `data.values[]` entry, that entry's `stacktrace.frames[]`. (Covers AC2, AC4.)
3. **Frame line emission.** Each extracted frame emits one line `<file>:<line> in <function>`, where `<file>` is the basename of `abs_path` (falling back to `filename`), `<line>` is `lineno`, and `<function>` is the frame's `function` (empty → `unknown`). Frames without a numeric `lineno` are skipped. A header line `stack_trace=<N> frames` precedes the frames (N ≥ 1). At most the first 30 frames are emitted. Only `filename`/`abs_path` basename, `lineno`, and `function` are read from the payload — never `context` lines, never the raw JSON. (Covers AC2, AC5.)
4. **Best-effort failure path.** Any event-fetch failure — curl non-zero (404 no events, 401/403, timeout), malformed/empty JSON, missing exception entry — emits exactly one `stack_trace unavailable (<reason>)` line in place of the header+frames, keeps exit code 0, and never suppresses or reorders the metadata block. The event fetch is guarded so it cannot fail the run under `set -euo pipefail`. Metadata fetch failure behavior is unchanged (still fails the run). (Covers AC4.)
5. **Header comment updated.** The script header documents the `stack_trace=<N> frames` / `stack_trace unavailable (<reason>)` output, the `file:line in function` frame format, the 30-frame cap, the accepted bare numeric id, and corrects the now-inaccurate "single GET" claim (two GETs). (Documentation; keeps the script self-describing for the agent that reads it.)

## Constraints

- Single bash script at `scripts/sentry-read.sh`; `set -euo pipefail`; `curl` for GETs; `python3` for JSON parsing (both present in the image). No new languages or dependencies.
- Read-only by construction: only GET requests, no mutations, no shell metacharacters — the argument is validated to `[0-9]+` before any URL interpolation.
- Env contract unchanged: `SENTRY_API_TOKEN` (required, non-empty → usage exit otherwise), `SENTRY_ORG` (default `bborbe`), `SENTRY_URL` (default `https://bborbe.sentry.io`).
- The 7 metadata keys, their meaning, and their emission order are frozen: `short_id`, `status`, `live_event_count`, `first_seen`, `last_seen`, `users_impacted`, `title`. Metadata is load-bearing: a metadata failure must still fail the run (existing behavior), so the agent's `needs_input` stop on script failure stays correct.
- Usage/id-extraction error keeps exit code 2; the `Bash(scripts/sentry-read.sh:*)` invocation contract and the agent prompts' field consumption are unchanged.
- Data privacy invariant: stack trace output is limited to `file:line in function`; context lines and raw payload bodies never appear in stdout (vault KB "Sentry Issue Analyzer Agent" data-privacy note — keep on-cluster).

## Failure Modes

| Trigger | Expected behavior | Detection | Recovery |
|---|---|---|---|
| `events/latest` returns 404 (issue has no events) | Event fetch best-effort path: `stack_trace unavailable (no events)`, exit 0, metadata emitted | `grep '^stack_trace unavailable'` on stdout | None needed — metadata is the deliverable; stack trace genuinely absent |
| `events/latest` returns 401/403 (token lacks event scope) | `stack_trace unavailable (auth)`, exit 0, metadata emitted | marker line; metadata still present | Operator checks `SENTRY_API_TOKEN` scope/team membership in Sentry |
| Event fetch times out (slow Sentry, `--max-time 30`) | `stack_trace unavailable (timeout)`, exit 0, metadata emitted | marker line | Retry the invocation |
| Event payload malformed/truncated or schema-drifted (fields renamed) | `stack_trace unavailable (no exception entry)`, exit 0, metadata emitted | marker line; frame count 0 | If schema drift suspected, file a follow-up spec to update the extraction |
| Metadata endpoint fails (Sentry down, network, 5xx, 429) | Script exits non-zero (unchanged), no output contract | non-zero exit + curl stderr | Retry after the rate-limit window / when Sentry recovers |
| Unexpectedly large event payload | Only ≤30 frame lines and the header are printed; raw body never printed | stdout line count bounded by metadata(7) + header + 30 | None — output size is bounded by design |
| Two concurrent invocations | Independent GETs, no shared state, no files written | n/a | n/a — concurrency-safe by construction |
| Clock skew | Timestamps (`first_seen`, `last_seen`, `title` etc.) passed through verbatim; no local time math | n/a | None — no time-dependent logic in this script |

## Security / Abuse Cases

The script touches HTTP (Sentry API) and accepts user-controlled input (issue id / URL), so security is in scope.

- **Input validation**: the argument is interpolated into a URL only after matching `[0-9]+`; the bare-id acceptance (DB1) must preserve this — a URL with embedded shell metacharacters or path injection still fails extraction and exits 2. No `eval`, no shell interpolation of the argument into anything but the validated numeric id.
- **Credential handling**: `SENTRY_API_TOKEN` is read from the environment and sent only as the `Authorization: Bearer` header; it is never printed, logged, or embedded in the URL. Env is already forwarded into the Claude subprocess (existing CHANGELOG fix) — no change.
- **Crossing trust boundaries**: the Sentry API is external; its response is parsed read-only by python3. A hostile/malformed response can only cause the extraction to find no frames (→ `unavailable` marker), never to emit payload bytes, because DB3 reads only three scalar fields per frame.
- **PII**: stack traces can contain paths, usernames, or secrets in function/context data. The invariant is that only `basename(file):line in function` is emitted; frame `context` arrays and raw JSON never leave the cluster. The analyzer's task write-back therefore receives only the stripped frame lines.
- **Hangs/retries**: both GETs carry `--max-time 30`; the event fetch failure path cannot hang the run (guarded, exit 0). No unbounded retry loops — none added.
- **Rate limiting**: Sentry 429s are covered by the failure modes above; the event fetch degrades to a marker rather than hammering the API.

## Suggested Decomposition

| # | Prompt focus | Covers DBs | Covers ACs | Depends on |
|---|---|---|---|---|
| 1 | Extend `scripts/sentry-read.sh`: bare-id acceptance, latest-event fetch, frame emission + 30-cap, best-effort marker, header comment | 1, 2, 3, 4, 5 | 1, 2, 3, 4, 5 | — |
| 2 | Mock-API test (python3 `http.server` fixtures for metadata + `events/latest`, 404 stub, 40+-frame fixture) asserting the AC1–AC5 output contract | — (tests 1-5) | 5, and supports 1-4 | prompt 1 (tests the new behavior) |

Rationale: a single behavioral change in one script, so prompt 1 is the whole feature; prompt 2 is the deterministic test harness that makes the cap and no-PII ACs (5) verifiable without a live Sentry issue and de-risks AC2–AC4. No cycles — the test depends on the behavior it asserts.

## Do-Nothing Option

Without this change, every escalated task keeps failing with "no stack trace/event payload available via the API", so the analyzer never reaches a real root-cause verdict and the Sentry resolution pipeline stays broken at the input step. The metadata-only output is insufficient for the feature's purpose — this spec exists because that state is the problem. Doing nothing is not acceptable for the vault task this serves.
