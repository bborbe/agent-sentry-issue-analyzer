// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package preflight provides the fail-fast check that the Sentry MCP tools the
// planning/execution prompts depend on are actually wired into the agent
// config. A missing tool fails the run immediately with a structured error
// instead of letting the LLM loop on empty tool results (see [[Fail-Fast
// Preflight for Tool-Dependent LLM Agents]]).
package preflight

import (
	"context"
	"sort"
	"strings"

	claudelib "github.com/bborbe/agent/claude"
	"github.com/bborbe/errors"
)

// requiredSentryTools are the mcp__sentry__* tools the domain prompts invoke.
var requiredSentryTools = []string{
	"mcp__sentry__whoami",
	"mcp__sentry__search_issues",
	"mcp__sentry__get_sentry_resource",
}

// ValidateSentryTools returns an error listing any required Sentry MCP tool
// absent from the allowed-tools set. Empty allowed tools is a hard failure —
// the agent would run without any Sentry access.
func ValidateSentryTools(ctx context.Context, allowed claudelib.AllowedTools) error {
	present := map[string]bool{}
	for _, t := range allowed {
		present[t] = true
	}

	var missing []string
	for _, t := range requiredSentryTools {
		if !present[t] {
			missing = append(missing, t)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	sort.Strings(missing)
	return errors.Errorf(
		ctx,
		"sentry MCP preflight failed: missing required tool(s) in ALLOWED_TOOLS: %s. Configure the Sentry MCP server and add these tools to the agent Config CRD ALLOWED_TOOLS list (see Sentry MCP guide).",
		strings.Join(missing, ", "),
	)
}
