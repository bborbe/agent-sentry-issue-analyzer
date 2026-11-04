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

func proposalBody(md *agentlib.Markdown) (string, bool) {
	section, ok := md.FindSection("### Jira Proposal")
	if !ok {
		return "", false
	}
	return section.Body, true
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
		).To(ContainSubstring("- description: https://seibert-group.sentry.io/issues/101632819/"))
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
		Expect(bodyA).To(ContainSubstring("issues/101632819/"))
		Expect(bodyB).To(ContainSubstring("issues/203344556/"))
		Expect(bodyA).To(HavePrefix("- summary: octopus-dev: "))
		Expect(bodyB).To(HavePrefix("- summary: octopus-prod: "))
		Expect(bodyA).NotTo(Equal(bodyB))
	})

	// A same-short-id mutation is what defeats a table keyed on the short-id:
	// fixtures D and E carry A's short-id and emit nothing, which no
	// short-id-keyed table can reproduce.
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
})
