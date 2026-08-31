---
status: completed
spec: [001-sentry-read-event-payload]
summary: Extended scripts/sentry-read.sh to accept bare numeric issue ids and emit up to 30 file:line in function stack trace frames from the latest event via a best-effort guarded fetch with a stack_trace unavailable marker, added the CHANGELOG Unreleased entry, and validated with bash -n, make test, and make precommit (exit 0).
execution_id: agent-sentry-issue-analyzer-sc1-exec-001-spec-001-sentry-read-event-payload
dark-factory-version: dev
created: "2026-09-01T12:00:00Z"
queued: "2026-08-31T22:39:05Z"
started: "2026-08-31T22:39:07Z"
completed: "2026-08-31T22:44:16Z"
branch: dark-factory/sentry-read-event-payload
---

# Extend the Sentry read script with latest-event stack trace frames

<summary>
- The Sentry read script now accepts a bare numeric issue id exactly like a full issue URL, fixing the case where a bare id (e.g. `5192501045`) errored out with "could not extract"
- After printing the unchanged metadata block, the script additionally fetches the latest event payload and emits its stack trace frames
- At most 30 frames are emitted, each as a single `file:line in function` line derived from only the three scalar fields needed (file path, line number, function name)
- A failed, empty, or malformed event fetch degrades to exactly one `stack_trace unavailable (<reason>)` line while the run still exits 0, and never disturbs or reorders the metadata block
- The metadata block and its failure behavior (a metadata fetch failure still fails the run) are unchanged
- The script header comment documents the new output, the fixed 30-frame cap, and the accepted bare numeric id, and corrects the now-inaccurate "single GET" claim
- A changelog entry is added documenting the feature and the bug fix
</summary>

<objective>
Extend the Sentry read script so a single invocation returns both the live issue metadata block (unchanged, always present) and, best-effort, up to 30 `file:line in function` stack trace frames from the latest event — so the analyzer can resolve the implicated file:line and never fails just because a stack trace is absent.
</objective>

<context>
Read CLAUDE.md for project conventions. There is no project-level CLAUDE.md at the repo root; the YOLO container's global conventions apply.

Read these files before making changes:

- `scripts/sentry-read.sh` — the only file being modified. Follow its existing style: `set -euo pipefail`, `: "${VAR:?...}"` for required env, `${VAR:-default}` for defaults, `printf 'key=%s\n' "$(...)"` for output, `curl -fsS --max-time 30` with the `Authorization: Bearer ${SENTRY_API_TOKEN}` header.
- `scripts/sentry-create-tasks.sh` — the repo's established pattern for `tmp_file="$(mktemp)"` + `trap 'rm -f ...' EXIT` cleanup, and for JSON parsing via `python3 -c '...' "${file}"` reading the file path from `sys.argv[1]`. Mirror this pattern for the event-fetch temp file and the frame extraction.
- `CHANGELOG.md` — for the existing entry format.
- `/home/node/.claude/plugins/marketplaces/coding/docs/changelog-guide.md` — for the `## Unreleased` section format and prefix rules.

The behavioral contract is defined in `specs/in-progress/001-sentry-read-event-payload.md` — read the Desired Behavior (DB1–DB5), Failure Modes, and Security sections; they are the requirements below restated.
</context>

<requirements>
1. **Accept a bare numeric issue id (DB1).** In `scripts/sentry-read.sh`, replace the id-extraction block (currently the single `issue_id="$(printf '%s' "$issue_id" | sed -nE 's#.*/issues/([0-9]+)/?.*#\1#p')"` command substitution under the comment `# Extract a numeric issue id from a URL like .../issues/1234567890/ or accept a bare id.`) so that:
   - If the argument matches `^[0-9]+$` it is used unchanged as `issue_id` (this is the bug fix — today a bare `5192501045` falls through the sed and errors out).
   - Otherwise the existing URL extraction applies unchanged: `printf '%s' "$issue_id" | sed -nE 's#.*/issues/([0-9]+)/?.*#\1#p'`.
   - If the result is empty (non-numeric, non-URL), keep the existing `echo "could not extract a numeric Sentry issue id from: $1" >&2` and `exit 2`.
   - Keep the empty-argument usage check (`usage: sentry-read.sh <issue-url-or-id>`, `exit 2`) unchanged at the top of the script.
   - Never interpolate the raw argument into a URL — only the validated numeric id is ever interpolated (Security § Input validation).

   The replacement block must behave exactly like:
   ```bash
   if printf '%s' "$issue_id" | grep -qE '^[0-9]+$'; then
     : # bare numeric id, use as-is
   else
     issue_id="$(printf '%s' "$issue_id" | sed -nE 's#.*/issues/([0-9]+)/?.*#\1#p')"
     if [ -z "$issue_id" ]; then
       echo "could not extract a numeric Sentry issue id from: $1" >&2
       exit 2
     fi
   fi
   ```

2. **Emit the metadata block unchanged.** The seven `printf` lines for `short_id`, `status`, `live_event_count`, `first_seen`, `last_seen`, `users_impacted`, `title` stay exactly as they are today, in this exact order, and remain the first and only unconditionally-emitted output. Metadata fetch behavior is unchanged: `curl -fsS --max-time 30` with the Bearer header against `${SENTRY_URL}/api/0/organizations/${SENTRY_ORG}/issues/${issue_id}/`; if it fails, the script exits non-zero (this is the existing, load-bearing `needs_input` stop — do not touch it).

3. **Best-effort fetch of the latest event (DB2, DB4).** Append AFTER the seven metadata `printf` lines, so the metadata block is always emitted first and is never wrapped in a conditional:
   - Create a temp file for the response body: `event_body="$(mktemp)"`, and add `trap 'rm -f "${event_body}"' EXIT` (mirror the `mktemp` + `trap` pattern in `scripts/sentry-create-tasks.sh`).
   - GET `${SENTRY_URL}/api/0/organizations/${SENTRY_ORG}/issues/${issue_id}/events/latest/` with the same `Authorization: Bearer ${SENTRY_API_TOKEN}` header.
   - Use `curl -sS --max-time 30 -o "${event_body}" -w '%{http_code}'` — do NOT use `-f` (you need the HTTP status to classify failures), and send curl's stderr to `/dev/null` so the best-effort path stays quiet.
   - Guard the fetch so it cannot fail the run under `set -euo pipefail`: wrap it in `set +e` / `set -e` and capture the curl exit code, e.g.:
     ```bash
     set +e
     event_http="$(curl -sS --max-time 30 -o "${event_body}" -w '%{http_code}' \
       -H "Authorization: Bearer ${SENTRY_API_TOKEN}" \
       "${SENTRY_URL}/api/0/organizations/${SENTRY_ORG}/issues/${issue_id}/events/latest/" 2>/dev/null)"
     event_rc=$?
     set -e
     ```
   - Classify the outcome into exactly one `stack_trace unavailable (<reason>)` line, using this decision tree:
     - curl exit code `28` (timeout) → reason `timeout`
     - HTTP status `404` → reason `no events`
     - HTTP status `401` or `403` → reason `auth`
     - any other non-zero exit or any status other than `200` → reason `fetch failed`
   - On any of the above, `printf 'stack_trace unavailable (%s)\n' "${reason}"` and continue (the run still exits 0).
   - On HTTP `200`, proceed to requirement 4.

4. **Extract and emit frames (DB3).** On HTTP `200`, run the event body through python3 using the `python3 -c '...' "${event_body}"` / `sys.argv[1]` pattern from `scripts/sentry-create-tasks.sh`, with the exact program below. This program reads ONLY `filename`/`abs_path` basename, `lineno`, and `function` from each frame — never `context`, never the raw JSON (the PII invariant depends on this):
   ```bash
   set +e
   frames="$(python3 -c '
   import json, sys, os
   try:
       data = json.load(open(sys.argv[1]))
       frames = []
       for entry in data.get("entries", []):
           if entry.get("type") == "exception":
               values = entry.get("data", {}).get("values", [])
               if values:
                   frames = values[0].get("stacktrace", {}).get("frames", [])
               break
   except Exception:
       sys.exit(0)
   lines = []
   for frame in frames[:30]:
       lineno = frame.get("lineno")
       if not isinstance(lineno, int):
           continue
       path = frame.get("abs_path") or frame.get("filename") or ""
       func = frame.get("function") or "unknown"
       lines.append("%s:%s in %s" % (os.path.basename(path), lineno, func))
   if lines:
       print("stack_trace=%d frames" % len(lines))
       print("\n".join(lines))
   ' "${event_body}")"
   frames_rc=$?
   set -e
   if [ "${frames_rc}" -ne 0 ] || [ -z "${frames}" ]; then
     printf 'stack_trace unavailable (no exception entry)\n'
   else
     printf '%s\n' "${frames}"
   fi
   ```
   - The frame line format is exactly `<file>:<line> in <function>` where `<file>` is the basename of `abs_path` (falling back to `filename`), `<line>` is `lineno`, and `<function>` is the frame's `function` (empty → `unknown`).
   - The 30-frame cap is the first 30 frames from `stacktrace.frames` AFTER skipping frames without a numeric `lineno`; the header `stack_trace=<N> frames` reports N = the number of emitted frame lines (N ≤ 30).
   - If the python command exits non-zero OR produces empty output — malformed/truncated JSON, a missing exception entry, or all frames skipped — emit `stack_trace unavailable (no exception entry)` and continue (exit 0). This maps the Failure Modes rows "malformed/truncated" and "missing exception entry" to this marker.

5. **Update the header comment (DB5).** Update the block of `#` comments at the top of `scripts/sentry-read.sh` to document:
   - the new `stack_trace=<N> frames` header line followed by `file:line in function` frame lines;
   - the best-effort `stack_trace unavailable (<reason>)` marker (event fetch failure never fails the run);
   - the fixed 30-frame cap;
   - the accepted bare numeric id;
   - correct the "single GET" claim to two GETs (metadata + latest event), noting the second is best-effort.
   Keep the existing `short_id, status, live_event_count, first_seen, last_seen, users_impacted, title` metadata description and the env contract description.

6. **CHANGELOG.** Add a `## Unreleased` section at the top of `CHANGELOG.md` (below the `# Changelog` preamble, never above it) per `/home/node/.claude/plugins/marketplaces/coding/docs/changelog-guide.md`, with one bullet per logical change, named specifically:
   - `- feat: ...` for the stack trace emission (script now fetches the latest event and emits up to 30 `file:line in function` frames, with a best-effort `stack_trace unavailable` marker)
   - `- fix: ...` for the bare numeric issue id acceptance
   If the section already exists, append to it.

7. **Self-check before finishing.** Re-run `<verification>` and confirm it passes. Then walk each acceptance criterion AC1–AC5 from the spec against the change: metadata block unchanged and first (AC1), frame emission format and header (AC2), bare-id and URL equivalence (AC3), best-effort failure marker with exit 0 and metadata intact (AC4), 30-frame cap and no PII/raw payload (AC5). Confirm your changes are limited to `scripts/sentry-read.sh` and `CHANGELOG.md` (plus `make precommit` auto-formatting if any).
</requirements>

<constraints>
- Single bash script at `scripts/sentry-read.sh`; `set -euo pipefail`; `curl` for GETs; `python3` for JSON parsing (both present in the image). No new languages or dependencies.
- Read-only by construction: only GET requests, no mutations, no shell metacharacters — the argument is validated to `[0-9]+` before any URL interpolation.
- Env contract unchanged: `SENTRY_API_TOKEN` (required, non-empty → usage exit otherwise), `SENTRY_ORG` (default `bborbe`), `SENTRY_URL` (default `https://bborbe.sentry.io`).
- The 7 metadata keys, their meaning, and their emission order are frozen: `short_id`, `status`, `live_event_count`, `first_seen`, `last_seen`, `users_impacted`, `title`. Metadata failure must still fail the run (existing behavior), so the agent's `needs_input` stop on script failure stays correct.
- Usage/id-extraction error keeps exit code 2; the `Bash(scripts/sentry-read.sh:*)` invocation contract is unchanged.
- Do NOT add a frame-count cap knob, an opt-out flag for the event fetch, or any retry/backoff logic — the cap is fixed at 30 and the fetch is always attempted.
- Do NOT change the Go collector/controller/agent code, the Config CRD, preflight, or `ALLOWED_TOOLS` — script-only. Do NOT update the agent planning/execution prompt text (a later step of the same vault task, out of scope here).
- Data privacy invariant: stack trace output is limited to `file:line in function`; context lines and raw payload bodies never appear in stdout.
- Do NOT commit — dark-factory handles git.
- Existing tests must still pass.
</constraints>

<verification>
Run `bash -n scripts/sentry-read.sh` — must exit 0.
Run `make precommit` — must exit 0 (the Go suite stays green; no Go code is changed).
</verification>
