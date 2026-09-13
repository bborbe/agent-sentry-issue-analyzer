// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package steps_test

import (
	"context"

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

var _ = Describe("ReassignExecutionStep via full agent", func() {
	// The step is where the Job actually dies: a verdict block that fails to parse
	// aborts the reassign step before any frontmatter is written, so both blocks
	// below are asserted through the step machinery rather than through
	// verdict.Parse alone. The colon block is the 2026-09-13 production failure,
	// verbatim from the pod logs (the recommended_fix prose was emitted as one
	// line; the surrounding keys are reconstructed to the schema, and their order
	// is what puts recommended_fix on line 13, matching the production error).
	DescribeTable("produces the reassigned frontmatter end-to-end (planning→execution)",
		func(block string) {
			ctx := context.Background()
			runner := &claudemocks.ClaudeRunner{}
			runner.RunReturns(&claudelib.ClaudeResult{Result: block}, nil)

			execution := steps.NewReassignExecutionStep(
				steps.NewExecutionStep(runner, prompts.BuildExecutionInstructions(), nil),
				"sentry-deep-analyzer",
				"sentry-deep-analyzer",
				verdict.NewDisqualifierEvaluator(libtime.NewCurrentDateTime()),
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
			Expect(deliverer.results).To(HaveLen(1))
			Expect(deliverer.results[0].Status).To(Equal(agentlib.AgentStatusInProgress))
			// The delivered output carries the reassigned frontmatter.
			Expect(
				deliverer.results[0].Output,
			).To(ContainSubstring("assignee: sentry-deep-analyzer"))
			Expect(deliverer.results[0].Output).To(ContainSubstring("phase: planning"))
			Expect(
				deliverer.results[0].Output,
			).To(ContainSubstring("task_type: sentry-deep-analyzer"))
		},
		Entry(
			"a schema-valid verdict block",
			"```yaml\nsentry_issue_id: OCTOPUS-PROD-1J\nverdict: real bug\nconfidence: high\nreason: clear defect\nlive_event_count: 50\nfirst_seen: 2026-08-20T00:00:00Z\nlast_seen: 2026-09-05T10:00:00Z\nsentry_status: unresolved\n```",
		),
		Entry(
			"the 2026-09-13 colon block",
			"```yaml\nsentry_issue_id: NUKE-PROD-XX\nverdict: real bug\nconfidence: high\nreason: Kafka topic deployment fails when the API server connection drops\nlive_event_count: 2\nfirst_seen: 2026-08-08T10:34:43Z\nlast_seen: 2026-09-13T12:20:00Z\nsentry_status: unresolved\ndisqualifiers_fired: []\nunderstanding: high\nfix_certainty: high\nroot_cause: Sarama wraps the TLS closeNotify error in *errors.dataError, so IsNetworkError does not unwrap it\nrecommended_fix: Extend IsNetworkError to unwrap Sarama *errors.dataError wrapper and check underlying error, or add IsTLSCloseError classifier for the tls: failed to send closeNotify pattern\n```",
		),
	)
})

type recordingDeliverer struct {
	results []agentlib.AgentResultInfo
}

func (d *recordingDeliverer) DeliverResult(_ context.Context, info agentlib.AgentResultInfo) error {
	d.results = append(d.results, info)
	return nil
}
