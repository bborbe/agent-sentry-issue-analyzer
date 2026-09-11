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
#      STAGE (default dev), GIT_REST_URL (required — vault observation),
#      GATEWAY_SECRET (optional — git-rest gateway auth),
#      VAULT_POLL_ATTEMPTS (default 12), VAULT_POLL_INTERVAL_SECONDS (default 5)
#
# Read-only by construction: single GET to Sentry (issues filtered to
# is:unresolved), then a publish to Kafka. Never echoes the token.
#
# The script reports TWO numbers, and they are not the same number:
#   fetched  — active unresolved alerts returned by Sentry
#   landed   — per-alert task files OBSERVED in the vault after publishing
# `published` (from /create-tasks) is the number of CreateTaskCommands put on
# Kafka; the controller owns dedup, so published != landed. A run whose
# publishes all succeed can still land zero task files — that is the
# 2026-08-26/27/28 outage signature this script exists to surface.
#
# The final line is machine-readable and the collector step gates on it:
#   sentry-create-tasks-result: fetched=<N> published=<n> expected_new=<k> landed=<m> observed=<true|false> status=<done|failed|unobserved>
#
# `observed=false` means the vault could not be read at all — that is NOT the
# same as an observed zero, and the step treats it as a failure rather than
# letting an unverifiable run report done.

set -euo pipefail

: "${SENTRY_API_TOKEN:?SENTRY_API_TOKEN is required}"
: "${KAFKA_BROKERS:?KAFKA_BROKERS is required}"
: "${GIT_REST_URL:?GIT_REST_URL is required}"
SENTRY_URL="${SENTRY_URL:-https://bborbe.sentry.io}"
SENTRY_ORG="${SENTRY_ORG:-bborbe}"
TOPIC_PREFIX="${TOPIC_PREFIX:-}"
TARGET_VAULT="${TARGET_VAULT:-personal}"
STAGE="${STAGE:-dev}"
VAULT_POLL_ATTEMPTS="${VAULT_POLL_ATTEMPTS:-12}"
VAULT_POLL_INTERVAL_SECONDS="${VAULT_POLL_INTERVAL_SECONDS:-5}"

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
} for issue in issues]
json.dump(compact, sys.stdout)
' "${pages_file}" > "${tmp_file}"

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
if ! existing_files="$("${vault_list}" "24 Tasks/Analyze Sentry issue *.md")"; then
  observed="false"
  unobserved_reason="vault-list failed while checking which alerts are already tracked"
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
create_out="$(/create-tasks \
  --alerts-file "${tmp_file}" \
  --kafka-brokers "${KAFKA_BROKERS}" \
  --topic-prefix "${TOPIC_PREFIX}" \
  --target-vault "${TARGET_VAULT}" \
  --stage "${STAGE}")"
create_rc=$?
set -e

published="$(printf '%s\n' "${create_out}" \
  | sed -n 's/^create-tasks-result: published=\([0-9]*\).*/\1/p' | tail -1)"
published="${published:-0}"

# Observe how many of the expected task files actually landed. The controller
# materializes them asynchronously off Kafka, so this is a bounded poll, not a
# single read. A failed listing ends the poll as UNOBSERVED — never as a
# zero-landing, which would be a false failure on an otherwise healthy run.
landed=0
if [ "${observed}" = "true" ]; then
  attempt=1
  while :; do
    current_files=""
    if ! current_files="$("${vault_list}" "24 Tasks/Analyze Sentry issue *.md")"; then
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

# Status rule: a zero-creation run is a FAILURE when new alerts were expected,
# and HEALTHY when every fetched alert was already tracked — dedup is the
# designed idempotency, so a quiet day must not alarm. An UNOBSERVED run is
# neither: it cannot prove anything, so it must not report done either.
status="done"
if [ "${observed}" != "true" ]; then
  status="unobserved"
elif [ "${expected_new}" -gt 0 ] && [ "${landed}" -eq 0 ]; then
  status="failed"
fi

echo "sentry-create-tasks-result: fetched=${count} published=${published} expected_new=${expected_new} landed=${landed} observed=${observed} status=${status}"
if [ "${status}" = "failed" ]; then
  echo "sentry-create-tasks: ${expected_new} new alert(s) expected but 0 task files landed (create-tasks rc=${create_rc})" >&2
fi
if [ "${status}" = "unobserved" ]; then
  echo "sentry-create-tasks: could not observe the creation phase — ${unobserved_reason}" >&2
fi
