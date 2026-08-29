// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package steps

import (
	agentlib "github.com/bborbe/agent"
	claudelib "github.com/bborbe/agent/claude"
)

// NewCollectorPlanningStep wraps a Claude invocation as the sentry-collector's
// planning-phase step. Claude runs scripts/sentry-create-tasks.sh to fetch the
// day's active unresolved Sentry alerts and publish one per-alert task per
// (short-id, date), then writes the summary under the ## Analysis section.
//
// NextPhase is "done" so a successful fan-out terminates the task: without it,
// the step is an in-place save that leaves the task planning/in_progress, the
// executor re-dispatches, ShouldRun skips (success section present), and the
// agent's nil result becomes deadline_exceeded → trigger_count churn (spec 051
// follow-up, verified live 2026-08-30 job ...220835).
func NewCollectorPlanningStep(
	runner claudelib.ClaudeRunner,
	instructions claudelib.Instructions,
	envContext map[string]string,
) agentlib.Step {
	return claudelib.NewAgentStep(claudelib.AgentStepConfig{
		Name:          "sentry-collector-planning",
		Runner:        runner,
		Instructions:  instructions,
		EnvContext:    envContext,
		OutputSection: "## Analysis",
		NextPhase:     "done",
	})
}
