#!/usr/bin/env bash
# Constrained read-only Sentry fetcher — replaces the mcp__sentry__* MCP tools.
#
# Usage: sentry-read.sh <issue-url-or-id>
#   e.g. sentry-read.sh https://bborbe.sentry.io/issues/5192501045/
#   or   sentry-read.sh 5192501045
#
# Emits the LIVE issue state the verdict rubric needs, as a flat key=value block:
#   short_id, status, live_event_count, first_seen, last_seen, users_impacted, title
# followed, best-effort, by the latest event's stack trace frames:
#   stack_trace=<N> frames
#   <file>:<line> in <function>        (frame with a usable line number; at most
#                                      30 frames; <file> is a basename)
#   <basename> in <function>           (frame without a usable line number)
# Frames are emitted whenever an exception entry carries at least one frame; an
# exception entry with zero frames degrades to 'no frames'; no exception entry
# degrades to 'no exception entry'. Every emitted frame value is single-line
# (control characters are neutralised). A failed, empty, or malformed event
# fetch degrades to exactly one
#   stack_trace unavailable (<reason>)
# line (reason: timeout | no events | auth | fetch failed | no frames | no exception entry)
# and never fails the run — only a metadata fetch failure exits non-zero.
#
# Env: SENTRY_API_TOKEN (required, Bearer token), SENTRY_ORG (default bborbe),
#      SENTRY_URL (default https://bborbe.sentry.io)
#
# Read-only by construction: two GETs (issue metadata + latest event, the second
# best-effort), no mutations, no shell metacharacter interpolation into the URL
# (issue id is validated to [0-9]+). Accepts a bare numeric issue id.

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
if printf '%s' "$issue_id" | grep -qE '^[0-9]+$'; then
  : # bare numeric id, use as-is
else
  issue_id="$(printf '%s' "$issue_id" | sed -nE 's#.*/issues/([0-9]+)/?.*#\1#p')"
  if [ -z "$issue_id" ]; then
    echo "could not extract a numeric Sentry issue id from: $1" >&2
    exit 2
  fi
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

# Best-effort fetch of the latest event's stack trace. A failure here must not
# fail the run (metadata is the deliverable), so the fetch is guarded and its
# outcome classified into exactly one stack_trace unavailable (<reason>) line.
event_body="$(mktemp)"
trap 'rm -f "${event_body}"' EXIT

set +e
event_http="$(curl -sS --max-time 30 -o "${event_body}" -w '%{http_code}' \
  -H "Authorization: Bearer ${SENTRY_API_TOKEN}" \
  "${SENTRY_URL}/api/0/organizations/${SENTRY_ORG}/issues/${issue_id}/events/latest/" 2>/dev/null)"
event_rc=$?
set -e

if [ "${event_rc}" -eq 28 ]; then
  reason="timeout"
elif [ "${event_http}" = "404" ]; then
  reason="no events"
elif [ "${event_http}" = "401" ] || [ "${event_http}" = "403" ]; then
  reason="auth"
elif [ "${event_rc}" -ne 0 ] || [ "${event_http}" != "200" ]; then
  reason="fetch failed"
else
  reason=""
fi

if [ -n "${reason}" ]; then
  printf 'stack_trace unavailable (%s)\n' "${reason}"
else
  # HTTP 200: three-state classification of the first exception value's frames —
  # emit the frames (basename, lineno, function only — never context, never the
  # raw payload) when at least one frame exists, 'no frames' when the exception
  # entry has zero frames, 'no exception entry' when none exists.
  set +e
  frames="$(python3 -c '
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
' "${event_body}")"
  frames_rc=$?
  set -e
  if [ "${frames_rc}" -ne 0 ] || [ -z "${frames}" ]; then
    printf 'stack_trace unavailable (no exception entry)\n'
  else
    printf '%s\n' "${frames}"
  fi
fi
