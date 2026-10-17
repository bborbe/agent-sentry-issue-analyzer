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

// The counter is only useful if it counts what actually happened: a run that
// increments it without reading a real verdict produces a series that grows
// while saying nothing. These specs pin the recording to the verdict the task
// file carries, which is the same cross-check the task's SC1 applies against
// the produced task files.
var _ = Describe("verdict recording", func() {
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

	newEvaluator := func() verdict.DisqualifierEvaluator {
		return verdict.NewDisqualifierEvaluator(
			libtime.CurrentDateTimeGetterFunc(func() libtime.DateTime {
				return libtime.DateTime(now)
			}),
		)
	}

	buildTask := func(body string) *agentlib.Markdown {
		md, err := agentlib.ParseMarkdown(
			ctx,
			"---\nstatus: in_progress\nphase: execution\nassignee: sentry-issue-analyzer\ntask_type: sentry-issue-analyzer\n---\n\n## Analysis\n\nroot cause\n\n"+body,
		)
		Expect(err).NotTo(HaveOccurred())
		return md
	}

	// verdictInSection reads the verdict back out of the task file, the way the
	// task's cross-check reads it from the produced files.
	verdictInSection := func(md *agentlib.Markdown) string {
		section, ok := md.FindSection("## Verdict")
		Expect(ok).To(BeTrue())
		parsed, err := verdict.Parse(ctx, "## Verdict\n\n"+section.Body)
		Expect(err).NotTo(HaveOccurred())
		return parsed.Verdict
	}

	Describe("triage (reassign) step", func() {
		buildStep := func(recorder *metricsmocks.Metrics) agentlib.Step {
			return steps.NewReassignExecutionStep(
				steps.NewExecutionStep(
					runner,
					prompts.BuildExecutionInstructions(),
					nil,
				),
				"sentry-deep-analyzer",
				"sentry-deep-analyzer",
				newEvaluator(),
				recorder,
				echoIdentityResolver(),
			)
		}

		It("records the verdict the task file carries", func() {
			// Asserted against the ## Verdict section rather than a hard-coded
			// value: a fired disqualifier legitimately rewrites the verdict, and
			// the counter must follow the file either way.
			runner.RunReturns(&claudelib.ClaudeResult{
				Result: "```yaml\nsentry_issue_id: OCTOPUS-PROD-C\nverdict: noise\nconfidence: high\nreason: kafka metadata sync\nlive_event_count: 50\nfirst_seen: 2026-08-20T00:00:00Z\nlast_seen: 2026-09-05T10:00:00Z\nsentry_status: unresolved\n```",
			}, nil)
			recorder := &metricsmocks.Metrics{}

			md := buildTask("")
			_, err := buildStep(recorder).Run(ctx, md)
			Expect(err).NotTo(HaveOccurred())

			Expect(recorder.RecordVerdictCallCount()).To(Equal(1))
			Expect(recorder.RecordVerdictArgsForCall(0)).NotTo(BeEmpty())
			Expect(recorder.RecordVerdictArgsForCall(0)).To(Equal(verdictInSection(md)))
		})

		It("counts an unanalyzable verdict", func() {
			// The value this whole metric exists to expose must reach the
			// counter — it is the 89% of runs that were previously invisible.
			// No live-state dates, so no disqualifier can override it.
			runner.RunReturns(&claudelib.ClaudeResult{
				Result: "```yaml\nsentry_issue_id: OCTOPUS-PROD-9Z\nverdict: unanalyzable\nreason: no first-party frame\nlive_event_count: 3\n```",
			}, nil)
			recorder := &metricsmocks.Metrics{}

			md := buildTask("")
			_, err := buildStep(recorder).Run(ctx, md)
			Expect(err).NotTo(HaveOccurred())

			Expect(recorder.RecordVerdictCallCount()).To(Equal(1))
			Expect(recorder.RecordVerdictArgsForCall(0)).To(Equal("unanalyzable"))
		})

		It("records nothing when the run wrote no verdict", func() {
			// An absent verdict is not a verdict. Counting it would inflate the
			// denominator of every ratio computed over this counter.
			runner.RunReturns(&claudelib.ClaudeResult{
				Result: "## Analysis\n\nno verdict block written\n",
			}, nil)
			recorder := &metricsmocks.Metrics{}

			md := buildTask("")
			_, err := buildStep(recorder).Run(ctx, md)
			Expect(err).NotTo(HaveOccurred())

			Expect(recorder.RecordVerdictCallCount()).To(BeZero())
		})
	})

	Describe("deep (disqualifier guard) step", func() {
		buildStep := func(recorder *metricsmocks.Metrics) agentlib.Step {
			return steps.NewDisqualifierGuardStep(
				steps.NewDeepExecutionStep(
					runner,
					prompts.BuildDeepExecutionInstructions(),
					nil,
				),
				newEvaluator(),
				recorder,
			)
		}

		It("records the verdict the task file carries", func() {
			runner.RunReturns(&claudelib.ClaudeResult{
				Result: "```yaml\nsentry_issue_id: OCTOPUS-PROD-1J\nverdict: noise\nunderstanding: Low\nfix_certainty: Low\nroot_cause: none\nlive_event_count: 50\nfirst_seen: 2026-08-20T00:00:00Z\nlast_seen: 2026-09-05T10:00:00Z\nsentry_status: unresolved\n```",
			}, nil)
			recorder := &metricsmocks.Metrics{}

			md := buildTask("")
			_, err := buildStep(recorder).Run(ctx, md)
			Expect(err).NotTo(HaveOccurred())

			Expect(recorder.RecordVerdictCallCount()).To(Equal(1))
			Expect(recorder.RecordVerdictArgsForCall(0)).To(Equal(verdictInSection(md)))
		})
	})
})
