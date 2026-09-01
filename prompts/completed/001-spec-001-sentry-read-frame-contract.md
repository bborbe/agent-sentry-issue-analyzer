---
status: completed
spec: [001-sentry-analyzer-repo-map-and-lineno-less-frames]
summary: sentry-read.sh now emits stack trace frames for lineno-less frames, adds a distinct no-frames marker, and neutralises control chars; mock-API test extended to AC1-AC6 with 222/333 fixtures and passes
execution_id: agent-sentry-issue-analyzer-e2e-exec-001-spec-001-sentry-read-frame-contract
dark-factory-version: dev
created: "2026-09-01T13:00:00Z"
queued: "2026-09-01T12:53:01Z"
started: "2026-09-01T12:53:03Z"
completed: "2026-09-01T12:55:06Z"
branch: dark-factory/sentry-analyzer-repo-map-and-lineno-less-frames
---

<summary>
- The Sentry reader no longer reports "no stack trace" when a stack trace actually exists but its frames lack line numbers
- Events whose exception entry carries at least one frame now always emit the frames: `file:line in function` when a line number is usable, otherwise `basename in function`
- Events whose exception entry exists but has zero frames now get a distinct "no frames" marker instead of the misleading "no exception entry" message
- "No exception entry" is now reserved for events that genuinely lack an exception entry
- Frame values are forced onto a single line so a crafted frame can no longer inject fake lines into the output block
- Existing behavior is untouched: metadata key order, the 30-frame cap, mixed-line-number frames, and the timeout/auth/fetch-failure markers all work as before
- The deterministic mock-API test gains a lineno-less fixture and an empty-frames fixture and now prints "PASS: all AC1-AC6 assertions passed"
- The changelog gains an "Unreleased" entry documenting the new frame-emission contract
</summary>

<objective>
Make `scripts/sentry-read.sh` report the latest event's stack trace truthfully: emit frames whenever an exception entry carries them (including when none have a usable line number), reserve `stack_trace unavailable (no exception entry)` for events that genuinely lack an exception entry, add a distinct `stack_trace unavailable (no frames)` marker, and keep every emitted frame value single-line — all locked by new deterministic mock-API fixtures in `scripts/test-sentry-read.sh`.
</objective>

<context>
Read CLAUDE.md for project conventions.

Read `scripts/sentry-read.sh` in full. The code to change is the python3 `-c` extraction block inside the `else` branch of the event-fetch classification: the block bound to `frames="$(python3 -c '...' "${event_body}")"`, followed by the `if [ "${frames_rc}" -ne 0 ] || [ -z "${frames}" ]` fallback. The `set +e` / `set -e` guard around it, the curl classification above it (timeout / 404 → `no events` / 401+403 → `auth` / other → `fetch failed`), and the 7-key metadata block must remain unchanged.

Read `scripts/test-sentry-read.sh` in full. The fixture server's `event_latest(self, issue)` method (the existing `elif issue == "999"` branch is the insertion anchor), the `fail()` helper, and the `check_failure` helper show the patterns to follow. The final line `echo "PASS: all AC1-AC5 assertions passed"` must become `AC1-AC6`.

Read `/home/node/.claude/plugins/marketplaces/coding/docs/changelog-guide.md` for the changelog entry format.
</context>

<requirements>
1. In `scripts/sentry-read.sh`, replace the entire python3 `-c` extraction block inside the `else` branch. Keep the `set +e` / `set -e` guard and the trailing shell fallback exactly as they are:

   ```bash
   if [ "${frames_rc}" -ne 0 ] || [ -z "${frames}" ]; then
     printf 'stack_trace unavailable (no exception entry)\n'
   else
     printf '%s\n' "${frames}"
   fi
   ```

   This fallback now fires only when python exits non-zero or produces empty output — a malformed/partial event body, which is pre-existing behavior (spec failure-mode "partial event body"; the reason enum is frozen to `timeout | no events | auth | fetch failed | no frames | no exception entry`, do NOT add a new reason string). <!-- REVIEW NOTE: on a 200 response whose body fails to parse (truncated download), the python block exits with empty output and the shell fallback emits `no exception entry` (pre-existing best-effort behavior; the spec freezes the reason enum to `timeout | no events | auth | fetch failed | no frames | no exception entry` and its Failure-Modes row for a partial body asks only for a best-effort marker). If you consider that marker still misleading for truncated bodies, veto this choice and require a dedicated fixture -- otherwise leave as-is. --> Replace only the python source inside the single-quoted heredoc with exactly this block — the three-state classification is load-bearing, do not paraphrase it. The fenced code blocks in this prompt are indented 3 spaces because they sit inside numbered-list items. When you copy a block into a script, strip that list indentation: the python here must start flush-left at column 0 (module-level statements must not be indented, or Python raises IndentationError).

   ```python
   import json, sys, os, re
   try:
       data = json.load(open(sys.argv[1]))
       exception_found = False
       frames = []
       for entry in data.get("entries", []):
           if entry.get("type") == "exception":
               exception_found = True
               values = entry.get("data", {}).get("values", [])
               if values:
                   frames = values[0].get("stacktrace", {}).get("frames", []) or []
               break
   except Exception:
       sys.exit(0)
   if not exception_found:
       print("stack_trace unavailable (no exception entry)")
       sys.exit(0)
   if not frames:
       print("stack_trace unavailable (no frames)")
       sys.exit(0)
   lineno_lines = []
   no_lineno_lines = []
   for frame in frames[:30]:
       path = (frame.get("abs_path") or frame.get("filename") or "").strip()
       func = (frame.get("function") or "unknown").strip()
       # single-line: neutralise control chars so a crafted payload cannot inject lines
       path = re.sub(r"[\x00-\x1f\x7f]+", " ", path).strip()
       func = re.sub(r"[\x00-\x1f\x7f]+", " ", func).strip()
       lineno = frame.get("lineno")
       if isinstance(lineno, int):
           lineno_lines.append("%s:%s in %s" % (os.path.basename(path), lineno, func))
       else:
           no_lineno_lines.append("%s in %s" % (os.path.basename(path), func))
   emitted = lineno_lines if lineno_lines else no_lineno_lines
   print("stack_trace=%d frames" % len(emitted))
   print("\n".join(emitted))
   ```

   The block classifies a successful (HTTP 200) fetch into exactly one output state:
   - An exception entry exists and its first value's `stacktrace.frames` has ≥1 element → print a `stack_trace=<N> frames` header, then one line per emitted frame (≤30, via `frames[:30]`): `basename:lineno in function` when `lineno` is an integer, otherwise `basename in function`. `N` is the number of emitted frame lines.
   - An exception entry exists but its first value's `stacktrace.frames` is empty or missing → print `stack_trace unavailable (no frames)`.
   - No exception entry → print `stack_trace unavailable (no exception entry)`.

   Behavior rules that must hold (spec Desired Behaviors 1–4 and Failure Modes):
   - Mixed frames (some with a usable integer `lineno`, some without): only the frames with a usable integer `lineno` are emitted as `file:line in function`; the others are dropped — current behavior unchanged, no new regression surface.
   - `lineno` present but a string (schema drift, e.g. `"42"`) is treated as lineno-less → emitted as `basename in function` — never a false `no exception entry`.
   - Single-line guarantee: C0 control characters (`\x00`–`\x1f`) and DEL (`\x7f`), including `\n`/`\r`, are replaced with a single space in both the basename and function before the line is built, so a crafted `function`/`filename` cannot inject extra lines into the output block (Security failure mode).
   - No-PII: only `abs_path`/`filename` basename and `function` are emitted — never `context`, never the raw payload.
   - The `except Exception: sys.exit(0)` parse-failure path is unchanged; the spec's reason enum stays exactly `timeout | no events | auth | fetch failed | no frames | no exception entry`.

2. Update the comment blocks in `scripts/sentry-read.sh` that describe the output contract:
   - The header comment at the top (the lines listing the emitted block and the `stack_trace unavailable (<reason>)` reasons) must document: the two frame line shapes (`<file>:<line> in <function>` and `<basename> in <function>`), the reason enum `timeout | no events | auth | fetch failed | no frames | no exception entry`, the three-state classification (frames are emitted whenever an exception entry carries ≥1 frame; `no frames` is emitted only when an exception entry exists with zero frames; `no exception entry` only when no exception entry exists), and the single-line guarantee.
   - The inline comment above the python block in the `else` branch (currently "HTTP 200: extract only filename/abs_path basename...") must mention the three-state classification.

3. In `scripts/test-sentry-read.sh`:
   a. Add two fixtures to the fixture server's `event_latest(self, issue)` method, directly after the existing `elif issue == "999"` branch (the `metadata()` method already serves any issue id, so no metadata change is needed). The following block is indented to sit inside the numbered list — copy it into `event_latest(self, issue)` with `elif` branches at 8 spaces and their bodies at 12 spaces, exactly matching the existing `elif issue == "999"` branch:

   ```python
   elif issue == "222":
       # Lineno-less frames (real nuke shape: issue 6727724202). None has an
       # integer lineno; frame 4 carries a string lineno ("42") to lock the
       # schema-drift path; frame 5 carries a newline+equals in `function`
       # to lock single-line stripping.
       self._send(200, {"entries": [
           {"type": "exception", "data": {"values": [
               {"stacktrace": {"frames": [
                   {"filename": "kafka/coordinator/consumer.py", "abs_path": None, "lineno": None, "function": "_maybe_auto_commit_offsets_sync", "context": ["RAW-CONTEXT-SENTINEL line 1"]},
                   {"filename": "kafka/protocol/fetch.py", "abs_path": None, "lineno": None, "function": "FetchRequest", "context": ["RAW-CONTEXT-SENTINEL line 2"]},
                   {"filename": "kafka/consumer/group.py", "abs_path": None, "lineno": None, "function": "_poll_once", "context": ["RAW-CONTEXT-SENTINEL line 3"]},
                   {"filename": "nuke/worker.py", "abs_path": None, "lineno": "42", "function": "sync_worker", "context": ["RAW-CONTEXT-SENTINEL line 4"]},
                   {"filename": "nuke/backlog.py", "abs_path": None, "lineno": None, "function": "dispatch\nCOUNT=999", "context": ["RAW-CONTEXT-SENTINEL line 5"]},
               ]}}
           ]}}
       ]})
   elif issue == "333":
       # Exception entry whose first value's stacktrace has zero frames.
       self._send(200, {"entries": [
           {"type": "exception", "data": {"values": [
               {"stacktrace": {"frames": []}}
           ]}}
       ]})
   ```

   b. After the existing AC4 failure-path assertions (the `check_failure "AC4 auth (401)" ...` block) and before the final `echo "PASS: ..."` line, add these assertions (they lock AC2 + the schema-drift and single-line failure modes for issue 222, and AC6 for issue 333; `check_failure` already asserts the 7 metadata keys and exactly one marker). The following block is indented to sit inside the numbered list — copy it into the test script using the file's 2-space indentation convention (top-level statements at 2 spaces, nested bodies at 4):

   ```bash
   # ---- AC2: lineno-less frames emitted truthfully (issue 222) ----
   if ! out_nolineno="$(bash "${SCRIPT}" 222 2>&1)"; then
     fail "AC2 lineno-less fixture exited non-zero"
   fi
   key_count="$(printf '%s\n' "${out_nolineno}" | grep -cE '^(short_id|status|live_event_count|first_seen|last_seen|users_impacted|title)=' || true)"
   if [ "${key_count}" -ne 7 ]; then
     fail "AC2 lineno-less fixture expected 7 metadata keys, got ${key_count}"
   fi
   nl_header="$(printf '%s\n' "${out_nolineno}" | grep -cE '^stack_trace=[1-9][0-9]* frames$' || true)"
   if [ "${nl_header}" -ne 1 ]; then
     fail "AC2 lineno-less fixture expected exactly one 'stack_trace=<N> frames' header, got ${nl_header}"
   fi
   if ! printf '%s\n' "${out_nolineno}" | grep -q '^stack_trace=5 frames$'; then
     fail "AC2 lineno-less fixture expected 'stack_trace=5 frames' header"
   fi
   nl_frames="$(printf '%s\n' "${out_nolineno}" | grep -cE '^[^=]+ in ' || true)"
   if [ "${nl_frames}" -ne 5 ]; then
     fail "AC2 lineno-less fixture expected 5 'basename in function' frame lines, got ${nl_frames}"
   fi
   if ! printf '%s\n' "${out_nolineno}" | grep -q '^consumer.py in _maybe_auto_commit_offsets_sync$'; then
     fail "AC2 lineno-less frame format mismatch (expected 'consumer.py in _maybe_auto_commit_offsets_sync')"
   fi
   if ! printf '%s\n' "${out_nolineno}" | grep -q '^worker.py in sync_worker$'; then
     fail "AC2 string-lineno frame not treated as lineno-less (expected 'worker.py in sync_worker')"
   fi
   if printf '%s\n' "${out_nolineno}" | grep -qE ':[0-9]+ in '; then
     fail "AC2 lineno-less fixture leaked a ':line' frame"
   fi
   if printf '%s\n' "${out_nolineno}" | grep -q 'no exception entry'; then
     fail "AC2 lineno-less fixture emitted 'no exception entry' despite frames existing"
   fi
   if printf '%s\n' "${out_nolineno}" | grep -q 'no frames'; then
     fail "AC2 lineno-less fixture emitted 'no frames' despite frames existing"
   fi
   if printf '%s\n' "${out_nolineno}" | grep -qE '"context"|"frames"|stacktrace'; then
     fail "AC4 raw JSON keys leaked from lineno-less fixture"
   fi
   if printf '%s\n' "${out_nolineno}" | grep -q 'RAW-CONTEXT-SENTINEL'; then
     fail "AC4 fixture context sentinel leaked from lineno-less fixture"
   fi
   if ! printf '%s\n' "${out_nolineno}" | grep -q '^backlog.py in dispatch COUNT=999$'; then
     fail "AC4 newline in frame function was not neutralised to a single line"
   fi
   if printf '%s\n' "${out_nolineno}" | grep -q '^COUNT=999$'; then
     fail "AC4 newline in frame function injected a standalone line"
   fi

   # ---- AC6: no-frames marker distinct from no-exception-entry (issue 333) ----
   if ! out_noframes="$(bash "${SCRIPT}" 333 2>&1)"; then
     fail "AC6 no-frames stub exited non-zero"
   fi
   check_failure "AC6 no frames" "${out_noframes}" "no frames"
   if printf '%s\n' "${out_noframes}" | grep -q 'no exception entry'; then
     fail "AC6 no-frames stub emitted 'no exception entry' instead of 'no frames'"
   fi
   ```

   c. Change the final line to `echo "PASS: all AC1-AC6 assertions passed"`.
   d. Update the test file's header comment so its spec reference points at `specs/in-progress/001-sentry-analyzer-repo-map-and-lineno-less-frames.md` and its AC summary mentions the new lineno-less (AC2) and empty-frames (AC6) fixtures. Do NOT weaken any existing assertion (metadata order, 30-frame cap on issue-123, failure stubs 456/789/999).

4. Add a fresh `## Unreleased` section at the very top of `CHANGELOG.md` (above the current `## v0.8.0` section; if a `## Unreleased` section already exists, append to it rather than duplicating) with one `fix:` entry describing the frame-emission contract change: `scripts/sentry-read.sh` now emits frames whenever an exception entry carries them — including when none have a usable line number (as `basename in function`), emits the distinct `stack_trace unavailable (no frames)` marker for an exception entry with zero frames, reserves `stack_trace unavailable (no exception entry)` for events with no exception entry, and neutralises control characters in emitted frame values so every frame line stays single-line. Follow the format in `/home/node/.claude/plugins/marketplaces/coding/docs/changelog-guide.md`.

5. Before finishing, re-run `<verification>` and confirm it passes; walk acceptance criteria AC2, AC3, AC4, AC6 and the bash-test part of AC5 against your change.
</requirements>

<constraints>
- Do NOT commit — dark-factory handles git.
- The metadata block contract is frozen: exactly the 7 key=value lines (`short_id`, `status`, `live_event_count`, `first_seen`, `last_seen`, `users_impacted`, `title`) in their current order; a metadata fetch failure still exits non-zero.
- The event fetch stays best-effort and non-fatal: it never fails the run and degrades to exactly one `stack_trace unavailable (<reason>)` line; the reason enum is exactly `timeout | no events | auth | fetch failed | no frames | no exception entry` — do NOT add new reason strings.
- No-PII is a hard invariant for both frame shapes: basename only, no context lines, no raw payload, capped 30 frames; every emitted frame value is single-line (control characters neutralised).
- Mixed-case frames (some with a usable integer `lineno`, some without) keep current behavior: only the frames with a usable `lineno` are emitted as `file:line in function` — no new regression surface.
- `scripts/repo-clone.sh`, the execution / deep-execution prompts, the verdict logic, and the `<output-format>` contract are untouched by this prompt.
- The existing `scripts/test-sentry-read.sh` assertions for metadata order, the 30-frame cap on the issue-123 fixture, and the failure stubs (456 no-exception / 789 404 / 999 401) must still pass without weakening.
- Do NOT create a new scenario file — the mock-API test plus the operator live probe cover the behavior (spec non-goal).
- This prompt changes no Go code — do not edit any `.go` file.
- Existing tests must still pass.
</constraints>

<verification>
This prompt changes no Go code, so `make precommit` is NOT required. Verify with:

- `bash scripts/test-sentry-read.sh` — must exit 0 and print `PASS: all AC1-AC6 assertions passed` (covers AC2/AC3/AC4/AC6 and preserves the existing AC1-AC5 assertions)
- `grep '## Unreleased' CHANGELOG.md` — must match the new section
</verification>
