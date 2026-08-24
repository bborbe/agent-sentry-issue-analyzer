// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package steps

import (
	agentlib "github.com/bborbe/agent"
	claudelib "github.com/bborbe/agent/claude"
)

// NewDeepExecutionStep wraps a Claude invocation as the deep-analyzer's
// execution-phase step. Claude reads the planning phase's ## Context,
// re-checks LIVE Sentry state, applies the octopus verdict rubric + noise
// disqualifiers, and writes the ## Verdict YAML block (U/F + file:line +
// disqualifiers_fired) back to the task body.
func NewDeepExecutionStep(
	runner claudelib.ClaudeRunner,
	instructions claudelib.Instructions,
	envContext map[string]string,
) agentlib.Step {
	return claudelib.NewAgentStep(claudelib.AgentStepConfig{
		Name:          "sentry-deep-execution",
		Runner:        runner,
		Instructions:  instructions,
		EnvContext:    envContext,
		OutputSection: "## Verdict",
		NextPhase:     "done",
	})
}
