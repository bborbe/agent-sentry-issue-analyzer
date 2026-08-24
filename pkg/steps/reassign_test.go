// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package steps_test

import (
	"context"
	"os"

	agentlib "github.com/bborbe/agent"
	claudelib "github.com/bborbe/agent/claude"
	claudemocks "github.com/bborbe/agent/mocks"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/agent-sentry-issue-analyzer/pkg/prompts"
	"github.com/bborbe/agent-sentry-issue-analyzer/pkg/steps"
)

var _ = Describe("ReassignExecutionStep", func() {
	var (
		ctx    context.Context
		runner *claudemocks.ClaudeRunner
	)

	BeforeEach(func() {
		ctx = context.Background()
		runner = &claudemocks.ClaudeRunner{}
	})

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
			Result: "```yaml\nsentry_issue_id: OCTOPUS-PROD-1J\nverdict: real bug\nconfidence: high\nreason: clear defect\n```",
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
			Result: "sentry_issue_id: OCTOPUS-PROD-C\nverdict: noise\nreason: kafka metadata sync\n",
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
})
