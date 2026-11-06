// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package steps

import (
	"context"

	agentlib "github.com/bborbe/agent"
)

// handedOffGuardStep skips the wrapped step once the task has been handed to
// another agent in this same run.
//
// The fix handoff publishes its phase move as Done + NextPhase "planning", and
// Agent.Run advances in-process into any NextPhase its own agent declares. The
// deep agent declares planning too, so without this guard a handed-off task
// whose deep planning output is absent — a run killed at its deadline and
// re-queued carries `## Failure` and no `## Context` — would be re-planned by
// the deep analyzer in the same pod instead of exiting for the fix agent.
// Skipping makes the phase's StepRunner return no result, and Agent.Run then
// returns the handoff result, so the pod exits.
type handedOffGuardStep struct {
	inner             agentlib.Step
	handedOffAssignee string
}

// NewHandedOffGuardStep wraps inner so it never runs on a task whose assignee
// is handedOffAssignee.
func NewHandedOffGuardStep(inner agentlib.Step, handedOffAssignee string) agentlib.Step {
	return &handedOffGuardStep{inner: inner, handedOffAssignee: handedOffAssignee}
}

func (s *handedOffGuardStep) Name() string {
	return s.inner.Name()
}

func (s *handedOffGuardStep) ShouldRun(
	ctx context.Context,
	md *agentlib.Markdown,
) (bool, error) {
	if assignee, _ := md.Frontmatter["assignee"].(string); assignee == s.handedOffAssignee {
		return false, nil
	}
	return s.inner.ShouldRun(ctx, md)
}

func (s *handedOffGuardStep) Run(
	ctx context.Context,
	md *agentlib.Markdown,
) (*agentlib.Result, error) {
	return s.inner.Run(ctx, md)
}
