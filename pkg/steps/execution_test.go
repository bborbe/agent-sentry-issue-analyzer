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

var _ = Describe("ExecutionStep", func() {
	var (
		ctx    context.Context
		runner *claudemocks.ClaudeRunner
	)

	BeforeEach(func() {
		ctx = context.Background()
		runner = &claudemocks.ClaudeRunner{}
	})

	It("writes the ## Verdict section and advances to done", func() {
		runner.RunReturns(&claudelib.ClaudeResult{
			Result: "verdict: real bug\nconfidence: high\nroot cause: nil deref\n",
		}, nil)

		step := steps.NewExecutionStep(
			runner,
			prompts.BuildExecutionInstructions(),
			nil,
		)

		md, err := agentlib.ParseMarkdown(
			ctx,
			"---\nstatus: in_progress\n---\n\n## Analysis\n\nroot cause analysis\n",
		)
		Expect(err).NotTo(HaveOccurred())

		result, err := step.Run(ctx, md)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Status).To(Equal(agentlib.AgentStatusDone))
		Expect(result.NextPhase).To(Equal("done"))

		section, ok := md.FindSection("## Verdict")
		Expect(ok).To(BeTrue())
		Expect(section.Body).To(ContainSubstring("verdict: real bug"))
	})

	It("skips Claude when ## Verdict already exists (idempotent resume)", func() {
		runner.RunReturns(&claudelib.ClaudeResult{Result: "verdict: noise\n"}, nil)
		step := steps.NewExecutionStep(
			runner,
			prompts.BuildExecutionInstructions(),
			nil,
		)

		md, err := agentlib.ParseMarkdown(
			ctx,
			"---\nstatus: in_progress\n---\n\n## Analysis\n\nanalysis\n\n## Verdict\n\nverdict: noise\n",
		)
		Expect(err).NotTo(HaveOccurred())

		shouldRun, err := step.ShouldRun(ctx, md)
		Expect(err).NotTo(HaveOccurred())
		Expect(shouldRun).To(BeFalse())
	})

	It("returns failed when Claude fails", func() {
		runner.RunReturns(nil, os.ErrPermission)
		step := steps.NewExecutionStep(
			runner,
			prompts.BuildExecutionInstructions(),
			nil,
		)

		md, err := agentlib.ParseMarkdown(
			ctx,
			"---\nstatus: in_progress\n---\n\n## Analysis\n\nanalysis\n",
		)
		Expect(err).NotTo(HaveOccurred())

		result, err := step.Run(ctx, md)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Status).To(Equal(agentlib.AgentStatusFailed))
	})
})
