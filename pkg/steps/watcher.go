// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package steps

import (
	agentlib "github.com/bborbe/agent"
	claudelib "github.com/bborbe/agent/claude"
)

// NewWatcherPlanningStep wraps a Claude invocation as the sentry-watcher's
// planning-phase step. Claude runs scripts/sentry-create-tasks.sh to fetch the
// day's active unresolved Sentry alerts and publish one per-alert task per
// (short-id, date), then writes the summary under the ## Analysis section.
// The watcher agent has a single phase — NextPhase is empty (terminal
// in-place save, per claudelib.NewAgentStep semantics).
func NewWatcherPlanningStep(
	runner claudelib.ClaudeRunner,
	instructions claudelib.Instructions,
	envContext map[string]string,
) agentlib.Step {
	return claudelib.NewAgentStep(claudelib.AgentStepConfig{
		Name:          "sentry-watcher-planning",
		Runner:        runner,
		Instructions:  instructions,
		EnvContext:    envContext,
		OutputSection: "## Analysis",
	})
}
