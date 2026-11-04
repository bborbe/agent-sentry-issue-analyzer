// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package steps_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	agentlib "github.com/bborbe/agent"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/agent-sentry-issue-analyzer/pkg/steps"
)

// stubStep stands in for the wrapped step: it does nothing, so the spec
// isolates the proposal rule from whatever phase it decorates.
type stubStep struct{}

func (stubStep) Name() string { return "stub" }

func (stubStep) ShouldRun(ctx context.Context, md *agentlib.Markdown) (bool, error) {
	return true, nil
}

func (stubStep) Run(ctx context.Context, md *agentlib.Markdown) (*agentlib.Result, error) {
	return &agentlib.Result{Status: agentlib.AgentStatusDone}, nil
}

// alreadyRanStep models a wrapped step that has already done its work: its
// ShouldRun is false, as the triage execution step's is once `## Verdict`
// exists and the fix step's is once `## Fix Result` exists. Its Run must never
// be called — re-running a step that already wrote its section would duplicate
// that write.
type alreadyRanStep struct{}

func (alreadyRanStep) Name() string { return "already-ran" }

func (alreadyRanStep) ShouldRun(ctx context.Context, md *agentlib.Markdown) (bool, error) {
	return false, nil
}

func (alreadyRanStep) Run(ctx context.Context, md *agentlib.Markdown) (*agentlib.Result, error) {
	panic("the inner step must not be re-run once it has already done its work")
}

// fixturePath reads a synthetic task from testdata. The fixtures live in the
// repo rather than a vault: they are evidence inputs, never alert tasks, and
// nothing in the rule writes back to the vault they would live in.
func fixturePath(name string) string {
	return filepath.Join("testdata", "jira-proposal", name)
}

func loadFixture(ctx context.Context, name string) *agentlib.Markdown {
	content, err := os.ReadFile(fixturePath(name))
	Expect(err).NotTo(HaveOccurred())
	md, err := agentlib.ParseMarkdown(ctx, string(content))
	Expect(err).NotTo(HaveOccurred())
	return md
}

// proposalBody extracts the block the way the consumer does — by scanning the
// marshaled text. agentlib's IsMarkdownSectionHeading accepts only `#` and
// `##`, so a three-hash heading is body text and FindSection cannot see it
// after a re-parse. Reading it through FindSection would pass in-memory and
// silently miss the real on-disk form.
func proposalBody(md *agentlib.Markdown) (string, bool) {
	content, err := md.Marshal(context.Background())
	if err != nil {
		return "", false
	}
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) != "### Jira Proposal" {
			continue
		}
		var body []string
		for _, next := range lines[i+1:] {
			trimmed := strings.TrimSpace(next)
			// A block runs to the next heading of the same or higher level.
			if strings.HasPrefix(trimmed, "### ") ||
				strings.HasPrefix(trimmed, "## ") ||
				strings.HasPrefix(trimmed, "# ") {
				break
			}
			body = append(body, next)
		}
		return strings.TrimSpace(strings.Join(body, "\n")), true
	}
	return "", false
}

var _ = Describe("JiraProposalStep", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	DescribeTable(
		"emits only for a qualifying verdict with no fix PR",
		func(fixture string, expectBlock bool) {
			md := loadFixture(ctx, fixture)
			step := steps.NewJiraProposalStep(stubStep{})

			shouldRun, err := step.ShouldRun(ctx, md)
			Expect(err).NotTo(HaveOccurred())
			Expect(shouldRun).To(BeTrue())

			_, err = step.Run(ctx, md)
			Expect(err).NotTo(HaveOccurred())

			body, ok := proposalBody(md)
			Expect(ok).To(Equal(expectBlock), "fixture %s", fixture)
			if expectBlock {
				Expect(body).NotTo(BeEmpty())
				GinkgoWriter.Printf("EMITTED %s:\n%s\n", fixture, body)
			} else {
				GinkgoWriter.Printf("NO BLOCK %s\n", fixture)
			}
		},
		Entry("qualifying real bug", "fixture-a-qualifying.md", true),
		Entry("qualifying regression", "fixture-b-qualifying.md", true),
		Entry("fixable real bug WITH a fix PR", "fixture-c-fixable-with-pr.md", false),
		Entry("noise verdict, same short-id as A", "fixture-d-noise-same-shortid.md", false),
		Entry(
			"real bug with no disqualifier, same short-id as A",
			"fixture-e-no-disqualifier-same-shortid.md",
			false,
		),
		Entry("qualifying real bug in the deep (fix-chain) shape", "fixture-h-deep-shape.md", true),
		Entry(
			"real bug with NO ## Fix Result — fix agent has not run yet",
			"fixture-f-realbug-no-fixresult.md",
			false,
		),
		Entry(
			"regression carrying a pr_url — already fixed",
			"fixture-g-regression-with-pr.md",
			false,
		),
	)

	It("derives the three field values from the fixture's own inputs", func() {
		md := loadFixture(ctx, "fixture-a-qualifying.md")
		_, err := steps.NewJiraProposalStep(stubStep{}).Run(ctx, md)
		Expect(err).NotTo(HaveOccurred())

		body, ok := proposalBody(md)
		Expect(ok).To(BeTrue())
		Expect(body).To(ContainSubstring("- label: sentry-OCTOPUS-DEV-11"))
		Expect(
			body,
		).To(ContainSubstring("- description: https://seibert-group.sentry.io/issues/OCTOPUS-DEV-11"))
		// The numeric id sits in the fixture's `issue_url` frontmatter but must
		// NOT reach the block: JQL on the numeric URL returns 0 BRO tickets, on
		// the short-id URL returns 1. The URL arm is dedup key 2.
		Expect(body).NotTo(ContainSubstring("101632819"))
		Expect(body).To(HavePrefix("- summary: octopus-dev: "))
	})

	// The decisive probe: fixture B differs from A only in short-id, component
	// and issue id, so a lookup table keyed on the fixture contents (or on the
	// short-id) cannot produce both blocks. Only a rule that reads the verdict
	// and the task's own fields can.
	It("derives a different block for the second qualifying fixture", func() {
		mdA := loadFixture(ctx, "fixture-a-qualifying.md")
		_, err := steps.NewJiraProposalStep(stubStep{}).Run(ctx, mdA)
		Expect(err).NotTo(HaveOccurred())

		mdB := loadFixture(ctx, "fixture-b-qualifying.md")
		_, err = steps.NewJiraProposalStep(stubStep{}).Run(ctx, mdB)
		Expect(err).NotTo(HaveOccurred())

		bodyA, okA := proposalBody(mdA)
		Expect(okA).To(BeTrue())
		bodyB, okB := proposalBody(mdB)
		Expect(okB).To(BeTrue())

		Expect(bodyA).To(ContainSubstring("sentry-OCTOPUS-DEV-11"))
		Expect(bodyB).To(ContainSubstring("sentry-OCTOPUS-PROD-8B"))
		Expect(bodyA).To(ContainSubstring("issues/OCTOPUS-DEV-11"))
		Expect(bodyB).To(ContainSubstring("issues/OCTOPUS-PROD-8B"))
		Expect(bodyA).To(HavePrefix("- summary: octopus-dev: "))
		Expect(bodyB).To(HavePrefix("- summary: octopus-prod: "))
		Expect(bodyA).NotTo(Equal(bodyB))
	})

	// The fix chain is the only chain that can fire the real-bug arm, and it
	// parses the DEEP verdict, which carries no `reason` key. A Reason-only
	// summary source would render every real-bug proposal as
	// "<project>: unresolved error" — a title that says nothing about the alert.
	It("sources the summary from root_cause on the deep verdict shape", func() {
		md := loadFixture(ctx, "fixture-h-deep-shape.md")
		_, err := steps.NewJiraProposalStep(stubStep{}).Run(ctx, md)
		Expect(err).NotTo(HaveOccurred())

		body, ok := proposalBody(md)
		Expect(ok).To(BeTrue())
		Expect(body).To(ContainSubstring(
			"- summary: octopus-prod: cdb writer drops the final batch on shutdown",
		))
		Expect(body).NotTo(ContainSubstring("unresolved error"))
		Expect(body).To(ContainSubstring("- label: sentry-OCTOPUS-PROD-8D"))
		Expect(body).To(ContainSubstring("issues/OCTOPUS-PROD-8D"))
	})

	// A task whose inner step already ran — a resume after a crash between the
	// inner write and this evaluation, or this feature deployed onto an existing
	// task — must still receive the block. Delegating ShouldRun to the inner
	// step would make the decorator unreachable in exactly that state.
	It("evaluates when the inner step has already run", func() {
		md := loadFixture(ctx, "fixture-a-qualifying.md")
		step := steps.NewJiraProposalStep(alreadyRanStep{})

		shouldRun, err := step.ShouldRun(ctx, md)
		Expect(err).NotTo(HaveOccurred())
		Expect(shouldRun).To(BeTrue())

		result, err := step.Run(ctx, md)
		Expect(err).NotTo(HaveOccurred())
		Expect(result.Status).To(Equal(agentlib.AgentStatusDone))

		body, ok := proposalBody(md)
		Expect(ok).To(BeTrue())
		Expect(body).To(ContainSubstring("- label: sentry-OCTOPUS-DEV-11"))
	})

	It("fails loudly when a qualifying task lacks a field the block needs", func() {
		// No short_id: the label cannot be derived, and emitting a guessed one
		// would create a dedup key that never matches.
		md, err := agentlib.ParseMarkdown(ctx, "---\nproject: octopus-dev\n---\n\n"+
			"## Verdict\n\n```yaml\nsentry_issue_id: \"1\"\nverdict: real bug\n"+
			"confidence: high\nreason: boom\nsentry_status: unresolved\n"+
			"disqualifiers_fired:\n  - sustained over 30 days\n```\n\n"+
			"## Fix Result\n\n- status: skipped\n")
		Expect(err).NotTo(HaveOccurred())

		_, err = steps.NewJiraProposalStep(stubStep{}).Run(ctx, md)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("short_id"))
		_, ok := proposalBody(md)
		Expect(ok).To(BeFalse())
	})

	It("refuses the same short-id when only the verdict inputs change", func() {
		for _, fixture := range []string{
			"fixture-d-noise-same-shortid.md",
			"fixture-e-no-disqualifier-same-shortid.md",
		} {
			md := loadFixture(ctx, fixture)
			_, err := steps.NewJiraProposalStep(stubStep{}).Run(ctx, md)
			Expect(err).NotTo(HaveOccurred())
			_, ok := proposalBody(md)
			Expect(ok).To(BeFalse(), "fixture %s must emit no block", fixture)
			Expect(md.Frontmatter["short_id"]).To(Equal("OCTOPUS-DEV-11"))
		}
	})

	It("uses a three-hash heading so the ## Verdict section still parses", func() {
		md := loadFixture(ctx, "fixture-a-qualifying.md")
		_, err := steps.NewJiraProposalStep(stubStep{}).Run(ctx, md)
		Expect(err).NotTo(HaveOccurred())

		content, err := md.Marshal(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(content).To(ContainSubstring("### Jira Proposal"))
		// A substring check cannot distinguish "###" from "##" (the former ends
		// with the latter), so compare whole lines: no line may be exactly the
		// two-hash heading the consumer treats as malformed.
		for line := range strings.SplitSeq(content, "\n") {
			Expect(strings.TrimSpace(line)).NotTo(Equal("## Jira Proposal"))
		}
		// The verdict section must remain readable after the block is added.
		section, ok := md.FindSection("## Verdict")
		Expect(ok).To(BeTrue())
		Expect(section.Body).To(ContainSubstring("verdict: real bug"))
	})

	It("is idempotent: a second run does not rewrite or duplicate the block", func() {
		md := loadFixture(ctx, "fixture-a-qualifying.md")
		step := steps.NewJiraProposalStep(stubStep{})

		_, err := step.Run(ctx, md)
		Expect(err).NotTo(HaveOccurred())
		first, _ := proposalBody(md)

		// ShouldRun is false once the block exists — the resume guard.
		shouldRun, err := step.ShouldRun(ctx, md)
		Expect(err).NotTo(HaveOccurred())
		Expect(shouldRun).To(BeFalse())

		content, err := md.Marshal(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(strings.Count(content, "### Jira Proposal")).To(Equal(1))
		second, _ := proposalBody(md)
		Expect(second).To(Equal(first))
	})

	// The delivered task is text: the next run re-parses it. agentlib treats a
	// three-hash heading as body text, so a FindSection-based guard would report
	// "no block" here and append a SECOND one — one alert, two proposals, two
	// Jira tickets. This is the regression test for that.
	It("stays idempotent across a marshal/parse round-trip", func() {
		md := loadFixture(ctx, "fixture-a-qualifying.md")
		step := steps.NewJiraProposalStep(stubStep{})

		_, err := step.Run(ctx, md)
		Expect(err).NotTo(HaveOccurred())

		delivered, err := md.Marshal(ctx)
		Expect(err).NotTo(HaveOccurred())
		reparsed, err := agentlib.ParseMarkdown(ctx, delivered)
		Expect(err).NotTo(HaveOccurred())

		shouldRun, err := step.ShouldRun(ctx, reparsed)
		Expect(err).NotTo(HaveOccurred())
		Expect(shouldRun).To(BeFalse(),
			"a re-parsed task that already carries the block must not run again")

		// Even a forced Run must not append a second block.
		_, err = step.Run(ctx, reparsed)
		Expect(err).NotTo(HaveOccurred())
		out, err := reparsed.Marshal(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(strings.Count(out, "### Jira Proposal")).To(Equal(1))
	})
})
