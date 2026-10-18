// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fixagent_test

import (
	"context"
	"os"
	"strings"

	claudelib "github.com/bborbe/agent/claude"
	claudemocks "github.com/bborbe/agent/mocks"
	"github.com/bborbe/errors"
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
			func(
				fileLine, modelOutput, implicatedRepo string,
				hasFirstPartyFrame bool,
				expected fixagent.Resolution,
			) {
				runner.RunReturns(&claudelib.ClaudeResult{Result: modelOutput}, nil)

				res, err := buildResolver().Resolve(ctx, deepverdict.Verdict{
					FileLine:      fileLine,
					SentryIssueID: "OCTOPUS-PROD-1J",
				}, implicatedRepo, hasFirstPartyFrame)

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
				"",
				true,
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
				"",
				true,
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
				"",
				true,
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
				"",
				false,
				fixagent.Resolution{},
			),
			Entry(
				"resolves a repo-relative citation from the analyzer's implicated_repo",
				"pkg/deepverdict/deepverdict.go:109",
				"```yaml\nrepo: bborbe/agent-sentry-issue-analyzer\nrule: \"resolved from frame path pkg/deepverdict/deepverdict.go\"\nfresh: true\nstale_reason: \"\"\n```",
				"bborbe/agent-sentry-issue-analyzer",
				true,
				fixagent.Resolution{
					Repo:  "bborbe/agent-sentry-issue-analyzer",
					Rule:  "resolved from frame path pkg/deepverdict/deepverdict.go",
					Fresh: true,
				},
			),
		)

		It("carries a non-empty implicated_repo into the rendered task content", func() {
			runner.RunReturns(&claudelib.ClaudeResult{
				Result: "```yaml\nrepo: bborbe/agent-sentry-issue-analyzer\nrule: \"resolved from frame path pkg/deepverdict/deepverdict.go\"\nfresh: true\nstale_reason: \"\"\n```",
			}, nil)
			_, err := buildResolver().Resolve(ctx, deepverdict.Verdict{
				FileLine:      "pkg/deepverdict/deepverdict.go:109",
				SentryIssueID: "OCTOPUS-PROD-1J",
			}, "bborbe/agent-sentry-issue-analyzer", true)
			Expect(err).NotTo(HaveOccurred())
			_, prompt := runner.RunArgsForCall(0)
			Expect(
				prompt,
			).To(ContainSubstring("implicated_repo: bborbe/agent-sentry-issue-analyzer"))
		})

		It("omits implicated_repo from the task content when no hint is supplied", func() {
			runner.RunReturns(&claudelib.ClaudeResult{
				Result: "```yaml\nrepo: bborbe/trading\nrule: \"resolved from frame path mt5/connector/mt5linux.py\"\nfresh: true\nstale_reason: \"\"\n```",
			}, nil)
			_, err := buildResolver().Resolve(ctx, deepverdict.Verdict{
				FileLine:      "mt5/connector/mt5linux.py:42",
				SentryIssueID: "OCTOPUS-PROD-1J",
			}, "", false)
			Expect(err).NotTo(HaveOccurred())
			_, prompt := runner.RunArgsForCall(0)
			// The instruction half names the literal key, so assert on the task
			// half only.
			taskStart := strings.LastIndex(prompt, "## Task")
			Expect(taskStart).To(BeNumerically(">=", 0))
			Expect(prompt[taskStart:]).NotTo(ContainSubstring("implicated_repo"))
		})

		It("carries the trace evidence the rule label is grounded in", func() {
			runner.RunReturns(&claudelib.ClaudeResult{
				Result: "```yaml\nrepo: bborbe/trading\nrule: \"candidate position 1 (bborbe/trading)\"\nfresh: true\nstale_reason: \"\"\n```",
			}, nil)
			res, err := buildResolver().Resolve(ctx, deepverdict.Verdict{
				FileLine:      "mt5/connector/mt5linux.py:42",
				SentryIssueID: "OCTOPUS-PROD-1J",
			}, "", false)
			Expect(err).NotTo(HaveOccurred())
			Expect(res.Rule).To(Equal("candidate position 1 (bborbe/trading)"))
			_, prompt := runner.RunArgsForCall(0)
			// A frame-path rule asserts trace evidence the model cannot infer
			// from the citation's shape, so the fact is carried into the task
			// content rather than left to the model to invent.
			Expect(prompt).To(ContainSubstring("has_first_party_frame: false"))
		})

		It("wraps ErrUnmappable when the model resolves no repo", func() {
			runner.RunReturns(&claudelib.ClaudeResult{
				Result: "```yaml\nrepo: \"\"\nrule: \"unmappable\"\nfresh: false\nstale_reason: \"\"\n```",
			}, nil)
			_, err := buildResolver().Resolve(ctx, deepverdict.Verdict{
				FileLine:      "netref.py:12",
				SentryIssueID: "OCTOPUS-PROD-1J",
			}, "", false)
			Expect(err).To(HaveOccurred())
			Expect(errors.Is(err, fixagent.ErrUnmappable)).To(BeTrue())
			Expect(err.Error()).To(ContainSubstring("unmappable"))
		})

		It("returns an error when the model output has no fenced yaml block", func() {
			runner.RunReturns(&claudelib.ClaudeResult{Result: "no yaml block here"}, nil)
			_, err := buildResolver().Resolve(ctx, deepverdict.Verdict{
				FileLine:      "mt5/connector/mt5linux.py:42",
				SentryIssueID: "OCTOPUS-PROD-1J",
			}, "", false)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("parse resolution block"))
		})

		It("falls back to the streamed partial text when the final result has no block", func() {
			runner.RunReturns(&claudelib.ClaudeResult{
				Result:  "The background clone confirmed it. My final resolution above stands.",
				Partial: "```yaml\nrepo: bborbe/trading\nrule: \"resolved from frame path mt5/connector/mt5linux.py\"\nfresh: true\nstale_reason: \"\"\n```\nThe background clone confirmed it. My final resolution above stands.",
			}, nil)
			res, err := buildResolver().Resolve(ctx, deepverdict.Verdict{
				FileLine:      "mt5/connector/mt5linux.py:42",
				SentryIssueID: "OCTOPUS-PROD-1J",
			}, "", false)
			Expect(err).NotTo(HaveOccurred())
			Expect(res.Repo).To(Equal("bborbe/trading"))
			Expect(res.Fresh).To(BeTrue())
		})

		It("returns an error when neither result nor partial has a block", func() {
			runner.RunReturns(
				&claudelib.ClaudeResult{Result: "no block", Partial: "still no block"},
				nil,
			)
			_, err := buildResolver().Resolve(ctx, deepverdict.Verdict{
				FileLine:      "mt5/connector/mt5linux.py:42",
				SentryIssueID: "OCTOPUS-PROD-1J",
			}, "", false)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("no fenced yaml block"))
		})

		It("returns an error when the claude run fails", func() {
			runner.RunReturns(nil, os.ErrPermission)
			_, err := buildResolver().Resolve(ctx, deepverdict.Verdict{
				FileLine:      "mt5/connector/mt5linux.py:42",
				SentryIssueID: "OCTOPUS-PROD-1J",
			}, "", false)
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
			}, "", false)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("unclosed yaml fence"))
		})
	})
})
