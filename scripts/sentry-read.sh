#!/usr/bin/env bash
# Constrained read-only Sentry fetcher — replaces the mcp__sentry__* MCP tools.
#
# Usage: sentry-read.sh <issue-url-or-id>
#   e.g. sentry-read.sh https://bborbe.sentry.io/issues/5192501045/
#   or   sentry-read.sh 5192501045
#
# Emits the LIVE issue state the verdict rubric needs, as a flat key=value block:
#   short_id, status, live_event_count, first_seen, last_seen, users_impacted, title
#
# Env: SENTRY_API_TOKEN (required, Bearer token), SENTRY_ORG (default bborbe),
#      SENTRY_URL (default https://bborbe.sentry.io)
#
# Read-only by construction: single GET, no mutations, no shell metacharacter
# interpolation into the URL (issue id is validated to [0-9]+).

set -euo pipefail

: "${SENTRY_API_TOKEN:?SENTRY_API_TOKEN is required}"
SENTRY_URL="${SENTRY_URL:-https://bborbe.sentry.io}"
SENTRY_ORG="${SENTRY_ORG:-bborbe}"

issue_id="${1:-}"
if [ -z "$issue_id" ]; then
  echo "usage: sentry-read.sh <issue-url-or-id>" >&2
  exit 2
fi

# Extract a numeric issue id from a URL like .../issues/1234567890/ or accept a bare id.
issue_id="$(printf '%s' "$issue_id" | sed -nE 's#.*/issues/([0-9]+)/?.*#\1#p')"
if [ -z "$issue_id" ]; then
  echo "could not extract a numeric Sentry issue id from: $1" >&2
  exit 2
fi

response="$(curl -fsS --max-time 30 \
  -H "Authorization: Bearer ${SENTRY_API_TOKEN}" \
  "${SENTRY_URL}/api/0/organizations/${SENTRY_ORG}/issues/${issue_id}/")"

# Emit the fields the verdict rubric consumes, in a stable order.
printf 'short_id=%s\n'    "$(printf '%s' "$response" | python3 -c 'import json,sys; print(json.load(sys.stdin)["shortId"])')"
printf 'status=%s\n'      "$(printf '%s' "$response" | python3 -c 'import json,sys; print(json.load(sys.stdin)["status"])')"
printf 'live_event_count=%s\n' "$(printf '%s' "$response" | python3 -c 'import json,sys; print(json.load(sys.stdin)["count"])')"
printf 'first_seen=%s\n'  "$(printf '%s' "$response" | python3 -c 'import json,sys; print(json.load(sys.stdin)["firstSeen"])')"
printf 'last_seen=%s\n'   "$(printf '%s' "$response" | python3 -c 'import json,sys; print(json.load(sys.stdin)["lastSeen"])')"
printf 'users_impacted=%s\n' "$(printf '%s' "$response" | python3 -c 'import json,sys; print(json.load(sys.stdin)["userCount"])')"
printf 'title=%s\n'       "$(printf '%s' "$response" | python3 -c 'import json,sys; print(json.load(sys.stdin)["title"])')"
