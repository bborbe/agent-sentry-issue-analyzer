// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package steps_test

import (
	"context"
	"os"
	"time"

	agentlib "github.com/bborbe/agent"
	claudelib "github.com/bborbe/agent/claude"
	claudemocks "github.com/bborbe/agent/mocks"
	libtime "github.com/bborbe/time"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/agent-sentry-issue-analyzer/pkg/prompts"
	"github.com/bborbe/agent-sentry-issue-analyzer/pkg/steps"
	"github.com/bborbe/agent-sentry-issue-analyzer/pkg/verdict"
)

var _ = Describe("ReassignExecutionStep", func() {
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

	buildStep := func() agentlib.Step {
		execution := steps.NewExecutionStep(
			runner,
			prompts.BuildExecutionInstructions(),
			nil,
		)
		return steps.NewReassignExecutionStep(
			execution,
			"sentry-deep-analyzer",
			"sentry-deep-analyzer",
			newEvaluator(),
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

	It("reassigns the task to the deep analyzer on a real-bug verdict", func() {
		runner.RunReturns(&claudelib.ClaudeResult{
			Result: "```yaml\nsentry_issue_id: OCTOPUS-PROD-1J\nverdict: real bug\nconfidence: high\nreason: clear defect\nlive_event_count: 50\nfirst_seen: 2026-08-20T00:00:00Z\nlast_seen: 2026-09-05T10:00:00Z\nsentry_status: unresolved\n```",
		}, nil)

		step := buildStep()
		md := buildTask("")

		result, err := step.Run(ctx, md)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Status).To(Equal(agentlib.AgentStatusInProgress))
		Expect(result.Message).To(ContainSubstring("sentry-deep-analyzer"))
		Expect(md.Frontmatter["assignee"]).To(Equal("sentry-deep-analyzer"))
		Expect(md.Frontmatter["phase"]).To(Equal("planning"))
		Expect(md.Frontmatter["task_type"]).To(Equal("sentry-deep-analyzer"))

		// The verdict section is still written for the deep agent to read.
		section, ok := md.FindSection("## Verdict")
		Expect(ok).To(BeTrue())
		Expect(section.Body).To(ContainSubstring("verdict: real bug"))
	})

	It("leaves the task untouched on a non-real-bug verdict", func() {
		runner.RunReturns(&claudelib.ClaudeResult{
			Result: "sentry_issue_id: OCTOPUS-PROD-C\nverdict: noise\nreason: kafka metadata sync\nlive_event_count: 50\nfirst_seen: 2026-08-20T00:00:00Z\nlast_seen: 2026-09-05T10:00:00Z\nsentry_status: unresolved\n",
		}, nil)

		step := buildStep()
		md := buildTask("")

		result, err := step.Run(ctx, md)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Status).To(Equal(agentlib.AgentStatusDone))
		Expect(md.Frontmatter["assignee"]).To(Equal("sentry-issue-analyzer"))
		Expect(md.Frontmatter["phase"]).To(Equal("execution"))
	})

	It("propagates a non-done result (Claude failure)", func() {
		runner.RunReturns(nil, os.ErrPermission)
		step := buildStep()
		md := buildTask("")

		result, err := step.Run(ctx, md)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Status).To(Equal(agentlib.AgentStatusFailed))
	})

	It("skips when the execution step would skip (idempotent resume)", func() {
		step := buildStep()
		md, err := agentlib.ParseMarkdown(
			ctx,
			"---\nstatus: in_progress\n---\n\n## Analysis\n\nalready written\n\n## Verdict\n\nverdict: noise\n",
		)
		Expect(err).NotTo(HaveOccurred())

		shouldRun, err := step.ShouldRun(ctx, md)
		Expect(err).NotTo(HaveOccurred())
		Expect(shouldRun).To(BeFalse())
	})

	It("forces real bug when a computed disqualifier fires despite a noise verdict", func() {
		runner.RunReturns(&claudelib.ClaudeResult{
			Result: "```yaml\nsentry_issue_id: NUKE-DEV-3A\nverdict: noise\nreason: rate < 1/day\nlive_event_count: 1262\nfirst_seen: 2024-04-14T08:14:20Z\nlast_seen: 2026-09-05T11:49:21Z\nsentry_status: unresolved\n```",
		}, nil)

		step := buildStep()
		md := buildTask("")

		result, err := step.Run(ctx, md)
		Expect(err).NotTo(HaveOccurred())
		// The forced real bug reassigns to the deep analyzer.
		Expect(result.Status).To(Equal(agentlib.AgentStatusInProgress))
		Expect(md.Frontmatter["assignee"]).To(Equal("sentry-deep-analyzer"))

		section, ok := md.FindSection("## Verdict")
		Expect(ok).To(BeTrue())
		Expect(section.Body).To(ContainSubstring("verdict: real bug"))
		Expect(section.Body).To(ContainSubstring("disqualifiers_fired"))
		Expect(section.Body).To(ContainSubstring("Sustained span"))
	})

	It("keeps noise when no disqualifier fires", func() {
		runner.RunReturns(&claudelib.ClaudeResult{
			Result: "```yaml\nsentry_issue_id: NUKE-DEV-94\nverdict: noise\nreason: pod shutdown race\nlive_event_count: 84\nfirst_seen: 2025-10-15T10:42:13Z\nlast_seen: 2026-09-05T12:14:43Z\nsentry_status: unresolved\n```",
		}, nil)

		step := buildStep()
		md := buildTask("")

		result, err := step.Run(ctx, md)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Status).To(Equal(agentlib.AgentStatusDone))
		// Untouched: still noise, no reassign, section not rewritten.
		Expect(md.Frontmatter["assignee"]).To(Equal("sentry-issue-analyzer"))
		Expect(md.Frontmatter["phase"]).To(Equal("execution"))

		section, ok := md.FindSection("## Verdict")
		Expect(ok).To(BeTrue())
		Expect(section.Body).To(ContainSubstring("verdict: noise"))
	})
})
