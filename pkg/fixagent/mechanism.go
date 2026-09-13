// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fixagent

import (
	"context"
	"strings"

	"github.com/bborbe/errors"
)

// FramePathRulePrefix is the claim the resolution prompts instruct the model to
// write when it resolved the repository from a stack frame's path. It is the
// only rule form that asserts trace evidence, and therefore the only one a
// trace can contradict.
const FramePathRulePrefix = "resolved from frame path"

//counterfeiter:generate -o mocks/trace-reader.go --fake-name TraceReader . TraceReader

// TraceReader reports whether the Sentry issue's latest event carries a
// first-party stack frame. It is the evidence a `resolved from frame path`
// claim is checked against: the check reads the trace itself, never the model's
// prose about the trace, because the prose is exactly what was wrong.
type TraceReader interface {
	HasFirstPartyFrame(ctx context.Context, sentryIssueID string) (bool, error)
}

// VerifyResolutionMechanism fails a resolution that claims a frame path when
// the issue's trace carries no first-party frame. Observed 2026-09-13 on
// sentry-analyzer-agent-4c46f7bb-20260913181909-4v9jj: the claim named the
// triage phase's root-cause conclusion while the alert had no frames at all,
// and nothing checked it, so the mislabel reached a generated bug spec.
//
// A candidate-list claim is never rejected here: it asserts no trace evidence,
// so a trace cannot contradict it.
func VerifyResolutionMechanism(
	ctx context.Context,
	res Resolution,
	hasFirstPartyFrame bool,
) error {
	if !claimsFramePath(res.Rule) || hasFirstPartyFrame {
		return nil
	}
	return errors.Errorf(
		ctx,
		"fix-agent: resolution mechanism unverifiable: rule claims a frame path "+
			"but the issue's trace carries no first-party frame (rule: %q)",
		strings.TrimSpace(res.Rule),
	)
}

// claimsFramePath reports whether rule asserts the frame-path mechanism. The
// match tolerates casing and surrounding whitespace: the model writes prose,
// and the check exists to catch the claim, not its spelling.
func claimsFramePath(rule string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(rule)), FramePathRulePrefix)
}
