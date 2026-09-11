// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fixagent_test

import (
	"context"
	"os"

	claudelib "github.com/bborbe/agent/claude"
	claudemocks "github.com/bborbe/agent/mocks"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/agent-sentry-issue-analyzer/pkg/deepverdict"
	"github.com/bborbe/agent-sentry-issue-analyzer/pkg/fixagent"
	"github.com/bborbe/agent-sentry-issue-analyzer/pkg/prompts"
)

var _ = Describe("PromptRepoResolver", func() {
	var (
		ctx    context.Context
		runner *claudemocks.ClaudeRunner
	)

	BeforeEach(func() {
		ctx = context.Background()
		runner = &claudemocks.ClaudeRunner{}
	})

	buildResolver := func() fixagent.RepoResolver {
		return fixagent.NewPromptRepoResolver(runner, prompts.BuildFixPlanningInstructions())
	}

	Describe("Resolve", func() {
		// AC-1: the load-bearing resolution test. The real PromptRepoResolver
		// (the parse path) is driven with the mock standing in for the model;
		// each fixture carries the verdict's file:line, the model's fenced-YAML
		// response, and the expected resolution — asserting the repo name AND
		// the rule that produced it, so a resolver that resolves nothing cannot
		// satisfy the table.
		DescribeTable("resolves the verdict's file:line via the model's fenced YAML block",
			func(fileLine, modelOutput string, expected fixagent.Resolution) {
				runner.RunReturns(&claudelib.ClaudeResult{Result: modelOutput}, nil)

				res, err := buildResolver().Resolve(ctx, deepverdict.Verdict{
					FileLine:      fileLine,
					SentryIssueID: "OCTOPUS-PROD-1J",
				})

				// The resolution is prompt-backed: the fix-planning instruction
				// is rendered and the citation is carried into the task content.
				Expect(runner.RunCallCount()).To(Equal(1))
				_, prompt := runner.RunArgsForCall(0)
				Expect(prompt).To(ContainSubstring("fix-planning"))
				Expect(prompt).To(ContainSubstring(fileLine))

				if expected.Repo == "" {
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(ContainSubstring("unmappable"))
					return
				}
				Expect(err).NotTo(HaveOccurred())
				Expect(res).To(Equal(expected))
			},
			Entry(
				"resolves the trading frame path",
				"mt5/connector/mt5linux.py:42",
				"```yaml\nrepo: bborbe/trading\nrule: \"resolved from frame path mt5/connector/mt5linux.py\"\nfresh: true\nstale_reason: \"\"\n```",
				fixagent.Resolution{
					Repo:  "bborbe/trading",
					Rule:  "resolved from frame path mt5/connector/mt5linux.py",
					Fresh: true,
				},
			),
			Entry(
				"resolves the kafka frame path",
				"pkg/kafka/consumer.go:120",
				"```yaml\nrepo: bborbe/kafka\nrule: \"resolved from frame path pkg/kafka/consumer.go\"\nfresh: true\nstale_reason: \"\"\n```",
				fixagent.Resolution{
					Repo:  "bborbe/kafka",
					Rule:  "resolved from frame path pkg/kafka/consumer.go",
					Fresh: true,
				},
			),
			Entry(
				"reports a stale citation with the stale path named",
				"mt5/connector/mt5linux.py:42",
				"```yaml\nrepo: bborbe/trading\nrule: \"resolved from frame path mt5/connector/mt5linux.py\"\nfresh: false\nstale_reason: \"mt5/connector/mt5linux.py no longer exists at HEAD of bborbe/trading\"\n```",
				fixagent.Resolution{
					Repo:        "bborbe/trading",
					Rule:        "resolved from frame path mt5/connector/mt5linux.py",
					Fresh:       false,
					StaleReason: "mt5/connector/mt5linux.py no longer exists at HEAD of bborbe/trading",
				},
			),
			Entry(
				"reports an unmappable third-party frame",
				"netref.py:12",
				"```yaml\nrepo: \"\"\nrule: \"unmappable\"\nfresh: false\nstale_reason: \"\"\n```",
				fixagent.Resolution{},
			),
		)

		It("returns an error when the model output has no fenced yaml block", func() {
			runner.RunReturns(&claudelib.ClaudeResult{Result: "no yaml block here"}, nil)
			_, err := buildResolver().Resolve(ctx, deepverdict.Verdict{
				FileLine:      "mt5/connector/mt5linux.py:42",
				SentryIssueID: "OCTOPUS-PROD-1J",
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("parse resolution block"))
		})

		It("returns an error when the claude run fails", func() {
			runner.RunReturns(nil, os.ErrPermission)
			_, err := buildResolver().Resolve(ctx, deepverdict.Verdict{
				FileLine:      "mt5/connector/mt5linux.py:42",
				SentryIssueID: "OCTOPUS-PROD-1J",
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("resolution claude run"))
		})

		It("returns an error when the fence is unclosed", func() {
			runner.RunReturns(
				&claudelib.ClaudeResult{Result: "```yaml\nrepo: bborbe/trading\n"},
				nil,
			)
			_, err := buildResolver().Resolve(ctx, deepverdict.Verdict{
				FileLine:      "mt5/connector/mt5linux.py:42",
				SentryIssueID: "OCTOPUS-PROD-1J",
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("unclosed yaml fence"))
		})
	})
})
