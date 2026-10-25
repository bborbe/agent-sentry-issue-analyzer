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
# Run: bash scripts/test-vault-list.sh

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
VAULT_LIST="${SCRIPT_DIR}/vault-list.sh"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

fails=0
pass() { printf 'ok   %s\n' "$1"; }
fail() {
	printf 'FAIL %s\n' "$1" >&2
	fails=$((fails + 1))
}

# serve <status> <body> — starts a one-shot server, sets PORT and SERVER_PID.
# Deliberately NOT called in a command substitution: a subshell would swallow
# SERVER_PID and `wait` would then read an unbound variable under `set -u`.
# The port is chosen by the OS (bind 0) so parallel runs cannot collide.
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
	# Wait for the port line rather than sleeping a fixed amount.
	for _ in $(seq 1 100); do
		[ -s "$WORK/port" ] && break
		sleep 0.05
	done
	if [ ! -s "$WORK/port" ]; then
		cat "$WORK/server.err" >&2
		wait "$SERVER_PID" 2>/dev/null
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
	wait "$SERVER_PID" 2>/dev/null
}

# --- happy path: a well-shaped answer is listed ------------------------------
run_against 200 '{"items":[{"path":"25 Tasks/A.md"},{"path":"25 Tasks/B.md"}],"total":2}'
if [ "$RC" -eq 0 ] && [ "$OUT" = "$(printf '25 Tasks/A.md\n25 Tasks/B.md')" ]; then
	pass "well-shaped answer lists both paths"
else
	fail "well-shaped answer lists both paths (rc=$RC out=$OUT err=$ERR)"
fi

# --- an error envelope must NOT read as an empty listing ---------------------
run_against 200 '{"error":"unauthorized"}'
if [ "$RC" -ne 0 ] && [ -z "$OUT" ]; then
	pass "error envelope fails instead of listing nothing"
else
	fail "error envelope fails instead of listing nothing (rc=$RC out=$OUT)"
fi

# --- a missing items key must NOT read as an empty listing ------------------
run_against 200 '{"total":0,"warnings":[]}'
if [ "$RC" -ne 0 ] && [ -z "$OUT" ]; then
	pass "missing items key fails instead of listing nothing"
else
	fail "missing items key fails instead of listing nothing (rc=$RC out=$OUT)"
fi

# --- items of the wrong type must NOT read as an empty listing --------------
run_against 200 '{"items":"not-a-list","total":0}'
if [ "$RC" -ne 0 ] && [ -z "$OUT" ]; then
	pass "non-list items fails instead of listing nothing"
else
	fail "non-list items fails instead of listing nothing (rc=$RC out=$OUT)"
fi

# --- a non-JSON body must NOT read as an empty listing ----------------------
run_against 200 'this is not json'
if [ "$RC" -ne 0 ] && [ -z "$OUT" ]; then
	pass "non-JSON body fails instead of listing nothing"
else
	fail "non-JSON body fails instead of listing nothing (rc=$RC out=$OUT)"
fi

# --- a transport failure must NOT read as an empty listing ------------------
# No server at all: curl fails, pipefail carries it, the caller sees non-zero.
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
