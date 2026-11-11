#!/usr/bin/env bash
# Constrained read-only Sentry fetch + per-alert task publish — the watcher step
# of the Sentry pipeline.
#
# Fetches the day's active unresolved Sentry alerts and publishes one
# CreateTaskCommand per (short-id, date) via /create-tasks so the
# sentry-issue-analyzer triage agent can classify each alert.
#
# Usage: sentry-create-tasks.sh
#
# Env: SENTRY_API_TOKEN (required, Bearer token), KAFKA_BROKERS (required),
#      SENTRY_URL (default https://bborbe.sentry.io), SENTRY_ORG (default bborbe),
#      TOPIC_PREFIX (default empty), TARGET_VAULT (default personal),
#      STAGE (default dev), VAULT_API_URL (required — typed vault-API listing),
#      GIT_REST_URL (optional — with VAULT_API_URL, enables verdict retention:
#      the prior "## Verdict"/"## Fix Result" of an already-judged issue is
#      spliced back into the fresh body and persisted to verdicts/<short-id>.md,
#      so a re-dispatch no longer discards it),
#      GATEWAY_SECRET (optional — gateway auth, presented to the vault API),
#      VAULT_POLL_ATTEMPTS (default 12), VAULT_POLL_INTERVAL_SECONDS (default 5),
#      CREATE_TASKS_BIN (TEST-ONLY, default /create-tasks — see below),
#      CREATE_TASKS_KAFKA_BROKERS (TEST-ONLY, default KAFKA_BROKERS — see below)
#
# CREATE_TASKS_KAFKA_BROKERS overrides the brokers passed to /create-tasks ONLY,
# leaving the agent's own producer on KAFKA_BROKERS. It exists to make a publish
# failure constructible: pointing it at an unreachable address fails the send
# while the agent stays up. Setting KAFKA_BROKERS itself cannot do this — the
# agent reads that at startup and dies with `create sync producer failed` before
# this script is ever reached (recorded 2026-09-12, Job Failed, pod_crash_no_stdout).
#
# It does NOT arrive on its own. main.go's buildClaudeEnv forwards an explicit
# list (KAFKA_BROKERS, TOPIC_PREFIX, TARGET_VAULT, GIT_REST_URL, GATEWAY_SECRET,
# STAGE, ...) into the Claude subprocess, and the runner's allowlist passes only
# HOME/PATH/USER/TZ/ZONEINFO/TMPDIR/LANG/LC_ALL — so a bare
# CREATE_TASKS_KAFKA_BROKERS set on the pod never reaches this script and the
# override silently does nothing. Inject it through CLAUDE_ENV instead: that is
# the documented passthrough, and it becomes the runner's highest-precedence env
# layer, which is where the vars above reach the script from too.
#
#   CLAUDE_ENV: "CREATE_TASKS_KAFKA_BROKERS=127.0.0.1:1"
#
# Verified 2026-09-13 against agent@v0.87.3 — claude-runner.go buildSubprocessEnv
# (3 layers) and main.go buildClaudeEnv. Never set it outside a probe.
#
# Read-only by construction: single GET to Sentry (issues filtered to
# is:unresolved), then a publish to Kafka. Never echoes the token.
#
# The script reports TWO numbers, and they are not the same number:
#   fetched  — active unresolved alerts returned by Sentry after excluding the
#              sentry-heartbeat liveness issue, i.e. the analysable alerts
#   landed   — per-alert task files OBSERVED in the vault after publishing
# The sentry-heartbeat liveness probe is dropped by exact title before this
# count, so it never becomes a task.
# `published` (from /create-tasks) is the number of CreateTaskCommands put on
# Kafka; the controller owns dedup, so published != landed. A run whose
# publishes all succeed can still land zero task files — that is the
# 2026-08-26/27/28 outage signature this script exists to surface.
#
# The final line is machine-readable and the collector step gates on it:
#   sentry-create-tasks-result: fetched=<N> published=<n> expected_new=<k> landed=<m> observed=<true|false> status=<done|failed|unobserved> refused=<r>
#
# `observed=false` means the vault could not be read at all — that is NOT the
# same as an observed zero, and the step treats it as a failure rather than
# letting an unverifiable run report done. An empty typed listing on a vault that
# demonstrably holds other tasks is an observed zero, not an unreadable vault.
#
# `refused=<r>` is the verdict-retention refusal count: alerts /create-tasks
# deliberately did NOT publish because the prior task body could not be read, so
# publishing would have replaced a task file that may already carry a verdict
# (the 2026-11-11 data loss). It is reported separately from `failed` — nothing
# errored, a write was withheld — and any non-zero value fails the run, because
# those alerts were not dispatched. The field is appended after `status=` so a
# reader matching the line's older prefix keeps working.

set -euo pipefail

: "${SENTRY_API_TOKEN:?SENTRY_API_TOKEN is required}"
: "${KAFKA_BROKERS:?KAFKA_BROKERS is required}"
: "${VAULT_API_URL:?VAULT_API_URL is required — the tracked-task listing is a typed vault-API query}"
SENTRY_URL="${SENTRY_URL:-https://bborbe.sentry.io}"
SENTRY_ORG="${SENTRY_ORG:-bborbe}"
TOPIC_PREFIX="${TOPIC_PREFIX:-}"
TARGET_VAULT="${TARGET_VAULT:-personal}"
STAGE="${STAGE:-dev}"
VAULT_POLL_ATTEMPTS="${VAULT_POLL_ATTEMPTS:-12}"
VAULT_POLL_INTERVAL_SECONDS="${VAULT_POLL_INTERVAL_SECONDS:-5}"
# TEST-ONLY, like CREATE_TASKS_KAFKA_BROKERS below: the publish binary lives at
# /create-tasks inside the image, so without a seam the script cannot run off a
# cluster — no test and no end-to-end check is possible on a Mac. Production
# never sets it and gets the absolute path unchanged.
CREATE_TASKS_BIN="${CREATE_TASKS_BIN:-/create-tasks}"

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

# The per-alert tasks are listed through the typed vault API, by task_type, never
# by a filename glob. The old glob baked the vault's layout into this image: the
# vault renumbered `24 Tasks/` -> `25 Tasks/` on 2026-09-13 and this script kept
# globbing the old name, which is what the zero-match guard below exists to catch.
# A typed query cannot repeat that failure, because it asks what a task IS rather
# than where its file sits.
# Where the result line is written for the Go step to read. This path is shared
# with pkg/steps/collector.go (collectorResultPath) — the step gates the task's
# terminal status on THIS FILE, never on the model's transcription of stdout.
RESULT_FILE="/tmp/sentry-create-tasks-result"

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
vault_list="${script_dir}/vault-list.sh"

tmp_file="$(mktemp)"
pages_file="$(mktemp)"
expected_file="$(mktemp)"
trap 'rm -f "${tmp_file}" "${pages_file}" "${expected_file}"' EXIT

# Paginate the Sentry issues API (Link-header cursor) so no active unresolved
# alert is silently dropped past the first 100.
base_url="${SENTRY_URL}/api/0/organizations/${SENTRY_ORG}/issues/?query=is:unresolved&limit=100"
cursor=""
while :; do
  url="${base_url}"
  if [ -n "${cursor}" ]; then
    url="${url}&cursor=${cursor}"
  fi
  headers="$(mktemp)"
  curl -fsS --max-time 30 -D "${headers}" \
    -H "Authorization: Bearer ${SENTRY_API_TOKEN}" \
    "${url}" >> "${pages_file}"
  printf '\n' >> "${pages_file}"
  # Extract the next-page cursor + results flag from the Link header's
  # rel="next" entry. Sentry always returns a next cursor, even on the last
  # page — results="false" is the real "no more pages" signal. Breaking only
  # on an empty cursor loops forever on 0-item pages (observed 2026-08-25:
  # page 1 = 68 items, then identical 0-item pages ad infinitum).
  link="$(grep -i '^Link:' "${headers}" | head -1)"
  cursor="$(printf '%s' "${link}" | sed -n 's/.*<[^>]*cursor=\([^&>]*\)[^>]*>; rel="next".*/\1/p')"
  results="$(printf '%s' "${link}" | sed -n 's/.*; rel="next"; results="\([^"]*\)".*/\1/p')"
  rm -f "${headers}"
  if [ -z "${cursor}" ] || [ "${results}" = "false" ]; then
    break
  fi
done

# Merge all pages into one array, then compact to the /create-tasks shape
# (project → slug string).
python3 -c '
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
' "${pages_file}" "${HEARTBEAT_TITLE}" > "${tmp_file}"

count="$(python3 -c 'import json, sys; print(len(json.load(open(sys.argv[1]))))' "${tmp_file}")"
short_ids="$(python3 -c 'import json, sys; print(" ".join(issue["shortId"] for issue in json.load(open(sys.argv[1]))))' "${tmp_file}")"

echo "sentry-create-tasks: ${count} active unresolved alerts: ${short_ids}"

# Which of these alerts have no per-alert task file yet? That is the set that
# SHOULD land. Computed from the alert's lastSeen UTC date, matching the
# filename /create-tasks will produce ("Analyze Sentry issue <short-id> - <date>.md").
# This is a dedup check, NOT the created count — the created count is observed
# after publishing, never predicted.
#
# A failed listing is NOT an empty listing. `|| true` here would make an auth or
# network failure indistinguishable from "nothing exists", and the caller gates
# on the number this produces — so the failure is recorded and surfaced instead.
observed="true"
unobserved_reason=""
existing_files=""
if ! existing_files="$("${vault_list}")"; then
  observed="false"
  unobserved_reason="vault-list failed while checking which alerts are already tracked"
fi

# A pattern that matches NOTHING while alerts were fetched means the pattern is
# wrong, not that no task exists. That is the exact shape the 2026-09-13
# renumbering hid behind: `24 Tasks/` still EXISTED and still held one stale
# file, so nothing errored — `existing` came back empty, every alert looked
# untracked, `expected_new` equalled the fetched count, and the landing clause
# fired on every non-empty run. A false `failed` on healthy traffic, and the
# mirror of the false `done` this script exists to prevent.
#
# `unobserved` is the honest verdict: it cannot prove anything, and unlike
# `failed` it never claims a landing failure it did not witness.
#
# ⚠️ An empty typed listing is NOT by itself evidence of a broken query. On a
# lane that has never published, the vault genuinely holds none of the three
# types, and treating that as unobservable made a healthy first run — every
# alert published, every file landed — report `status=unobserved`, so a
# successful publish could never be proved. The two populations are told apart
# by asking the same API a question that does not depend on the three types: how
# many tasks does this vault hold AT ALL? A vault holding tasks of other types
# while matching none of ours is the correct empty population; a vault holding
# nothing, or a population read that fails or answers non-numerically, still
# cannot be told apart from a broken query and stays unobserved.
#
# The population read runs ONLY on this path, so a vault with tracked tasks
# never pays for it.
if [ "${observed}" = "true" ] && [ "${count}" -gt 0 ] && [ -z "${existing_files}" ]; then
  population=""
  population_reason=""
  if ! population="$("${vault_list}" --population)"; then
    population_reason="the vault population read failed"
  elif ! printf '%s' "${population}" | grep -Eq '^[0-9]+$'; then
    population_reason="the vault population read returned a non-numeric answer (${population})"
  elif [ "${population}" -eq 0 ]; then
    population_reason="the vault holds no tasks at all"
  fi
  if [ -n "${population_reason}" ]; then
    observed="false"
    unobserved_reason="the typed listing returned no tracked tasks at all while ${count} alert(s) were fetched, and ${population_reason} — an empty vault and a broken query cannot be told apart"
  else
    echo "sentry-create-tasks: the typed listing matched nothing, but the vault holds ${population} task(s) of other types — accepting the empty set as the correct population, not a broken query" >&2
  fi
fi

expected_new=0
if [ "${observed}" = "true" ]; then
  python3 -c '
import datetime, json, sys
alerts = json.load(open(sys.argv[1]))
existing = sys.argv[2]
for a in alerts:
    t = datetime.datetime.fromisoformat(a["lastSeen"].replace("Z", "+00:00"))
    date = t.astimezone(datetime.timezone.utc).strftime("%Y-%m-%d")
    name = a["shortId"] + " - " + date + ".md"
    if name not in existing:
        print(name)
' "${tmp_file}" "${existing_files}" > "${expected_file}"
  expected_new="$(grep -c . "${expected_file}" || true)"
fi

# Publish. NOT exec'd: the script needs a return path so it can observe what
# actually landed (exec replaces this shell and discards the counts).
set +e
# -logtostderr -v=2 is load-bearing for the retention evidence, not cosmetic.
# glog writes to files under /tmp by default, and /create-tasks is a SEPARATE
# binary from the image's /main entrypoint — so the `-logtostderr` the
# Dockerfile puts on /main does not reach it. Without these flags its per-alert
# `verdict retention outcome=...` lines and the `REFUSED to publish ...` error
# stay in /tmp inside the pod, and a refusal would be visible only as a count in
# this script's own result line. Both flags are glog flags registered at package
# init and parsed through flag.CommandLine, so passing them cannot fail startup.
create_out="$("${CREATE_TASKS_BIN}" \
  -logtostderr \
  -v=2 \
  --alerts-file "${tmp_file}" \
  --kafka-brokers "${CREATE_TASKS_KAFKA_BROKERS:-${KAFKA_BROKERS}}" \
  --topic-prefix "${TOPIC_PREFIX}" \
  --target-vault "${TARGET_VAULT}" \
  --stage "${STAGE}" \
  --vault-api-url "${VAULT_API_URL:-}" \
  --git-rest-url "${GIT_REST_URL:-}" \
  --gateway-secret "${GATEWAY_SECRET:-}")"
create_rc=$?
set -e

published="$(printf '%s\n' "${create_out}" \
  | sed -n 's/^create-tasks-result: published=\([0-9]*\).*/\1/p' | tail -1)"
published="${published:-0}"

# Refusals are counted apart from publish failures: /create-tasks withheld the
# write for these alerts because the prior task body could not be read, so it
# could not know whether publishing would replace a verdict. Absent (an older
# binary) reads as 0.
refused="$(printf '%s\n' "${create_out}" \
  | sed -n 's/^create-tasks-result: .*refused=\([0-9]*\).*/\1/p' | tail -1)"
refused="${refused:-0}"

# Observe how many of the expected task files actually landed. The controller
# materializes them asynchronously off Kafka, so this is a bounded poll, not a
# single read. A failed listing ends the poll as UNOBSERVED — never as a
# zero-landing, which would be a false failure on an otherwise healthy run.
landed=0
if [ "${observed}" = "true" ]; then
  attempt=1
  while :; do
    current_files=""
    if ! current_files="$("${vault_list}")"; then
      observed="false"
      unobserved_reason="vault-list failed while observing which task files landed"
      break
    fi
    landed="$(python3 -c '
import sys
current = sys.argv[1]
n = 0
for line in open(sys.argv[2]):
    name = line.strip()
    if name and name in current:
        n += 1
print(n)
' "${current_files}" "${expected_file}")"
    if [ "${landed}" -ge "${expected_new}" ] || [ "${attempt}" -ge "${VAULT_POLL_ATTEMPTS}" ]; then
      break
    fi
    attempt=$((attempt + 1))
    sleep "${VAULT_POLL_INTERVAL_SECONDS}"
  done
fi

# Status rule: a run that PUBLISHED NOTHING is a failure whatever the controller
# would have done with it, and a zero-creation run is a FAILURE when new alerts
# were expected. It is HEALTHY when every fetched alert was already tracked —
# dedup is the designed idempotency, so a quiet day must not alarm. Note that a
# quiet day still PUBLISHES every fetched alert and lands none of them, so it
# reads published=<fetched>; published=0 means the publish itself failed. An
# UNOBSERVED run is neither: it cannot prove anything, so it must not report
# done either.
#
# A REFUSED alert is checked first, because it is the one failure whose cause
# the other rules would misname: it is not a publish fault and not an
# observation gap, it is a deliberate withheld write, and the operator's action
# is to re-run once the vault is back rather than to investigate the pipeline.
status="done"
failure_reason=""
if [ "${refused}" -gt 0 ]; then
  status="failed"
  failure_reason="${refused} of ${count} alert(s) refused: the prior verdict could not be read, so publishing would have replaced a task file that may carry one — re-run once the vault is reachable (create-tasks rc=${create_rc})"
elif [ "${observed}" != "true" ]; then
  status="unobserved"
elif [ "${count}" -gt 0 ] && [ "${published}" -eq 0 ]; then
  status="failed"
  failure_reason="fetched ${count} alert(s) but published 0 — the publish step failed (create-tasks rc=${create_rc})"
elif [ "${expected_new}" -gt 0 ] && [ "${landed}" -eq 0 ]; then
  status="failed"
  failure_reason="${expected_new} new alert(s) expected but 0 task files landed (create-tasks rc=${create_rc})"
fi

result_line="sentry-create-tasks-result: fetched=${count} published=${published} expected_new=${expected_new} landed=${landed} observed=${observed} status=${status} refused=${refused}"
echo "${result_line}"
# Write it for the Go step as well. The script computes every count itself, so
# handing them to the model to re-type adds a failure mode without adding
# information — observed on dev 2026-09-12, where the model wrote a prose JSON
# summary and never copied this line at all.
printf '%s\n' "${result_line}" > "${RESULT_FILE}"
if [ -n "${failure_reason}" ]; then
  echo "sentry-create-tasks: ${failure_reason}" >&2
fi
if [ "${status}" = "unobserved" ]; then
  echo "sentry-create-tasks: could not observe the creation phase — ${unobserved_reason}" >&2
fi
