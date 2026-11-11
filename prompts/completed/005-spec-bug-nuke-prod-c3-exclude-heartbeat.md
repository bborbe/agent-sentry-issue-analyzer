---
status: completed
spec: [bug-nuke-prod-c3]
summary: Excluded the exact-title sentry-heartbeat liveness issue during Sentry page compaction, documented fetched semantics, added a fixture arm proving only the normal alert is published, and recorded the fix under CHANGELOG Unreleased.
execution_id: d0bc353a-f019-50be-ae21-8cf42b7cf187-exec-005-spec-bug-nuke-prod-c3-exclude-heartbeat
dark-factory-version: dev
created: "2026-11-11T22:16:38Z"
queued: "2026-11-11T22:24:28Z"
started: "2026-11-11T22:24:29Z"
completed: "2026-11-11T22:35:37Z"
---

# Stop the collector from creating an analyzer task for the sentry-heartbeat liveness issue

<summary>
- The daily collector poll no longer turns the `sentry-heartbeat` liveness probe into an analyzer task.
- The heartbeat is excluded by its exact issue title, so the exclusion cannot swallow a real alert whose title merely mentions the phrase.
- Only the analysable alerts are fetched, counted and published; the machine-readable `fetched` number now means the set the pipeline can actually act on.
- A normal unresolved alert in the same Sentry response is still fetched and still published.
- A new fixture arm feeds one normal alert and one heartbeat through the real script and asserts that only the normal alert reaches the publish step.
- The result-line format and every status rule are unchanged; no Go code, flag, environment variable or Sentry query changes.
- The changelog records the fix under an unreleased heading.
</summary>

<objective>
Exclude the `bborbe/sentry-watcher` `sentry-heartbeat` liveness issue from `scripts/sentry-create-tasks.sh` before it is counted or published, and pin that behaviour with a fixture test that runs the real script against the mock Sentry/vault server. After this change a quiet day whose only unresolved issue is the heartbeat publishes nothing and reports `status=done` instead of filing a synthetic analyzer task every day.
</objective>

<context>
Read the approved spec first: `specs/in-progress/bug-nuke-prod-c3.md`.

Read before editing:
- `scripts/sentry-create-tasks.sh` — the whole file, especially:
  - the header comment block that documents the two reported numbers (`fetched`, `landed`) and the result line (around lines 48-70);
  - the defaults block where `CREATE_TASKS_BIN` is defined (line 88);
  - the paginated Sentry fetch whose `base_url` is `.../issues/?query=is:unresolved&limit=100` (line 111) — do not change this URL;
  - the `python3 -c` compaction block that reads `sys.argv[1]` (the pages file), builds `compact`, and writes `${tmp_file}` (lines 139-159);
  - the `count` / `short_ids` derivations from `${tmp_file}` (lines 161-164).
- `scripts/test-sentry-create-tasks.sh` — the whole file, especially:
  - the shared `/create-tasks` stub created at `cat > "${WORK}/create-tasks" <<STUB` (around lines 133-142) and its `chmod +x`;
  - `ALERT_JSON` / `TRACKED_JSON` fixtures (around lines 145-147);
  - `run_case`, `expect`, `refuse` (around lines 149-218);
  - the existing arms, in particular arm 7 `typed-populated` and arm 9 `retention-refused` (around lines 273-329), so the new arm matches their style and is inserted in a place where the healthy stub is still installed.
- `CHANGELOG.md` — there is currently no `## Unreleased` section; the newest heading is `## v0.25.5` at the top. The project requires an entry under `## Unreleased` (`docs/dod.md`).
- `Makefile.precommit` — `bash scripts/test-sentry-create-tasks.sh` is part of `make test` (line 43). The fixture test is the acceptance gate for this change.

Background, verified against the emitter and consumer source:
- `bborbe/sentry-watcher` `pkg/heartbeat_sender.go` builds the probe event with `level: "error"`, `logger: "sentry-heartbeat"`, `message: "sentry-heartbeat"`, and `tags: {"source": "sentry-heartbeat"}`. Because it is a level:error event, Sentry files it as an unresolved issue, and Sentry derives the issue title from the event message — so the issue title is exactly `sentry-heartbeat`.
- `bborbe/sentry-alert-consumer` v0.2.0 skips the heartbeat with an exact match on the event logger (`env.Event.Logger == "sentry-heartbeat"`), never a substring or case-insensitive match. Mirror that narrowness.
- The script must not rely on Sentry search syntax. `bborbe/sentry-watcher`'s own heartbeat checker deliberately enforces its bounds in Go rather than in the search expression because "no Sentry search-syntax assumption is load-bearing". A post-fetch filter is also what makes the fixture test exercise the real path (the mock server ignores the query string).

Relevant coding plugin doc (read in the YOLO container at this path):
- `/home/node/.claude/plugins/marketplaces/coding/docs/changelog-guide.md`

<!-- OPEN QUESTION (non-interactive generation): the emitter source proves the event's message, logger and source tag are all `sentry-heartbeat`, and Sentry titles a message event with its message, so the exact title is expected to be `sentry-heartbeat`. If the live org renders the issue title differently, change the constant to the observed EXACT title — do not widen the match to a substring or case-insensitive comparison, because that is how a real alert would be silently dropped. -->
</context>

<requirements>
1. In `scripts/sentry-create-tasks.sh`, add a documented constant immediately after the `CREATE_TASKS_BIN="${CREATE_TASKS_BIN:-/create-tasks}"` line (line 88) and before the comment block that begins "The per-alert tasks are listed through the typed vault API". Use exactly:

```bash
# The sentry-heartbeat liveness probe (bborbe/sentry-watcher pkg.HeartbeatTag)
# emits a deliberate level:error event whose message, logger and source tag are
# all "sentry-heartbeat". Sentry files it as an unresolved issue whose title is
# that message, so an is:unresolved poll would otherwise create an analyzer task
# for it every day — a synthetic issue that can never be a real bug. Excluded by
# EXACT title, mirroring sentry-alert-consumer v0.2.0's exact-match heartbeat
# skip: never a substring or case-insensitive match, so a real alert whose title
# merely contains the phrase is still analysed. This is a fixed, narrow
# exclusion, not a configuration knob.
HEARTBEAT_TITLE="sentry-heartbeat"
```

2. In the `python3 -c` compaction block in `scripts/sentry-create-tasks.sh` (the one invoked as `python3 -c '...' "${pages_file}" > "${tmp_file}"`, around lines 139-159), exclude issues whose title equals the heartbeat title. Pass the constant as a second argv argument (the block is single-quoted, so it cannot interpolate the shell variable). Change the invocation to end with `' "${pages_file}" "${HEARTBEAT_TITLE}" > "${tmp_file}"` and change the Python to:

```python
import json, sys
heartbeat_title = sys.argv[2]
issues = []
for line in open(sys.argv[1]):
    line = line.strip()
    if line:
        issues.extend(json.loads(line))
compact = [{
    "id": issue["id"],
    "shortId": issue["shortId"],
    "title": issue["title"],
    "lastSeen": issue["lastSeen"],
    "firstSeen": issue["firstSeen"],
    "count": issue["count"],
    "status": issue["status"],
    "userCount": issue["userCount"],
    "permalink": issue["permalink"],
    "project": issue["project"]["slug"],
} for issue in issues if issue.get("title") != heartbeat_title]
json.dump(compact, sys.stdout)
```

Keep every projected key and the `project` → `project["slug"]` mapping exactly as they are. Use `issue.get("title")` so a payload without a title is kept rather than raising. Do NOT change the Sentry `base_url` query string and do NOT add a `!source:sentry-heartbeat` search term.

3. Update the header comment block in `scripts/sentry-create-tasks.sh` so the documented meaning of `fetched` is honest. In the "The script reports TWO numbers" block (around lines 48-54), change the `fetched` line from `fetched  — active unresolved alerts returned by Sentry` to state that it is the active unresolved alerts returned by Sentry **after excluding the sentry-heartbeat liveness issue**, i.e. the analysable alerts. Add one short sentence (for example: `The sentry-heartbeat liveness probe is dropped by exact title before this count, so it never becomes a task.`) immediately below the two-line list. Do not change the result-line field order or any other field's meaning.

4. In `scripts/test-sentry-create-tasks.sh`, extend the shared `/create-tasks` stub (the `cat > "${WORK}/create-tasks" <<STUB` block around lines 133-142) so each run records the alerts file it was handed. Keep the existing `create-args` recording and the `create-tasks-result: published=1` output, and add, before the `printf '%s\n' "\$@" > "${WORK}/create-args"` line:

```bash
prev=""
for arg in "\$@"; do
  if [ "\${prev}" = "--alerts-file" ]; then
    cp "\${arg}" "${WORK}/alerts-seen"
  fi
  prev="\${arg}"
done
```

The heredoc is unquoted, so `${WORK}` must expand at stub-creation time while `\$@`, `\${prev}` and `\${arg}` stay literal for stub-run time. Do not change the stub's stdout line.

5. In `scripts/test-sentry-create-tasks.sh`, add a fixture for the heartbeat next to `ALERT_JSON` and `TRACKED_JSON` (around lines 145-147), and build the combined two-alert array with a real JSON merge. Do NOT use shell array syntax such as `${ALERT_JSON}[0]` — `ALERT_JSON` and `HEARTBEAT_JSON` are strings, not bash arrays, and that expression would emit invalid JSON.

```bash
HEARTBEAT_JSON='[{"id":"2","shortId":"NUKE-PROD-HB","title":"sentry-heartbeat","lastSeen":"2026-11-04T10:00:00Z","firstSeen":"2026-11-04T09:00:00Z","count":1,"status":"unresolved","userCount":0,"permalink":"https://example.invalid/2","project":{"slug":"nuke-prod"}}]'
SENTRY_WITH_HEARTBEAT="$(python3 -c 'import json, sys; print(json.dumps(json.loads(sys.argv[1]) + json.loads(sys.argv[2])))' "${ALERT_JSON}" "${HEARTBEAT_JSON}")"
```

6. In `scripts/test-sentry-create-tasks.sh`, insert a new arm immediately before the existing `# --- arm 9: a withheld write is its own failure, never a quiet skip ---------` comment (line 302), so it runs while the healthy stub from requirement 4 is still installed and before the `retention-refused` arm overwrites `${WORK}/create-tasks`. Give it a comment in the file's style, for example:

```bash
# --- arm: the sentry-heartbeat liveness issue is never published -------------
# The 6-hourly heartbeat is a deliberate level:error event, so Sentry files it
# as an unresolved issue titled "sentry-heartbeat". The poll must drop it by
# exact title and keep publishing the real alert beside it — a substring or
# case-insensitive match here would silently drop real alerts.
rm -f "${WORK}/alerts-seen"
result="$(run_case "heartbeat-excluded" \
	"{\"sentry\": ${SENTRY_WITH_HEARTBEAT}, \"typed\": ${TRACKED_JSON}}")"
printf '%s\n' "${result}" > "${WORK}/r"
expect "heartbeat-excluded" "${WORK}/r" "fetched=1"
expect "heartbeat-excluded" "${WORK}/r" "published=1"
expect "heartbeat-excluded" "${WORK}/r" "expected_new=0"
expect "heartbeat-excluded" "${WORK}/r" "status=done"
expect "heartbeat-excluded" "${WORK}/alerts-seen" "OCTOPUS-DEV-1"
refuse "heartbeat-excluded" "${WORK}/alerts-seen" "NUKE-PROD-HB"
refuse "heartbeat-excluded" "${WORK}/out" "NUKE-PROD-HB"
```

With `typed` populated by `TRACKED_JSON`, the normal alert is already tracked, so `expected_new=0` and the landing clause cannot fire — the arm asserts exclusion, not landing. If the heartbeat were still in the compacted alert set, `fetched` would be 2 and `NUKE-PROD-HB` would appear in both the script's stdout and the alerts file handed to `/create-tasks`.

7. Add a `## Unreleased` section at the very top of `CHANGELOG.md`, directly above `## v0.25.5`, and put this bullet under it (one blank line between the heading and the bullet, and between the bullet and `## v0.25.5`):

```markdown
- fix: exclude the `sentry-heartbeat` liveness issue from the collector's `is:unresolved` poll. `bborbe/sentry-watcher`'s 6-hourly heartbeat is a deliberate `level:error` event, so Sentry files it as an unresolved issue titled `sentry-heartbeat` and `scripts/sentry-create-tasks.sh` created an analyzer task for it every day — a synthetic issue that can never be a bug. The script now drops issues whose title is exactly `sentry-heartbeat` while compacting the fetched pages, mirroring `sentry-alert-consumer` v0.2.0's exact-match heartbeat skip, so a real alert whose title merely contains the phrase is still analysed. `fetched` now counts only the analysable alerts. A fixture test runs one normal alert and one heartbeat through the real script and asserts that only the normal alert reaches `/create-tasks`.
```

8. Self-check before finishing: re-run every command in `<verification>` and confirm each passes; then walk the summary bullets against the diff — heartbeat dropped by exact title, normal alert still published, `fetched` semantics documented, result-line format unchanged, one unreleased changelog heading holding the new bullet.
</requirements>

<constraints>
- Do NOT commit — dark-factory handles git.
- Existing tests must still pass.
- Only `scripts/sentry-create-tasks.sh`, `scripts/test-sentry-create-tasks.sh` and `CHANGELOG.md` change. Do not edit `cmd/create-tasks/`, `pkg/steps/`, `pkg/prompts/`, README or docs.
- Do not change the machine-readable result-line format or field order: `sentry-create-tasks-result: fetched=<N> published=<n> expected_new=<k> landed=<m> observed=<true|false> status=<done|failed|unobserved> refused=<r>`. `pkg/steps/collector.go` parses it.
- Do not add an environment variable, flag or configuration knob for the heartbeat title; the exclusion is a fixed, narrow constant.
- Match the heartbeat by exact title equality only — no substring, no prefix, no lowercasing, no regex.
- Do not change the Sentry request query. The exclusion is post-fetch so the fixture test exercises the real compaction path.
- Do not change any status rule in `scripts/sentry-create-tasks.sh`; a run whose only unresolved issue is the heartbeat must fall through to `status=done` with `fetched=0`, `published=0`, `expected_new=0`.
- Keep the test's `expect` / `refuse` helper style and the existing arm ordering. Do not delete or weaken any existing arm.
- Do not run `go mod vendor`, and do not change `go.mod` / `go.sum`.
</constraints>

<verification>
Run from the repo root:

```bash
set -e
set -o pipefail

# The script is syntactically valid.
bash -n scripts/sentry-create-tasks.sh

# The exclusion constant and the argv plumbing exist.
grep -q 'HEARTBEAT_TITLE="sentry-heartbeat"' scripts/sentry-create-tasks.sh
grep -q 'heartbeat_title = sys.argv\[2\]' scripts/sentry-create-tasks.sh
grep -q 'if issue.get("title") != heartbeat_title' scripts/sentry-create-tasks.sh

# The fixture test carries the heartbeat arm and its fixture.
grep -q 'NUKE-PROD-HB' scripts/test-sentry-create-tasks.sh
grep -q 'heartbeat-excluded' scripts/test-sentry-create-tasks.sh

# The full fixture suite (all existing arms plus the new one) passes.
bash scripts/test-sentry-create-tasks.sh

# Exactly one Unreleased heading, holding the new bullet.
test "$(grep -c '^## Unreleased' CHANGELOG.md)" = "1"
awk '/^## /{sec=$0} /exclude the `sentry-heartbeat` liveness issue/{print sec}' CHANGELOG.md | grep -qx '## Unreleased'
```
</verification>
