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
	identitymocks "github.com/bborbe/agent-sentry-issue-analyzer/pkg/identity/mocks"
	metricsmocks "github.com/bborbe/agent-sentry-issue-analyzer/pkg/metrics/mocks"
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

		// Echo resolver: the wiring spec's verdict already carries a canonical
		// identity, so echoing keeps every assertion below meaning what it meant
		// before the normalization was added.
		resolver := &identitymocks.Resolver{}
		resolver.ResolveStub = func(_ context.Context, reference string) (string, error) {
			return reference, nil
		}

		agent := factory.CreateDeepAgentFromRunner(
			runner,
			nil,
			libtime.NewCurrentDateTime(),
			&metricsmocks.Metrics{},
			resolver,
		)
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
		Expect(info.Status).To(Equal(agentlib.AgentStatusDone))

		handedOff, err = agentlib.ParseMarkdown(ctx, info.Output)
		Expect(err).NotTo(HaveOccurred())
	})

	// The phase the controller writes is the one the deliverer PUBLISHES, not the
	// one left in the marshaled output: the bborbe/agent deliverer deletes
	// `phase` from every result that advances none (Status InProgress, or Done
	// with no NextPhase), so an in-memory `phase: planning` never reached disk.
	// Measured 2026-11-06 on the Seibert lane: OCTOPUS-DEV-4A was handed off and
	// then refused by the executor with `gate=phase_not_triggered
	// task_type=sentry-fix phase=execution`, because the fix Config triggers on
	// planning only. Done + NextPhase is the only form the deliverer publishes.
	It("publishes the phase change as NextPhase planning, the form the deliverer honours", func() {
		_, info := deliverer.DeliverResultArgsForCall(0)
		Expect(info.NextPhase).To(Equal("planning"))
	})

	It("stamps the live sentry-fix-agent Config CR as the assignee", func() {
		Expect(handedOff.Frontmatter["assignee"]).To(Equal("sentry-fix-agent"))
	})

	It("stamps sentry-fix as the task type", func() {
		Expect(handedOff.Frontmatter["task_type"]).To(Equal("sentry-fix"))
	})

	It("declares the reassignment so the controller's assignee guard honours it", func() {
		// Load-bearing, not decoration: without the declaration
		// agent-task-controller's operator-owned-assignee guard keeps the on-disk
		// analyzer and the task fails the executor's task_type filter. The key
		// spelling is a frozen cross-repo contract — see reassignDeclarationKey.
		Expect(handedOff.Frontmatter["assignee_reassigned_by"]).To(
			Equal("sentry-deep-execution-fix-handoff"),
		)
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

// NextPhase "planning" names a phase the deep agent ALSO declares, and
// Agent.Run advances in-process into any NextPhase its own agent has. A task
// carrying `## Failure` and no `## Context` — exactly the shape of a deep run
// killed at its deadline and re-queued, as OCTOPUS-DEV-4A was — makes the deep
// planning step's ShouldRun true, so without a guard the handed-off task would
// be re-planned by the deep analyzer in the same pod instead of exiting for the
// fix agent.
var _ = Describe("CreateDeepAgentFromRunner after a handoff", func() {
	It("exits for the fix agent instead of re-running deep planning in-process", func() {
		ctx := context.Background()

		runner := &agentmocks.ClaudeRunner{}
		runner.RunReturns(&claudelib.ClaudeResult{
			Result: "```yaml\nsentry_issue_id: OCTOPUS-DEV-4A\nverdict: real bug\nunderstanding: high\nfix_certainty: high\nroot_cause: uppercase Job name\nrecommended_fix: deploy executor >= v0.26.4\nfile:line: pkg/spawner/job_spawner.go:667\nlive_event_count: 50\nfirst_seen: 2026-08-20T00:00:00Z\nlast_seen: 2026-09-05T10:00:00Z\nsentry_status: unresolved\n```",
		}, nil)
		deliverer := &agentmocks.AgentResultDeliverer{}
		resolver := &identitymocks.Resolver{}
		resolver.ResolveStub = func(_ context.Context, reference string) (string, error) {
			return reference, nil
		}

		agent := factory.CreateDeepAgentFromRunner(
			runner,
			nil,
			libtime.NewCurrentDateTime(),
			&metricsmocks.Metrics{},
			resolver,
		)
		result, err := agent.Run(
			ctx,
			domain.TaskPhaseExecution,
			"---\nstatus: in_progress\nphase: execution\nassignee: sentry-analyzer-agent\ntask_type: sentry-deep-analyzer\n---\n\n## Failure\n\n- **Reason:** deadline_exceeded\n",
			deliverer,
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).NotTo(BeNil())
		Expect(result.Status).To(Equal(agentlib.AgentStatusDone))
		Expect(result.NextPhase).To(Equal("planning"))

		// One Claude call (the deep execution) and one delivery (the handoff):
		// the deep planning step must not run after the task left this agent.
		Expect(runner.RunCallCount()).To(Equal(1))
		Expect(deliverer.DeliverResultCallCount()).To(Equal(1))
	})
})
