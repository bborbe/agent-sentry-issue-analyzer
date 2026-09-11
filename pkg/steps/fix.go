// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package steps

import (
	"context"
	"fmt"
	"strings"

	agentlib "github.com/bborbe/agent"
	"github.com/bborbe/errors"

	"github.com/bborbe/agent-sentry-issue-analyzer/pkg/deepverdict"
	"github.com/bborbe/agent-sentry-issue-analyzer/pkg/fixagent"
)

// fixStep is the sentry-fix agent's single planning step: it turns a High/High
// real-bug deep verdict into a filed `kind: bug` spec, or a recorded no-op when
// the citation is stale or the verdict is missing its required fields. Parse
// the deep verdict (## Verdict) → resolve the repo through the injectable
// resolver → honor staleness → build the spec → file it through the injectable
// writer → record the outcome in ## Fix Result. Never batch: one fix step per
// High/High verdict, idempotent per task (## Fix Result presence gates resume).
//
// Resolution and writing are injected so production (prompt-backed resolver +
// GitHub API writer) is stub-able in tests. The task body is only mutated (via
// ## Fix Result) on non-error outcomes: an unmappable resolution, an
// out-of-scope repo, or an API failure returns a Go error so the Job fails and
// the controller retries, with the error message carrying the repo name / HTTP
// status.
//
// The in-place ## Fix Result mutation in Run is safe only under the executor's
// single-threaded delivery per task: the StepRunner delivers once per step on
// the same *Markdown pointer, and one Job processes one task at a time. Run
// must not be invoked concurrently on the same task content.
type fixStep struct {
	// resolver resolves the deep verdict's file:line to the repository
	// holding the source (prompt-backed in production).
	resolver fixagent.RepoResolver
	// writer files the built spec through the GitHub Contents API.
	writer fixagent.SpecWriter
}

// NewFixStep wraps the fix-agent orchestration as an agentlib.Step. Run
// parses the deep verdict, resolves the repo through the injectable
// resolver, honors staleness, builds the spec, and files it through the
// injectable writer. Resolution and writing are injected so production
// (prompt-backed resolver + GitHub API writer) is stub-able in tests.
func NewFixStep(resolver fixagent.RepoResolver, writer fixagent.SpecWriter) agentlib.Step {
	return &fixStep{resolver: resolver, writer: writer}
}

func (s *fixStep) Name() string {
	return "sentry-fix"
}

func (s *fixStep) ShouldRun(
	ctx context.Context,
	md *agentlib.Markdown,
) (bool, error) {
	// Idempotent resume: a task that already carries ## Fix Result was handled
	// (filed or recorded no-op) and must never be re-run.
	_, exists := md.FindSection("## Fix Result")
	return !exists, nil
}

func (s *fixStep) Run(
	ctx context.Context,
	md *agentlib.Markdown,
) (*agentlib.Result, error) {
	content, err := md.Marshal(ctx)
	if err != nil {
		return nil, errors.Wrapf(ctx, err, "fix-agent: marshal task")
	}
	v, err := deepverdict.Parse(ctx, content)
	if err != nil {
		return nil, errors.Wrapf(ctx, err, "fix-agent: parse verdict")
	}

	// Only a `real bug` deep verdict is a machine-actionable fix-spec worth
	// filing. Every other classification is a recorded no-op, not an error.
	if v.Verdict != "real bug" {
		s.recordResult(ctx, md,
			"sentry_issue_id: "+v.SentryIssueID,
			"status: skipped",
			"message: verdict is not real bug; no spec filed",
		)
		return &agentlib.Result{Status: agentlib.AgentStatusDone, NextPhase: "done"}, nil
	}

	// A guard-forced handoff can rewrite ## Verdict with the triage schema
	// render, which has no file:line/root_cause/recommended_fix fields. Tolerate
	// that here (do NOT run deepverdict.Validate): record a skipped no-op naming
	// the missing field(s) instead of failing the Job.
	if missing := missingVerdictFields(v); len(missing) > 0 {
		s.recordResult(
			ctx,
			md,
			"sentry_issue_id: "+v.SentryIssueID,
			"status: skipped",
			"message: verdict missing required field(s) "+strings.Join(
				missing,
				", ",
			)+"; no spec filed",
		)
		return &agentlib.Result{Status: agentlib.AgentStatusDone, NextPhase: "done"}, nil
	}

	resolution, err := s.resolver.Resolve(ctx, v)
	if err != nil {
		return nil, errors.Wrapf(ctx, err, "fix-agent: resolve repo")
	}

	// A stale citation files nothing and records why — AC-2 / FM-1.
	if !resolution.Fresh {
		s.recordResult(ctx, md,
			"sentry_issue_id: "+v.SentryIssueID,
			"status: stale",
			"message: stale: "+resolution.StaleReason,
		)
		return &agentlib.Result{Status: agentlib.AgentStatusDone, NextPhase: "done"}, nil
	}

	spec, err := fixagent.BuildSpec(ctx, v, resolution)
	if err != nil {
		return nil, errors.Wrapf(ctx, err, "fix-agent: build spec")
	}

	// An out-of-scope repo or API failure returns the wrapped error (loud, the
	// writer's wrap names the repo — AC-6 / FM-3) so the Job fails and the
	// controller retries; it is never a silent no-op.
	if err := s.writer.Write(ctx, spec); err != nil {
		return nil, errors.Wrapf(ctx, err, "fix-agent: file spec")
	}

	s.recordResult(ctx, md,
		"sentry_issue_id: "+v.SentryIssueID,
		"status: filed",
		"repo: "+resolution.Repo,
		"spec_path: "+spec.Path,
		"message: filed kind: bug spec for Sentry issue "+v.SentryIssueID,
	)
	return &agentlib.Result{Status: agentlib.AgentStatusDone, NextPhase: "done"}, nil
}

// missingVerdictFields names the required real-bug verdict fields that are
// absent from v, so a guard-rewritten (triage-schema) verdict is reported
// explicitly rather than guessed.
func missingVerdictFields(v deepverdict.Verdict) []string {
	var missing []string
	if v.FileLine == "" {
		missing = append(missing, "file:line")
	}
	if v.RootCause == "" {
		missing = append(missing, "root_cause")
	}
	if v.RecommendedFix == "" {
		missing = append(missing, "recommended_fix")
	}
	return missing
}

// recordResult writes the ## Fix Result section for the given verdict: a
// `- key: value` list that always begins with the sentry issue id and carries
// status + message (plus repo/spec_path where set). The section is the durable
// outcome record — present and parseable by tests, and the ShouldRun
// idempotency gate.
func (s *fixStep) recordResult(
	ctx context.Context,
	md *agentlib.Markdown,
	lines ...string,
) {
	var body []string
	for _, line := range lines {
		select {
		case <-ctx.Done():
			return
		default:
		}
		if line == "" {
			continue
		}
		body = append(body, fmt.Sprintf("- %s", line))
	}
	md.ReplaceSection(agentlib.Section{Heading: "## Fix Result", Body: strings.Join(body, "\n")})
}
