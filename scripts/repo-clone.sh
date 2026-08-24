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
# interpolation into a git command line.
#
# Emits (clone mode) a flat key=value block:
#   clone_path, head_sha, default_branch
#
# Read-only by construction: the clone is created in REPO_CLONE_DIR (default
# /tmp/repos), then the ENTIRE working tree — including .git — is chmod a-w.
# The agent can read every file but cannot modify, commit, or push anything;
# a `git log` on the read-only tree still works (it only reads objects).
#
# Env: REPO_CLONE_DIR (default /tmp/repos), GIT_CLONE_DEPTH (default 0 = full
#      history so git log regression checks work), GIT_CLONE_TOKEN (optional;
#      never echoed).

set -euo pipefail

REPO_CLONE_DIR="${REPO_CLONE_DIR:-/tmp/repos}"
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
    dest="${REPO_CLONE_DIR}/${repo_path}"
    rm -rf "$dest"
    mkdir -p "$(dirname "$dest")"

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
