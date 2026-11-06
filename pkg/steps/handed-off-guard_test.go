// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package steps_test

import (
	"context"

	agentlib "github.com/bborbe/agent"
	agentmocks "github.com/bborbe/agent/mocks"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/agent-sentry-issue-analyzer/pkg/steps"
)

var _ = Describe("HandedOffGuardStep", func() {
	var (
		ctx   context.Context
		inner *agentmocks.AgentStep
		step  agentlib.Step
	)

	BeforeEach(func() {
		ctx = context.Background()
		inner = &agentmocks.AgentStep{}
		inner.NameReturns("sentry-deep-planning")
		inner.ShouldRunReturns(true, nil)
		step = steps.NewHandedOffGuardStep(inner, "sentry-fix-agent")
	})

	taskWithAssignee := func(assignee string) *agentlib.Markdown {
		md, err := agentlib.ParseMarkdown(
			ctx,
			"---\nstatus: in_progress\nphase: planning\nassignee: "+assignee+"\n---\n\n## Failure\n\n- **Reason:** deadline_exceeded\n",
		)
		Expect(err).NotTo(HaveOccurred())
		return md
	}

	It("skips a task already handed to the fix agent, without asking the inner step", func() {
		shouldRun, err := step.ShouldRun(ctx, taskWithAssignee("sentry-fix-agent"))
		Expect(err).NotTo(HaveOccurred())
		Expect(shouldRun).To(BeFalse())
		Expect(inner.ShouldRunCallCount()).To(Equal(0))
	})

	It("defers to the inner step for a task still owned by the analyzer", func() {
		shouldRun, err := step.ShouldRun(ctx, taskWithAssignee("sentry-analyzer-agent"))
		Expect(err).NotTo(HaveOccurred())
		Expect(shouldRun).To(BeTrue())
		Expect(inner.ShouldRunCallCount()).To(Equal(1))
	})

	It("keeps the inner step's name", func() {
		Expect(step.Name()).To(Equal("sentry-deep-planning"))
	})

	It("delegates Run to the inner step", func() {
		inner.RunReturns(&agentlib.Result{Status: agentlib.AgentStatusDone}, nil)
		result, err := step.Run(ctx, taskWithAssignee("sentry-analyzer-agent"))
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Status).To(Equal(agentlib.AgentStatusDone))
		Expect(inner.RunCallCount()).To(Equal(1))
	})
})
