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
#      STAGE (default dev)
#
# Read-only by construction: single GET to Sentry (issues filtered to
# is:unresolved), then a publish to Kafka. Never echoes the token.

set -euo pipefail

: "${SENTRY_API_TOKEN:?SENTRY_API_TOKEN is required}"
: "${KAFKA_BROKERS:?KAFKA_BROKERS is required}"
SENTRY_URL="${SENTRY_URL:-https://bborbe.sentry.io}"
SENTRY_ORG="${SENTRY_ORG:-bborbe}"
TOPIC_PREFIX="${TOPIC_PREFIX:-}"
TARGET_VAULT="${TARGET_VAULT:-personal}"
STAGE="${STAGE:-dev}"

tmp_file="$(mktemp)"
pages_file="$(mktemp)"
trap 'rm -f "${tmp_file}" "${pages_file}"' EXIT

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

exec /create-tasks \
  --alerts-file "${tmp_file}" \
  --kafka-brokers "${KAFKA_BROKERS}" \
  --topic-prefix "${TOPIC_PREFIX}" \
  --target-vault "${TARGET_VAULT}" \
  --stage "${STAGE}"
