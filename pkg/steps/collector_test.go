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

	It("returns failed with no NextPhase when new alerts were expected but none landed", func() {
		// The 2026-08-26/27/28 signature: publishes succeed, the controller
		// drops everything, zero per-alert task files land.
		runner.RunReturns(&claudelib.ClaudeResult{
			Result: "Fetched 48 active unresolved alerts.\n" +
				"sentry-create-tasks-result: fetched=48 published=48 expected_new=48 landed=0 observed=true status=failed",
		}, nil)

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
		// Failure contract: no NextPhase — the controller owns unassign +
		// ## Failure. "human_review" is reserved for a successful verdict
		// that needs a human, never for a failure.
		Expect(result.NextPhase).To(BeEmpty())
	})

	It("stays done on a quiet day where every fetched alert was already tracked", func() {
		// Dedup is the designed idempotency: zero created with zero new alerts
		// expected is healthy, not an alarm.
		runner.RunReturns(&claudelib.ClaudeResult{
			Result: "sentry-create-tasks-result: fetched=48 published=0 expected_new=0 landed=0 observed=true status=done",
		}, nil)

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
		Expect(result.NextPhase).To(Equal("done"))
	})

	It("stays done when task files landed", func() {
		runner.RunReturns(&claudelib.ClaudeResult{
			Result: "sentry-create-tasks-result: fetched=48 published=48 expected_new=48 landed=2 observed=true status=done",
		}, nil)

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
		Expect(result.NextPhase).To(Equal("done"))
	})

	It("returns failed when the vault could not be observed at all", func() {
		// observed=false is NOT an observed zero. A run that could not read the
		// vault proves nothing, so it must not report success — otherwise an
		// auth/network failure becomes a silent green.
		runner.RunReturns(&claudelib.ClaudeResult{
			Result: "sentry-create-tasks-result: fetched=48 published=48 expected_new=0 landed=0 observed=false status=unobserved",
		}, nil)

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
		Expect(result.NextPhase).To(BeEmpty())
	})

	It("stays done when the summary carries no result line", func() {
		// Only positive evidence downgrades the status — a missing line must
		// never turn every run into a failure.
		runner.RunReturns(&claudelib.ClaudeResult{Result: "summary with no result line"}, nil)

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
		Expect(result.NextPhase).To(Equal("done"))
	})
})
