// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package steps holds the per-phase agent steps for the Sentry analyzer.
//
// Two active phases per the spec (planning → execution, no ai_review — the
// two-active-phase pattern is intentional, analogous to the backtest agent):
//   - planning  → claude.NewAgentStep (planning prompt + MCP tools): fetch
//     LIVE state for the single alert, read implicated source (read-only),
//     write ## Analysis
//   - execution → claude.NewAgentStep (execution prompt): apply the 6-verdict
//     rubric + noise disqualifiers, write ## Verdict back to the task body
package steps

import (
	agentlib "github.com/bborbe/agent"
	claudelib "github.com/bborbe/agent/claude"
	"github.com/bborbe/vault-cli/pkg/domain"
)

// NewPlanningStep wraps a Claude invocation as the planning-phase step.
// Claude fetches LIVE state for the single alert, reads the implicated source
// read-only, and writes the ## Analysis section consumed by the execution
// phase.
func NewPlanningStep(
	runner claudelib.ClaudeRunner,
	instructions claudelib.Instructions,
	envContext map[string]string,
) agentlib.Step {
	return claudelib.NewAgentStep(claudelib.AgentStepConfig{
		Name:          "sentry-planning",
		Runner:        runner,
		Instructions:  instructions,
		EnvContext:    envContext,
		OutputSection: "## Analysis",
		NextPhase:     string(domain.TaskPhaseExecution),
	})
}
