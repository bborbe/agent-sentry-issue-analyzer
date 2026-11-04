#!/usr/bin/env bash
# Regression test for scripts/sentry-create-tasks.sh's empty-listing guard.
#
# The guard used to treat ANY empty typed listing as a broken query. On a lane
# that has never published the vault genuinely tracks none of the three alert
# task types, so a healthy first run — every alert published, every file landed —
# reported `status=unobserved` and a successful publish could never be proved.
#
# Deleting the guard is not the fix: without it a filter that stopped matching
# re-creates every alert as a duplicate on every run, which is the 2026-09-13
# incident. The two populations are told apart by asking the same vault API how
# many tasks it holds AT ALL — so this test exercises BOTH arms plus the failure
# shapes that must still refuse:
#
#   empty typed + populated vault        -> observed=true   (the correct empty set)
#   empty typed + empty vault            -> observed=false  (cannot tell apart)
#   empty typed + population read fails  -> observed=false
#   empty typed + population non-numeric -> observed=false
#   typed listing fails                  -> observed=false
#   typed listing wrong-shaped (3 ways)  -> observed=false
#   populated typed listing              -> observed=true   (unchanged behaviour)
#
# It runs the REAL script and the REAL vault-list.sh against a mock HTTP server,
# not a parser in isolation, because the failure lives in how curl, the parser,
# the guard and `pipefail` compose. `/create-tasks` is replaced through the
# script's own CREATE_TASKS_BIN seam — that seam exists for this test.
#
# Run: bash scripts/test-sentry-create-tasks.sh

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
UNDER_TEST="${SCRIPT_DIR}/sentry-create-tasks.sh"
WORK="$(mktemp -d)"
SERVER_PID=""

cleanup() {
	if [ -n "${SERVER_PID}" ] && kill -0 "${SERVER_PID}" 2>/dev/null; then
		kill "${SERVER_PID}" 2>/dev/null
		wait "${SERVER_PID}" 2>/dev/null
	fi
	rm -rf "${WORK}"
}
trap cleanup EXIT

fails=0
pass() { printf 'ok   %s\n' "$1"; }
fail() {
	printf 'FAIL %s\n' "$1" >&2
	fails=$((fails + 1))
}

# The mock. It serves the Sentry issues API and the typed vault API from one
# port, and switches scenario by re-reading the JSON file on every request, so a
# case can be changed without restarting it.
cat > "${WORK}/mock.py" <<'PY'
import json, sys
from http.server import BaseHTTPRequestHandler, HTTPServer
from urllib.parse import urlparse, parse_qs

SCENARIO = sys.argv[1]
state = {"typed_calls": 0}


def load():
    with open(SCENARIO) as handle:
        return json.load(handle)


class Handler(BaseHTTPRequestHandler):
    def log_message(self, *args):
        pass

    def _send(self, status, body, headers=None):
        raw = body.encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(raw)))
        for name, value in (headers or {}).items():
            self.send_header(name, value)
        self.end_headers()
        self.wfile.write(raw)

    def do_GET(self):
        scenario = load()
        url = urlparse(self.path)
        query = parse_qs(url.query)

        if url.path.startswith("/api/0/organizations/"):
            status = scenario.get("sentry_status", 200)
            if status != 200:
                return self._send(status, '{"detail": "boom"}')
            # The script parses the Link header for its pagination cursor and
            # aborts on a reply that has none — the real API always sends one,
            # and results="false" is its "no more pages" signal.
            link = {
                "Link": '<http://127.0.0.1/cursor=abc>; rel="next"; results="false"'
            }
            return self._send(200, json.dumps(scenario.get("sentry", [])), link)

        if url.path == "/api/v1/tasks":
            if "task_type" in query:
                status = scenario.get("typed_status", 200)
                if status != 200:
                    return self._send(status, '{"error": "typed listing refused"}')
                if "typed_raw" in scenario:
                    return self._send(200, scenario["typed_raw"])
                state["typed_calls"] += 1
                items = scenario.get("typed", [])
                if state["typed_calls"] > 1:
                    items = scenario.get("typed_after", items)
                return self._send(
                    200, json.dumps({"items": items, "total": len(items)})
                )

            status = scenario.get("population_status", 200)
            if status != 200:
                return self._send(status, '{"error": "population refused"}')
            if "population_raw" in scenario:
                return self._send(200, scenario["population_raw"])
            items = scenario.get("population", [])
            total = scenario.get("population_total", len(items))
            return self._send(200, json.dumps({"items": items, "total": total}))

        return self._send(404, '{"error": "not found"}')


server = HTTPServer(("127.0.0.1", 0), Handler)
print(server.server_address[1], flush=True)
server.serve_forever()
PY

cat > "${WORK}/create-tasks" <<'STUB'
#!/usr/bin/env bash
# Stands in for the image's /create-tasks binary. The script only reads the
# published= count off stdout.
echo "create-tasks-result: published=1"
STUB
chmod +x "${WORK}/create-tasks"

# One alert, so `count>0` and the guard is reachable. lastSeen pins the dedup
# filename the script derives: "<shortId> - <UTC date>.md".
ALERT_JSON='[{"id":"1","shortId":"OCTOPUS-DEV-1","title":"boom","lastSeen":"2026-11-04T10:00:00Z","firstSeen":"2026-11-04T09:00:00Z","count":3,"status":"unresolved","userCount":1,"permalink":"https://example.invalid/1","project":{"slug":"octopus-dev"}}]'
TRACKED_JSON='[{"path":"/vault/tasks/OCTOPUS-DEV-1 - 2026-11-04.md"}]'

# run_case <name> <scenario-json>
# Starts a mock with that scenario, runs the real script, echoes its result line.
run_case() {
	local name="$1" scenario="$2"
	printf '%s' "${scenario}" > "${WORK}/scenario.json"
	rm -f "${WORK}/port"

	python3 "${WORK}/mock.py" "${WORK}/scenario.json" > "${WORK}/port" 2>"${WORK}/mock.err" &
	SERVER_PID=$!
	local deadline=$((SECONDS + 5))
	while [ ! -s "${WORK}/port" ]; do
		if [ "${SECONDS}" -ge "${deadline}" ]; then
			# run_case runs inside a command substitution, so this subshell's
			# SERVER_PID never reaches the parent's trap — kill it here or the
			# mock outlives the suite.
			kill "${SERVER_PID}" 2>/dev/null
			wait "${SERVER_PID}" 2>/dev/null
			SERVER_PID=""
			fail "${name}: mock server did not start"
			return 1
		fi
		sleep 0.05
	done
	local port
	port="$(head -1 "${WORK}/port")"

	SENTRY_API_TOKEN=test \
		KAFKA_BROKERS=127.0.0.1:1 \
		VAULT_API_URL="http://127.0.0.1:${port}" \
		SENTRY_URL="http://127.0.0.1:${port}" \
		SENTRY_ORG=seibert-group \
		TARGET_VAULT=seibert-agent \
		STAGE=dev \
		VAULT_POLL_ATTEMPTS=1 \
		VAULT_POLL_INTERVAL_SECONDS=0 \
		CREATE_TASKS_BIN="${WORK}/create-tasks" \
		bash "${UNDER_TEST}" > "${WORK}/out" 2> "${WORK}/err"
	local rc=$?

	kill "${SERVER_PID}" 2>/dev/null
	wait "${SERVER_PID}" 2>/dev/null
	SERVER_PID=""

	if [ "${rc}" -ne 0 ]; then
		printf 'rc=%s\n' "${rc}" > "${WORK}/result"
	else
		grep -h '^sentry-create-tasks-result:' "${WORK}/out" > "${WORK}/result" || true
	fi
	cat "${WORK}/result"
}

# expect <name> <file> <substring> — asserts the substring is present.
expect() {
	local name="$1" file="$2" needle="$3"
	if grep -qF -- "${needle}" "${file}"; then
		pass "${name}: ${needle}"
	else
		fail "${name}: expected '${needle}' in: $(tr '\n' ' ' < "${file}")"
	fi
}

# refuse <name> <file> <substring> — asserts the substring is absent.
refuse() {
	local name="$1" file="$2" needle="$3"
	if grep -qF -- "${needle}" "${file}"; then
		fail "${name}: must NOT contain '${needle}' in: $(tr '\n' ' ' < "${file}")"
	else
		pass "${name}: no '${needle}'"
	fi
}

# --- arm 1: empty typed listing, populated vault -> the correct empty set -----
result="$(run_case "empty-typed/populated-vault" \
	"{\"sentry\": ${ALERT_JSON}, \"typed\": [], \"typed_after\": ${TRACKED_JSON}, \"population\": [{\"path\": \"/vault/tasks/other.md\"}]}")"
printf '%s\n' "${result}" > "${WORK}/r"
expect "empty-typed/populated-vault" "${WORK}/r" "observed=true"
expect "empty-typed/populated-vault" "${WORK}/r" "expected_new=1"
refuse "empty-typed/populated-vault" "${WORK}/r" "status=unobserved"

# --- arm 2: empty typed listing, empty vault -> still unobservable -----------
result="$(run_case "empty-typed/empty-vault" \
	"{\"sentry\": ${ALERT_JSON}, \"typed\": [], \"population\": []}")"
printf '%s\n' "${result}" > "${WORK}/r"
expect "empty-typed/empty-vault" "${WORK}/r" "observed=false"
expect "empty-typed/empty-vault" "${WORK}/r" "status=unobserved"

# --- arm 3: population read fails -> unobservable ---------------------------
result="$(run_case "population-fails" \
	"{\"sentry\": ${ALERT_JSON}, \"typed\": [], \"population_status\": 500}")"
printf '%s\n' "${result}" > "${WORK}/r"
expect "population-fails" "${WORK}/r" "observed=false"
expect "population-fails" "${WORK}/r" "status=unobserved"

# --- arm 4: population read answers non-numerically -> unobservable ----------
# A negative total is an integer, so vault-list.sh prints it and exits 0; it is
# the caller's own `^[0-9]+$` guard that must reject it. A non-integer total
# would fail earlier, inside vault-list.sh, and land in arm 3's branch instead.
result="$(run_case "population-non-numeric" \
	"{\"sentry\": ${ALERT_JSON}, \"typed\": [], \"population_raw\": \"{\\\"items\\\": [], \\\"total\\\": -5}\"}")"
printf '%s\n' "${result}" > "${WORK}/r"
expect "population-non-numeric" "${WORK}/r" "observed=false"
# The reason is on stderr, not in the machine-readable result line.
expect "population-non-numeric" "${WORK}/err" "non-numeric answer"

# --- arm 5: the typed listing itself fails -> unobservable ------------------
result="$(run_case "typed-listing-fails" \
	"{\"sentry\": ${ALERT_JSON}, \"typed_status\": 500, \"population\": [{\"path\": \"/vault/tasks/other.md\"}]}")"
printf '%s\n' "${result}" > "${WORK}/r"
expect "typed-listing-fails" "${WORK}/r" "observed=false"
expect "typed-listing-fails" "${WORK}/r" "status=unobserved"

# --- arm 6: wrong-shaped typed listings -> unobservable ---------------------
for shape in not-json error-envelope no-items-key; do
	case "${shape}" in
	not-json) raw='"<html>nope</html>"' ;;
	error-envelope) raw='"{\"error\": \"invalid_filter\"}"' ;;
	no-items-key) raw='"{\"tasks\": []}"' ;;
	esac
	result="$(run_case "typed-${shape}" \
		"{\"sentry\": ${ALERT_JSON}, \"typed_raw\": ${raw}, \"population\": [{\"path\": \"/vault/tasks/other.md\"}]}")"
	printf '%s\n' "${result}" > "${WORK}/r"
	expect "typed-${shape}" "${WORK}/r" "observed=false"
done

# --- arm 7: populated typed listing -> unchanged, and no population read -----
result="$(run_case "typed-populated" \
	"{\"sentry\": ${ALERT_JSON}, \"typed\": ${TRACKED_JSON}, \"population_status\": 500}")"
printf '%s\n' "${result}" > "${WORK}/r"
expect "typed-populated" "${WORK}/r" "observed=true"
expect "typed-populated" "${WORK}/r" "expected_new=0"
refuse "typed-populated" "${WORK}/r" "status=unobserved"

# --- arm 8: a filter that stopped matching is DETECTED, not hidden ----------
# This is the 2026-09-13 shape and the reason the guard cannot simply be
# deleted: the typed query matches nothing while the vault is demonstrably
# populated, so the empty set is accepted (observed=true) and every alert looks
# new (expected_new=count). Publishing stays unconditional, so nothing lands and
# the landing clause must fail the run — `failed`, never `unobserved`, and never
# a false `done`. Without this arm the CHANGELOG's "detected" claim is untested.
result="$(run_case "broken-filter-detected" \
	"{\"sentry\": ${ALERT_JSON}, \"typed\": [], \"population\": [{\"path\": \"/vault/tasks/pr-review-x.md\"}]}")"
printf '%s\n' "${result}" > "${WORK}/r"
expect "broken-filter-detected" "${WORK}/r" "observed=true"
expect "broken-filter-detected" "${WORK}/r" "expected_new=1"
expect "broken-filter-detected" "${WORK}/r" "landed=0"
expect "broken-filter-detected" "${WORK}/r" "status=failed"
refuse "broken-filter-detected" "${WORK}/r" "status=unobserved"

echo
if [ "${fails}" -eq 0 ]; then
	printf 'all cases passed\n'
	exit 0
fi
printf '%s case(s) failed\n' "${fails}"
exit 1
