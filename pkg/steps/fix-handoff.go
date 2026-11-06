// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package steps

import (
	"context"
	"strings"

	agentlib "github.com/bborbe/agent"
	"github.com/bborbe/errors"

	"github.com/bborbe/agent-sentry-issue-analyzer/pkg/deepverdict"
)

// reassignDeclarationKey is the frontmatter key this step sets to declare that the
// assignee change is a deliberate reassignment rather than an echo of the agent's
// spawn-time snapshot. agent-task-controller's operator-owned-assignee guard
// (pkg/result/result_writer.go) keeps the on-disk assignee over a differing incoming
// one — because an agent normally authors that key from its spawn snapshot — and
// honours a non-empty incoming assignee only when this key is *fresh* in the write.
// The spelling is therefore a frozen cross-repo contract: renaming it here silently
// reverts every handoff to a discard, with the task failing the executor's task_type
// filter and nothing naming the cause.
const reassignDeclarationKey = "assignee_reassigned_by"

// fixHandoffStep wraps the deep execution step (which writes ## Verdict with
// the deep schema) with the High/High fix handoff trigger. When the deep
// verdict is `real bug` with BOTH understanding and fix_certainty at High, the
// step flips the SAME task's frontmatter to the fix agent (assignee,
// task_type) and returns Status Done with NextPhase planning — the one result
// shape the deliverer publishes a phase for — so the controller writes phase
// planning at status in_progress, the scanner re-publishes, and the executor
// re-routes the task to the Config CR named by fixAssignee. Every other verdict — a `real bug` at
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
	//
	// The certainty fields are compared case-insensitively because the model
	// authors them free-form and its casing varies run to run. Measured
	// 2026-11-05 on the Seibert lane: `Analyze Sentry issue OCTOPUS-DEV-4A -
	// 2026-11-04` — the only High/High real bug on that lane — wrote
	// `understanding: high` / `fix_certainty: high`, and an exact-string gate
	// silently refused it, so no fix agent was ever dispatched and the failure
	// named nothing. Case is presentation, not meaning; the value set is what
	// carries the decision.
	if v.Verdict != "real bug" ||
		!strings.EqualFold(v.Understanding, "High") ||
		!strings.EqualFold(v.FixCertainty, "High") {
		return result, nil
	}

	// Hand the same task to the fix agent at phase planning, the only phase its
	// Config triggers on.
	//
	// The phase move MUST travel as Done + NextPhase. The bborbe/agent deliverer
	// deletes `phase` from every result that advances none — Status InProgress,
	// or Done with no NextPhase — so the controller's on-disk-first merge keeps
	// the current phase, and a `phase` set only on this frontmatter never
	// reaches disk. Measured 2026-11-06 on the Seibert lane: OCTOPUS-DEV-4A was
	// handed off at `execution`, and the executor refused the fix Job with
	// `gate=phase_not_triggered task_type=sentry-fix phase=execution`.
	// Done + NextPhase "planning" is published as phase planning with status
	// in_progress, so the executor re-routes the task instead of completing it;
	// vault-api permits execution -> planning.
	//
	// The deep agent also declares a planning phase and Agent.Run advances
	// in-process into any NextPhase its own agent has, so the deep planning step
	// is wrapped in NewHandedOffGuardStep: it reads the assignee set below and
	// skips, and the run exits for the fix agent.
	md.Frontmatter["assignee"] = s.fixAssignee
	md.Frontmatter["phase"] = "planning"
	md.Frontmatter["task_type"] = s.fixTaskType
	// Declare the reassignment, or the controller's operator-owned-assignee guard
	// discards the assignee above and keeps the on-disk analyzer — the task then
	// reaches the executor still addressed to the analyzer and fails its task_type
	// filter. s.Name() records which step declared it.
	md.Frontmatter[reassignDeclarationKey] = s.Name()
	return &agentlib.Result{
		Status:    agentlib.AgentStatusDone,
		NextPhase: "planning",
		Message:   "reassigned to " + s.fixAssignee + " for fix",
	}, nil
}
