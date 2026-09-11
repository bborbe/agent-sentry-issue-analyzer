// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package steps

import (
	"context"

	agentlib "github.com/bborbe/agent"
	"github.com/bborbe/errors"

	"github.com/bborbe/agent-sentry-issue-analyzer/pkg/deepverdict"
)

// fixHandoffStep wraps the deep execution step (which writes ## Verdict with
// the deep schema) with the High/High fix handoff trigger. When the deep
// verdict is `real bug` with BOTH understanding and fix_certainty at High, the
// step flips the SAME task's frontmatter to the fix agent (assignee, phase:
// planning, task_type) and returns Status InProgress — an in-place save that
// the deliverer writes with phase=planning preserved, so the controller
// applies it, the scanner re-publishes, and the executor re-routes the task to
// the Config CR named by fixAssignee. Every other verdict — a `real bug` at
// Medium or Low certainty, or any non-`real bug` classification — returns the
// wrapped step's own result and completes exactly as it did before the
// handoff. Never batch: one handoff per High/High verdict, idempotent per
// task.
//
// The gate is the plain three-field predicate (real bug + High + High), NOT
// deepverdict.Validate: the disqualifier guard can rewrite ## Verdict with the
// triage schema render, which has no file:line field, and a full-Validate gate
// would break the guard-forced handoff. Idempotency is keyed on the current
// assignee: a task already stamped sentry-fix-agent was handed off and must
// never be handed off twice.
//
// The frontmatter mutation in Run is safe only under the executor's
// single-threaded delivery per task: the StepRunner delivers once per step on
// the same *Markdown pointer, and one Job processes one task at a time. Run
// must not be invoked concurrently on the same task content.
type fixHandoffStep struct {
	// execution is the underlying deep execution step (Claude writes ## Verdict).
	execution agentlib.Step
	// fixAssignee is the Config CR assignee the task is handed off to on High/High.
	fixAssignee string
	// fixTaskType is the task_type frontmatter value set on handoff.
	fixTaskType string
}

// NewFixHandoffStep wraps the deep execution step with the High/High fix
// handoff trigger.
func NewFixHandoffStep(
	execution agentlib.Step,
	fixAssignee string,
	fixTaskType string,
) agentlib.Step {
	return &fixHandoffStep{
		execution:   execution,
		fixAssignee: fixAssignee,
		fixTaskType: fixTaskType,
	}
}

func (s *fixHandoffStep) Name() string {
	return "sentry-deep-execution-fix-handoff"
}

func (s *fixHandoffStep) ShouldRun(
	ctx context.Context,
	md *agentlib.Markdown,
) (bool, error) {
	return s.execution.ShouldRun(ctx, md)
}

func (s *fixHandoffStep) Run(
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

	// Idempotency: the handoff flips the frontmatter in place, so a task that
	// was already handed off carries the fix assignee. Read the current
	// assignee and return the wrapped step's own result untouched — never hand
	// off twice.
	if current, _ := md.Frontmatter["assignee"].(string); current == s.fixAssignee {
		return result, nil
	}

	content, err := md.Marshal(ctx)
	if err != nil {
		return nil, errors.Wrapf(ctx, err, "fix-handoff: marshal task")
	}
	v, err := deepverdict.Parse(ctx, content)
	if err != nil {
		return nil, errors.Wrapf(ctx, err, "fix-handoff: parse verdict")
	}

	// Only a `real bug` the model is High/High certain about is a complete,
	// machine-actionable fix-spec worth dispatching the fix agent for. A real
	// bug at Medium or Low certainty, or any other classification, completes
	// exactly as it does today.
	if v.Verdict != "real bug" || v.Understanding != "High" || v.FixCertainty != "High" {
		return result, nil
	}

	// Hand the same task to the fix agent. Returning InProgress keeps the task
	// at status in_progress and phase planning (set below), so the executor
	// re-routes it instead of marking it done.
	md.Frontmatter["assignee"] = s.fixAssignee
	md.Frontmatter["phase"] = "planning"
	md.Frontmatter["task_type"] = s.fixTaskType
	return &agentlib.Result{
		Status:  agentlib.AgentStatusInProgress,
		Message: "reassigned to " + s.fixAssignee + " for fix",
	}, nil
}
