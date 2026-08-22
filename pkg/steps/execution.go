// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package steps

import (
	agentlib "github.com/bborbe/agent"
	claudelib "github.com/bborbe/agent/claude"
)

// NewExecutionStep wraps a Claude invocation as the execution-phase step.
// Claude reads the planning phase's ## Analysis, re-checks LIVE Sentry state,
// applies the 6-verdict rubric + noise disqualifiers, and writes the ## Verdict
// YAML block back to the task body. Write-verification is part of this phase —
// no separate ai_review phase (two-active-phase pattern, per the spec).
func NewExecutionStep(
	runner claudelib.ClaudeRunner,
	instructions claudelib.Instructions,
	envContext map[string]string,
) agentlib.Step {
	return claudelib.NewAgentStep(claudelib.AgentStepConfig{
		Name:          "sentry-execution",
		Runner:        runner,
		Instructions:  instructions,
		EnvContext:    envContext,
		OutputSection: "## Verdict",
		NextPhase:     "done",
	})
}
