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
})
