#!/usr/bin/env bash
# Deterministic mock-API test for scripts/jira-read.sh.
#
# Serves fixture Jira search payloads from an embedded python3 http.server bound
# to 127.0.0.1 on an ephemeral port, then drives the script under test against
# http://127.0.0.1:<port> and asserts the output contract:
#   * a `returned=<n>` header followed by one tab-separated line per issue
#     (<KEY> <statusCategory.key> <status.name> <summary>)
#   * `returned=0` on an empty result set, with exit 0 - an empty search is a
#     measurement, not a failure
#   * a 401 exits non-zero and prints NOTHING to stdout, so a failed call can
#     never be mistaken for "no open ticket matches"
#   * the JQL reaches the server URL-decoded and byte-identical, including the
#     spaces, quotes and `!=` the verdict query uses
#   * the request path is the API-gateway search endpoint, not the site URL
#   * tabs and newlines in a summary cannot break the line contract
#   * missing required env fails before any request is made
#
# Container-executable: python3 + curl + bash only; no external network, no
# real credentials (dummy JIRA_API_TOKEN=x). Exits non-zero with a
# 'FAIL: <name>' message on the first assertion miss.

set -euo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
script="$here/jira-read.sh"

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

cat > "$work/server.py" <<'PY'
import json, sys, threading
from http.server import BaseHTTPRequestHandler, HTTPServer
from urllib.parse import urlparse, parse_qs

FIXTURES = {
    # two open issues; the second carries a tab and a newline in its summary
    "TWO": {
        "issues": [
            {"key": "BRO-1", "fields": {"summary": "Circuit breaker tripped",
                                        "status": {"name": "Open",
                                                   "statusCategory": {"key": "new"}}}},
            {"key": "BRO-2", "fields": {"summary": "bad\tsummary\nhere",
                                        "status": {"name": "In Bearbeitung",
                                                   "statusCategory": {"key": "indeterminate"}}}},
        ]
    },
    "NONE": {"issues": []},
}

state = {"jql": None, "path": None, "auth": None}

class H(BaseHTTPRequestHandler):
    def log_message(self, *a):
        pass

    def do_GET(self):
        parsed = urlparse(self.path)
        state["path"] = parsed.path
        state["auth"] = self.headers.get("Authorization")
        qs = parse_qs(parsed.query)
        state["jql"] = (qs.get("jql") or [None])[0]
        jql = state["jql"] or ""

        if "UNAUTHORIZED" in jql:
            self.send_response(401)
            self.end_headers()
            self.wfile.write(b'{"errorMessages":["unauthorized"]}')
            return

        body = json.dumps(FIXTURES["NONE" if "NOTHING" in jql else "TWO"]).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_POST(self):
        self.send_response(405)
        self.end_headers()

srv = HTTPServer(("127.0.0.1", 0), H)
print(srv.server_port, flush=True)

def dump_state():
    with open(sys.argv[1], "w") as fh:
        json.dump(state, fh)

# Dump on every request by wrapping the handler.
_orig = H.do_GET
def do_GET(self):
    _orig(self)
    dump_state()
H.do_GET = do_GET

srv.serve_forever()
PY

python3 "$work/server.py" "$work/state.json" > "$work/port" &
server_pid=$!
# disown keeps bash from printing a job-control "Terminated" line when the EXIT
# trap kills the server, which would otherwise land in the test's stderr.
disown "$server_pid" 2>/dev/null || true
trap 'kill "$server_pid" 2>/dev/null || true; rm -rf "$work"' EXIT

for _ in $(seq 1 100); do
  [ -s "$work/port" ] && break
  sleep 0.05
done
port="$(head -1 "$work/port")"
[ -n "$port" ] || { echo "FAIL: mock server did not start" >&2; exit 1; }

fail() { echo "FAIL: $1" >&2; exit 1; }

run() { # run <jql> ; stdout in $work/out, exit code in $rc
  set +e
  JIRA_CLOUD_ID=cloud-123 \
  JIRA_EMAIL=agent@example.com \
  JIRA_API_TOKEN=dummy \
  JIRA_GATEWAY_URL="http://127.0.0.1:$port" \
  bash "$script" "$1" > "$work/out" 2> "$work/err"
  rc=$?
  set -e
}

# 1. two issues, exact line contract
run 'project = BRO AND statusCategory != Done'
[ "$rc" -eq 0 ] || fail "two-issue search exited $rc (stderr: $(cat "$work/err"))"
expected=$'returned=2\nBRO-1\tnew\tOpen\tCircuit breaker tripped\nBRO-2\tindeterminate\tIn Bearbeitung\tbad summary here'
[ "$(cat "$work/out")" = "$expected" ] || fail "two-issue output mismatch: $(cat "$work/out")"

# 2. the request path is the gateway search endpoint
grep -q '/ex/jira/cloud-123/rest/api/3/search/jql' "$work/state.json" \
  || fail "request did not hit the gateway search endpoint: $(cat "$work/state.json")"

# 3. the JQL arrives byte-identical (URL-encoding round-trips)
grep -qF '"project = BRO AND statusCategory != Done"' "$work/state.json" \
  || fail "jql not transmitted verbatim: $(cat "$work/state.json")"

# 4. empty result is a measurement, not a failure
run 'project = BRO AND NOTHING'
[ "$rc" -eq 0 ] || fail "empty search exited $rc"
[ "$(cat "$work/out")" = "returned=0" ] || fail "empty search output: $(cat "$work/out")"

# 5. auth failure exits non-zero and prints nothing to stdout
run 'UNAUTHORIZED'
[ "$rc" -ne 0 ] || fail "401 exited 0"
if [ -s "$work/out" ]; then fail "401 wrote to stdout: $(cat "$work/out")"; fi

# 6. a missing required env fails before any request
set +e
JIRA_CLOUD_ID=cloud-123 JIRA_EMAIL=agent@example.com \
  bash "$script" 'project = BRO' > "$work/out" 2> "$work/err"
rc=$?
set -e
[ "$rc" -ne 0 ] || fail "missing JIRA_API_TOKEN exited 0"
grep -q 'JIRA_API_TOKEN is required' "$work/err" || fail "missing-token error: $(cat "$work/err")"

# 7. no arguments is a usage error
set +e
JIRA_CLOUD_ID=c JIRA_EMAIL=e JIRA_API_TOKEN=t bash "$script" > "$work/out" 2> "$work/err"
rc=$?
set -e
[ "$rc" -ne 0 ] || fail "no-argument invocation exited 0"
grep -q 'Usage: jira-read.sh' "$work/err" || fail "usage error: $(cat "$work/err")"

echo "OK: jira-read.sh contract holds"
