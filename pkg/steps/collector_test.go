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

var _ = Describe("CollectorPlanningStep", func() {
	var (
		ctx    context.Context
		runner *claudemocks.ClaudeRunner
	)

	BeforeEach(func() {
		ctx = context.Background()
		runner = &claudemocks.ClaudeRunner{}
	})

	It("writes the ## Analysis summary section and advances to done (terminates the task)", func() {
		runner.RunReturns(&claudelib.ClaudeResult{Result: "2 tasks: SENTRY-X-1 SENTRY-X-2"}, nil)

		step := steps.NewCollectorPlanningStep(
			runner,
			prompts.BuildCollectorPlanningInstructions(),
			nil,
		)

		md, err := agentlib.ParseMarkdown(
			ctx,
			"---\nstatus: in_progress\n---\n\n## Task\n\ndaily sentry-collector trigger\n",
		)
		Expect(err).NotTo(HaveOccurred())

		result, err := step.Run(ctx, md)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Status).To(Equal(agentlib.AgentStatusDone))
		// NextPhase "done" is the terminal-literal path (agent_agent.go:91): the
		// task flips to phase done / status completed, so the executor stops
		// re-dispatching — without it the success-section re-dispatch churns
		// (deadline_exceeded, trigger_count climb) per spec 051 follow-up.
		Expect(result.NextPhase).To(Equal("done"))

		section, ok := md.FindSection("## Analysis")
		Expect(ok).To(BeTrue())
		Expect(section.Body).To(Equal("2 tasks: SENTRY-X-1 SENTRY-X-2"))
	})

	It("skips Claude when ## Analysis already exists (idempotent resume)", func() {
		runner.RunReturns(&claudelib.ClaudeResult{Result: "summary"}, nil)
		step := steps.NewCollectorPlanningStep(
			runner,
			prompts.BuildCollectorPlanningInstructions(),
			nil,
		)

		md, err := agentlib.ParseMarkdown(
			ctx,
			"---\nstatus: in_progress\n---\n\n## Analysis\n\nalready written\n",
		)
		Expect(err).NotTo(HaveOccurred())

		shouldRun, err := step.ShouldRun(ctx, md)
		Expect(err).NotTo(HaveOccurred())
		Expect(shouldRun).To(BeFalse())
	})

	It("returns failed when Claude fails", func() {
		runner.RunReturns(nil, os.ErrPermission)
		step := steps.NewCollectorPlanningStep(
			runner,
			prompts.BuildCollectorPlanningInstructions(),
			nil,
		)

		md, err := agentlib.ParseMarkdown(
			ctx,
			"---\nstatus: in_progress\n---\n\n## Task\n\ndaily sentry-collector trigger\n",
		)
		Expect(err).NotTo(HaveOccurred())

		result, err := step.Run(ctx, md)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Status).To(Equal(agentlib.AgentStatusFailed))
	})
})
