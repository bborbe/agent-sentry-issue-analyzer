// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package steps_test

import (
	"context"
	"os"

	agentlib "github.com/bborbe/agent"
	claudelib "github.com/bborbe/agent/claude"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/agent-sentry-issue-analyzer/pkg/prompts"
	"github.com/bborbe/agent-sentry-issue-analyzer/pkg/steps"
)

// resultPath is where scripts/sentry-create-tasks.sh writes its machine-readable
// line (RESULT_FILE there, collectorResultPath in pkg/steps). It is spelled out
// here rather than referenced so this suite fails if the two ever drift apart.
const resultPath = "/tmp/sentry-create-tasks-result"

// scriptRunner stands in for the claude CLI. The real script writes the result
// line itself, so the stub writes it INSIDE Run: the step clears the file
// before invoking the runner, and a fixture that pre-wrote it would be erased.
// resultLine empty stands for a script that never ran; err for a CLI failure.
type scriptRunner struct {
	resultLine string
	summary    string
	err        error
	ran        bool
}

func (r *scriptRunner) Run(_ context.Context, _ string) (*claudelib.ClaudeResult, error) {
	r.ran = true
	if r.err != nil {
		return nil, r.err
	}
	if r.resultLine != "" {
		if err := os.WriteFile(resultPath, []byte(r.resultLine+"\n"), 0o600); err != nil {
			return nil, err
		}
	}
	return &claudelib.ClaudeResult{Result: r.summary}, nil
}

var _ = Describe("CollectorPlanningStep", func() {
	var ctx context.Context

	newStep := func(r claudelib.ClaudeRunner) agentlib.Step {
		return steps.NewCollectorPlanningStep(
			r,
			prompts.BuildCollectorPlanningInstructions(),
			nil,
		)
	}

	newMarkdown := func() *agentlib.Markdown {
		md, err := agentlib.ParseMarkdown(
			ctx,
			"---\nstatus: in_progress\n---\n\n## Task\n\ndaily sentry-collector trigger\n",
		)
		Expect(err).NotTo(HaveOccurred())
		return md
	}

	BeforeEach(func() {
		ctx = context.Background()
		Expect(os.RemoveAll(resultPath)).To(Succeed())
	})

	AfterEach(func() {
		Expect(os.RemoveAll(resultPath)).To(Succeed())
	})

	It("writes the ## Analysis summary section and advances to done (terminates the task)", func() {
		// A complete observation: new alerts were expected and task files landed,
		// so the step may report done.
		summary := "Fetched 2 alerts; 2 task files landed: SENTRY-X-1 SENTRY-X-2"
		runner := &scriptRunner{
			resultLine: "sentry-create-tasks-result: fetched=2 published=2 expected_new=2 landed=2 observed=true status=done",
			summary:    summary,
		}

		md := newMarkdown()
		result, err := newStep(runner).Run(ctx, md)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Status).To(Equal(agentlib.AgentStatusDone))
		// NextPhase "done" is the terminal-literal path (agent_agent.go:91): the
		// task flips to phase done / status completed, so the executor stops
		// re-dispatching — without it the success-section re-dispatch churns
		// (deadline_exceeded, trigger_count climb) per spec 051 follow-up.
		Expect(result.NextPhase).To(Equal("done"))

		section, ok := md.FindSection("## Analysis")
		Expect(ok).To(BeTrue())
		Expect(section.Body).To(Equal(summary))
	})

	It("skips Claude when ## Analysis already exists (idempotent resume)", func() {
		runner := &scriptRunner{summary: "summary"}

		md, err := agentlib.ParseMarkdown(
			ctx,
			"---\nstatus: in_progress\n---\n\n## Analysis\n\nalready written\n",
		)
		Expect(err).NotTo(HaveOccurred())

		shouldRun, err := newStep(runner).ShouldRun(ctx, md)
		Expect(err).NotTo(HaveOccurred())
		Expect(shouldRun).To(BeFalse())
		Expect(runner.ran).To(BeFalse())
	})

	It("returns failed when Claude fails", func() {
		result, err := newStep(&scriptRunner{err: os.ErrPermission}).Run(ctx, newMarkdown())
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Status).To(Equal(agentlib.AgentStatusFailed))
	})

	It("returns failed with no NextPhase when new alerts were expected but none landed", func() {
		// The 2026-08-26/27/28 signature: publishes succeed, the controller
		// drops everything, zero per-alert task files land.
		runner := &scriptRunner{
			resultLine: "sentry-create-tasks-result: fetched=48 published=48 expected_new=48 landed=0 observed=true status=failed",
			summary:    "Fetched 48 active unresolved alerts.",
		}

		result, err := newStep(runner).Run(ctx, newMarkdown())
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Status).To(Equal(agentlib.AgentStatusFailed))
		// Failure contract: no NextPhase — the controller owns unassign +
		// ## Failure. "human_review" is reserved for a successful verdict
		// that needs a human, never for a failure.
		Expect(result.NextPhase).To(BeEmpty())
	})

	It("stays done on a quiet day where every fetched alert was already tracked", func() {
		// Dedup is the designed idempotency: zero created with zero new alerts
		// expected is healthy, not an alarm. published is 48, NOT 0 — the script
		// publishes every fetched alert and the controller dedups them, so a quiet
		// day still publishes. This fixture previously read published=0, which is
		// not a quiet day at all: it is the failed-publish shape covered below,
		// and it only passed because the rule ignored published.
		runner := &scriptRunner{
			resultLine: "sentry-create-tasks-result: fetched=48 published=48 expected_new=0 landed=0 observed=true status=done",
			summary:    "quiet day",
		}

		result, err := newStep(runner).Run(ctx, newMarkdown())
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Status).To(Equal(agentlib.AgentStatusDone))
		Expect(result.NextPhase).To(Equal("done"))
	})

	It("returns failed with no NextPhase when alerts were fetched but none were published", func() {
		// The gap this rule change closes. expected_new is 0 here, so the landing
		// clause below cannot fire and the old rule reported done — a run whose
		// publish failed outright was indistinguishable from a quiet day.
		runner := &scriptRunner{
			resultLine: "sentry-create-tasks-result: fetched=11 published=0 expected_new=0 landed=0 observed=true status=failed",
			summary:    "Fetched 11 active unresolved alerts.",
		}

		result, err := newStep(runner).Run(ctx, newMarkdown())
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Status).To(Equal(agentlib.AgentStatusFailed))
		Expect(result.Message).To(ContainSubstring("published none"))
		Expect(result.NextPhase).To(BeEmpty())
	})

	It("attributes a failed publish to the publish, not to the landing it prevented", func() {
		// Both clauses match: published == 0 AND expected_new > 0 with landed == 0.
		// The publish is checked first because the landing failure is its
		// consequence — "no task files landed" would be true but would point the
		// reader at the controller instead of at Kafka.
		runner := &scriptRunner{
			resultLine: "sentry-create-tasks-result: fetched=11 published=0 expected_new=11 landed=0 observed=true status=failed",
			summary:    "Fetched 11 active unresolved alerts.",
		}

		result, err := newStep(runner).Run(ctx, newMarkdown())
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Status).To(Equal(agentlib.AgentStatusFailed))
		Expect(result.Message).To(ContainSubstring("published none"))
		Expect(result.NextPhase).To(BeEmpty())
	})

	It("stays done when Sentry returned no alerts at all", func() {
		// fetched == 0 means there was nothing to publish, so published == 0 is
		// the correct outcome rather than a failed publish. This is what the
		// fetched > 0 guard on the new clause protects.
		runner := &scriptRunner{
			resultLine: "sentry-create-tasks-result: fetched=0 published=0 expected_new=0 landed=0 observed=true status=done",
			summary:    "no active unresolved alerts",
		}

		result, err := newStep(runner).Run(ctx, newMarkdown())
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Status).To(Equal(agentlib.AgentStatusDone))
		Expect(result.NextPhase).To(Equal("done"))
	})

	It("stays done when task files landed", func() {
		runner := &scriptRunner{
			resultLine: "sentry-create-tasks-result: fetched=48 published=48 expected_new=48 landed=2 observed=true status=done",
			summary:    "2 landed",
		}

		result, err := newStep(runner).Run(ctx, newMarkdown())
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Status).To(Equal(agentlib.AgentStatusDone))
		Expect(result.NextPhase).To(Equal("done"))
	})

	It("returns failed when the vault could not be observed at all", func() {
		// observed=false is NOT an observed zero. A run that could not read the
		// vault proves nothing, so it must not report success — otherwise an
		// auth/network failure becomes a silent green. This is the dev
		// 2026-09-12 shape: the collector has no GATEWAY_SECRET, so git-rest
		// rejects the listing with 500 and the count is unknowable.
		runner := &scriptRunner{
			resultLine: "sentry-create-tasks-result: fetched=48 published=48 expected_new=0 landed=0 observed=false status=unobserved",
			summary:    "published 48, could not verify",
		}

		result, err := newStep(runner).Run(ctx, newMarkdown())
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Status).To(Equal(agentlib.AgentStatusFailed))
		Expect(result.NextPhase).To(BeEmpty())
	})

	It("returns failed when the script wrote no result file", func() {
		// No result file means the script never ran to completion. Reporting done
		// here is the false-green this step exists to close — observed on dev
		// 2026-09-11, when the script never executed because its Bash grant did
		// not match the `bash scripts/...` form the model actually used, and the
		// task was recorded `status: completed` having done nothing.
		runner := &scriptRunner{summary: "summary with no result line"}

		result, err := newStep(runner).Run(ctx, newMarkdown())
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Status).To(Equal(agentlib.AgentStatusFailed))
		Expect(result.NextPhase).To(BeEmpty())
	})

	It("gates on the result file even when the model's prose claims success", func() {
		// The reason this step reads a file instead of the summary. Observed on
		// dev 2026-09-12: the model wrote a confident prose summary and never
		// copied the result line, so a run that had observed nothing looked
		// exactly like one that had. A summary is the model's *claim*; the file
		// is the script's *measurement*, and the measurement wins.
		runner := &scriptRunner{
			resultLine: "sentry-create-tasks-result: fetched=11 published=11 expected_new=0 landed=0 observed=false status=unobserved",
			summary: "sentry-create-tasks-result: fetched=11 published=11 " +
				"expected_new=11 landed=11 observed=true status=done",
		}

		result, err := newStep(runner).Run(ctx, newMarkdown())
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Status).To(Equal(agentlib.AgentStatusFailed))
		Expect(result.NextPhase).To(BeEmpty())
	})

	It("ignores a result file left behind by an earlier run in the same container", func() {
		// A stale file must never be read as this run's observation, or a run
		// that produced nothing inherits the previous run's healthy counts. The
		// stub writes nothing, standing in for a script that did not run; the
		// step's pre-run clear is what turns that into a failure.
		Expect(os.WriteFile(
			resultPath,
			[]byte(
				"sentry-create-tasks-result: fetched=48 published=48 expected_new=48 landed=48 observed=true status=done\n",
			),
			0o600,
		)).To(Succeed())

		result, err := newStep(&scriptRunner{summary: "did nothing"}).Run(ctx, newMarkdown())
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Status).To(Equal(agentlib.AgentStatusFailed))
	})
})
