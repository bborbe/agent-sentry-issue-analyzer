// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package steps

import (
	"context"
	"strings"

	agentlib "github.com/bborbe/agent"
	"github.com/bborbe/errors"
	libtime "github.com/bborbe/time"

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
	// disqualifiers computes the noise-to-real-bug disqualifiers from the
	// verdict's live-state fields, overriding the model's verdict when any fire.
	disqualifiers verdict.DisqualifierEvaluator
}

// NewReassignExecutionStep wraps the triage execution step with the real-bug
// reassign trigger.
func NewReassignExecutionStep(
	execution agentlib.Step,
	deepAssignee string,
	deepTaskType string,
	disqualifiers verdict.DisqualifierEvaluator,
) agentlib.Step {
	return &reassignExecutionStep{
		execution:     execution,
		deepAssignee:  deepAssignee,
		deepTaskType:  deepTaskType,
		disqualifiers: disqualifiers,
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

	// The rubric makes a fired disqualifier authoritative over the model's
	// verdict. The disqualifiers used to be evaluated by the model in prose,
	// which got the events/day arithmetic wrong (biased toward `noise`); the
	// numeric thresholds are now computed here, and a fired disqualifier
	// forces the verdict to `real bug` (rewriting the ## Verdict section).
	// Verified-absent resource stays a model judgment. Only run when a real
	// verdict parsed — an absent verdict has no live-state fields to evaluate.
	if v.Verdict != "" {
		if err := applyDisqualifiers(ctx, md, &v, s.disqualifiers); err != nil {
			return nil, errors.Wrapf(ctx, err, "reassign: apply disqualifiers")
		}
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

// applyDisqualifiers evaluates the computed disqualifiers against the
// verdict's live-state fields, forces `real bug` when any fire, and records
// the fired disqualifiers as evidence in the ## Verdict section. Shared by the
// reassign step (triage path) and the disqualifier guard (deep path).
func applyDisqualifiers(
	ctx context.Context,
	md *agentlib.Markdown,
	v *verdict.Verdict,
	disqualifiers verdict.DisqualifierEvaluator,
) error {
	firstSeen, err := libtime.ParseDateTime(ctx, v.FirstSeen)
	if err != nil {
		return errors.Wrapf(ctx, err, "apply disqualifiers: parse first_seen %q", v.FirstSeen)
	}
	lastSeen, err := libtime.ParseDateTime(ctx, v.LastSeen)
	if err != nil {
		return errors.Wrapf(ctx, err, "apply disqualifiers: parse last_seen %q", v.LastSeen)
	}
	fired, err := disqualifiers.Evaluate(ctx, verdict.DisqualifierInput{
		LiveEventCount: v.LiveEventCount,
		FirstSeen:      firstSeen.Time(),
		LastSeen:       lastSeen.Time(),
		SentryStatus:   v.SentryStatus,
	})
	if err != nil {
		return errors.Wrapf(ctx, err, "apply disqualifiers: evaluate")
	}
	if len(fired) == 0 {
		return nil
	}

	names := make([]string, 0, len(fired))
	for _, d := range fired {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		names = append(names, string(d))
	}
	if v.Verdict != "real bug" {
		v.Verdict = "real bug"
		v.Confidence = "high"
		if v.Reason != "" {
			v.Reason = "disqualifier fired (" + strings.Join(names, ", ") + "): " + v.Reason
		} else {
			v.Reason = "disqualifier fired (" + strings.Join(names, ", ") + ")"
		}
	}
	v.DisqualifiersFired = names

	rendered, err := verdict.Render(ctx, *v)
	if err != nil {
		return errors.Wrapf(ctx, err, "apply disqualifiers: render verdict")
	}
	if section, ok := md.FindSection("## Verdict"); ok {
		section.Body = rendered
	}
	return nil
}
