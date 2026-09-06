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

// disqualifierGuardStep wraps an execution step (which writes the ## Verdict
// section) with the disqualifier override: when a computed disqualifier fires,
// the verdict is forced to `real bug` and the ## Verdict section is rewritten
// with the fired disqualifiers, regardless of what the model wrote. Applied to
// BOTH the triage execution step and the deep execution step — the deep model
// runs a full re-analysis and must not be able to override a disqualifier that
// already forced `real bug` (observed on NUKE-DEV-A4: the triage forced real
// bug via sustained span, the deep re-analysis then wrote `noise` with the same
// events/day arithmetic error).
type disqualifierGuardStep struct {
	// execution is the wrapped step whose model writes ## Verdict.
	execution agentlib.Step
	// disqualifiers computes the noise-to-real-bug disqualifiers from the
	// verdict's live-state fields, overriding the model's verdict when any fire.
	disqualifiers verdict.DisqualifierEvaluator
}

// NewDisqualifierGuardStep wraps an execution step with the disqualifier
// override. Used on the triage path (inside the reassign step) and on the deep
// path (around the deep execution step).
func NewDisqualifierGuardStep(
	execution agentlib.Step,
	disqualifiers verdict.DisqualifierEvaluator,
) agentlib.Step {
	return &disqualifierGuardStep{
		execution:     execution,
		disqualifiers: disqualifiers,
	}
}

func (s *disqualifierGuardStep) Name() string {
	return "sentry-execution-disqualifier-guard"
}

func (s *disqualifierGuardStep) ShouldRun(
	ctx context.Context,
	md *agentlib.Markdown,
) (bool, error) {
	return s.execution.ShouldRun(ctx, md)
}

func (s *disqualifierGuardStep) Run(
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
		return nil, errors.Wrapf(ctx, err, "disqualifier-guard: marshal task")
	}
	v, err := verdict.Parse(ctx, content)
	if err != nil {
		return nil, errors.Wrapf(ctx, err, "disqualifier-guard: parse verdict")
	}
	if v.Verdict == "" {
		return result, nil
	}
	if err := applyDisqualifiers(ctx, md, &v, s.disqualifiers); err != nil {
		return nil, errors.Wrapf(ctx, err, "disqualifier-guard: apply disqualifiers")
	}
	return result, nil
}
