// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package steps_test

import (
	"context"
	"time"

	agentlib "github.com/bborbe/agent"
	claudelib "github.com/bborbe/agent/claude"
	claudemocks "github.com/bborbe/agent/mocks"
	libtime "github.com/bborbe/time"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	metricsmocks "github.com/bborbe/agent-sentry-issue-analyzer/pkg/metrics/mocks"
	"github.com/bborbe/agent-sentry-issue-analyzer/pkg/prompts"
	"github.com/bborbe/agent-sentry-issue-analyzer/pkg/steps"
	"github.com/bborbe/agent-sentry-issue-analyzer/pkg/verdict"
)

// The urgency band must reach the ## Verdict block the execution step writes
// back, not merely exist as a function — that is what the operator reads, and
// a unit test on the band alone would pass while the block carried nothing.
var _ = Describe("urgency band in the rendered verdict block", func() {
	var (
		ctx    context.Context
		runner *claudemocks.ClaudeRunner
		now    time.Time
	)

	BeforeEach(func() {
		ctx = context.Background()
		runner = &claudemocks.ClaudeRunner{}
		now = time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	})

	buildStep := func() agentlib.Step {
		return steps.NewReassignExecutionStep(
			steps.NewExecutionStep(runner, prompts.BuildExecutionInstructions(), nil),
			"sentry-deep-analyzer",
			"sentry-deep-analyzer",
			verdict.NewDisqualifierEvaluator(
				libtime.CurrentDateTimeGetterFunc(func() libtime.DateTime {
					return libtime.DateTime(now)
				}),
			),
			&metricsmocks.Metrics{},
			echoIdentityResolver(),
		)
	}

	// renderBlock runs the real step over a task with no ## Verdict section, so
	// the wrapped execution step runs and writes one, and returns the block the
	// step wrote back.
	renderBlock := func(modelBlock string) string {
		md, err := agentlib.ParseMarkdown(
			ctx,
			"---\nstatus: in_progress\nphase: execution\nassignee: sentry-issue-analyzer\ntask_type: sentry-issue-analyzer\n---\n\n## Analysis\n\nroot cause\n",
		)
		Expect(err).NotTo(HaveOccurred())
		runner.RunReturns(&claudelib.ClaudeResult{Result: modelBlock}, nil)
		_, err = buildStep().Run(ctx, md)
		Expect(err).NotTo(HaveOccurred())
		section, ok := md.FindSection("## Verdict")
		Expect(ok).To(BeTrue())
		return section.Body
	}

	modelBlock := func(
		label string,
		count string,
		lastSeen string,
		extra string,
	) string {
		return "```yaml\n" +
			"sentry_issue_id: OCTOPUS-PROD-1J\n" +
			"verdict: " + label + "\n" +
			"confidence: high\n" +
			"reason: clear defect\n" +
			"live_event_count: " + count + "\n" +
			"first_seen: 2026-08-20T00:00:00Z\n" +
			"last_seen: " + lastSeen + "\n" +
			"sentry_status: unresolved\n" +
			extra +
			"```"
	}

	It("splits two real-bug alerts differing only in live_event_count", func() {
		high := renderBlock(modelBlock("real bug", "20000", "2026-09-05T10:00:00Z", ""))
		low := renderBlock(modelBlock("real bug", "5", "2026-09-05T10:00:00Z", ""))

		Expect(high).To(ContainSubstring("urgency: now"))
		Expect(low).To(ContainSubstring("urgency: someday"))
		Expect(high).NotTo(Equal(low))
	})

	It("splits two alerts with equal live_event_count differing only in last_seen", func() {
		recent := renderBlock(modelBlock("real bug", "500", "2026-09-06T10:00:00Z", ""))
		stale := renderBlock(modelBlock("real bug", "500", "2026-07-01T00:00:00Z", ""))

		Expect(recent).To(ContainSubstring("urgency: soon"))
		Expect(stale).To(ContainSubstring("urgency: someday"))
		Expect(recent).NotTo(Equal(stale))
	})

	It("overwrites an urgency value the model wrote itself", func() {
		// The model must never choose the band. A contradicting value proves the
		// binary sets it rather than merely defaulting when it is absent.
		block := renderBlock(
			modelBlock("real bug", "5", "2026-07-01T00:00:00Z", "urgency: now\n"),
		)

		Expect(block).To(ContainSubstring("urgency: someday"))
		Expect(block).NotTo(ContainSubstring("urgency: now"))
	})

	It("marks a promoted alert and leaves a declared defect unmarked", func() {
		promoted := renderBlock(modelBlock("noise", "20000", "2026-09-05T10:00:00Z", ""))
		declared := renderBlock(modelBlock("real bug", "5", "2026-07-01T00:00:00Z", ""))

		Expect(promoted).To(ContainSubstring("promoted: disqualifier-fired (Volume); urgency now"))
		Expect(declared).NotTo(ContainSubstring("promoted:"))
	})

	It("strips model-written binary-owned fields when the dates are unavailable", func() {
		// A derived-key alert is where the prompt tells the model to write
		// `unavailable` for the live-state fields, so no band can be derived and
		// applyDisqualifiers returns early. That path must still strip the
		// binary-owned keys: it is the case most likely to carry an invented
		// value, not the least.
		block := renderBlock("```yaml\n" +
			"sentry_issue_id: OCTOPUS-PROD-1J\n" +
			"verdict: real bug\n" +
			"confidence: low\n" +
			"reason: derived-key alert, no live state\n" +
			"live_event_count: unavailable\n" +
			"first_seen: unavailable\n" +
			"last_seen: unavailable\n" +
			"sentry_status: unknown\n" +
			"urgency: now\n" +
			"promoted: disqualifier-fired (Volume); urgency now\n" +
			"understanding: low\n" +
			"fix_certainty: low\n" +
			"root_cause: unknown\n" +
			"recommended_fix: unknown\n" +
			"```")

		Expect(block).NotTo(ContainSubstring("urgency:"))
		Expect(block).NotTo(ContainSubstring("promoted:"))
		// Everything else the model wrote still stands.
		Expect(block).To(ContainSubstring("verdict: real bug"))
	})
})

// The deep path renders through the disqualifier guard, not the reassign step.
// An earlier revision skipped that re-render when no disqualifier fired, on the
// belief that a re-render would drop the deep verdict's `file:line`. It would
// not — verdict.RestoreUnmodeledLines already preserves it, as the
// analyzability guard proves on the same path — and the skip discarded the
// computed band on every non-promoted deep verdict, which is the final verdict
// for a real-bug task, while letting a model-written `urgency:` through.
var _ = Describe("urgency band on the deep path", func() {
	var (
		ctx    context.Context
		runner *claudemocks.ClaudeRunner
		now    time.Time
	)

	BeforeEach(func() {
		ctx = context.Background()
		runner = &claudemocks.ClaudeRunner{}
		now = time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	})

	buildStep := func() agentlib.Step {
		return steps.NewDisqualifierGuardStep(
			steps.NewDeepExecutionStep(
				runner,
				prompts.BuildDeepExecutionInstructions(),
				nil,
			),
			verdict.NewDisqualifierEvaluator(
				libtime.CurrentDateTimeGetterFunc(func() libtime.DateTime {
					return libtime.DateTime(now)
				}),
			),
			&metricsmocks.Metrics{},
			echoDeepIdentityResolver(),
		)
	}

	renderDeep := func(modelBlock string) string {
		md, err := agentlib.ParseMarkdown(
			ctx,
			"---\nstatus: in_progress\nphase: execution\nassignee: sentry-analyzer-agent\ntask_type: sentry-deep-analyzer\n---\n\n## Context\n\ndeep analysis\n",
		)
		Expect(err).NotTo(HaveOccurred())
		runner.RunReturns(&claudelib.ClaudeResult{Result: modelBlock}, nil)
		_, err = buildStep().Run(ctx, md)
		Expect(err).NotTo(HaveOccurred())
		section, ok := md.FindSection("## Verdict")
		Expect(ok).To(BeTrue())
		return section.Body
	}

	// firstSeen/lastSeen are parameters because the sustained-span disqualifier
	// is a function of the span, so a fixed pair would silently add it to the
	// cases meant to isolate a different one.
	deepBlock := func(count, firstSeen, lastSeen, extra string) string {
		return "```yaml\n" +
			"sentry_issue_id: NUKE-DEV-94\n" +
			"verdict: real bug\n" +
			"understanding: high\n" +
			"fix_certainty: high\n" +
			"root_cause: a cause\n" +
			"recommended_fix: a fix\n" +
			"file:line: pkg/foo/bar.go:42\n" +
			"live_event_count: " + count + "\n" +
			"first_seen: " + firstSeen + "\n" +
			"last_seen: " + lastSeen + "\n" +
			"sentry_status: unresolved\n" +
			extra +
			"```"
	}

	// Five events over a 259-day span: the rate stays below 1/day and the count
	// below 100, so the sustained-span disqualifier does not fire either.
	quiet := func(extra string) string {
		return deepBlock("5", "2025-10-15T10:42:13Z", "2026-07-01T00:00:00Z", extra)
	}

	It("stamps the band when no disqualifier fires", func() {
		block := renderDeep(quiet(""))
		Expect(block).To(ContainSubstring("urgency: someday"))
		Expect(block).NotTo(ContainSubstring("promoted:"))
	})

	It("keeps the deep verdict's file:line through the re-render", func() {
		// The pointer the High/High fix handoff resolves. It is why the render
		// used to be skipped, so it is the thing to assert on.
		Expect(renderDeep(quiet(""))).To(ContainSubstring("file:line: pkg/foo/bar.go:42"))
	})

	It("overwrites a model-written urgency", func() {
		block := renderDeep(quiet("urgency: now\n"))
		Expect(block).To(ContainSubstring("urgency: someday"))
		Expect(block).NotTo(ContainSubstring("urgency: now"))
	})

	It("clears a model-written promoted when nothing fires", func() {
		block := renderDeep(quiet("promoted: model invented this\n"))
		Expect(block).NotTo(ContainSubstring("model invented this"))
	})

	It("marks a promoted verdict and still keeps file:line", func() {
		// A 16-day span, so Volume is the only disqualifier that can fire.
		block := renderDeep(
			deepBlock("20000", "2026-08-20T00:00:00Z", "2026-09-05T10:00:00Z", ""),
		)
		Expect(block).To(ContainSubstring("urgency: now"))
		Expect(block).To(ContainSubstring("promoted: disqualifier-fired (Volume); urgency now"))
		Expect(block).To(ContainSubstring("file:line: pkg/foo/bar.go:42"))
	})

	It("omits the band when the dates cannot be parsed", func() {
		// The guard skips evaluation without first_seen/last_seen, so no band can
		// be derived. It must be absent — a fabricated band would assert a live
		// state the run never observed.
		block := renderDeep(
			"```yaml\nsentry_issue_id: NUKE-DEV-A4\nverdict: noise\nlive_event_count: 193\n```",
		)
		Expect(block).NotTo(ContainSubstring("urgency:"))
	})
})
