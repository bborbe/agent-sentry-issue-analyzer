// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package verdict_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/agent-sentry-issue-analyzer/pkg/verdict"
)

var _ = Describe("Parse", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	It("returns zero verdict when no ## Verdict section exists", func() {
		v, err := verdict.Parse(ctx, "# Task\n\njust a task body\n")
		Expect(err).NotTo(HaveOccurred())
		Expect(v.Verdict).To(BeEmpty())
	})

	It("parses a real-bug verdict YAML block from the ## Verdict section", func() {
		content := `---
status: in_progress
---

## Task

something

## Analysis

root cause analysis

## Verdict

` + "```yaml\n" + `sentry_issue_id: OCTOPUS-PROD-1J
verdict: real bug
understanding: High
fix_certainty: Medium
root_cause: nil check missing
recommended_fix: add nil guard
file:line: pkg/handler/handler.go:142
disqualifiers_fired: [Volume]
live_event_count: 142
` + "```\n"
		v, err := verdict.Parse(ctx, content)
		Expect(err).NotTo(HaveOccurred())
		Expect(v.SentryIssueID).To(Equal("OCTOPUS-PROD-1J"))
		Expect(v.Verdict).To(Equal("real bug"))
		Expect(v.Understanding).To(Equal("High"))
		Expect(v.FixCertainty).To(Equal("Medium"))
		Expect(v.RootCause).To(Equal("nil check missing"))
		Expect(v.RecommendedFix).To(Equal("add nil guard"))
		Expect(v.FileLine).To(Equal("pkg/handler/handler.go:142"))
		Expect(v.DisqualifiersFired).To(Equal([]string{"Volume"}))
		Expect(v.LiveEventCount).To(Equal(142))
	})

	It("parses a noise verdict with no disqualifiers fired", func() {
		content := "## Verdict\n\n" + "```yaml\nsentry_issue_id: OCTOPUS-PROD-C\nverdict: noise\ndisqualifiers_fired: []\n```\n"
		v, err := verdict.Parse(ctx, content)
		Expect(err).NotTo(HaveOccurred())
		Expect(v.Verdict).To(Equal("noise"))
		Expect(v.DisqualifiersFired).To(BeEmpty())
	})

	It("returns a parse error when a block has malformed YAML", func() {
		content := "## Verdict\n\n" + "```yaml\nverdict: [unclosed\n```\n"
		_, err := verdict.Parse(ctx, content)
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("Validate", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	It("rejects a missing sentry_issue_id", func() {
		err := verdict.Validate(ctx, verdict.Verdict{Verdict: "noise"})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("sentry_issue_id"))
	})

	It("rejects an unknown verdict", func() {
		err := verdict.Validate(ctx, verdict.Verdict{SentryIssueID: "X", Verdict: "bogus"})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("unknown verdict"))
	})

	It("accepts each of the 6 octopus verdicts", func() {
		for _, v := range []string{"real bug", "noise", "duplicate", "closed-fixed-in-prod", "not-a-defect", "track"} {
			ver := verdict.Verdict{SentryIssueID: "X", Verdict: v}
			if v == "real bug" {
				ver.Understanding = "High"
				ver.FixCertainty = "High"
				ver.FileLine = "a.go:1"
				ver.RootCause = "rc"
				ver.RecommendedFix = "fix"
			}
			err := verdict.Validate(ctx, ver)
			Expect(err).NotTo(HaveOccurred(), "verdict %q should validate", v)
		}
	})

	It("rejects a real-bug verdict with invalid understanding", func() {
		err := verdict.Validate(ctx, verdict.Verdict{
			SentryIssueID:  "X",
			Verdict:        "real bug",
			Understanding:  "certainly",
			FixCertainty:   "High",
			FileLine:       "a.go:1",
			RootCause:      "rc",
			RecommendedFix: "fix",
		})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("invalid understanding"))
	})

	It("rejects a real-bug verdict with invalid fix_certainty", func() {
		err := verdict.Validate(ctx, verdict.Verdict{
			SentryIssueID:  "X",
			Verdict:        "real bug",
			Understanding:  "High",
			FixCertainty:   "maybe",
			FileLine:       "a.go:1",
			RootCause:      "rc",
			RecommendedFix: "fix",
		})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("invalid fix_certainty"))
	})

	It("rejects a real-bug verdict missing file:line", func() {
		err := verdict.Validate(ctx, verdict.Verdict{
			SentryIssueID:  "X",
			Verdict:        "real bug",
			Understanding:  "High",
			FixCertainty:   "High",
			RootCause:      "rc",
			RecommendedFix: "fix",
		})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("file:line"))
	})

	It("rejects a real-bug verdict missing root_cause", func() {
		err := verdict.Validate(ctx, verdict.Verdict{
			SentryIssueID:  "X",
			Verdict:        "real bug",
			Understanding:  "High",
			FixCertainty:   "High",
			FileLine:       "a.go:1",
			RecommendedFix: "fix",
		})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("root_cause"))
	})

	It("rejects a real-bug verdict missing recommended_fix", func() {
		err := verdict.Validate(ctx, verdict.Verdict{
			SentryIssueID: "X",
			Verdict:       "real bug",
			Understanding: "High",
			FixCertainty:  "High",
			FileLine:      "a.go:1",
			RootCause:     "rc",
		})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("recommended_fix"))
	})

	It("accepts a fully-populated real-bug verdict", func() {
		err := verdict.Validate(ctx, verdict.Verdict{
			SentryIssueID:      "X",
			Verdict:            "real bug",
			Understanding:      "High",
			FixCertainty:       "Low",
			RootCause:          "nil deref",
			RecommendedFix:     "add guard",
			FileLine:           "pkg/handler/handler.go:142",
			DisqualifiersFired: []string{"Volume"},
			LiveEventCount:     10000,
		})
		Expect(err).NotTo(HaveOccurred())
	})
})
