// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package steps

import (
	agentlib "github.com/bborbe/agent"
	claudelib "github.com/bborbe/agent/claude"
	"github.com/bborbe/vault-cli/pkg/domain"
)

// NewDeepPlanningStep wraps a Claude invocation as the deep-analyzer's
// planning-phase step. Claude fetches LIVE Sentry state for the single
// flagged alert, clones the implicated repo read-only, and writes the
// ## Context section consumed by the deep execution phase.
func NewDeepPlanningStep(
	runner claudelib.ClaudeRunner,
	instructions claudelib.Instructions,
	envContext map[string]string,
) agentlib.Step {
	return claudelib.NewAgentStep(claudelib.AgentStepConfig{
		Name:          "sentry-deep-planning",
		Runner:        runner,
		Instructions:  instructions,
		EnvContext:    envContext,
		OutputSection: "## Context",
		NextPhase:     string(domain.TaskPhaseExecution),
	})
}
