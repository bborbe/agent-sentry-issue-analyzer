---
status: approved
spec: [001-sentry-read-event-payload]
created: "2026-09-01T12:00:00Z"
queued: "2026-08-31T22:39:05Z"
branch: dark-factory/sentry-read-event-payload
---

# Add a deterministic mock-API test for the Sentry read script

<summary>
- Adds a self-contained bash test that drives the read script against a local fixture HTTP server
- The fixture server is a python3 http.server bound only to 127.0.0.1 on an ephemeral port, so no external network, Docker socket, or real credentials are involved — it is container-executable
- The test asserts the full AC1–AC5 output contract: the 7 metadata keys in their frozen order, the `file:line in function` frame format, the fixed 30-frame cap, and the no-raw-payload / no-PII invariant
- Bare numeric id and full-URL invocations are shown to produce byte-identical output
- Failure stubs (no events, no exception entry, auth denied) each produce exactly one `stack_trace unavailable` marker while the metadata block stays intact
- The harness exits non-zero with a named FAIL message on any assertion miss, so it is safe to run in CI
</summary>

<objective>
Create a deterministic, container-executable test harness that locks the Sentry read script's output contract (acceptance criteria AC1–AC5) without any live Sentry access, so the frame cap and the no-PII invariant are verifiable now and guard against regressions later.
</objective>

<context>
Read CLAUDE.md for project conventions. There is no project-level CLAUDE.md at the repo root.

Read these files before making changes:

- `scripts/sentry-read.sh` — the script under test. This prompt depends on the preceding prompt having extended it to emit `stack_trace=<N> frames` / `file:line in function` lines and `stack_trace unavailable (<reason>)` markers. If the script does NOT yet contain an `events/latest` fetch, STOP and report that this prompt is out of order.
- `scripts/sentry-create-tasks.sh` — the repo's established style for `mktemp` + `trap 'rm -f ...' EXIT` cleanup and `python3 -c '...'` JSON handling; mirror it.

The assertion contract is defined in `specs/in-progress/001-sentry-read-event-payload.md` — Acceptance Criteria AC1–AC5 and Verification § Container-executable name the exact greps below.

Container-executability note: `python3`, `curl`, and `bash` are all installed in the image (see Dockerfile `RUN apk --no-cache add ... bash ... curl python3 ...`). The fixture server binds only `127.0.0.1` on an ephemeral port and the script under test is pointed at `SENTRY_URL=http://127.0.0.1:<port>`, so the test never touches the external network and needs no Docker or cluster tooling.
</context>

<requirements>
1. **Create the test harness.** Create `scripts/test-sentry-read.sh`, executable (`chmod +x` to match the other `scripts/*.sh`), with `set -euo pipefail`. It must be self-contained and cwd-independent (resolve the script under test from its own directory, e.g. `SCRIPT="$(cd "$(dirname "$0")" && pwd)/sentry-read.sh"`). The harness:
   - embeds the fixture server (requirement 2) via a quoted heredoc into a `$(mktemp)` file, then runs it with `python3 "${server_py}"` in the background;
   - captures the ephemeral port from the server's first stdout line (the server prints it once bound) by polling the server log with a bounded loop (e.g. 50 × 0.1 s sleep), failing loudly with the server log if it never appears;
   - cleans up with a `trap ... EXIT` that kills the server process and removes the temp files;
   - sets `SENTRY_API_TOKEN=x`, `SENTRY_ORG=test`, `SENTRY_URL="http://127.0.0.1:${PORT}"` for every invocation of the script under test (the `x` is a dummy token — the script only requires it non-empty);
   - exits 0 only if every assertion passes, and exits non-zero with a `FAIL: <named assertion>` message (on stderr) on the first miss.

2. **Fixture server (embedded python3).** The server routes on the request path and must serve these exact fixtures over HTTP GET on an ephemeral port bound to `127.0.0.1` (bind port 0 and print `server.server_address[1]` on stdout with `flush=True`, then `serve_forever()`):
   - Issue `123`: metadata JSON + `/events/latest/` returns an `exception` entry whose first `values[0].stacktrace.frames` is a list of exactly 40 frames. Every frame has `abs_path` (e.g. `/usr/src/app/pkg/foo/bar1.go`), `filename` (e.g. `pkg/foo/bar1.go`), a distinct numeric `lineno` (e.g. `40 + i`), a distinct `function` (e.g. `Func1`), and a `context` array whose content includes a distinctive sentinel string that never appears in the script's output, e.g. `["RAW-CONTEXT-SENTINEL line %d" % i]`. This fixture drives the 30-frame cap and the no-PII checks.
   - Issue `456`: metadata JSON + `/events/latest/` returns an event with NO `exception` entry (e.g. a `message` entry only).
   - Issue `789`: metadata JSON + `/events/latest/` returns HTTP 404 (no events).
   - Issue `999`: metadata JSON + `/events/latest/` returns HTTP 401 (auth).
   - The metadata fixture has all 7 keys with distinctive values: `shortId` (e.g. `TEST-123`), `status`, `count`, `firstSeen`, `lastSeen`, `userCount`, `title`. Any other path returns 404.
   - Server must set `Content-Type: application/json` and `Content-Length`, and override `log_message` to stay quiet.

3. **Happy-path assertions (AC1, AC2, AC3, AC5).** Run the script under test twice and capture stdout+stderr:
   - `out_bare="$(bash "${SCRIPT}" 123 2>&1)"`
   - `out_url="$(bash "${SCRIPT}" "http://127.0.0.1:${PORT}/api/0/organizations/test/issues/123/" 2>&1)"`
   Assert:
   - **AC3**: `out_bare` and `out_url` are byte-identical; neither contains `could not extract` (and the bare-id run necessarily exited 0, which `set -e` enforces).
   - **AC1**: exactly the 7 metadata keys appear, in the frozen order `short_id, status, live_event_count, first_seen, last_seen, users_impacted, title` — e.g. `[ "$(printf '%s\n' "$out_bare" | grep -cE '^(short_id|status|live_event_count|first_seen|last_seen|users_impacted|title)=')" -eq 7 ]`, plus an order check that extracts key names in line order (e.g. `grep -oE '^(short_id|status|live_event_count|first_seen|last_seen|users_impacted|title)=' | tr -d '=' | paste -sd, -`) and compares against `short_id,status,live_event_count,first_seen,last_seen,users_impacted,title`.
   - **AC2**: exactly one `stack_trace=<N> frames` header line with N ≥ 1 (e.g. `grep -cE '^stack_trace=[1-9][0-9]* frames$' == 1`), and at least one frame line matching `:[0-9]+ in `.
   - **AC5 cap**: exactly 30 frame lines — `[ "$(printf '%s\n' "$out_bare" | grep -cE '^[^=]+:[0-9]+ in ')" -eq 30 ]` — and the 31st+ frame files are absent (e.g. `grep -q 'bar31.go'` must fail).
   - **AC5 no-PII**: no quoted JSON keys on stdout — `grep -qE '"context"|"frames"|stacktrace'` must fail — and the sentinel from the fixture's `context` arrays (e.g. `RAW-CONTEXT-SENTINEL`) must not appear in stdout.
   - **Frame format**: a concrete frame line matches exactly (e.g. `grep -q '^bar1.go:41 in Func1$'`).

4. **Failure-path assertions (AC4).** Run the script under test against the failure stubs and assert each: exit 0 (enforced by `set -e`), the 7-key metadata block intact, and exactly one `stack_trace unavailable (<reason>)` line with the correct reason:
   - `out_404="$(bash "${SCRIPT}" 789 2>&1)"` → `^stack_trace unavailable (no events)$`
   - `out_noexc="$(bash "${SCRIPT}" 456 2>&1)"` → `^stack_trace unavailable (no exception entry)$`
   - `out_auth="$(bash "${SCRIPT}" 999 2>&1)"` → `^stack_trace unavailable (auth)$`
   For each, assert the metadata key count is 7 and the `stack_trace unavailable` count is exactly 1.

5. **Self-check before finishing.** Re-run `<verification>` and confirm it passes. Confirm your changes are limited to `scripts/test-sentry-read.sh` (do not modify `scripts/sentry-read.sh` or any Go file). Do NOT add a CHANGELOG entry — the feature entry was added by the preceding prompt and test infrastructure is not user-facing.
</requirements>

<constraints>
- Test-only change: do NOT modify `scripts/sentry-read.sh`, any Go code, the Config CRD, preflight, or `ALLOWED_TOOLS`.
- The test must be container-executable: python3 + curl only, fixture server on `127.0.0.1` ephemeral port, `SENTRY_URL` pointing at localhost, dummy `SENTRY_API_TOKEN=x`. No Docker socket, no external network, no real credentials.
- The assertions must encode the spec's frozen contract: 7 metadata keys in order, `file:line in function` format, 30-frame cap fixed at 30 (no cap knob), and the no-PII invariant (context lines and raw JSON never on stdout).
- The dummy token value must remain clearly non-secret (a single short placeholder like `x`) so no secret-scanner false positive.
- Do NOT commit — dark-factory handles git.
- Existing tests must still pass.
</constraints>

<verification>
Run `bash -n scripts/test-sentry-read.sh` — must exit 0.
Run `bash scripts/test-sentry-read.sh` — must pass all assertions and exit 0.
Run `make precommit` — must exit 0 (Go suite stays green; no Go code is changed).
</verification>
