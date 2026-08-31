#!/usr/bin/env bash
# Deterministic mock-API test for scripts/sentry-read.sh.
#
# Serves fixture Sentry metadata + events/latest payloads from an embedded
# python3 http.server bound to 127.0.0.1 on an ephemeral port, then drives the
# script under test against http://127.0.0.1:<port> and asserts the AC1-AC5
# output contract from specs/in-progress/001-sentry-read-event-payload.md:
#   * 7 metadata keys in their frozen order (AC1)
#   * <file>:<line> in <function> frame lines under a stack_trace=<N> frames
#     header (AC2)
#   * a fixed 30-frame cap, with the 31st+ fixture frames absent (AC5)
#   * no raw JSON keys, no fixture context sentinel on stdout (AC5 no-PII)
#   * bare numeric id and full-URL invocations produce byte-identical output
#     (AC3)
#   * failure stubs (404 / no exception entry / 401) each emit exactly one
#     'stack_trace unavailable (<reason>)' marker with the metadata intact (AC4)
#
# Container-executable: python3 + curl + bash only; no external network, no
# Docker socket, no real credentials (dummy SENTRY_API_TOKEN=x). Exits non-zero
# with a 'FAIL: <name>' message on the first assertion miss.

set -euo pipefail

SCRIPT="$(cd "$(dirname "$0")" && pwd)/sentry-read.sh"
[ -f "${SCRIPT}" ] || { echo "FAIL: script under test missing: ${SCRIPT}" >&2; exit 1; }

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

server_py="$(mktemp)"
server_log="$(mktemp)"
server_pid=""

cleanup() {
  if [ -n "${server_pid}" ]; then
    kill "${server_pid}" 2>/dev/null || true
  fi
  rm -f "${server_py}" "${server_log}"
}
trap cleanup EXIT

# ---- Fixture server (embedded python3 http.server on 127.0.0.1:0) ----
cat > "${server_py}" <<'PYEOF'
import json
import re
from http.server import BaseHTTPRequestHandler, HTTPServer


class QuietHandler(BaseHTTPRequestHandler):
    def log_message(self, fmt, *args):
        pass

    def _send(self, code, obj):
        body = json.dumps(obj).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def metadata(self, issue):
        self._send(200, {
            "shortId": "TEST-%s" % issue,
            "status": "unresolved",
            "count": 42,
            "firstSeen": "2026-08-01T10:00:00Z",
            "lastSeen": "2026-08-31T10:00:00Z",
            "userCount": 7,
            "title": "Mock issue %s" % issue,
        })

    @staticmethod
    def frames(n):
        # 40+ frames each carrying a context array whose sentinel must never
        # appear in the script's stdout (drives the 30-frame cap and no-PII).
        return [
            {
                "abs_path": "/usr/src/app/pkg/foo/bar%d.go" % i,
                "filename": "pkg/foo/bar%d.go" % i,
                "lineno": 40 + i,
                "function": "Func%d" % i,
                "context": ["RAW-CONTEXT-SENTINEL line %d" % i],
            }
            for i in range(1, n + 1)
        ]

    def event_latest(self, issue):
        if issue == "123":
            self._send(200, {"entries": [
                {"type": "exception", "data": {"values": [
                    {"stacktrace": {"frames": self.frames(40)}}
                ]}}
            ]})
        elif issue == "456":
            # Event with no exception entry (message only) -> no exception entry.
            self._send(200, {"entries": [
                {"type": "message", "data": {"message": "hello"}}
            ]})
        elif issue == "789":
            self._send(404, {"detail": "no events"})
        elif issue == "999":
            self._send(401, {"detail": "invalid token"})
        else:
            self._send(404, {"detail": "not found"})

    def do_GET(self):
        m = re.match(r"^/api/0/organizations/test/issues/(\d+)/events/latest/?$", self.path)
        if m:
            self.event_latest(m.group(1))
            return
        m = re.match(r"^/api/0/organizations/test/issues/(\d+)/?$", self.path)
        if m:
            self.metadata(m.group(1))
            return
        self._send(404, {"detail": "not found"})


server = HTTPServer(("127.0.0.1", 0), QuietHandler)
print(server.server_address[1], flush=True)
server.serve_forever()
PYEOF

python3 "${server_py}" > "${server_log}" 2>&1 &
server_pid=$!

# Wait for the server to report its ephemeral port (bounded poll, 50 x 0.1 s);
# fail loudly with the server log if it never appears.
PORT=""
for ((i = 0; i < 50; i++)); do
  PORT="$(head -1 "${server_log}" 2>/dev/null || true)"
  if [ -n "${PORT}" ]; then
    break
  fi
  sleep 0.1
done
if ! printf '%s' "${PORT}" | grep -qE '^[0-9]+$'; then
  echo "FAIL: fixture server never reported a valid port" >&2
  cat "${server_log}" >&2
  exit 1
fi

export SENTRY_API_TOKEN=x  # dummy token; the script only requires it non-empty
export SENTRY_ORG=test
export SENTRY_URL="http://127.0.0.1:${PORT}"

# The container may route curl through an HTTP proxy (http_proxy/HTTPS_PROXY are
# set in the YOLO image); force curl to connect directly to the loopback fixture
# server so the test never leaves 127.0.0.1.
export NO_PROXY=127.0.0.1,localhost
export no_proxy=127.0.0.1,localhost

# ---- Happy path: bare id and full-URL invocations (AC3) ----
if ! out_bare="$(bash "${SCRIPT}" 123 2>&1)"; then
  fail "AC3 bare-id invocation exited non-zero"
fi
if ! out_url="$(bash "${SCRIPT}" "http://127.0.0.1:${PORT}/api/0/organizations/test/issues/123/" 2>&1)"; then
  fail "AC3 URL invocation exited non-zero"
fi

# AC3: byte-identical output; no id-extraction error.
if [ "${out_bare}" != "${out_url}" ]; then
  fail "AC3 bare-id and URL invocations produced different output"
fi
if printf '%s\n' "${out_bare}" | grep -q 'could not extract'; then
  fail "AC3 bare-id invocation hit the id-extraction error path"
fi

# AC1: exactly 7 metadata keys in the frozen order.
key_count="$(printf '%s\n' "${out_bare}" | grep -cE '^(short_id|status|live_event_count|first_seen|last_seen|users_impacted|title)=' || true)"
if [ "${key_count}" -ne 7 ]; then
  fail "AC1 expected 7 metadata keys, got ${key_count}"
fi
key_order="$(printf '%s\n' "${out_bare}" | grep -oE '^(short_id|status|live_event_count|first_seen|last_seen|users_impacted|title)=' | tr -d '=' | paste -sd, -)"
if [ "${key_order}" != "short_id,status,live_event_count,first_seen,last_seen,users_impacted,title" ]; then
  fail "AC1 metadata keys not in frozen order (got '${key_order}')"
fi

# AC2: exactly one stack_trace=<N> frames header (N >= 1) and >= 1 frame line.
header_count="$(printf '%s\n' "${out_bare}" | grep -cE '^stack_trace=[1-9][0-9]* frames$' || true)"
if [ "${header_count}" -ne 1 ]; then
  fail "AC2 expected exactly one 'stack_trace=<N> frames' header, got ${header_count}"
fi
frame_count="$(printf '%s\n' "${out_bare}" | grep -cE ':[0-9]+ in ' || true)"
if [ "${frame_count}" -lt 1 ]; then
  fail "AC2 expected at least one '<file>:<line> in <function>' frame"
fi
if ! printf '%s\n' "${out_bare}" | grep -q '^bar1.go:41 in Func1$'; then
  fail "AC2 frame format mismatch (expected 'bar1.go:41 in Func1')"
fi

# AC5 cap: exactly 30 frame lines; the 31st+ fixture frames must be absent.
cap_count="$(printf '%s\n' "${out_bare}" | grep -cE '^[^=]+:[0-9]+ in ' || true)"
if [ "${cap_count}" -ne 30 ]; then
  fail "AC5 expected exactly 30 capped frame lines, got ${cap_count}"
fi
if printf '%s\n' "${out_bare}" | grep -q 'bar31.go'; then
  fail "AC5 frames past the 30-frame cap leaked into output"
fi

# AC5 no-PII: no quoted JSON keys, no raw payload, no context sentinel.
if printf '%s\n' "${out_bare}" | grep -qE '"context"|"frames"|stacktrace'; then
  fail "AC5 raw JSON keys leaked into output"
fi
if printf '%s\n' "${out_bare}" | grep -q 'RAW-CONTEXT-SENTINEL'; then
  fail "AC5 fixture context sentinel leaked into output"
fi

# ---- Failure paths (AC4): metadata intact + exactly one marker each ----
check_failure() {
  local name="$1"
  local out="$2"
  local reason="$3"
  local keys markers
  keys="$(printf '%s\n' "${out}" | grep -cE '^(short_id|status|live_event_count|first_seen|last_seen|users_impacted|title)=' || true)"
  if [ "${keys}" -ne 7 ]; then
    fail "${name}: expected 7 metadata keys, got ${keys}"
  fi
  markers="$(printf '%s\n' "${out}" | grep -cE '^stack_trace unavailable' || true)"
  if [ "${markers}" -ne 1 ]; then
    fail "${name}: expected exactly one 'stack_trace unavailable' marker, got ${markers}"
  fi
  if [ "$(printf '%s\n' "${out}" | grep -cE "^stack_trace unavailable \(${reason}\)$" || true)" -ne 1 ]; then
    fail "${name}: expected exactly one 'stack_trace unavailable (${reason})' marker"
  fi
}

if ! out_404="$(bash "${SCRIPT}" 789 2>&1)"; then
  fail "AC4 no-events stub exited non-zero"
fi
check_failure "AC4 no events (404)" "${out_404}" "no events"

if ! out_noexc="$(bash "${SCRIPT}" 456 2>&1)"; then
  fail "AC4 no-exception-entry stub exited non-zero"
fi
check_failure "AC4 no exception entry" "${out_noexc}" "no exception entry"

if ! out_auth="$(bash "${SCRIPT}" 999 2>&1)"; then
  fail "AC4 auth stub exited non-zero"
fi
check_failure "AC4 auth (401)" "${out_auth}" "auth"

echo "PASS: all AC1-AC5 assertions passed"
