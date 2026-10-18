// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package verdict_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/agent-sentry-issue-analyzer/pkg/verdict"
)

var _ = Describe("Analyzability", func() {
	// The two frozen verdict-quality review cases this rule exists for: row 4
	// (NUKE-PROD-AJ) and row 10 (NUKE-DEV-FB). Both carried a full root_cause
	// beside `verdict: unanalyzable`, which is the contradiction — a block that
	// names its own cause has been analysed.
	Describe("ContradictsAnalysis", func() {
		It("is true when unanalyzable names a root cause", func() {
			Expect(verdict.ContradictsAnalysis(verdict.Verdict{
				Verdict:   "unanalyzable",
				RootCause: "bborbe/kafka kafka_consumer-offset.go:496 — the consumer offset loop closes the messages channel",
			})).To(BeTrue())
		})

		It("is true when unanalyzable names a recommended fix", func() {
			Expect(verdict.ContradictsAnalysis(verdict.Verdict{
				Verdict:        "unanalyzable",
				RecommendedFix: "guard the offset loop against a closed channel before ranging",
			})).To(BeTrue())
		})

		DescribeTable(
			"is false when the analysis fields carry a placeholder",
			func(rootCause, recommendedFix string) {
				Expect(verdict.ContradictsAnalysis(verdict.Verdict{
					Verdict:        "unanalyzable",
					RootCause:      rootCause,
					RecommendedFix: recommendedFix,
				})).To(BeFalse())
			},
			Entry("both empty", "", ""),
			Entry("unknown", "unknown", "unknown"),
			Entry("unavailable", "unavailable", "unavailable"),
			Entry("n/a", "n/a", "n/a"),
			Entry("none", "none", "none"),
			Entry(
				"a not-known phrase with an explanation",
				"unknown (no first-party frame)",
				"n/a",
			),
			Entry("an em-dash placeholder", "unknown — no frames", "none"),
		)

		It("is false for an analyzable verdict that names a cause", func() {
			Expect(verdict.ContradictsAnalysis(verdict.Verdict{
				Verdict:   "real bug",
				RootCause: "nil deref",
			})).To(BeFalse())
		})
	})

	Describe("ApplyAnalyzabilityGuard", func() {
		It("rewrites a contradictory unanalyzable verdict to real bug", func() {
			v := verdict.Verdict{
				SentryIssueID:  "NUKE-PROD-AJ",
				Verdict:        "unanalyzable",
				Reason:         "All 4 frames are in_app=unknown",
				FixCertainty:   "medium",
				RootCause:      "bborbe/kafka kafka_consumer-offset.go:496",
				RecommendedFix: "guard the closed channel",
			}

			Expect(verdict.ApplyAnalyzabilityGuard(&v)).To(BeTrue())
			Expect(v.Verdict).To(Equal("real bug"))
			Expect(v.Confidence).To(Equal("medium"))
			Expect(v.Reason).To(ContainSubstring("analyzability guard"))
			// The model's own rationale is preserved as evidence.
			Expect(v.Reason).To(ContainSubstring("All 4 frames are in_app=unknown"))
		})

		It("defaults confidence to low when fix_certainty is not a confidence word", func() {
			v := verdict.Verdict{
				Verdict:   "unanalyzable",
				RootCause: "bborbe/kafka kafka_consumer-offset.go:496",
			}

			Expect(verdict.ApplyAnalyzabilityGuard(&v)).To(BeTrue())
			Expect(v.Confidence).To(Equal("low"))
		})

		It("leaves a genuinely unanalyzable verdict untouched", func() {
			v := verdict.Verdict{
				Verdict:   "unanalyzable",
				Reason:    "no exception entry (stack trace unavailable)",
				RootCause: "unknown",
			}

			Expect(verdict.ApplyAnalyzabilityGuard(&v)).To(BeFalse())
			Expect(v.Verdict).To(Equal("unanalyzable"))
			Expect(v.Confidence).To(BeEmpty())
		})
	})

	Describe("RestoreUnmodeledLines", func() {
		It("carries a deep verdict's file:line through a re-render", func() {
			original := "```yaml\nsentry_issue_id: NUKE-PROD-AJ\nverdict: unanalyzable\nfile:line: pkg/kafka/kafka_consumer-offset.go:496\nroot_cause: closed channel\n```"
			rendered := "```yaml\nsentry_issue_id: NUKE-PROD-AJ\nverdict: real bug\nroot_cause: closed channel\n```"

			got := verdict.RestoreUnmodeledLines(original, rendered)

			Expect(got).To(ContainSubstring("file:line: pkg/kafka/kafka_consumer-offset.go:496"))
			// The re-rendered value wins; the original entry is not duplicated.
			Expect(got).To(ContainSubstring("verdict: real bug"))
			Expect(got).NotTo(ContainSubstring("verdict: unanalyzable"))
			// Restored inside the fence, not after it.
			Expect(got).To(MatchRegexp(`file:line:.*\n` + "```"))
		})

		It("returns the rendered block unchanged when nothing is unmodeled", func() {
			rendered := "```yaml\nsentry_issue_id: X\nverdict: real bug\n```"
			Expect(verdict.RestoreUnmodeledLines(rendered, rendered)).To(Equal(rendered))
		})
	})
})
