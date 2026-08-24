// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package steps_test

import (
	"context"

	agentlib "github.com/bborbe/agent"
	claudelib "github.com/bborbe/agent/claude"
	claudemocks "github.com/bborbe/agent/mocks"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/agent-sentry-issue-analyzer/pkg/prompts"
	"github.com/bborbe/agent-sentry-issue-analyzer/pkg/steps"
)

var _ = Describe("ReassignExecutionStep via full agent", func() {
	It("produces the reassigned frontmatter end-to-end (planning→execution)", func() {
		ctx := context.Background()
		runner := &claudemocks.ClaudeRunner{}
		runner.RunReturns(&claudelib.ClaudeResult{
			Result: "```yaml\nsentry_issue_id: OCTOPUS-PROD-1J\nverdict: real bug\nconfidence: high\nreason: clear defect\n```",
		}, nil)

		execution := steps.NewReassignExecutionStep(
			steps.NewExecutionStep(runner, prompts.BuildExecutionInstructions(), nil),
			"sentry-deep-analyzer",
			"sentry-deep-analyzer",
		)
		agent := agentlib.NewAgent(agentlib.NewPhase("execution", execution))

		deliverer := &recordingDeliverer{}
		_, err := agent.Run(
			ctx,
			"execution",
			"---\nstatus: in_progress\nphase: execution\nassignee: sentry-issue-analyzer\ntask_type: sentry-issue-analyzer\n---\n\n## Analysis\n\nroot cause\n",
			deliverer,
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(len(deliverer.results)).To(Equal(1))
		Expect(deliverer.results[0].Status).To(Equal(agentlib.AgentStatusInProgress))
		// The delivered output carries the reassigned frontmatter.
		Expect(deliverer.results[0].Output).To(ContainSubstring("assignee: sentry-deep-analyzer"))
		Expect(deliverer.results[0].Output).To(ContainSubstring("phase: planning"))
		Expect(deliverer.results[0].Output).To(ContainSubstring("task_type: sentry-deep-analyzer"))
	})
})

type recordingDeliverer struct {
	results []agentlib.AgentResultInfo
}

func (d *recordingDeliverer) DeliverResult(_ context.Context, info agentlib.AgentResultInfo) error {
	d.results = append(d.results, info)
	return nil
}
