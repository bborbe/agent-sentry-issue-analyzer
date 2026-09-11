// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package factory_test

import (
	"context"

	agentlib "github.com/bborbe/agent"
	claudelib "github.com/bborbe/agent/claude"
	agentmocks "github.com/bborbe/agent/mocks"
	libtime "github.com/bborbe/time"
	"github.com/bborbe/vault-cli/pkg/domain"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/agent-sentry-issue-analyzer/pkg/factory"
)

var _ = Describe("CreateDeepAgentFromRunner fix handoff wiring", func() {
	var (
		ctx       context.Context
		runner    *agentmocks.ClaudeRunner
		deliverer *agentmocks.AgentResultDeliverer
		handedOff *agentlib.Markdown
	)

	BeforeEach(func() {
		ctx = context.Background()

		runner = &agentmocks.ClaudeRunner{}
		runner.RunReturns(&claudelib.ClaudeResult{
			Result: "```yaml\nsentry_issue_id: OCTOPUS-PROD-1J\nverdict: real bug\nunderstanding: High\nfix_certainty: High\nroot_cause: nil check missing\nrecommended_fix: add nil guard\nfile:line: pkg/handler/handler.go:142\nlive_event_count: 50\nfirst_seen: 2026-08-20T00:00:00Z\nlast_seen: 2026-09-05T10:00:00Z\nsentry_status: unresolved\n```",
		}, nil)

		deliverer = &agentmocks.AgentResultDeliverer{}

		agent := factory.CreateDeepAgentFromRunner(runner, nil, libtime.NewCurrentDateTime())
		_, err := agent.Run(
			ctx,
			domain.TaskPhaseExecution,
			"---\nstatus: in_progress\nphase: execution\nassignee: sentry-analyzer-agent\ntask_type: sentry-deep-analyzer\n---\n\n## Context\n\ndeep analysis\n",
			deliverer,
		)
		Expect(err).NotTo(HaveOccurred())

		// Agent.Run parses the markdown internally and never returns the
		// *Markdown, so read the re-marshaled task out of the deliverer.
		Expect(deliverer.DeliverResultCallCount()).To(Equal(1))
		_, info := deliverer.DeliverResultArgsForCall(0)
		Expect(info.Status).To(Equal(agentlib.AgentStatusInProgress))

		handedOff, err = agentlib.ParseMarkdown(ctx, info.Output)
		Expect(err).NotTo(HaveOccurred())
	})

	It("stamps the live sentry-fix-agent Config CR as the assignee", func() {
		Expect(handedOff.Frontmatter["assignee"]).To(Equal("sentry-fix-agent"))
	})

	It("stamps sentry-fix as the task type", func() {
		Expect(handedOff.Frontmatter["task_type"]).To(Equal("sentry-fix"))
	})

	It("keeps assignee and task_type as distinct values", func() {
		Expect(
			handedOff.Frontmatter["assignee"],
		).NotTo(Equal(handedOff.Frontmatter["task_type"]))
	})

	It("hands the task back at phase planning for the fix run", func() {
		Expect(handedOff.Frontmatter["phase"]).To(Equal("planning"))
	})
})
