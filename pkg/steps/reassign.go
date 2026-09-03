// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package steps

import (
	"context"

	agentlib "github.com/bborbe/agent"
	"github.com/bborbe/errors"

	"github.com/bborbe/agent-sentry-issue-analyzer/pkg/verdict"
)

// reassignExecutionStep wraps the triage execution step (which writes ##
// Verdict) with the real-bug reassign trigger. When the triage verdict is
// `real bug`, the step flips the SAME task's frontmatter to the deep analyzer
// (assignee, phase: planning, task_type) and returns Status InProgress — an
// in-place save that the deliverer writes with phase=planning preserved, so
// the controller applies it, the scanner re-publishes, and the executor
// re-routes the task to the Config CR named by deepAssignee. Never batch: one
// reassign per real-bug verdict, idempotent per task.
//
// The frontmatter mutation in Run is safe only under the executor's
// single-threaded delivery per task: the StepRunner delivers once per step on
// the same *Markdown pointer, and one Job processes one task at a time. Run
// must not be invoked concurrently on the same task content.
type reassignExecutionStep struct {
	// execution is the underlying triage execution step (Claude writes ## Verdict).
	execution agentlib.Step
	// deepAssignee is the Config CR assignee the task is reassigned to on real-bug.
	deepAssignee string
	// deepTaskType is the task_type frontmatter value set on reassign.
	deepTaskType string
}

// NewReassignExecutionStep wraps the triage execution step with the real-bug
// reassign trigger.
func NewReassignExecutionStep(
	execution agentlib.Step,
	deepAssignee string,
	deepTaskType string,
) agentlib.Step {
	return &reassignExecutionStep{
		execution:    execution,
		deepAssignee: deepAssignee,
		deepTaskType: deepTaskType,
	}
}

func (s *reassignExecutionStep) Name() string {
	return "sentry-execution-reassign"
}

func (s *reassignExecutionStep) ShouldRun(
	ctx context.Context,
	md *agentlib.Markdown,
) (bool, error) {
	return s.execution.ShouldRun(ctx, md)
}

func (s *reassignExecutionStep) Run(
	ctx context.Context,
	md *agentlib.Markdown,
) (*agentlib.Result, error) {
	result, err := s.execution.Run(ctx, md)
	if err != nil {
		return result, err
	}
	if result.Status != agentlib.AgentStatusDone {
		return result, nil
	}

	content, err := md.Marshal(ctx)
	if err != nil {
		return nil, errors.Wrapf(ctx, err, "reassign: marshal task")
	}
	v, err := verdict.Parse(ctx, content)
	if err != nil {
		return nil, errors.Wrapf(ctx, err, "reassign: parse verdict")
	}
	if v.Verdict != "real bug" {
		return result, nil
	}

	// Reassign the same task to the deep analyzer. Returning InProgress keeps
	// the task at status in_progress and phase planning (set below), so the
	// executor re-routes it instead of marking it done.
	md.Frontmatter["assignee"] = s.deepAssignee
	md.Frontmatter["phase"] = "planning"
	md.Frontmatter["task_type"] = s.deepTaskType
	return &agentlib.Result{
		Status:  agentlib.AgentStatusInProgress,
		Message: "reassigned to " + s.deepAssignee + " for deep analysis",
	}, nil
}
