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
# Usage:
#   vault-list.sh               the tracked Sentry-alert task paths, one per line
#   vault-list.sh --population  the vault's TOTAL task count, any type
#
# --population exists to separate the two populations a caller cannot tell apart
# from a typed listing alone: a vault that genuinely tracks none of the three
# alert types (correct on a lane that has never published) and a query that
# stopped matching (the 2026-09-13 renumbering). A non-zero total says the API
# works and the vault holds tasks — so an empty typed answer is the real
# population. Zero, a failed read, or a non-numeric answer proves nothing.
#
# stdout = one task path per line (empty when nothing matches); stderr = diagnostics.
# With --population: a single integer on stdout.

set -e -u -o pipefail

: "${VAULT_API_URL:?VAULT_API_URL is required}"

mode="typed"
if [ "$#" -gt 1 ]; then
  echo "vault-list: expected at most one argument, got $# — refusing rather than half-honouring a mode" >&2
  exit 2
fi
case "${1:-}" in
  "") ;;
  --population) mode="population" ;;
  *)
    echo "vault-list: unknown argument: $1 (expected none or --population)" >&2
    exit 2
    ;;
esac

# Optional gateway auth headers — injected when GATEWAY_SECRET is non-empty.
gateway_args=()
if [ -n "${GATEWAY_SECRET:-}" ]; then
  gateway_args=(-H "X-Gateway-Secret: $GATEWAY_SECRET" -H "X-Gateway-Initator: agent-sentry-collector")
fi

# The type filter is what makes this a dedup listing; --population deliberately
# sends none, so the answer covers every task in the vault.
type_args=()
if [ "${mode}" = "typed" ]; then
  type_args=(
    --data-urlencode "task_type=sentry-issue-analyzer"
    --data-urlencode "task_type=sentry-deep-analyzer"
    --data-urlencode "task_type=sentry-fix"
  )
fi

# -sS: silent progress, but SHOW errors — a failed listing must be distinguishable
# from an empty one, since the caller gates on the count it produces. `pipefail`
# carries a curl failure through the pipe, so a failed request cannot look like a
# successful empty listing.
curl -sfS --max-time 30 \
  ${gateway_args[@]+"${gateway_args[@]}"} \
  -G \
  ${type_args[@]+"${type_args[@]}"} \
  "${VAULT_API_URL}/api/v1/tasks" |
  python3 -c '
import json, sys

mode = sys.argv[1]

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

if mode == "population":
    # `total` is the API own count; fall back to the item count so an older
    # server still answers. A non-integer must fail HERE rather than reach the
    # caller numeric test, where it would abort the script instead of reporting
    # an unobservable vault.
    total = body.get("total", len(items))
    if not isinstance(total, int) or isinstance(total, bool):
        sys.exit("vault-list: total is %r, not an integer" % (total,))
    print(total)
else:
    for task in items:
        print(task["path"])
' "${mode}"
