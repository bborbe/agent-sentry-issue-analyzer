#!/usr/bin/env bash
# jira-read.sh - Constrained read-only Jira search via the Atlassian API gateway.
#
# Answers the ONE question the `already-tracked` verdict asks on the Jira side:
# "is there an OPEN Jira issue matching this alert?" Before this script the
# rubric named "open Jira ticket" as an `already-tracked` input with no
# mechanism behind it, so the verdict rested on an unbacked LLM judgment.
#
# Gateway, not the site URL. A scoped API token sent to
# https://<site>.atlassian.net answers 401; the supported path for scoped
# tokens is https://api.atlassian.com/ex/jira/<cloudId>. Both are configurable
# so a test server can stand in, but the default is the gateway.
#
# Usage: jira-read.sh '<jql>'
#   e.g. jira-read.sh 'project = BRO AND statusCategory != Done AND text ~ "circuit breaker"'
#
# Env:
#   JIRA_CLOUD_ID       (required) Atlassian cloud id - the gateway path segment
#   JIRA_EMAIL          (required) account email for Basic auth
#   JIRA_API_TOKEN      (required) API token (teamvault-rendered)
#   JIRA_GATEWAY_URL    (optional) default https://api.atlassian.com
#   JIRA_MAX_RESULTS    (optional) default 20
#
# stdout = a `returned=<n>` line followed by one line per matching issue:
#   <KEY>	<statusCategory.key>	<status.name>	<summary>
# `returned=0` is a real measurement - "no open ticket matches" - and is
# distinct from a failed call, which exits non-zero and prints nothing to
# stdout. stderr = diagnostics.
#
# Read-only by construction: exactly one GET, no mutations, and the JQL is
# passed through `curl -G --data-urlencode` so no shell metacharacter reaches
# the URL. The credential is handed to curl on stdin (`-K -`) rather than on
# the command line, so it never appears in the process table.

set -e -u -o pipefail

: "${JIRA_CLOUD_ID:?JIRA_CLOUD_ID is required}"
: "${JIRA_EMAIL:?JIRA_EMAIL is required}"
: "${JIRA_API_TOKEN:?JIRA_API_TOKEN is required}"
JIRA_GATEWAY_URL="${JIRA_GATEWAY_URL:-https://api.atlassian.com}"
JIRA_MAX_RESULTS="${JIRA_MAX_RESULTS:-20}"

if [ $# -ne 1 ] || [ -z "$1" ]; then
  echo "Usage: jira-read.sh '<jql>'" >&2
  exit 1
fi

# The current search endpoint. /rest/api/3/search was removed by Atlassian and
# answers 410 with a migration pointer; /rest/api/3/search/jql is its successor.
url="${JIRA_GATEWAY_URL%/}/ex/jira/${JIRA_CLOUD_ID}/rest/api/3/search/jql"

# -K - : read the credential from stdin so it is absent from argv/ps.
# -sfS : silent progress, but SHOW errors - a failed search must be
#        distinguishable from an empty one, since the verdict gates on the count.
response="$(
  printf 'user = "%s:%s"\n' "$JIRA_EMAIL" "$JIRA_API_TOKEN" |
    curl -sfS --max-time 30 -K - \
      -G --data-urlencode "jql=$1" \
      --data-urlencode "maxResults=${JIRA_MAX_RESULTS}" \
      --data-urlencode "fields=key,summary,status" \
      "$url"
)"

printf '%s' "$response" | python3 -c '
import json, sys

data = json.load(sys.stdin)
issues = data.get("issues") or []
print("returned=%d" % len(issues))
for issue in issues:
    fields = issue.get("fields") or {}
    status = fields.get("status") or {}
    category = (status.get("statusCategory") or {}).get("key") or ""
    summary = (fields.get("summary") or "").replace("\t", " ").replace("\n", " ")
    key = issue.get("key") or ""
    name = status.get("name") or ""
    print("%s\t%s\t%s\t%s" % (key, category, name, summary))
'
