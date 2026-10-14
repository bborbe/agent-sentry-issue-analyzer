#!/usr/bin/env bash
# Constrained read-only repo cloner — gives the planning phase a local copy of
# the implicated source repo so it can Read/Grep the code and resolve the
# root-cause file:line. Replaces ad-hoc `git clone` (which the agent's
# ALLOWED_TOOLS does not grant — no bare Bash, no bare git).
#
# Usage:
#   repo-clone.sh clone <repo>            # clone read-only, emit key=value block
#   repo-clone.sh log <clone_path> <file> # recent commits touching <file> (regression check)
#
# <repo> accepts https://github.com/<owner>/<name>(.git), git@github.com:<owner>/<name>.git,
# or bare <owner>/<name>. The reference is validated to a strict charset and
# normalized to an https URL before git sees it — no shell metacharacter
# interpolation into a git command line. The charset check alone is NOT enough:
# `.` and `..` match it, so an owner/name segment that is exactly `.` or `..` is
# rejected outright, and the resolved clone destination is checked to be strictly
# inside REPO_CLONE_DIR (symlinks resolved) before any `rm -rf` or `chmod -R` acts
# on it. Without both, `https://github.com/../..` resolves the delete target above
# the clone dir and wipes the agent root.
#
# Emits (clone mode) a flat key=value block:
#   clone_path, head_sha, default_branch
#
# Read-only by construction: the clone is created in REPO_CLONE_DIR (default
# /agent/repos), then the ENTIRE working tree — including .git — is chmod a-w.
# The agent can read every file but cannot modify, commit, or push anything;
# a `git log` on the read-only tree still works (it only reads objects).
#
# The default MUST stay inside the agent root. Claude's tool sandbox is its
# WorkingDirectory, which is AGENT_DIR — default the relative string "agent",
# resolved against the runtime image's cwd "/", i.e. /agent. A clone outside
# that root succeeds and is then unreadable: Read/Glob/Bash all refuse it, so
# the run wastes the clone at the last step and escalates. Use a dedicated
# subdir, never /agent itself — the agent root also holds the agent's own
# .claude/ config and scripts/, and cloned repo content is untrusted.
#
# Env: REPO_CLONE_DIR (default /agent/repos), GIT_CLONE_DEPTH (default 0 = full
#      history so git log regression checks work), GIT_CLONE_TOKEN (optional;
#      never echoed).

set -euo pipefail

REPO_CLONE_DIR="${REPO_CLONE_DIR:-/agent/repos}"
GIT_CLONE_DEPTH="${GIT_CLONE_DEPTH:-0}"

# normalize_repo validates a repo reference and echoes the canonical https URL.
# Accepts https://github.com/<owner>/<name>[/.git], git@github.com:<owner>/<name>.git,
# or bare <owner>/<name>. Anything else is rejected before git runs.
normalize_repo() {
  local ref="$1"
  local out=""
  case "$ref" in
    https://github.com/*)
      out="${ref}"
      ;;
    git@github.com:*)
      out="https://github.com/${ref#git@github.com:}"
      ;;
    */*)
      out="https://github.com/${ref}"
      ;;
    *)
      echo "invalid repo reference: ${ref} (expected https://github.com/<owner>/<name>, git@github.com:<owner>/<name>.git, or <owner>/<name>)" >&2
      exit 2
      ;;
  esac
  # Strip trailing .git and any trailing slash.
  out="${out%.git}"
  out="${out%/}"
  # Strict final validation: only word, dot, dash, underscore chars after the host.
  if ! printf '%s' "$out" | grep -qE '^https://github\.com/[A-Za-z0-9._-]+/[A-Za-z0-9._-]+$'; then
    echo "invalid repo reference: ${ref}" >&2
    exit 2
  fi
  # The charset admits `.` and `..`, which are path segments rather than names.
  # Reject them per segment: `https://github.com/../..` otherwise resolves the
  # clone destination above REPO_CLONE_DIR (see the containment guard in `clone`).
  local repo_path="${out#https://github.com/}"
  local seg rest="$repo_path"
  while [ -n "$rest" ]; do
    seg="${rest%%/*}"
    if [ "$seg" = "." ] || [ "$seg" = ".." ]; then
      echo "invalid repo reference: ${ref} ('.' and '..' are not valid owner or name segments)" >&2
      exit 2
    fi
    case "$rest" in
      */*) rest="${rest#*/}" ;;
      *) rest="" ;;
    esac
  done
  printf '%s\n' "$out"
}

# url_for builds the clone URL, injecting an optional token without echoing it.
url_for() {
  local url="$1"
  if [ -n "${GIT_CLONE_TOKEN:-}" ]; then
    printf 'https://x-access-token:%s@github.com/%s' "${GIT_CLONE_TOKEN}" "${url#https://github.com/}"
  else
    printf '%s\n' "$url"
  fi
}

cmd="${1:-}"
case "$cmd" in
  clone)
    [ $# -ge 2 ] || { echo "usage: repo-clone.sh clone <repo>" >&2; exit 2; }
    url="$(normalize_repo "$2")"
    # owner/name derived from the normalized https URL.
    repo_path="${url#https://github.com/}"
    # Belt-and-braces: normalize_repo already rejects `.`/`..` segments, but this
    # guard is the containment boundary itself and must hold on its own — it also
    # catches a symlinked component under REPO_CLONE_DIR that a name check cannot.
    case "$repo_path" in
      *"/../"*|*"/./"*|"../"*|"./"*|*"/.."|*"/."|".."|".")
        echo "invalid repo reference: ${url} (path escapes the clone dir)" >&2
        exit 2
        ;;
    esac
    dest="${REPO_CLONE_DIR}/${repo_path}"
    # The clone dir itself may not exist yet on a fresh container; create it so the
    # containment check below has a real path to resolve against. (The original
    # script created only the destination's parent, which had the same effect.)
    mkdir -p "$REPO_CLONE_DIR"
    # Create the parent BEFORE resolving: realpath resolves symlinks only along a
    # path that exists, so the parent must be real before the check means anything.
    # mkdir -p follows a symlinked component too, but only ever *creates* dirs — it
    # removes nothing, and the guard below still refuses before rm/chmod run.
    dest_parent="$(dirname "$dest")"
    mkdir -p "$dest_parent"
    # Containment, resolved on the PARENT: `realpath` on the full destination is
    # platform-dependent (BusyBox resolves a not-yet-created leaf; GNU/macOS
    # realpath fails outright), and the leaf's parent is what the escape travels
    # through. `..` is already rejected above, so a symlinked component is the only
    # remaining way out and it shows up here.
    real_clone_dir="$(realpath "$REPO_CLONE_DIR")"
    real_dest_parent="$(realpath "$dest_parent")"
    case "$real_dest_parent" in
      "${real_clone_dir}"|"${real_clone_dir}/"*) : ;;
      *)
        echo "refusing to act on ${dest}: its parent resolves to ${real_dest_parent}, outside REPO_CLONE_DIR (${real_clone_dir})" >&2
        exit 2
        ;;
    esac
    # A symlink standing where the clone belongs would redirect both the rm and the
    # clone out of the tree; a real clone destination is never a symlink.
    if [ -L "$dest" ]; then
      echo "refusing to act on ${dest}: it is a symlink (resolves to $(readlink "$dest"))" >&2
      exit 2
    fi
    rm -rf "$dest"

    clone_args=(clone)
    if [ "${GIT_CLONE_DEPTH}" != "0" ]; then
      clone_args+=(--depth "${GIT_CLONE_DEPTH}" --single-branch)
    fi
    if ! git "${clone_args[@]}" "$(url_for "$url")" "$dest" >/dev/null 2>&1; then
      echo "git clone failed for ${url} (is the repo public or is GIT_CLONE_TOKEN set?)" >&2
      exit 1
    fi

    # Make the whole tree read-only: the agent can read but cannot modify/commit/push.
    chmod -R a-w "$dest"

    head_sha="$(git -C "$dest" rev-parse HEAD)"
    branch="$(git -C "$dest" branch --show-current)"
    printf 'clone_path=%s\n' "$dest"
    printf 'head_sha=%s\n' "$head_sha"
    printf 'default_branch=%s\n' "${branch:-HEAD}"
    ;;
  log)
    [ $# -ge 3 ] || { echo "usage: repo-clone.sh log <clone_path> <file>" >&2; exit 2; }
    clone_path="$2"
    file="$3"
    if [ ! -d "$clone_path/.git" ]; then
      echo "not a git clone: ${clone_path}" >&2
      exit 2
    fi
    # The file path is a plain relative path — reject anything with metacharacters.
    if ! printf '%s' "$file" | grep -qE '^[A-Za-z0-9_./-]+$'; then
      echo "invalid file path: ${file}" >&2
      exit 2
    fi
    if ! git -C "$clone_path" log --oneline -10 -- "$file" >/dev/null 2>&1; then
      echo "no commits found for ${file} (is the path repo-relative?)" >&2
      exit 2
    fi
    git -C "$clone_path" log --oneline -10 -- "$file"
    ;;
  *)
    echo "usage: repo-clone.sh <clone|log> ..." >&2
    exit 2
    ;;
esac
