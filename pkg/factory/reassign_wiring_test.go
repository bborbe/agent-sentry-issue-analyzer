// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package factory_test

import (
	"context"

	agentlib "github.com/bborbe/agent"
	claudelib "github.com/bborbe/agent/claude"
	agentmocks "github.com/bborbe/agent/mocks"
	"github.com/bborbe/vault-cli/pkg/domain"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/agent-sentry-issue-analyzer/pkg/factory"
)

var _ = Describe("CreateAgentFromRunner reassign wiring", func() {
	var (
		ctx        context.Context
		runner     *agentmocks.ClaudeRunner
		deliverer  *agentmocks.AgentResultDeliverer
		reassigned *agentlib.Markdown
	)

	BeforeEach(func() {
		ctx = context.Background()

		runner = &agentmocks.ClaudeRunner{}
		runner.RunReturns(&claudelib.ClaudeResult{
			Result: "```yaml\nsentry_issue_id: OCTOPUS-PROD-1J\nverdict: real bug\nconfidence: high\nreason: clear defect\n```",
		}, nil)

		deliverer = &agentmocks.AgentResultDeliverer{}

		agent := factory.CreateAgentFromRunner(runner, nil)
		_, err := agent.Run(
			ctx,
			domain.TaskPhaseExecution,
			"---\nstatus: in_progress\nphase: execution\nassignee: sentry-issue-analyzer\ntask_type: sentry-issue-analyzer\n---\n\n## Analysis\n\nroot cause\n",
			deliverer,
		)
		Expect(err).NotTo(HaveOccurred())

		// Agent.Run parses the markdown internally and never returns the
		// *Markdown, so read the re-marshaled task out of the deliverer.
		Expect(deliverer.DeliverResultCallCount()).To(Equal(1))
		_, info := deliverer.DeliverResultArgsForCall(0)
		Expect(info.Status).To(Equal(agentlib.AgentStatusInProgress))

		reassigned, err = agentlib.ParseMarkdown(ctx, info.Output)
		Expect(err).NotTo(HaveOccurred())
	})

	It("stamps the live sentry-analyzer-agent Config CR as the assignee", func() {
		Expect(reassigned.Frontmatter["assignee"]).To(Equal("sentry-analyzer-agent"))
	})

	It("keeps sentry-deep-analyzer as the task type", func() {
		Expect(reassigned.Frontmatter["task_type"]).To(Equal("sentry-deep-analyzer"))
	})

	It("keeps assignee and task_type as distinct values", func() {
		Expect(
			reassigned.Frontmatter["assignee"],
		).NotTo(Equal(reassigned.Frontmatter["task_type"]))
	})

	It("hands the task back at phase planning for the deep run", func() {
		Expect(reassigned.Frontmatter["phase"]).To(Equal("planning"))
	})
})
