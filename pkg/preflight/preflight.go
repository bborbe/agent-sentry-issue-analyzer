// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package preflight provides the fail-fast check that the token-based Sentry
// access path the planning/execution prompts depend on is actually wired into
// the agent config: the constrained `scripts/sentry-read.sh` Bash tool in
// ALLOWED_TOOLS plus the SENTRY_API_TOKEN env var. A missing piece fails the
// run immediately with a structured error instead of letting the LLM loop on
// empty results (see [[Fail-Fast Preflight for Tool-Dependent LLM Agents]]).
package preflight

import (
	"context"
	"sort"
	"strings"

	claudelib "github.com/bborbe/agent/claude"
	"github.com/bborbe/errors"
)

// sentryReadToolPrefix is the constrained Bash tool the domain prompts invoke
// to fetch LIVE Sentry state. ALLOWED_TOOLS must grant it with its script
// constraint so the agent can call nothing but the read-only fetcher.
const sentryReadToolPrefix = "Bash(scripts/sentry-read.sh"

// ValidateSentryTools returns an error if the token-based Sentry access path is
// not wired: the `Bash(scripts/sentry-read.sh:*` tool must be present in
// ALLOWED_TOOLS, and SENTRY_API_TOKEN must be set. Empty allowed tools or a
// missing token are hard failures — the agent would run without any Sentry
// access.
func ValidateSentryTools(ctx context.Context, allowed claudelib.AllowedTools, apiToken string) error {
	present := map[string]bool{}
	for _, t := range allowed {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		present[t] = true
	}

	var missing []string
	if !hasSentryReadTool(allowed) {
		missing = append(missing, "Bash(scripts/sentry-read.sh:*)")
	}
	if apiToken == "" {
		missing = append(missing, "SENTRY_API_TOKEN")
	}
	if len(missing) == 0 {
		return nil
	}
	sort.Strings(missing)
	return errors.Errorf(
		ctx,
		"sentry preflight failed: missing required piece(s) for token-based Sentry access: %s. Grant the Bash(scripts/sentry-read.sh:*) tool in the agent Config CRD ALLOWED_TOOLS and set SENTRY_API_TOKEN (teamvault-sourced).",
		strings.Join(missing, ", "),
	)
}

// hasSentryReadTool reports whether allowed contains the constrained script
// tool (prefix match: "Bash(scripts/sentry-read.sh:*" or a stricter scope).
func hasSentryReadTool(allowed claudelib.AllowedTools) bool {
	for _, t := range allowed {
		if strings.HasPrefix(t, sentryReadToolPrefix) {
			return true
		}
	}
	return false
}
