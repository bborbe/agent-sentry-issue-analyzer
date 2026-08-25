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
// The collector agent has a single phase — NextPhase is empty (terminal
// in-place save, per claudelib.NewAgentStep semantics).
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
	})
}
