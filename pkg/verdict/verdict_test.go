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
confidence: high
reason: clear nil deref on nullable field
live_event_count: 142
last_seen: 2026-06-26T06:55:11Z
sentry_status: unresolved
understanding: high
fix_certainty: medium
root_cause: nil check missing
recommended_fix: add nil guard
` + "```\n"
		v, err := verdict.Parse(ctx, content)
		Expect(err).NotTo(HaveOccurred())
		Expect(v.SentryIssueID).To(Equal("OCTOPUS-PROD-1J"))
		Expect(v.Verdict).To(Equal("real bug"))
		Expect(v.Confidence).To(Equal("high"))
		Expect(v.LiveEventCount).To(Equal(142))
	})

	It("parses a noise verdict", func() {
		content := "## Verdict\n\n" + "```yaml\nsentry_issue_id: OCTOPUS-PROD-C\nverdict: noise\nreason: kafka metadata sync\n```\n"
		v, err := verdict.Parse(ctx, content)
		Expect(err).NotTo(HaveOccurred())
		Expect(v.Verdict).To(Equal("noise"))
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

	It("accepts each of the 6 valid verdicts", func() {
		for _, v := range []string{"already-tracked", "regression", "real bug", "noise", "duplicate", "not-a-defect"} {
			ver := verdict.Verdict{SentryIssueID: "X", Verdict: v}
			if v == "real bug" {
				ver.Confidence = "high"
			}
			err := verdict.Validate(ctx, ver)
			Expect(err).NotTo(HaveOccurred(), "verdict %q should validate", v)
		}
	})

	It("rejects a real-bug verdict with invalid confidence", func() {
		err := verdict.Validate(ctx, verdict.Verdict{
			SentryIssueID: "X",
			Verdict:       "real bug",
			Confidence:    "definitely",
		})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("invalid confidence"))
	})

	It("accepts a fully-populated real-bug verdict", func() {
		err := verdict.Validate(ctx, verdict.Verdict{
			SentryIssueID:  "X",
			Verdict:        "real bug",
			Confidence:     "low",
			RootCause:      "nil deref",
			RecommendedFix: "add guard",
		})
		Expect(err).NotTo(HaveOccurred())
	})
})
