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

// repoCloneToolPrefix is the constrained Bash tool the planning prompt invokes
// to clone the implicated source repo read-only. Like sentry-read.sh, it is
// granted only under its script constraint — no bare Bash, no bare git.
const repoCloneToolPrefix = "Bash(scripts/repo-clone.sh"

// collectorScriptToolPrefix is the constrained Bash tool the collector-step prompt
// invokes to fetch the day's active unresolved Sentry alerts and publish the
// per-alert tasks. It is granted only under its script constraint.
const collectorScriptToolPrefix = "Bash(scripts/sentry-create-tasks.sh"

// ValidateSentryTools returns an error if the token-based Sentry access path is
// not wired: the `Bash(scripts/sentry-read.sh:*` tool must be present in
// ALLOWED_TOOLS, and SENTRY_API_TOKEN must be set. Empty allowed tools or a
// missing token are hard failures — the agent would run without any Sentry
// access.
func ValidateSentryTools(
	ctx context.Context,
	allowed claudelib.AllowedTools,
	apiToken string,
) error {
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
	if !hasSentryReadTool(ctx, allowed) {
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
func hasSentryReadTool(ctx context.Context, allowed claudelib.AllowedTools) bool {
	for _, t := range allowed {
		select {
		case <-ctx.Done():
			return false
		default:
		}
		if strings.HasPrefix(t, sentryReadToolPrefix) {
			return true
		}
	}
	return false
}

// ValidateCollectorTools returns an error if the collector step's tool path is not
// wired: the `Bash(scripts/sentry-create-tasks.sh:*` tool must be present in
// ALLOWED_TOOLS, and SENTRY_API_TOKEN must be set. The collector agent needs
// none of the triage/deep tools (sentry-read.sh, repo-clone.sh), so it is
// validated against its own constrained script instead.
func ValidateCollectorTools(
	ctx context.Context,
	allowed claudelib.AllowedTools,
	apiToken string,
) error {
	present := false
	for _, t := range allowed {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if strings.HasPrefix(t, collectorScriptToolPrefix) {
			present = true
		}
	}

	var missing []string
	if !present {
		missing = append(missing, "Bash(scripts/sentry-create-tasks.sh:*)")
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
		"sentry-collector preflight failed: missing required piece(s) for the collector step: %s. Grant the Bash(scripts/sentry-create-tasks.sh:*) tool in the agent Config CRD ALLOWED_TOOLS and set SENTRY_API_TOKEN (teamvault-sourced).",
		strings.Join(missing, ", "),
	)
}

// ValidateRepoCloneTools returns an error if the read-only repo clone tool the
// planning prompt depends on is not wired into ALLOWED_TOOLS: the
// `Bash(scripts/repo-clone.sh:*` tool must be present. A missing tool is a
// hard failure — the planning phase would have no way to read the implicated
// source and resolve the root-cause file:line.
func ValidateRepoCloneTools(ctx context.Context, allowed claudelib.AllowedTools) error {
	for _, t := range allowed {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if strings.HasPrefix(t, repoCloneToolPrefix) {
			return nil
		}
	}
	return errors.Errorf(
		ctx,
		"repo-clone preflight failed: missing Bash(scripts/repo-clone.sh:*) tool. Grant it in the agent Config CRD ALLOWED_TOOLS.",
	)
}
