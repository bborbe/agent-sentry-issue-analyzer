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

	"github.com/bborbe/agent-sentry-issue-analyzer/pkg/identity"
	"github.com/bborbe/agent-sentry-issue-analyzer/pkg/metrics"
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
	// verdictMetrics counts the finalized verdict so the unanalyzable rate is
	// observable. Recorded after applyDisqualifiers, so the counted value is
	// the one written to the task file's ## Verdict section.
	verdictMetrics metrics.Metrics
	// identityResolver maps the model-authored sentry_issue_id to the canonical
	// short id Sentry returns for that issue, so the written identity does not
	// depend on which source the model happened to read.
	identityResolver identity.Resolver
}

// NewReassignExecutionStep wraps the triage execution step with the real-bug
// reassign trigger.
func NewReassignExecutionStep(
	execution agentlib.Step,
	deepAssignee string,
	deepTaskType string,
	disqualifiers verdict.DisqualifierEvaluator,
	verdictMetrics metrics.Metrics,
	identityResolver identity.Resolver,
) agentlib.Step {
	return &reassignExecutionStep{
		execution:        execution,
		deepAssignee:     deepAssignee,
		deepTaskType:     deepTaskType,
		disqualifiers:    disqualifiers,
		verdictMetrics:   verdictMetrics,
		identityResolver: identityResolver,
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
		if err := s.finalizeVerdict(ctx, md, &v); err != nil {
			return nil, err
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

// finalizeVerdict normalizes the identity, applies the analyzability and
// disqualifier guards in that order, and records the finalized verdict. Split
// out of Run so the sequence is one call rather than a nested block.
func (s *reassignExecutionStep) finalizeVerdict(
	ctx context.Context,
	md *agentlib.Markdown,
	v *verdict.Verdict,
) error {
	// Normalize the identity BEFORE the disqualifier override re-renders the
	// section, so that re-render is fed the already-normalized verdict and
	// cannot reintroduce the model's form. The line rewrite below also covers
	// the unparseable-dates case, where applyDisqualifiers returns early and
	// the section is otherwise left exactly as the model wrote it. This runs
	// for every verdict label, not only `real bug`: the identity flip is a
	// form-based rule about the issue reference, not a verdict judgment. A
	// resolution failure fails the run and writes nothing — the model's raw
	// form is never substituted.
	canonical, err := s.identityResolver.Resolve(ctx, v.SentryIssueID)
	if err != nil {
		return errors.Wrapf(ctx, err, "reassign: resolve identity")
	}
	v.SentryIssueID = canonical
	if section, ok := md.FindSection("## Verdict"); ok {
		section.Body = verdict.RewriteIdentityLine(section.Body, canonical)
	}
	if err := applyAnalyzabilityGuard(ctx, md, v); err != nil {
		return errors.Wrapf(ctx, err, "reassign: apply analyzability guard")
	}
	if err := applyDisqualifiers(ctx, md, v, s.disqualifiers); err != nil {
		return errors.Wrapf(ctx, err, "reassign: apply disqualifiers")
	}
	// Count the finalized verdict — after applyDisqualifiers, so the value
	// matches what the task file's ## Verdict section carries, override
	// included. An absent verdict is not counted at all.
	s.verdictMetrics.RecordVerdict(v.Verdict)
	return nil
}

// applyAnalyzabilityGuard rewrites a verdict that contradicts its own analysis
// — `unanalyzable` while the same block names a root_cause or a
// recommended_fix — and re-renders the ## Verdict section when it does.
//
// It runs before applyDisqualifiers on both verdict-writing paths and is
// independent of live state: a block that names its own cause and fix is
// analysable whatever the dates say, so this cannot sit behind the
// disqualifiers' first_seen/last_seen guard.
func applyAnalyzabilityGuard(
	ctx context.Context,
	md *agentlib.Markdown,
	v *verdict.Verdict,
) error {
	if !verdict.ApplyAnalyzabilityGuard(v) {
		return nil
	}
	rendered, err := verdict.Render(ctx, *v)
	if err != nil {
		return errors.Wrapf(ctx, err, "analyzability guard: render verdict")
	}
	if section, ok := md.FindSection("## Verdict"); ok {
		// Restore anything the triage schema does not model — the deep verdict's
		// `file:line` above all — or the re-render silently drops the pointer the
		// High/High fix handoff resolves.
		section.Body = verdict.RestoreUnmodeledLines(section.Body, rendered)
	}
	return nil
}

// applyDisqualifiers evaluates the computed disqualifiers against the
// verdict's live-state fields, forces `real bug` when any fire — except for
// `unanalyzable` verdicts, which are left exactly as the classifier wrote them
// — and records the fired disqualifiers as evidence in the ## Verdict section.
// It also stamps the urgency band, which is computed for every verdict, not
// only promoted ones.
// Shared by the reassign step (triage path) and the disqualifier guard (deep
// path). Both render unconditionally: the band has to reach the operator on a
// model-declared verdict too, which is the common case and the one the whole
// feature exists for.
//
// An earlier revision skipped the render on the deep path to protect the deep
// verdict's `file:line`, which verdict.Verdict has no field for. That was
// wrong twice over: RestoreUnmodeledLines already preserves it (the
// analyzability guard uses the same call), and skipping the render discarded
// the computed band on every non-promoted deep verdict — which is the final
// verdict for a real-bug task — while leaving a model-written `urgency:` line
// untouched.
func applyDisqualifiers(
	ctx context.Context,
	md *agentlib.Markdown,
	v *verdict.Verdict,
	disqualifiers verdict.DisqualifierEvaluator,
) error {
	// Binary-owned fields are cleared FIRST, before any early return below, and
	// the early returns strip them from the block rather than returning bare.
	// Clearing the struct field alone would not be enough: the section body is
	// what the operator reads, so a model-written `urgency:` or `promoted:` line
	// survives unless the block is rewritten. That matters most on these paths,
	// not least — a derived-key alert is precisely where the prompt tells the
	// model to write `first_seen: unavailable`, and where it has no live state
	// to derive either field from.
	//
	// The strip is targeted rather than a re-render: with no live state to
	// evaluate, the model's own block must otherwise survive verbatim, and a
	// full render would also normalise things it should not — including closing
	// a malformed fence that downstream parsing relies on rejecting.
	v.Urgency = ""
	v.Promoted = ""
	stripBinaryOwned := func() {
		if section, ok := md.FindSection("## Verdict"); ok {
			section.Body = verdict.StripKeys(section.Body, "urgency", "promoted")
		}
	}

	// A verdict that omits the live-state date fields cannot be evaluated:
	// sustained span and active burst both need first_seen/last_seen. Skip the
	// disqualifier override rather than crashing on empty dates — observed live
	// on NUKE-DEV-A4: the deep model wrote a verdict without first_seen and the
	// guard errored, failing every deep job. With the dates absent the model's
	// own verdict stands.
	if v.FirstSeen == "" || v.LastSeen == "" {
		stripBinaryOwned()
		return nil
	}
	firstSeen, err := libtime.ParseDateTime(ctx, v.FirstSeen)
	if err != nil {
		// A non-date token here is the expected shape, not a defect: the prompt
		// tells the model to leave the live-state fields unavailable for
		// derived-key alerts, and it renders that instruction as a word
		// ("unknown", "unavailable"). It means the same thing as an empty field
		// — there is no live state to evaluate, so the disqualifier override
		// does not apply. Treating it as a parse failure crashed every such Job
		// and left no verdict recorded.
		stripBinaryOwned()
		return nil
	}
	lastSeen, err := libtime.ParseDateTime(ctx, v.LastSeen)
	if err != nil {
		stripBinaryOwned()
		return nil
	}
	input := verdict.DisqualifierInput{
		LiveEventCount: v.LiveEventCount,
		FirstSeen:      firstSeen.Time(),
		LastSeen:       lastSeen.Time(),
		SentryStatus:   v.SentryStatus,
	}

	// The band is stamped for EVERY verdict, not only promoted ones. That is
	// the whole point: a model-declared `real bug` and a noise spike the
	// disqualifiers promoted both land in `real bug`, and the operator needs
	// to tell them apart without re-deriving the arithmetic.
	// One call, one clock reading: the band and the fired list describe the same
	// instant, so they cannot straddle the 24h or 7d boundary and disagree about
	// the same alert.
	evaluation, err := disqualifiers.EvaluateAll(ctx, input)
	if err != nil {
		return errors.Wrapf(ctx, err, "apply disqualifiers: evaluate")
	}
	v.Urgency = evaluation.Urgency

	fired := evaluation.Disqualifiers

	names := make([]string, 0, len(fired))
	for _, d := range fired {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		names = append(names, string(d))
	}

	if len(fired) == 0 {
		// Nothing to override, but the band still has to reach the block — on
		// both paths, because a model-declared verdict is the final verdict for
		// most real-bug tasks.
		return renderVerdictSection(ctx, md, v)
	}

	// A volume or span disqualifier says the alert is too big to be noise,
	// which is not a claim about analysability — we may have failed to analyse
	// it precisely BECAUSE it is large and long-running. Asserting
	// confidence: high about an alert we explicitly could not analyse is
	// false. Without this exemption, a sustained-span `unanalyzable` alert
	// would be relabelled `real bug`, dispatched to the deep analyzer, and
	// escalated there — the original defect wearing a different name. The
	// fired disqualifiers are still recorded as evidence below.
	if v.Verdict != "real bug" && v.Verdict != "unanalyzable" {
		v.Verdict = "real bug"
		v.Confidence = "high"
		if v.Reason != "" {
			v.Reason = "disqualifier fired (" + strings.Join(names, ", ") + "): " + v.Reason
		} else {
			v.Reason = "disqualifier fired (" + strings.Join(names, ", ") + ")"
		}
	}
	v.DisqualifiersFired = names
	// The promotion marker. `reason` and `disqualifiers_fired` already record
	// WHICH disqualifier fired, but neither states what it means for the
	// operator — this field carries both the provenance and the urgency
	// consequence, so a promoted alert and a model-declared defect do not read
	// the same. Set whenever a disqualifier fired, including the `unanalyzable`
	// exemption above, where the verdict stands but the live state still
	// promoted the alert's priority.
	v.Promoted = "disqualifier-fired (" + strings.Join(names, ", ") +
		"); urgency " + string(evaluation.Urgency)

	return renderVerdictSection(ctx, md, v)
}

// renderVerdictSection re-renders the verdict into the task's ## Verdict
// section. Extracted so both the changed and unchanged branches write the
// section the same way.
func renderVerdictSection(
	ctx context.Context,
	md *agentlib.Markdown,
	v *verdict.Verdict,
) error {
	rendered, err := verdict.Render(ctx, *v)
	if err != nil {
		return errors.Wrapf(ctx, err, "apply disqualifiers: render verdict")
	}
	if section, ok := md.FindSection("## Verdict"); ok {
		// Restore anything the triage schema does not model — the deep verdict's
		// `file:line` above all — or the re-render silently drops the pointer the
		// High/High fix handoff resolves. Same call the analyzability guard makes.
		section.Body = verdict.RestoreUnmodeledLines(section.Body, rendered)
	}
	return nil
}
