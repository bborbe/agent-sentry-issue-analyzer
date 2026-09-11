#!/bin/bash
# vault-list.sh - List files in the target Obsidian vault via a git-rest glob.
#
# Used by sentry-create-tasks.sh to observe how many per-alert tasks actually
# LANDED after publishing. The controller owns dedup, so the publish count is
# not the created count — a run whose publishes all succeed can still land zero
# task files (the 2026-08-26/27/28 outage signature).
#
# Vault endpoint via env var:
#   GIT_REST_URL  e.g. http://vault-obsidian-personal:9090
#
# Glob uses filepath.Match semantics: * matches within a directory segment,
# ** double-star is NOT supported by git-rest.
#
# Usage: vault-list.sh <glob>
#
# Examples:
#   vault-list.sh "24 Tasks/*NUKE-DEV-3A*"
#   vault-list.sh "24 Tasks/*.md"
#
# stdout = one path per line (empty when nothing matches); stderr = diagnostics.

set -e -u -o pipefail

: "${GIT_REST_URL:?GIT_REST_URL is required}"

if [ $# -ne 1 ]; then
  echo "Usage: vault-list.sh <glob>" >&2
  exit 1
fi

# Optional gateway auth headers — injected when GATEWAY_SECRET is non-empty.
gateway_args=()
if [ -n "${GATEWAY_SECRET:-}" ]; then
  gateway_args=(-H "X-Gateway-Secret: $GATEWAY_SECRET" -H "X-Gateway-Initator: agent-sentry-collector")
fi

# The URL path segment is ignored by git-rest — only ?glob matters.
# `curl -G --data-urlencode` safely encodes spaces and special chars in the pattern.
exec curl -sf --max-time 30 \
  ${gateway_args[@]+"${gateway_args[@]}"} \
  -G --data-urlencode "glob=$1" \
  "${GIT_REST_URL}/api/v1/files/"
