// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package steps_test

import (
	"context"
	"os"
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

var _ = Describe("FixHandoffStep", func() {
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

	// buildDeepStep mirrors the deep path exactly as CreateDeepAgentFromRunner
	// wires it: deep execution wrapped in the disqualifier guard, wrapped in
	// the fix handoff (handoff nests OUTSIDE the guard so it observes the
	// guard's overrides).
	buildDeepStep := func() agentlib.Step {
		execution := steps.NewDeepExecutionStep(
			runner,
			prompts.BuildDeepExecutionInstructions(),
			nil,
		)
		guarded := steps.NewDisqualifierGuardStep(execution, newEvaluator())
		return steps.NewFixHandoffStep(guarded, "sentry-fix-agent", "sentry-fix")
	}

	buildTask := func(body string) *agentlib.Markdown {
		md, err := agentlib.ParseMarkdown(
			ctx,
			"---\nstatus: in_progress\nphase: execution\nassignee: sentry-analyzer-agent\ntask_type: sentry-deep-analyzer\n---\n\n## Context\n\ndeep analysis\n\n"+body,
		)
		Expect(err).NotTo(HaveOccurred())
		return md
	}

	// nonFiringLiveState fires NO disqualifier against the fixed clock: count 50
	// < 100 and < 10000, ~16-day span (< 30 days), last_seen > 24h old,
	// status unresolved.
	nonFiringLiveState := "live_event_count: 50\nfirst_seen: 2026-08-20T00:00:00Z\nlast_seen: 2026-09-05T10:00:00Z\nsentry_status: unresolved\n"

	realBugYAML := func(understanding, fixCertainty string) string {
		return "sentry_issue_id: OCTOPUS-PROD-1J\nverdict: real bug\nunderstanding: " + understanding +
			"\nfix_certainty: " + fixCertainty +
			"\nroot_cause: nil check missing\nrecommended_fix: add nil guard\nfile:line: pkg/handler/handler.go:142\n" +
			nonFiringLiveState
	}

	// realBugVariant is realBugYAML with distinct issue id, root cause and
	// file:line. SC1 requires >=5 High/High verdicts in with >=5 handoffs out,
	// so the positive class cannot be satisfied by one hand-picked fixture: a
	// gate keyed on any fixture-specific string would pass the single case and
	// fail these.
	realBugVariant := func(issueID, rootCause, fileLine string) string {
		return "sentry_issue_id: " + issueID +
			"\nverdict: real bug\nunderstanding: High\nfix_certainty: High" +
			"\nroot_cause: " + rootCause +
			"\nrecommended_fix: add nil guard\nfile:line: " + fileLine + "\n" +
			nonFiringLiveState
	}

	nonRealBugYAML := func(verdictKey string) string {
		return "sentry_issue_id: OCTOPUS-PROD-1J\nverdict: " + verdictKey +
			"\nunderstanding: High\nfix_certainty: High\n" + nonFiringLiveState
	}

	DescribeTable(
		"hands off to the fix agent exactly when the deep verdict is High/High real bug",
		func(verdictYAML string, expected int) {
			runner.RunReturns(&claudelib.ClaudeResult{
				Result: "```yaml\n" + verdictYAML + "\n```",
			}, nil)

			step := buildDeepStep()
			md := buildTask("")

			result, err := step.Run(ctx, md)
			Expect(err).NotTo(HaveOccurred())

			handoffCount := 0
			if result.Status == agentlib.AgentStatusInProgress {
				handoffCount = 1
			}
			Expect(handoffCount).To(Equal(expected))

			if expected == 1 {
				Expect(result.Status).To(Equal(agentlib.AgentStatusInProgress))
				Expect(result.Message).To(ContainSubstring("sentry-fix-agent"))
				Expect(md.Frontmatter["assignee"]).To(Equal("sentry-fix-agent"))
				Expect(md.Frontmatter["phase"]).To(Equal("planning"))
				Expect(md.Frontmatter["task_type"]).To(Equal("sentry-fix"))
			} else {
				// AC-5: a non-firing verdict completes exactly as it does today —
				// the wrapped step's own result, frontmatter untouched.
				Expect(result.Status).To(Equal(agentlib.AgentStatusDone))
				Expect(md.Frontmatter["assignee"]).To(Equal("sentry-analyzer-agent"))
				Expect(md.Frontmatter["phase"]).To(Equal("execution"))
				Expect(md.Frontmatter["task_type"]).To(Equal("sentry-deep-analyzer"))
			}
		},
		Entry("real bug with High/High hands off", realBugYAML("High", "High"), 1),
		// SC1's positive class: >=5 High/High verdicts in, >=5 handoffs out,
		// each with a distinct issue id, root cause and file:line so the gate
		// cannot be passing on one fixture's incidental content.
		Entry(
			"real bug with High/High hands off (trading)",
			realBugVariant(
				"OCTOPUS-PROD-2K",
				"unbounded retry loop",
				"mt5/connector/mt5linux.py:88",
			),
			1,
		),
		Entry(
			"real bug with High/High hands off (kafka)",
			realBugVariant(
				"NUKE-PROD-3M",
				"offset committed before processing",
				"pkg/kafka/consumer.go:214",
			),
			1,
		),
		Entry(
			"real bug with High/High hands off (nuke)",
			realBugVariant("NUKE-DEV-4N", "missing resource limit", "charts/agent/values.yaml:12"),
			1,
		),
		Entry(
			"real bug with High/High hands off (analyzer)",
			realBugVariant(
				"NUKE-DEV-5P",
				"verdict parsed with the wrong schema",
				"pkg/steps/reassign.go:41",
			),
			1,
		),
		Entry("real bug with High/Medium does not hand off", realBugYAML("High", "Medium"), 0),
		Entry("real bug with High/Low does not hand off", realBugYAML("High", "Low"), 0),
		Entry("real bug with Medium/High does not hand off", realBugYAML("Medium", "High"), 0),
		Entry("real bug with Medium/Medium does not hand off", realBugYAML("Medium", "Medium"), 0),
		Entry("real bug with Medium/Low does not hand off", realBugYAML("Medium", "Low"), 0),
		Entry("real bug with Low/High does not hand off", realBugYAML("Low", "High"), 0),
		Entry("real bug with Low/Medium does not hand off", realBugYAML("Low", "Medium"), 0),
		Entry("real bug with Low/Low does not hand off", realBugYAML("Low", "Low"), 0),
		Entry("noise with High/High does not hand off", nonRealBugYAML("noise"), 0),
		Entry("duplicate with High/High does not hand off", nonRealBugYAML("duplicate"), 0),
		Entry(
			"closed-fixed-in-prod with High/High does not hand off",
			nonRealBugYAML("closed-fixed-in-prod"),
			0,
		),
		Entry("not-a-defect with High/High does not hand off", nonRealBugYAML("not-a-defect"), 0),
		Entry("track with High/High does not hand off", nonRealBugYAML("track"), 0),
	)

	It("hands off when the guard forces real bug from a noise verdict", func() {
		// The guard fires the Sustained span disqualifier (count 193 >= 100,
		// ~300-day span, no clock dependency) and rewrites ## Verdict with the
		// forced real bug. The handoff nests OUTSIDE the guard, so it observes
		// the override — a handoff nested inside would read the model's noise
		// and skip.
		runner.RunReturns(&claudelib.ClaudeResult{
			Result: "```yaml\nsentry_issue_id: NUKE-DEV-A4\nverdict: noise\nreason: count 193 < 100\nunderstanding: High\nfix_certainty: High\nroot_cause: nil deref\nrecommended_fix: add guard\nfile:line: pkg/handler/handler.go:142\nlive_event_count: 193\nfirst_seen: 2025-11-08T11:17:10Z\nlast_seen: 2026-09-05T12:28:01Z\nsentry_status: unresolved\n```",
		}, nil)

		step := buildDeepStep()
		md := buildTask("")

		result, err := step.Run(ctx, md)
		Expect(err).NotTo(HaveOccurred())

		// The guard forced real bug and recorded the fired disqualifiers.
		section, ok := md.FindSection("## Verdict")
		Expect(ok).To(BeTrue())
		Expect(section.Body).To(ContainSubstring("verdict: real bug"))
		Expect(section.Body).To(ContainSubstring("disqualifiers_fired"))
		Expect(section.Body).To(ContainSubstring("Sustained span"))

		// ...and the handoff still fires.
		Expect(result.Status).To(Equal(agentlib.AgentStatusInProgress))
		Expect(result.Message).To(ContainSubstring("sentry-fix-agent"))
		Expect(md.Frontmatter["assignee"]).To(Equal("sentry-fix-agent"))
		Expect(md.Frontmatter["phase"]).To(Equal("planning"))
		Expect(md.Frontmatter["task_type"]).To(Equal("sentry-fix"))
	})

	It("never hands off twice on the same task", func() {
		runner.RunReturns(&claudelib.ClaudeResult{
			Result: "```yaml\n" + realBugYAML("High", "High") + "\n```",
		}, nil)

		step := buildDeepStep()
		md := buildTask("")

		first, err := step.Run(ctx, md)
		Expect(err).NotTo(HaveOccurred())
		Expect(first.Status).To(Equal(agentlib.AgentStatusInProgress))
		Expect(md.Frontmatter["assignee"]).To(Equal("sentry-fix-agent"))

		// The task already carries the fix assignee: the idempotency check
		// returns the wrapped step's own result, no second handoff.
		second, err := step.Run(ctx, md)
		Expect(err).NotTo(HaveOccurred())
		Expect(second.Status).To(Equal(agentlib.AgentStatusDone))
		Expect(md.Frontmatter["assignee"]).To(Equal("sentry-fix-agent"))
		Expect(md.Frontmatter["phase"]).To(Equal("planning"))
		Expect(md.Frontmatter["task_type"]).To(Equal("sentry-fix"))
	})

	It("propagates a non-done result (Claude failure) without handing off", func() {
		runner.RunReturns(nil, os.ErrPermission)
		step := buildDeepStep()
		md := buildTask("")

		result, err := step.Run(ctx, md)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Status).To(Equal(agentlib.AgentStatusFailed))
		Expect(md.Frontmatter["assignee"]).To(Equal("sentry-analyzer-agent"))
		Expect(md.Frontmatter["phase"]).To(Equal("execution"))
	})

	It("skips when the execution step would skip (idempotent resume)", func() {
		step := buildDeepStep()
		md, err := agentlib.ParseMarkdown(
			ctx,
			"---\nstatus: in_progress\n---\n\n## Context\n\ndeep analysis\n\n## Verdict\n\nverdict: noise\n",
		)
		Expect(err).NotTo(HaveOccurred())

		shouldRun, err := step.ShouldRun(ctx, md)
		Expect(err).NotTo(HaveOccurred())
		Expect(shouldRun).To(BeFalse())
	})

	It("propagates a parse error when the deep verdict block is unclosed", func() {
		// The guard's triage parser accepts the unclosed fence (its fencedBlocks
		// appends the trailing block), but deepverdict.Parse rejects it — so the
		// handoff must fail loudly rather than hand off on a half-read verdict.
		runner.RunReturns(&claudelib.ClaudeResult{
			Result: "```yaml\nsentry_issue_id: OCTOPUS-PROD-1J\nverdict: real bug\nunderstanding: High\nfix_certainty: High\n",
		}, nil)

		step := buildDeepStep()
		md := buildTask("")

		result, err := step.Run(ctx, md)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("fix-handoff: parse verdict"))
		Expect(result).To(BeNil())
		Expect(md.Frontmatter["assignee"]).To(Equal("sentry-analyzer-agent"))
	})

	It("propagates the guard's date-parse error without handing off", func() {
		// applyDisqualifiers fails parsing first_seen and the guard returns the
		// wrapped error; the handoff propagates it untouched — no handoff, no
		// panic.
		runner.RunReturns(&claudelib.ClaudeResult{
			Result: "```yaml\nsentry_issue_id: NUKE-DEV-A4\nverdict: noise\nreason: count 193 < 100\nlive_event_count: 193\nfirst_seen: not-a-date\nlast_seen: 2026-09-05T12:28:01Z\nsentry_status: unresolved\n```",
		}, nil)

		step := buildDeepStep()
		md := buildTask("")

		result, err := step.Run(ctx, md)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("disqualifier-guard"))
		Expect(result).To(BeNil())
		Expect(md.Frontmatter["assignee"]).To(Equal("sentry-analyzer-agent"))
		Expect(md.Frontmatter["phase"]).To(Equal("execution"))
	})
})
