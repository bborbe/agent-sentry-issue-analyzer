#!/usr/bin/env bash
# Regression test for scripts/vault-list.sh's response-shape guards.
#
# The script's own header says a failed listing MUST be distinguishable from an
# empty one, because the caller gates on the count it produces: an empty answer
# is a legitimate "this vault tracks nothing", and a wrong-shaped answer read as
# empty would make every run publish its alerts again as duplicates.
#
# The guards are the safety mechanism the typed-API migration argues for, so
# they are exercised here through the REAL script against a mock HTTP server —
# not by testing a parser function in isolation — because the failure this
# prevents lives in how the script composes curl, the parser and `pipefail`.
#
# Each failing case asserts the DISTINCT diagnostic it should produce, not just
# a non-zero exit: a bare `rc != 0` is satisfied by any unrelated crash, which
# would let a broken parser pass this suite.
#
# Run: bash scripts/test-vault-list.sh

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
VAULT_LIST="${SCRIPT_DIR}/vault-list.sh"
WORK="$(mktemp -d)"
SERVER_PID=""
# The mock server is one-shot, so a case that never reaches its request would
# leave it blocked in handle_request() forever. The trap kills it, and every
# wait is bounded, so a hang in the script under test fails this suite instead
# of hanging CI.
cleanup() {
	if [ -n "${SERVER_PID}" ] && kill -0 "${SERVER_PID}" 2>/dev/null; then
		kill "${SERVER_PID}" 2>/dev/null
		wait "${SERVER_PID}" 2>/dev/null
	fi
	rm -rf "$WORK"
}
trap cleanup EXIT

fails=0
pass() { printf 'ok   %s\n' "$1"; }
fail() {
	printf 'FAIL %s\n' "$1" >&2
	fails=$((fails + 1))
}

# wait_bounded <pid> — returns 1 (and kills) if the process outlives 5s.
wait_bounded() {
	local pid="$1" deadline=$((SECONDS + 5))
	while kill -0 "$pid" 2>/dev/null; do
		if [ "$SECONDS" -ge "$deadline" ]; then
			kill "$pid" 2>/dev/null
			return 1
		fi
		sleep 0.05
	done
	return 0
}

# serve <status> <body> — starts a one-shot server, sets PORT and SERVER_PID.
# Deliberately NOT called in a command substitution: a subshell would swallow
# SERVER_PID and `wait` would then read an unbound variable under `set -u`.
serve() {
	local status="$1" body="$2"
	cat >"$WORK/server.py" <<'PY'
import http.server, socketserver, sys
status = int(sys.argv[1])
body = sys.argv[2].encode()
class Handler(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)
    def log_message(self, *args):
        pass
with socketserver.TCPServer(("127.0.0.1", 0), Handler) as httpd:
    print(httpd.server_address[1], flush=True)
    httpd.handle_request()
PY
	rm -f "$WORK/port"
	python3 "$WORK/server.py" "$status" "$body" >"$WORK/port" 2>"$WORK/server.err" &
	SERVER_PID=$!
	for _ in $(seq 1 100); do
		[ -s "$WORK/port" ] && break
		sleep 0.05
	done
	if [ ! -s "$WORK/port" ]; then
		cat "$WORK/server.err" >&2
		return 1
	fi
	PORT="$(head -1 "$WORK/port")"
}

# run_against <status> <body> -> sets RC, OUT, ERR
run_against() {
	if ! serve "$1" "$2"; then
		RC=99
		OUT=""
		ERR="mock server failed to start"
		return
	fi
	OUT="$(VAULT_API_URL="http://127.0.0.1:${PORT}" bash "$VAULT_LIST" 2>"$WORK/stderr")"
	RC=$?
	ERR="$(cat "$WORK/stderr")"
	if ! wait_bounded "$SERVER_PID"; then
		fail "mock server did not exit — the script never issued its request"
	fi
	SERVER_PID=""
}

# expect_guard <label> <status> <body> <stderr-substring>
# Fails unless the script exits non-zero, prints nothing to stdout, AND names
# the specific diagnostic — so an unrelated crash cannot satisfy the case.
expect_guard() {
	local label="$1" status="$2" body="$3" want="$4"
	run_against "$status" "$body"
	if [ "$RC" -ne 0 ] && [ -z "$OUT" ] && [[ "$ERR" == *"$want"* ]]; then
		pass "$label"
	else
		fail "$label (rc=$RC out=$OUT err=$ERR want=$want)"
	fi
}

# --- happy path: a well-shaped answer is listed ------------------------------
run_against 200 '{"items":[{"path":"25 Tasks/A.md"},{"path":"25 Tasks/B.md"}],"total":2}'
if [ "$RC" -eq 0 ] && [ "$OUT" = "$(printf '25 Tasks/A.md\n25 Tasks/B.md')" ]; then
	pass "well-shaped answer lists both paths"
else
	fail "well-shaped answer lists both paths (rc=$RC out=$OUT err=$ERR)"
fi

# --- an error envelope must NOT read as an empty listing ---------------------
expect_guard "error envelope fails and names the refusal" 200 \
	'{"error":"unauthorized"}' "API refused the listing"

# --- a missing items key must NOT read as an empty listing ------------------
expect_guard "missing items key fails and names the missing field" 200 \
	'{"total":0,"warnings":[]}' "no items field"

# --- items of the wrong type must NOT read as an empty listing --------------
expect_guard "non-list items fails and names the actual type" 200 \
	'{"items":"not-a-list","total":0}' "items is str, not a list"

# --- a non-JSON body must NOT read as an empty listing ----------------------
expect_guard "non-JSON body fails and names the parse error" 200 \
	'this is not json' "response is not JSON"

# --- a JSON body that is not an object --------------------------------------
expect_guard "non-object JSON fails and says so" 200 \
	'[1,2,3]' "not a JSON object"

# --- a non-2xx status is refused by curl itself -----------------------------
run_against 401 '{"error":"unauthorized"}'
if [ "$RC" -ne 0 ] && [ -z "$OUT" ]; then
	pass "HTTP 401 fails instead of listing nothing"
else
	fail "HTTP 401 fails instead of listing nothing (rc=$RC out=$OUT)"
fi

# --- a malformed items entry still fails closed -----------------------------
# vault-list.sh indexes task["path"] unguarded, so this raises rather than
# emitting the clean diagnostic the other guards produce. It must still exit
# non-zero — the property the caller depends on — and this case pins that,
# recording the rough edge rather than pretending it is a clean refusal.
run_against 200 '{"items":[{"title":"no path here"}],"total":1}'
if [ "$RC" -ne 0 ]; then
	pass "malformed items entry fails closed"
else
	fail "malformed items entry fails closed (rc=$RC out=$OUT)"
fi

# --- a transport failure must NOT read as an empty listing ------------------
# Nothing listens on port 1 without root, so curl gets ECONNREFUSED.
OUT="$(VAULT_API_URL="http://127.0.0.1:1" bash "$VAULT_LIST" 2>"$WORK/stderr")"
RC=$?
if [ "$RC" -ne 0 ] && [ -z "$OUT" ]; then
	pass "unreachable API fails instead of listing nothing"
else
	fail "unreachable API fails instead of listing nothing (rc=$RC out=$OUT)"
fi

# --- an empty items list IS a legitimate answer -----------------------------
run_against 200 '{"items":[],"total":0}'
if [ "$RC" -eq 0 ] && [ -z "$OUT" ]; then
	pass "genuinely empty answer is a success with no output"
else
	fail "genuinely empty answer is a success with no output (rc=$RC out=$OUT err=$ERR)"
fi

if [ "$fails" -ne 0 ]; then
	printf '\n%d case(s) failed\n' "$fails" >&2
	exit 1
fi
printf '\nall cases passed\n'
