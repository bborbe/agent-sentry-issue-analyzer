// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package factory_test

import (
	"context"

	agentlib "github.com/bborbe/agent"
	agentmocks "github.com/bborbe/agent/mocks"
	libtime "github.com/bborbe/time"
	"github.com/bborbe/vault-cli/pkg/domain"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/agent-sentry-issue-analyzer/pkg/factory"
	fixagentmocks "github.com/bborbe/agent-sentry-issue-analyzer/pkg/fixagent/mocks"
)

// These specs assert the Jira proposal decorator is actually composed into the
// factory chains. Without them, dropping NewJiraProposalStep from either chain
// would leave every other test green — fix_wiring_test.go drives the fix chain
// but never looks for the block.
var _ = Describe("Jira proposal wiring", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	It("emits the block on the fix chain when the fix step skips without a PR", func() {
		runner := &agentmocks.ClaudeRunner{}
		server := newFakeFixGithubAPI(GinkgoT())
		deliver := &agentmocks.AgentResultDeliverer{}

		trace := &fixagentmocks.TraceReader{}
		trace.HasFirstPartyFrameReturns(true, nil)

		agent := factory.CreateFixAgentFromRunner(
			runner,
			server.client,
			[]string{"bborbe/trading"},
			trace,
			libtime.NewCurrentDateTime(),
		)

		// `file:line` is deliberately absent, so the fix step records
		// `status: skipped` with no pr_url — the exact state the real-bug arm
		// exists to catch. The verdict otherwise carries the DEEP schema
		// (root_cause, no `reason`), so the summary must come from root_cause.
		_, err := agent.Run(
			ctx,
			domain.TaskPhasePlanning,
			"---\nstatus: in_progress\nphase: planning\nassignee: sentry-fix-agent\ntask_type: sentry-fix\n"+
				"short_id: OCTOPUS-PROD-8D\nproject: octopus-prod\n"+
				"issue_url: https://seibert-group.sentry.io/issues/203344558/\n---\n\n"+
				"## Verdict\n\n```yaml\n"+
				"sentry_issue_id: \"203344558\"\n"+
				"verdict: real bug\n"+
				"understanding: High\n"+
				"fix_certainty: Medium\n"+
				"root_cause: cdb writer drops the final batch on shutdown\n"+
				"recommended_fix: flush the pending batch before closing the writer\n"+
				"disqualifiers_fired: [Volume]\n"+
				"live_event_count: 142\n"+
				"first_seen: 2025-11-08T11:17:10Z\n"+
				"last_seen: 2026-09-05T12:28:01Z\n"+
				"sentry_status: unresolved\n"+
				"```\n",
			deliver,
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(deliver.DeliverResultCallCount()).To(Equal(1))
		_, info := deliver.DeliverResultArgsForCall(0)

		md, err := agentlib.ParseMarkdown(ctx, info.Output)
		Expect(err).NotTo(HaveOccurred())

		// The fix step really did skip without filing.
		fixResult, ok := md.FindSection("## Fix Result")
		Expect(ok).To(BeTrue())
		Expect(fixResult.Body).To(ContainSubstring("status: skipped"))
		Expect(fixResult.Body).NotTo(ContainSubstring("pr_url:"))

		// And the decorator turned that into a proposal. Asserted on the raw
		// output, not FindSection: agentlib's IsMarkdownSectionHeading accepts
		// only `#`/`##`, so a three-hash heading is body text to it — but the
		// consumer (/daily-maintenance) scans text, which is what matters.
		Expect(info.Output).To(ContainSubstring("### Jira Proposal"))
		Expect(info.Output).To(ContainSubstring(
			"- summary: octopus-prod: cdb writer drops the final batch on shutdown",
		))
		Expect(info.Output).To(ContainSubstring(
			"- description: https://seibert-group.sentry.io/issues/203344558/",
		))
		Expect(info.Output).To(ContainSubstring("- label: sentry-OCTOPUS-PROD-8D"))
	})
})
