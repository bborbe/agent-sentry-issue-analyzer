#!/usr/bin/env bash
# Deterministic mock-API test for scripts/sentry-read.sh.
#
# Serves fixture Sentry metadata + events/latest payloads from an embedded
# python3 http.server bound to 127.0.0.1 on an ephemeral port, then drives the
# script under test against http://127.0.0.1:<port> and asserts the AC1-AC8
# output contract from specs/in-progress/001-bug-unanalyzable-overtrigger.md:
#   * 7 metadata keys in their frozen order (AC6)
#   * <path>:<line> in <function> in_app=<0|1|unknown> frame lines under a
#     stack_trace=<N> frames header, and <path> in <function> in_app=<0|1|unknown>
#     lines for frames without a usable integer lineno; <path> is the
#     repo-relative Sentry filename, or a basename when no relative path exists
#     in the payload (AC1/AC2; the in_app fixture on issue 111 and the
#     lineno-less fixture on issue 222 lock the relative-path shape, the
#     string-lineno / string-in_app schema-drift paths, and the abs_path-only
#     basename branch; the three-state fixtures on issues 444/555 lock the
#     in_app=<0|1|unknown> mapping — in_app=None (and absent/non-boolean
#     values) emit unknown, never collapsed into 0 — and the truthful
#     root_cause_in_app fallback)
#   * a root_cause_* pointer block after the frame lines naming the deepest
#     first-party frame, falling back to the deepest frame overall when none is
#     first-party; root_cause_line is present only when the chosen frame has an
#     integer lineno (AC3/AC4/AC5)
#   * a fixed 30-frame cap, with the 31st+ fixture frames absent (AC6)
#   * no raw JSON keys, no fixture context sentinel, no absolute path, and
#     every emitted value single-line on stdout (AC7 no-PII)
#   * bare numeric id and full-URL invocations produce byte-identical output
#   * failure stubs (404 / no exception entry / 401) each emit exactly one
#     'stack_trace unavailable (<reason>)' marker with the metadata intact
#   * an exception entry with zero frames emits 'no frames', distinct from
#     'no exception entry' (empty-frames fixture on issue 333; a null
#     stacktrace payload — an exception entry present but values[0].stacktrace
#     is null — degrades the same way, on issue 666)
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
                "in_app": False,
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
        elif issue == "222":
            # Lineno-less frames (real nuke shape: issue 6727724202). None has an
            # integer lineno; frame 4 carries a string lineno ("42") to lock the
            # schema-drift path; frame 5 carries a newline+equals in `function`
            # to lock single-line stripping; frame 3 carries a string in_app
            # ("true") and frame 5 a numeric in_app (1) to lock the flag's
            # schema-drift vs numeric-tolerance paths; frame 6 is abs_path-only
            # to lock the basename branch when no relative path exists.
            self._send(200, {"entries": [
                {"type": "exception", "data": {"values": [
                    {"stacktrace": {"frames": [
                        {"filename": "kafka/coordinator/consumer.py", "abs_path": None, "lineno": None, "function": "_maybe_auto_commit_offsets_sync", "in_app": False, "context": ["RAW-CONTEXT-SENTINEL line 1"]},
                        {"filename": "kafka/protocol/fetch.py", "abs_path": None, "lineno": None, "function": "FetchRequest", "in_app": False, "context": ["RAW-CONTEXT-SENTINEL line 2"]},
                        {"filename": "kafka/consumer/group.py", "abs_path": None, "lineno": None, "function": "_poll_once", "in_app": "true", "context": ["RAW-CONTEXT-SENTINEL line 3"]},
                        {"filename": "nuke/worker.py", "abs_path": None, "lineno": "42", "function": "sync_worker", "in_app": True, "context": ["RAW-CONTEXT-SENTINEL line 4"]},
                        {"filename": "nuke/backlog.py", "abs_path": None, "lineno": None, "function": "dispatch\nCOUNT=999", "in_app": 1, "context": ["RAW-CONTEXT-SENTINEL line 5"]},
                        {"abs_path": "/usr/lib/python3.11/site-packages/rpyc/core/netref.py", "filename": None, "lineno": None, "function": "__call__", "in_app": False},
                    ]}}
                ]}}
            ]})
        elif issue == "333":
            # Exception entry whose first value's stacktrace has zero frames.
            self._send(200, {"entries": [
                {"type": "exception", "data": {"values": [
                    {"stacktrace": {"frames": []}}
                ]}}
            ]})
        elif issue == "111":
            # First-party frame (innermost, repo-relative filename, boolean
            # in_app) above a third-party rpyc frame — locks the in_app flag,
            # the repo-relative path shape, and the deepest-first-party pointer.
            self._send(200, {"entries": [
                {"type": "exception", "data": {"values": [
                    {"stacktrace": {"frames": [
                        {"filename": "nuke/worker.py", "abs_path": None, "lineno": 102, "function": "select_symbol", "in_app": True},
                        {"filename": "rpyc/core/netref.py", "abs_path": None, "lineno": 55, "function": "__call__", "in_app": False},
                    ]}}
                ]}}
            ]})
        elif issue == "444":
            # Three-state in_app fixture: one unclassified frame (in_app None),
            # one explicit third-party (False), one explicit first-party (True)
            # — locks the None -> unknown mapping (never collapsed into 0) and
            # the pointer naming the first in_app=1 frame.
            self._send(200, {"entries": [
                {"type": "exception", "data": {"values": [
                    {"stacktrace": {"frames": [
                        {"filename": "mt5/connector/mt5linux.py", "abs_path": None, "lineno": 88, "function": "shutdown", "in_app": None},
                        {"filename": "rpyc/core/netref.py", "abs_path": None, "lineno": 55, "function": "__call__", "in_app": False},
                        {"filename": "pkg/kafka.py", "abs_path": None, "lineno": 21, "function": "send", "in_app": True},
                    ]}}
                ]}}
            ]})
        elif issue == "555":
            # All-frames-unclassified fixture: every frame carries in_app None
            # and none is first-party — locks the truthful root_cause_in_app=
            # unknown fallback (the pointer is NOT forced to 0 when no in_app=1
            # frame exists).
            self._send(200, {"entries": [
                {"type": "exception", "data": {"values": [
                    {"stacktrace": {"frames": [
                        {"filename": "rpyc/core/netref.py", "abs_path": None, "lineno": 55, "function": "__call__", "in_app": None},
                        {"filename": "rpyc/core/protocol.py", "abs_path": None, "lineno": 42, "function": "dispatch", "in_app": None},
                    ]}}
                ]}}
            ]})
        elif issue == "666":
            # Null-stacktrace fixture: an exception entry IS present but its
            # first value's stacktrace is JSON null — locks the degrade to
            # 'no frames', not the misleading 'no exception entry'.
            self._send(200, {"entries": [
                {"type": "exception", "data": {"values": [
                    {"stacktrace": None}
                ]}}
            ]})
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
if ! printf '%s\n' "${out_bare}" | grep -q '^pkg/foo/bar1.go:41 in Func1 in_app=0$'; then
  fail "AC2 frame format mismatch (expected 'pkg/foo/bar1.go:41 in Func1 in_app=0')"
fi

# AC5 cap: exactly 30 frame lines; the 31st+ fixture frames must be absent.
cap_count="$(printf '%s\n' "${out_bare}" | grep -cE '^[^=]+:[0-9]+ in ' || true)"
if [ "${cap_count}" -ne 30 ]; then
  fail "AC5 expected exactly 30 capped frame lines, got ${cap_count}"
fi
if printf '%s\n' "${out_bare}" | grep -q 'bar31.go'; then
  fail "AC5 frames past the 30-frame cap leaked into output"
fi

# AC5 no-PII: no absolute path leaks (fixture abs_path is /usr/src/app/...).
if printf '%s\n' "${out_bare}" | grep -q '/usr/src'; then
  fail "AC5 absolute path leaked from fixture abs_path into output"
fi

# AC4 fallback pointer: issue-123 has zero in_app=1 frames, so the pointer must
# name the deepest frame overall with root_cause_in_app=0.
if ! printf '%s\n' "${out_bare}" | grep -q '^root_cause_file=pkg/foo/bar1.go$'; then
  fail "AC4 fallback pointer did not name the deepest frame overall (root_cause_file)"
fi
if ! printf '%s\n' "${out_bare}" | grep -q '^root_cause_line=41$'; then
  fail "AC4 fallback pointer missing root_cause_line for the deepest frame overall"
fi
if ! printf '%s\n' "${out_bare}" | grep -q '^root_cause_function=Func1$'; then
  fail "AC4 fallback pointer missing root_cause_function for the deepest frame overall"
fi
if ! printf '%s\n' "${out_bare}" | grep -q '^root_cause_in_app=0$'; then
  fail "AC4 fallback pointer missing root_cause_in_app=0 (no first-party frame)"
fi

# AC5 no-PII: no quoted JSON keys, no raw payload, no context sentinel.
if printf '%s\n' "${out_bare}" | grep -qE '"context"|"frames"|stacktrace'; then
  fail "AC5 raw JSON keys leaked into output"
fi
if printf '%s\n' "${out_bare}" | grep -q 'RAW-CONTEXT-SENTINEL'; then
  fail "AC5 fixture context sentinel leaked into output"
fi

# ---- AC1/AC2/AC3/AC7: in_app flag, repo-relative path, pointer block (111) ----
if ! out_inapp="$(bash "${SCRIPT}" 111 2>&1)"; then
  fail "AC1 in_app fixture exited non-zero"
fi
if ! printf '%s\n' "${out_inapp}" | grep -q '^stack_trace=2 frames$'; then
  fail "AC1 in_app fixture expected exactly one 'stack_trace=2 frames' header"
fi
if [ "$(printf '%s\n' "${out_inapp}" | grep -c ' in_app=1$' || true)" -ne 1 ]; then
  fail "AC1 expected exactly one 'in_app=1' frame line"
fi
if [ "$(printf '%s\n' "${out_inapp}" | grep -c ' in_app=0$' || true)" -ne 1 ]; then
  fail "AC1 expected exactly one 'in_app=0' frame line"
fi
if ! printf '%s\n' "${out_inapp}" | grep -q '^nuke/worker.py:102 in select_symbol in_app=1$'; then
  fail "AC2 in_app fixture expected 'nuke/worker.py:102 in select_symbol in_app=1'"
fi
if ! printf '%s\n' "${out_inapp}" | grep -q '^rpyc/core/netref.py:55 in __call__ in_app=0$'; then
  fail "AC2 in_app fixture expected 'rpyc/core/netref.py:55 in __call__ in_app=0'"
fi
if printf '%s\n' "${out_inapp}" | grep -q '^worker.py:'; then
  fail "AC2 bare basename 'worker.py' leaked where repo-relative path expected"
fi
if printf '%s\n' "${out_inapp}" | grep -q '^netref.py:'; then
  fail "AC2 bare basename 'netref.py' leaked where repo-relative path expected"
fi
if ! printf '%s\n' "${out_inapp}" | grep -q '^root_cause_file=nuke/worker.py$'; then
  fail "AC3 pointer did not name the deepest first-party frame (root_cause_file)"
fi
if ! printf '%s\n' "${out_inapp}" | grep -q '^root_cause_line=102$'; then
  fail "AC3 pointer missing root_cause_line for the deepest first-party frame"
fi
if ! printf '%s\n' "${out_inapp}" | grep -q '^root_cause_function=select_symbol$'; then
  fail "AC3 pointer missing root_cause_function for the deepest first-party frame"
fi
if ! printf '%s\n' "${out_inapp}" | grep -q '^root_cause_in_app=1$'; then
  fail "AC3 pointer missing root_cause_in_app=1 for the deepest first-party frame"
fi
if printf '%s\n' "${out_inapp}" | grep -qE '"context"|"frames"|stacktrace|RAW-CONTEXT-SENTINEL'; then
  fail "AC7 raw JSON keys or context sentinel leaked from in_app fixture"
fi

# ---- AC1: three-state in_app mapping (issue 444) ----
if ! out_three="$(bash "${SCRIPT}" 444 2>&1)"; then
  fail "AC1 three-state fixture exited non-zero"
fi
if ! printf '%s\n' "${out_three}" | grep -q '^stack_trace=3 frames$'; then
  fail "AC1 three-state fixture expected exactly one 'stack_trace=3 frames' header"
fi
if ! printf '%s\n' "${out_three}" | grep -q '^mt5/connector/mt5linux.py:88 in shutdown in_app=unknown$'; then
  fail "AC1 in_app=None frame not emitted as in_app=unknown (expected 'mt5/connector/mt5linux.py:88 in shutdown in_app=unknown')"
fi
if ! printf '%s\n' "${out_three}" | grep -q '^rpyc/core/netref.py:55 in __call__ in_app=0$'; then
  fail "AC1 in_app=False frame not emitted as in_app=0 (expected 'rpyc/core/netref.py:55 in __call__ in_app=0')"
fi
if ! printf '%s\n' "${out_three}" | grep -q '^pkg/kafka.py:21 in send in_app=1$'; then
  fail "AC1 in_app=True frame not emitted as in_app=1 (expected 'pkg/kafka.py:21 in send in_app=1')"
fi
if ! printf '%s\n' "${out_three}" | grep -q '^root_cause_file=pkg/kafka.py$'; then
  fail "AC1 pointer did not name the first in_app=1 frame (root_cause_file)"
fi
if ! printf '%s\n' "${out_three}" | grep -q '^root_cause_line=21$'; then
  fail "AC1 pointer missing root_cause_line for the first in_app=1 frame"
fi
if ! printf '%s\n' "${out_three}" | grep -q '^root_cause_function=send$'; then
  fail "AC1 pointer missing root_cause_function for the first in_app=1 frame"
fi
if ! printf '%s\n' "${out_three}" | grep -q '^root_cause_in_app=1$'; then
  fail "AC1 pointer missing root_cause_in_app=1 for the first in_app=1 frame"
fi
if printf '%s\n' "${out_three}" | grep -qE '"context"|"frames"|stacktrace|RAW-CONTEXT-SENTINEL'; then
  fail "AC7 raw JSON keys or context sentinel leaked from three-state fixture"
fi

# ---- AC1: all-unknown fallback pointer is truthful (issue 555) ----
if ! out_unknown="$(bash "${SCRIPT}" 555 2>&1)"; then
  fail "AC1 all-unknown fixture exited non-zero"
fi
if ! printf '%s\n' "${out_unknown}" | grep -q '^stack_trace=2 frames$'; then
  fail "AC1 all-unknown fixture expected exactly one 'stack_trace=2 frames' header"
fi
if [ "$(printf '%s\n' "${out_unknown}" | grep -c ' in_app=unknown$' || true)" -ne 2 ]; then
  fail "AC1 all-unknown fixture expected every frame line to be in_app=unknown"
fi
if printf '%s\n' "${out_unknown}" | grep -qE ' in_app=(0|1)$'; then
  fail "AC1 all-unknown fixture emitted a classified in_app flag for an unclassified frame"
fi
if ! printf '%s\n' "${out_unknown}" | grep -q '^rpyc/core/netref.py:55 in __call__ in_app=unknown$'; then
  fail "AC1 all-unknown fixture expected 'rpyc/core/netref.py:55 in __call__ in_app=unknown'"
fi
if ! printf '%s\n' "${out_unknown}" | grep -q '^root_cause_file=rpyc/core/netref.py$'; then
  fail "AC1 all-unknown fallback pointer did not name the first frame (root_cause_file)"
fi
if ! printf '%s\n' "${out_unknown}" | grep -q '^root_cause_line=55$'; then
  fail "AC1 all-unknown fallback pointer missing root_cause_line for the first frame"
fi
if ! printf '%s\n' "${out_unknown}" | grep -q '^root_cause_function=__call__$'; then
  fail "AC1 all-unknown fallback pointer missing root_cause_function for the first frame"
fi
if ! printf '%s\n' "${out_unknown}" | grep -q '^root_cause_in_app=unknown$'; then
  fail "AC1 all-unknown fallback pointer not truthful (expected root_cause_in_app=unknown, not forced to 0)"
fi
if printf '%s\n' "${out_unknown}" | grep -q '^root_cause_in_app=0$'; then
  fail "AC1 all-unknown fallback pointer forced to 0 despite no classified frame"
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

# ---- AC2: lineno-less frames emitted truthfully (issue 222) ----
if ! out_nolineno="$(bash "${SCRIPT}" 222 2>&1)"; then
  fail "AC2 lineno-less fixture exited non-zero"
fi
key_count="$(printf '%s\n' "${out_nolineno}" | grep -cE '^(short_id|status|live_event_count|first_seen|last_seen|users_impacted|title)=' || true)"
if [ "${key_count}" -ne 7 ]; then
  fail "AC2 lineno-less fixture expected 7 metadata keys, got ${key_count}"
fi
nl_header="$(printf '%s\n' "${out_nolineno}" | grep -cE '^stack_trace=[1-9][0-9]* frames$' || true)"
if [ "${nl_header}" -ne 1 ]; then
  fail "AC2 lineno-less fixture expected exactly one 'stack_trace=<N> frames' header, got ${nl_header}"
fi
if ! printf '%s\n' "${out_nolineno}" | grep -q '^stack_trace=6 frames$'; then
  fail "AC2 lineno-less fixture expected 'stack_trace=6 frames' header"
fi
nl_frames="$(printf '%s\n' "${out_nolineno}" | grep -cE '^[^=]+ in ' || true)"
if [ "${nl_frames}" -ne 6 ]; then
  fail "AC2 lineno-less fixture expected 6 'path in function' frame lines, got ${nl_frames}"
fi
if ! printf '%s\n' "${out_nolineno}" | grep -q '^kafka/coordinator/consumer.py in _maybe_auto_commit_offsets_sync in_app=0$'; then
  fail "AC2 lineno-less frame format mismatch (expected 'kafka/coordinator/consumer.py in _maybe_auto_commit_offsets_sync in_app=0')"
fi
if ! printf '%s\n' "${out_nolineno}" | grep -q '^nuke/worker.py in sync_worker in_app=1$'; then
  fail "AC2 string-lineno frame not treated as lineno-less (expected 'nuke/worker.py in sync_worker in_app=1')"
fi
if printf '%s\n' "${out_nolineno}" | grep -qE ':[0-9]+ in '; then
  fail "AC2 lineno-less fixture leaked a ':line' frame"
fi
if printf '%s\n' "${out_nolineno}" | grep -q 'no exception entry'; then
  fail "AC2 lineno-less fixture emitted 'no exception entry' despite frames existing"
fi
if printf '%s\n' "${out_nolineno}" | grep -q 'no frames'; then
  fail "AC2 lineno-less fixture emitted 'no frames' despite frames existing"
fi
if printf '%s\n' "${out_nolineno}" | grep -qE '"context"|"frames"|stacktrace'; then
  fail "AC4 raw JSON keys leaked from lineno-less fixture"
fi
if printf '%s\n' "${out_nolineno}" | grep -q 'RAW-CONTEXT-SENTINEL'; then
  fail "AC4 fixture context sentinel leaked from lineno-less fixture"
fi
if ! printf '%s\n' "${out_nolineno}" | grep -q '^nuke/backlog.py in dispatch COUNT=999 in_app=1$'; then
  fail "AC4 newline in frame function was not neutralised to a single line (expected 'nuke/backlog.py in dispatch COUNT=999 in_app=1')"
fi
if ! printf '%s\n' "${out_nolineno}" | grep -q '^netref.py in __call__ in_app=0$'; then
  fail "AC2 abs_path-only frame did not emit its basename (expected 'netref.py in __call__ in_app=0')"
fi
if ! printf '%s\n' "${out_nolineno}" | grep -q '^kafka/consumer/group.py in _poll_once in_app=unknown$'; then
  fail "AC1 string-in_app schema drift not classified unknown (expected 'kafka/consumer/group.py in _poll_once in_app=unknown')"
fi
if printf '%s\n' "${out_nolineno}" | grep -q '^COUNT=999$'; then
  fail "AC4 newline in frame function injected a standalone line"
fi

# AC5 pointer: deepest first-party frame is frame 4 (nuke/worker.py), which has
# a string lineno — root_cause_line must be ABSENT, and frame 5 (also first-party)
# must not be picked.
if ! printf '%s\n' "${out_nolineno}" | grep -q '^root_cause_file=nuke/worker.py$'; then
  fail "AC5 pointer did not name the deepest first-party frame (root_cause_file)"
fi
if ! printf '%s\n' "${out_nolineno}" | grep -q '^root_cause_function=sync_worker$'; then
  fail "AC5 pointer missing root_cause_function for the deepest first-party frame"
fi
if ! printf '%s\n' "${out_nolineno}" | grep -q '^root_cause_in_app=1$'; then
  fail "AC5 pointer missing root_cause_in_app=1 for the deepest first-party frame"
fi
if printf '%s\n' "${out_nolineno}" | grep -q '^root_cause_line='; then
  fail "AC5 root_cause_line emitted for a frame without an integer lineno"
fi
if printf '%s\n' "${out_nolineno}" | grep -q '^root_cause_file=nuke/backlog.py$'; then
  fail "AC3 pointer did not pick the deepest first-party frame"
fi

# ---- AC6: no-frames marker distinct from no-exception-entry (issue 333) ----
if ! out_noframes="$(bash "${SCRIPT}" 333 2>&1)"; then
  fail "AC6 no-frames stub exited non-zero"
fi
check_failure "AC6 no frames" "${out_noframes}" "no frames"
if printf '%s\n' "${out_noframes}" | grep -q 'no exception entry'; then
  fail "AC6 no-frames stub emitted 'no exception entry' instead of 'no frames'"
fi

# ---- AC6: null stacktrace degrades to 'no frames' (issue 666) ----
if ! out_nullst="$(bash "${SCRIPT}" 666 2>&1)"; then
  fail "AC6 null-stacktrace stub exited non-zero"
fi
check_failure "AC6 null stacktrace" "${out_nullst}" "no frames"
if printf '%s\n' "${out_nullst}" | grep -q 'no exception entry'; then
  fail "AC6 null-stacktrace stub emitted 'no exception entry' instead of 'no frames'"
fi

echo "PASS: all assertions passed"
