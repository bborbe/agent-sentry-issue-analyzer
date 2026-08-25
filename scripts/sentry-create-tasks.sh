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
trap 'rm -f "${tmp_file}"' EXIT

response="$(curl -fsS --max-time 30 \
  -H "Authorization: Bearer ${SENTRY_API_TOKEN}" \
  "${SENTRY_URL}/api/0/organizations/${SENTRY_ORG}/issues/?query=is:unresolved&limit=100")"

# Compact each issue to the shape /create-tasks consumes (project → slug string).
printf '%s' "${response}" | python3 -c '
import json, sys
issues = json.load(sys.stdin)
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
' > "${tmp_file}"

count="$(python3 -c 'import json, sys; print(len(json.load(open(sys.argv[1]))))' "${tmp_file}")"
short_ids="$(python3 -c 'import json, sys; print(" ".join(issue["shortId"] for issue in json.load(open(sys.argv[1]))))' "${tmp_file}")"

echo "sentry-create-tasks: ${count} active unresolved alerts: ${short_ids}"

exec /create-tasks \
  --alerts-file "${tmp_file}" \
  --kafka-brokers "${KAFKA_BROKERS}" \
  --topic-prefix "${TOPIC_PREFIX}" \
  --target-vault "${TARGET_VAULT}" \
  --stage "${STAGE}"
