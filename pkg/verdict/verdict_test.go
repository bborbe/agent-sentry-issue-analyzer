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
		Expect(v.LiveEventCount).To(Equal(verdict.EventCount{Count: 142, Known: true}))
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

	It("parses a verdict JSON block fenced as json", func() {
		content := "## Verdict\n\n" + "```json\n" + `{"sentry_issue_id":"OCTOPUS-PROD-1J","verdict":"real bug","confidence":"high","live_event_count":142}` + "\n```\n"
		v, err := verdict.Parse(ctx, content)
		Expect(err).NotTo(HaveOccurred())
		Expect(v.Verdict).To(Equal("real bug"))
		Expect(v.Confidence).To(Equal("high"))
		Expect(v.LiveEventCount).To(Equal(verdict.EventCount{Count: 142, Known: true}))
	})

	It("parses a legacy unfenced raw JSON verdict", func() {
		content := "## Verdict\n\n" + `{"sentry_issue_id":"OCTOPUS-PROD-C","verdict":"noise","reason":"kafka metadata sync"}` + "\n"
		v, err := verdict.Parse(ctx, content)
		Expect(err).NotTo(HaveOccurred())
		Expect(v.Verdict).To(Equal("noise"))
	})

	It("skips a fenced JSON envelope without a verdict key", func() {
		content := "## Verdict\n\n" + "```json\n" + `{"status":"done","message":"Verdict written","files":[]}` + "\n```\n"
		v, err := verdict.Parse(ctx, content)
		Expect(err).NotTo(HaveOccurred())
		Expect(v.Verdict).To(BeEmpty())
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
		Expect(err.Error()).To(ContainSubstring("unanalyzable"))
	})

	It("accepts each of the 7 valid verdicts", func() {
		for _, v := range []string{"already-tracked", "regression", "real bug", "noise", "duplicate", "not-a-defect", "unanalyzable"} {
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

var _ = Describe("Vocabulary", func() {
	// Adding a verdict to validVerdicts requires updating this golden slice
	// AND adding the value to the accepts-each-of-the-7 case, because the
	// runtime validator is the only pre-deploy guard.
	It("locks the triage verdict vocabulary at 7 sorted values", func() {
		Expect(verdict.Vocabulary()).To(HaveLen(7))
		Expect(verdict.Vocabulary()).To(Equal([]string{
			"already-tracked",
			"duplicate",
			"noise",
			"not-a-defect",
			"real bug",
			"regression",
			"unanalyzable",
		}))
	})
})

var _ = Describe("Parse not-evaluable live-state tokens", func() {
	// The execution prompt tells the model to leave the live-state fields
	// unavailable for derived-key alerts, and it renders that instruction as a
	// token. The token drifts with the prompt's own adjective, so both
	// production spellings are pinned here as verbatim blocks - one per token,
	// because the int field is what crashes and a block carrying `unknown` only
	// on `sentry_status` (a string field) would not exercise it.
	DescribeTable(
		"parses the production block and marks the count not evaluable",
		func(block string) {
			v, err := verdict.Parse(context.Background(), "## Verdict\n\n"+block)
			Expect(err).NotTo(HaveOccurred())
			Expect(v.Verdict).To(Equal("unanalyzable"))
			// The whole point: a token must not be observable as a number. A
			// coerced 0 would present "volume unknown" to the disqualifier
			// arithmetic as "no events" - a fabricated measurement.
			Expect(v.LiveEventCount.Known).To(BeFalse())
			Expect(v.LiveEventCount.Count).To(Equal(0))
		},
		Entry("the 2026-09-12 block (`unknown`)", "```yaml\nverdict: unanalyzable\nconfidence: low\nlive_event_count: unknown\nfirst_seen: unknown\nlast_seen: unknown\nsentry_status: unknown\n```"),
		Entry("the 2026-09-13 block (`unavailable`)", "```yaml\nverdict: unanalyzable\nconfidence: low\nlive_event_count: unavailable\nfirst_seen: unavailable\nlast_seen: unavailable\nsentry_status: unknown\n```"),
		Entry("an unobserved paraphrase, to keep the set open", "```yaml\nverdict: unanalyzable\nconfidence: low\nlive_event_count: not-determined\nfirst_seen: not-determined\nlast_seen: not-determined\nsentry_status: unknown\n```"),
	)

	It("still reports a real zero as a known count", func() {
		// The mirror case: 0 must remain distinguishable from not-evaluable, or
		// the distinction this change exists to preserve is lost in the other
		// direction.
		v, err := verdict.Parse(context.Background(), "## Verdict\n\n```yaml\nverdict: noise\nlive_event_count: 0\nfirst_seen: 2026-09-05T12:28:01Z\nlast_seen: 2026-09-05T12:28:01Z\nsentry_status: unresolved\n```")
		Expect(err).NotTo(HaveOccurred())
		Expect(v.LiveEventCount.Known).To(BeTrue())
		Expect(v.LiveEventCount.Count).To(Equal(0))
	})
})
