#!/bin/bash
# vault-list.sh - List the tracked Sentry-alert tasks through the typed vault API.
#
# Used by sentry-create-tasks.sh as a DEDUP check — "which alerts are already
# tracked" — before creating tasks, and again to observe what actually landed.
# It therefore needs the WHOLE population, not one alert's rows: a subset answer
# makes the caller re-create the missing ones as duplicate tasks on every run.
#
# Vault endpoint via env var:
#   VAULT_API_URL  e.g. http://vault-api-personal:9090
#
# ⚠️ Why three task_type values and not one. Over the 1,898 pages matching the
# old filename glob, the types are sentry-issue-analyzer 1,726,
# sentry-deep-analyzer 118, sentry-fix 53, and 1 page carrying no type at all
# (a manual page whose title happens to begin "Analyze Sentry issue"). The three
# typed values therefore cover 1,897; the one untyped page is a glob false
# positive, and this query correctly omits it.
#
# A single-value filter returns 1,726 of those 1,897, leaving 171 typed pages
# looking untracked. The caller's own "matched nothing" guard does not fire on
# that, because 1,726 is neither empty nor correct.
#
# The values are a union: a page matches when its type is any one of them.
#
# stdout = one task path per line (empty when nothing matches); stderr = diagnostics.

set -e -u -o pipefail

: "${VAULT_API_URL:?VAULT_API_URL is required}"

# Optional gateway auth headers — injected when GATEWAY_SECRET is non-empty.
gateway_args=()
if [ -n "${GATEWAY_SECRET:-}" ]; then
  gateway_args=(-H "X-Gateway-Secret: $GATEWAY_SECRET" -H "X-Gateway-Initator: agent-sentry-collector")
fi

# -sS: silent progress, but SHOW errors — a failed listing must be distinguishable
# from an empty one, since the caller gates on the count it produces. `pipefail`
# carries a curl failure through the pipe, so a failed request cannot look like a
# successful empty listing.
curl -sfS --max-time 30 \
  ${gateway_args[@]+"${gateway_args[@]}"} \
  -G \
  --data-urlencode "task_type=sentry-issue-analyzer" \
  --data-urlencode "task_type=sentry-deep-analyzer" \
  --data-urlencode "task_type=sentry-fix" \
  "${VAULT_API_URL}/api/v1/tasks" |
  python3 -c '
import json, sys

# A wrong-shaped answer must FAIL, never read as an empty listing. An error
# envelope, a renamed key or a wrapper object would all become [] with exit 0,
# and the caller cannot tell that from "this vault tracks nothing" — it would
# then publish every alert again.
try:
    body = json.load(sys.stdin)
except json.JSONDecodeError as err:
    sys.exit("vault-list: response is not JSON: %s" % err)

if not isinstance(body, dict):
    sys.exit("vault-list: response is not a JSON object")
for key in ("error", "message"):
    if key in body:
        sys.exit("vault-list: API refused the listing: %s" % body[key])
if "items" not in body:
    sys.exit("vault-list: response has no items field (keys: %s)" % sorted(body))
items = body["items"]
if not isinstance(items, list):
    sys.exit("vault-list: items is %s, not a list" % type(items).__name__)

for task in items:
    print(task["path"])
'
