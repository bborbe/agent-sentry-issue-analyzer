#!/usr/bin/env bash
# Regression test for scripts/repo-clone.sh path containment.
#
# Covers the defect where `.` / `..` matched the repo-reference charset, so a
# crafted reference resolved the clone destination outside REPO_CLONE_DIR and
# `rm -rf` / `chmod -R` acted on it (e.g. https://github.com/../.. -> /agent).
#
# Every case drives the real `clone` entrypoint — not the validation function in
# isolation — so the test proves the guard holds on the path the script actually
# takes, including the symlinked-component case a charset check cannot see.
# No network access is needed: each hostile case is refused before `git` runs.
#
# Run: bash scripts/test-repo-clone-containment.sh

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CLONE_SCRIPT="${SCRIPT_DIR}/repo-clone.sh"
WORK="$(mktemp -d)"
# The script chmods every clone read-only by design, so the scratch tree must be
# made writable again before removal or the trap leaks it.
cleanup() { chmod -R u+w "$WORK" 2>/dev/null; rm -rf "$WORK"; }
trap cleanup EXIT

fails=0
pass() { printf 'ok   %s\n' "$1"; }
fail() { printf 'FAIL %s\n' "$1" >&2; fails=$((fails + 1)); }

# run_clone <clone_dir> <ref> -> prints "<exit_code>|<first output line>"
run_clone() {
  local dir="$1" ref="$2" out code
  out="$(REPO_CLONE_DIR="$dir" bash "$CLONE_SCRIPT" clone "$ref" 2>&1)"
  code=$?
  printf '%s|%s' "$code" "$(printf '%s' "$out" | head -1)"
}

# --- Hostile refs must be refused, naming the reason, before any rm/chmod ----
while IFS= read -r ref; do
  [ -n "$ref" ] || continue
  dir="$WORK/hostile"; mkdir -p "$dir"
  res="$(run_clone "$dir" "$ref")"
  code="${res%%|*}"; line="${res#*|}"
  if [ "$code" = "0" ]; then
    fail "hostile ref accepted: $ref"
  elif ! printf '%s' "$line" | grep -q 'invalid repo reference'; then
    fail "hostile ref refused without naming the reason: $ref -> $line"
  else
    pass "hostile ref refused: $ref"
  fi
done <<'REFS'
https://github.com/../x
https://github.com/x/..
https://github.com/../..
https://github.com/./.
https://github.com/./x
https://github.com/x/.
REFS

# --- Legitimate refs must still pass validation (no over-tightening) --------
# A rejected ref prints "invalid repo reference" and exits 2 before `git` runs.
# Everything else means validation accepted it; the clone itself is not what is
# under test here, so a network failure is an acceptable outcome.
for ref in "bborbe/trading" "bborbe/docker-registry.helm" "bborbe/poste.io"; do
  dir="$WORK/legit"; mkdir -p "$dir"
  res="$(run_clone "$dir" "$ref")"
  line="${res#*|}"
  if printf '%s' "$line" | grep -q 'invalid repo reference'; then
    fail "legitimate ref rejected (charset over-tightened): $ref"
  else
    pass "legitimate ref accepted: $ref"
  fi
done

# --- One real clone, to prove the guard does not block the happy path -------
# bborbe/run is small; a network-less environment fails the clone, which is not
# a containment regression, so only a validation refusal is treated as failure.
dir="$WORK/realclone"; mkdir -p "$dir"
res="$(REPO_CLONE_DIR="$dir" GIT_CLONE_DEPTH=1 timeout 120 bash "$CLONE_SCRIPT" clone bborbe/run 2>&1)"
code=$?
if printf '%s' "$res" | grep -q 'invalid repo reference'; then
  fail "legitimate ref refused by the containment guard: bborbe/run"
elif [ "$code" = "0" ]; then
  if printf '%s' "$res" | grep -q "^clone_path=${dir}/bborbe/run$"; then
    pass "real clone succeeded and landed inside the clone dir"
  else
    fail "real clone reported success but an unexpected clone_path: $(printf '%s' "$res" | head -1)"
  fi
else
  printf 'skip real clone (network unavailable — exit %s)\n' "$code"
fi

# --- Symlinked component under REPO_CLONE_DIR must not escape ---------------
# The case a name check alone cannot catch: a valid-charset ref whose resolved
# destination leaves the tree through a symlink.
dir="$WORK/symlink"; mkdir -p "$dir" "$WORK/outside-parent"
ln -s "$WORK/outside-parent" "$dir/evil"
res="$(run_clone "$dir" "evil/repo")"
code="${res%%|*}"; line="${res#*|}"
if [ "$code" = "0" ]; then
  fail "symlinked component escaped: evil/repo was accepted"
elif ! printf '%s' "$line" | grep -q 'outside REPO_CLONE_DIR'; then
  fail "symlinked component refused without naming containment: $line"
elif [ -n "$(ls -A "$WORK/outside-parent" 2>/dev/null)" ]; then
  fail "symlinked component wrote outside the clone dir: $(ls -A "$WORK/outside-parent")"
else
  pass "symlinked component refused, nothing written outside"
fi

# --- A symlink standing where the clone belongs must be refused -------------
dir="$WORK/leaf"; mkdir -p "$dir/owner" "$WORK/outside-leaf"
ln -s "$WORK/outside-leaf" "$dir/owner/repo"
res="$(run_clone "$dir" "owner/repo")"
code="${res%%|*}"; line="${res#*|}"
if [ "$code" = "0" ]; then
  fail "symlink leaf escaped: owner/repo was accepted"
elif ! printf '%s' "$line" | grep -q 'it is a symlink'; then
  fail "symlink leaf refused without naming the reason: $line"
elif [ -n "$(ls -A "$WORK/outside-leaf" 2>/dev/null)" ]; then
  fail "symlink leaf wrote outside the clone dir"
else
  pass "symlink leaf refused, nothing written outside"
fi

# --- A symlinked REPO_CLONE_DIR itself must still work ----------------------
# The guard resolves REPO_CLONE_DIR, so a clone dir reached through a symlink
# (a real deployment shape: the PVC mount) must not be read as an escape.
mkdir -p "$WORK/real-dir"
ln -s "$WORK/real-dir" "$WORK/linked-dir"
res="$(REPO_CLONE_DIR="$WORK/linked-dir" GIT_CLONE_DEPTH=1 timeout 120 bash "$CLONE_SCRIPT" clone bborbe/run 2>&1)"
code=$?
if printf '%s' "$res" | grep -qE 'outside REPO_CLONE_DIR|it is a symlink'; then
  fail "guard over-blocked a symlinked REPO_CLONE_DIR: $(printf '%s' "$res" | head -1)"
elif [ "$code" = "0" ] && printf '%s' "$res" | grep -q "^clone_path=$WORK/linked-dir/bborbe/run$"; then
  pass "symlinked REPO_CLONE_DIR still clones"
else
  printf 'skip symlinked clone dir (network unavailable — exit %s)\n' "$code"
fi

if [ "$fails" -ne 0 ]; then
  printf '\n%d check(s) failed\n' "$fails" >&2
  exit 1
fi
printf '\nall containment checks passed\n'
