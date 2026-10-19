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
	libtime "github.com/bborbe/time"

	"github.com/bborbe/agent-sentry-issue-analyzer/pkg/deepverdict"
	"github.com/bborbe/agent-sentry-issue-analyzer/pkg/fixagent"
	"github.com/bborbe/agent-sentry-issue-analyzer/pkg/identity"
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
// ## Fix Result) on non-error outcomes: an out-of-scope repo or an API failure
// returns a Go error so the Job fails and the controller retries, with the
// error message carrying the repo name / HTTP status. An unmappable
// resolution is deterministic and is recorded as a skip instead.
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
	// trace reports whether the issue's latest event carries a first-party
	// stack frame — the evidence a `resolved from frame path` claim is checked
	// against, since the claim itself is only the model's prose about the trace.
	trace fixagent.TraceReader
	// currentDateTime supplies the spec's approval timestamp, so the emitted
	// frontmatter records the run's own clock rather than a hardcoded date.
	currentDateTime libtime.CurrentDateTimeGetter
}

// NewFixStep wraps the fix-agent orchestration as an agentlib.Step. Run
// parses the deep verdict, resolves the repo through the injectable
// resolver, honors staleness, checks the resolution's claimed mechanism
// against the trace, builds the spec, and files it through the injectable
// writer. Resolution, tracing and writing are injected so production
// (prompt-backed resolver + Sentry trace reader + GitHub API writer) is
// stub-able in tests. The injected clock supplies the spec's approval
// timestamp.
func NewFixStep(
	resolver fixagent.RepoResolver,
	writer fixagent.SpecWriter,
	trace fixagent.TraceReader,
	currentDateTime libtime.CurrentDateTimeGetter,
) agentlib.Step {
	return &fixStep{
		resolver:        resolver,
		writer:          writer,
		trace:           trace,
		currentDateTime: currentDateTime,
	}
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

	hasFirstPartyFrame, done, err := s.readTraceEvidence(ctx, md, v.SentryIssueID)
	if err != nil || done != nil {
		return done, err
	}

	resolution, done, err := s.resolveRepo(ctx, md, v, hasFirstPartyFrame)
	if err != nil || done != nil {
		return done, err
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

	if done := s.verifyMechanism(ctx, md, v.SentryIssueID, resolution, hasFirstPartyFrame); done != nil {
		return done, nil
	}

	spec, err := fixagent.BuildSpec(ctx, v, resolution, s.currentDateTime)
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

// readTraceEvidence reports whether the issue's latest event carries a
// first-party stack frame. The trace is read BEFORE the resolution because it
// is the evidence the resolution's `rule` label is grounded in: a
// `resolved from frame path` rule asserts the citation came from such a frame,
// and only the trace can establish that. Reading it here lets the resolver be
// told the fact instead of inventing one from the citation's shape — the defect
// this step exists to prevent (a frame-less alert yielding a frame-path rule).
//
// An issue Sentry does not know (a 404) is deterministic: retrying can only
// fail identically, so it returns a recorded no-op Result the caller returns
// unchanged. A derived-key alert carries a key Sentry never serves as an issue
// id, so it lands here when the task knows no real id; the run used to return
// that as an error, fail the Job, exhaust its backoff limit, and be reported by
// the executor as `pod_crash_no_stdout`. Every other failure stays retryable.
func (s *fixStep) readTraceEvidence(
	ctx context.Context,
	md *agentlib.Markdown,
	issueID string,
) (bool, *agentlib.Result, error) {
	hasFirstPartyFrame, err := s.trace.HasFirstPartyFrame(ctx, traceIssueID(md, issueID))
	if err != nil {
		if errors.Is(err, fixagent.ErrIssueNotFound) {
			s.recordResult(ctx, md,
				"sentry_issue_id: "+issueID,
				"status: skipped",
				"message: "+err.Error(),
			)
			return false, &agentlib.Result{Status: agentlib.AgentStatusDone, NextPhase: "done"}, nil
		}
		return false, nil, errors.Wrapf(ctx, err, "fix-agent: read trace")
	}
	return hasFirstPartyFrame, nil, nil
}

// resolveRepo resolves the verdict's file:line to a repository, handing the
// resolver the trace evidence the rule label is grounded in. An unmappable
// resolution is deterministic for a given citation — retrying can only fail
// identically — so it returns a recorded no-op Result the caller returns
// unchanged. Every other error stays retryable.
func (s *fixStep) resolveRepo(
	ctx context.Context,
	md *agentlib.Markdown,
	v deepverdict.Verdict,
	hasFirstPartyFrame bool,
) (fixagent.Resolution, *agentlib.Result, error) {
	resolution, err := s.resolver.Resolve(
		ctx,
		v,
		implicatedRepoHint(ctx, md),
		hasFirstPartyFrame,
	)
	if err != nil {
		if errors.Is(err, fixagent.ErrUnmappable) {
			s.recordResult(ctx, md,
				"sentry_issue_id: "+v.SentryIssueID,
				"status: skipped",
				"message: "+err.Error(),
			)
			return fixagent.Resolution{}, &agentlib.Result{
				Status:    agentlib.AgentStatusDone,
				NextPhase: "done",
			}, nil
		}
		return fixagent.Resolution{}, nil, errors.Wrapf(ctx, err, "fix-agent: resolve repo")
	}
	return resolution, nil, nil
}

// verifyMechanism checks the resolution's claimed mechanism against the trace
// evidence the step already read. It returns a non-nil Result when the run is
// finished with a recorded no-op, nil when the caller should continue.
//
// A rule claiming a frame path is checked against the trace itself: on an issue
// with no first-party frame that claim is false, and a spec built on it carries
// the falsehood into a human-read document. Recorded as a no-op rather than
// returned as an error — the claim is deterministic, so an error would make the
// controller retry a run that can only fail identically.
//
// The gate is the backstop, not the prevention. The resolver is handed
// hasFirstPartyFrame so the unsupported claim is not written in the first
// place; this check catches it if the model writes one anyway.
func (s *fixStep) verifyMechanism(
	ctx context.Context,
	md *agentlib.Markdown,
	issueID string,
	resolution fixagent.Resolution,
	hasFirstPartyFrame bool,
) *agentlib.Result {
	if err := fixagent.VerifyResolutionMechanism(ctx, resolution, hasFirstPartyFrame); err != nil {
		s.recordResult(ctx, md,
			"sentry_issue_id: "+issueID,
			"status: skipped",
			"message: "+err.Error(),
		)
		return &agentlib.Result{Status: agentlib.AgentStatusDone, NextPhase: "done"}
	}
	return nil
}

// implicatedRepoHint extracts the repository the analyzer already resolved
// from the task's ## Analysis section. It is a hint extractor only: it neither
// validates the value nor decides scope — the writer's REPO_ALLOWLIST owns
// both. A value that is a bare repo-relative path yields a non-repo hint
// (`pkg/deepverdict` for `pkg/deepverdict/deepverdict.go:109`), which the
// resolver's model rejects and falls through on, so the outcome is the same as
// an absent hint. Deliberately no shape heuristic guesses between
// `owner/name/path` and `dir/dir/file`: the two are indistinguishable from
// segments alone, and a wrong guess turns a working citation into a permanent
// skip.
func implicatedRepoHint(ctx context.Context, md *agentlib.Markdown) string {
	section, ok := md.FindSection("## Analysis")
	if !ok {
		return ""
	}
	for line := range strings.SplitSeq(section.Body, "\n") {
		select {
		case <-ctx.Done():
			return ""
		default:
		}
		if !strings.Contains(strings.ToLower(line), "implicated repo") {
			continue
		}
		value := firstBacktickValue(line)
		if value == "" {
			return ""
		}
		return firstTwoSegments(value)
	}
	return ""
}

// firstBacktickValue returns the text between the first complete backtick pair
// on line, or "" when the line has no pair.
func firstBacktickValue(line string) string {
	parts := strings.Split(line, "`")
	if len(parts) < 3 {
		return ""
	}
	return parts[1]
}

// firstTwoSegments returns the first two `/`-separated segments of value, or ""
// when fewer than two non-empty segments are present.
func firstTwoSegments(value string) string {
	segments := strings.Split(value, "/")
	if len(segments) < 2 || segments[0] == "" || segments[1] == "" {
		return ""
	}
	return segments[0] + "/" + segments[1]
}

// traceIssueID picks the id the trace lookup should use. A verdict whose
// identity is a derived key names no Sentry issue, but the task's own
// frontmatter carries the real id once the collector or analyzer resolved it —
// measured 2026-10-18: verdict `b334dfcdc68af0c19f7ee5bdf78c3953`, task
// `short_id: 7196177490` (short id NUKE-PROD-9B, 2,255 events). Looking the
// derived key up instead could only 404, which is what turned a fixable alert
// into a failed Job. Anything other than a derived verdict id is used as-is.
func traceIssueID(md *agentlib.Markdown, verdictID string) string {
	if !identity.IsDerivedKey(verdictID) {
		return verdictID
	}
	// short_id is rendered through %v because the frontmatter is a generic map:
	// a numeric id written unquoted parses as an int, a quoted one as a string.
	if raw, ok := md.Frontmatter["short_id"]; ok && raw != nil {
		short := strings.TrimSpace(fmt.Sprintf("%v", raw))
		if short != "" && !identity.IsDerivedKey(short) {
			return short
		}
	}
	return verdictID
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
