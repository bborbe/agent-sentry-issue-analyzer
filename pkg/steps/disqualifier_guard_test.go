// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package steps_test

import (
	"context"
	"time"

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

var _ = Describe("DisqualifierGuardStep", func() {
	var (
		ctx    context.Context
		runner *claudemocks.ClaudeRunner
		now    time.Time
	)

	BeforeEach(func() {
		ctx = context.Background()
		runner = &claudemocks.ClaudeRunner{}
		now = time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	})

	newEvaluator := func() verdict.DisqualifierEvaluator {
		return verdict.NewDisqualifierEvaluator(
			libtime.CurrentDateTimeGetterFunc(func() libtime.DateTime {
				return libtime.DateTime(now)
			}),
		)
	}

	// buildDeepStep mirrors the deep path: a deep execution step wrapped in the
	// disqualifier guard (as CreateDeepAgentFromRunner wires it). The deep
	// model re-runs the verdict; a fired disqualifier must still force
	// `real bug` even when the deep model wrote `noise` — the regression the
	// guard exists for (NUKE-DEV-A4: triage forced real bug via sustained
	// span, deep re-analysis overwrote it with noise).
	buildDeepStep := func() agentlib.Step {
		execution := steps.NewDeepExecutionStep(
			runner,
			prompts.BuildDeepExecutionInstructions(),
			nil,
		)
		return steps.NewDisqualifierGuardStep(execution, newEvaluator())
	}

	buildTask := func(body string) *agentlib.Markdown {
		md, err := agentlib.ParseMarkdown(
			ctx,
			"---\nstatus: in_progress\nphase: execution\nassignee: sentry-analyzer-agent\ntask_type: sentry-deep-analyzer\n---\n\n## Context\n\ndeep analysis\n\n"+body,
		)
		Expect(err).NotTo(HaveOccurred())
		return md
	}

	It("forces real bug when a disqualifier fires even though the deep model wrote noise", func() {
		runner.RunReturns(&claudelib.ClaudeResult{
			Result: "```yaml\nsentry_issue_id: NUKE-DEV-A4\nverdict: noise\nreason: count 193 < 100\nlive_event_count: 193\nfirst_seen: 2025-11-08T11:17:10Z\nlast_seen: 2026-09-05T12:28:01Z\nsentry_status: unresolved\n```",
		}, nil)

		step := buildDeepStep()
		md := buildTask("")

		result, err := step.Run(ctx, md)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Status).To(Equal(agentlib.AgentStatusDone))

		// The ## Verdict section is rewritten with the forced verdict.
		section, ok := md.FindSection("## Verdict")
		Expect(ok).To(BeTrue())
		Expect(section.Body).To(ContainSubstring("verdict: real bug"))
		Expect(section.Body).To(ContainSubstring("disqualifiers_fired"))
		Expect(section.Body).To(ContainSubstring("Sustained span"))
	})

	It("keeps noise when no disqualifier fires on the deep path", func() {
		runner.RunReturns(&claudelib.ClaudeResult{
			Result: "```yaml\nsentry_issue_id: NUKE-DEV-94\nverdict: noise\nreason: pod shutdown race\nlive_event_count: 84\nfirst_seen: 2025-10-15T10:42:13Z\nlast_seen: 2026-09-05T12:14:43Z\nsentry_status: unresolved\n```",
		}, nil)

		step := buildDeepStep()
		md := buildTask("")

		result, err := step.Run(ctx, md)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Status).To(Equal(agentlib.AgentStatusDone))

		section, ok := md.FindSection("## Verdict")
		Expect(ok).To(BeTrue())
		Expect(section.Body).To(ContainSubstring("verdict: noise"))
		Expect(section.Body).NotTo(ContainSubstring("Sustained span"))
	})
})
