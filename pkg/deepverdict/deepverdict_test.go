// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package deepverdict_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/agent-sentry-issue-analyzer/pkg/deepverdict"
)

var _ = Describe("Parse", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	It("returns zero verdict when no ## Verdict section exists", func() {
		v, err := deepverdict.Parse(ctx, "# Task\n\njust a task body\n")
		Expect(err).NotTo(HaveOccurred())
		Expect(v.Verdict).To(BeEmpty())
	})

	It("parses a real-bug verdict YAML block from the ## Verdict section", func() {
		content := `---
status: in_progress
---

## Task

something

## Context

deep root cause

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
		v, err := deepverdict.Parse(ctx, content)
		Expect(err).NotTo(HaveOccurred())
		Expect(v.SentryIssueID).To(Equal("OCTOPUS-PROD-1J"))
		Expect(v.Verdict).To(Equal("real bug"))
		Expect(v.Understanding).To(Equal("High"))
		Expect(v.FixCertainty).To(Equal("Medium"))
		Expect(v.RootCause).To(Equal("nil check missing"))
		Expect(v.RecommendedFix).To(Equal("add nil guard"))
		Expect(v.FileLine).To(Equal("pkg/handler/handler.go:142"))
		Expect(v.DisqualifiersFired).To(Equal([]string{"Volume"}))
		Expect(v.LiveEventCount).To(Equal(deepverdict.EventCount{Count: 142, Known: true}))
	})

	It("parses a noise verdict with no disqualifiers fired", func() {
		content := "## Verdict\n\n" + "```yaml\nsentry_issue_id: OCTOPUS-PROD-C\nverdict: noise\ndisqualifiers_fired: []\n```\n"
		v, err := deepverdict.Parse(ctx, content)
		Expect(err).NotTo(HaveOccurred())
		Expect(v.Verdict).To(Equal("noise"))
		Expect(v.DisqualifiersFired).To(BeEmpty())
	})

	It("returns a parse error when a block has malformed YAML", func() {
		content := "## Verdict\n\n" + "```yaml\nverdict: [unclosed\n```\n"
		_, err := deepverdict.Parse(ctx, content)
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("Validate", func() {
	var ctx context.Context

	BeforeEach(func() {
		ctx = context.Background()
	})

	It("rejects a missing sentry_issue_id", func() {
		err := deepverdict.Validate(ctx, deepverdict.Verdict{Verdict: "noise"})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("sentry_issue_id"))
	})

	It("rejects an unknown verdict", func() {
		err := deepverdict.Validate(ctx, deepverdict.Verdict{SentryIssueID: "X", Verdict: "bogus"})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("unknown verdict"))
	})

	It("accepts each of the 6 octopus verdicts", func() {
		for _, v := range []string{"real bug", "noise", "duplicate", "closed-fixed-in-prod", "not-a-defect", "track"} {
			ver := deepverdict.Verdict{SentryIssueID: "X", Verdict: v}
			if v == "real bug" {
				ver.Understanding = "High"
				ver.FixCertainty = "High"
				ver.FileLine = "a.go:1"
				ver.RootCause = "rc"
				ver.RecommendedFix = "fix"
			}
			err := deepverdict.Validate(ctx, ver)
			Expect(err).NotTo(HaveOccurred(), "verdict %q should validate", v)
		}
	})

	It("rejects a real-bug verdict with invalid understanding", func() {
		err := deepverdict.Validate(ctx, deepverdict.Verdict{
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
		err := deepverdict.Validate(ctx, deepverdict.Verdict{
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
		err := deepverdict.Validate(ctx, deepverdict.Verdict{
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
		err := deepverdict.Validate(ctx, deepverdict.Verdict{
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
		err := deepverdict.Validate(ctx, deepverdict.Verdict{
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
		err := deepverdict.Validate(ctx, deepverdict.Verdict{
			SentryIssueID:      "X",
			Verdict:            "real bug",
			Understanding:      "High",
			FixCertainty:       "Low",
			RootCause:          "nil deref",
			RecommendedFix:     "add guard",
			FileLine:           "pkg/handler/handler.go:142",
			DisqualifiersFired: []string{"Volume"},
			LiveEventCount:     deepverdict.EventCount{Count: 10000, Known: true},
		})
		Expect(err).NotTo(HaveOccurred())
	})
})

var _ = Describe("Parse not-evaluable live-state tokens", func() {
	// The deep execution prompt leaves the live-state fields unavailable for
	// derived-key alerts, and the model renders that instruction as a token
	// rather than a number. The token drifts with the prompt's own adjective, so
	// both production spellings are pinned here as verbatim blocks - plus one
	// unobserved paraphrase, because the accepted set is open by design and an
	// enumerated implementation must fail it.
	DescribeTable(
		"parses the production block and marks the count NotEvaluable",
		func(block string) {
			v, err := deepverdict.Parse(context.Background(), "## Verdict\n\n"+block)
			Expect(err).NotTo(HaveOccurred())
			Expect(v.LiveEventCount.Known).To(BeFalse())
			Expect(v.LiveEventCount.Count).To(Equal(0))
		},
		Entry(
			"the 2026-09-12 block (`unknown` on the event count)",
			"```yaml\nsentry_issue_id: NUKE-PROD-BT\nverdict: unanalyzable\nlive_event_count: unknown\nfirst_seen: unknown\nlast_seen: unknown\nsentry_status: unknown\n```",
		),
		Entry(
			"the 2026-09-13 block (`unavailable` on the event count)",
			"```yaml\nsentry_issue_id: NUKE-PROD-BT\nverdict: unanalyzable\nlive_event_count: unavailable\nfirst_seen: unavailable\nlast_seen: unavailable\nsentry_status: unknown\n```",
		),
		Entry(
			"an unobserved paraphrase, to keep the accepted set open",
			"```yaml\nsentry_issue_id: NUKE-PROD-BT\nverdict: unanalyzable\nlive_event_count: not-determined\nfirst_seen: not-determined\nlast_seen: not-determined\nsentry_status: unknown\n```",
		),
	)

	It("never exposes a non-numeric token as a coerced zero", func() {
		// A coerced zero would be EventCount{Count: 0, Known: true} - the exact
		// shape of a measured "no events", so the distinction this change exists
		// to preserve would be lost.
		v, err := deepverdict.Parse(
			context.Background(),
			"## Verdict\n\n```yaml\nsentry_issue_id: NUKE-PROD-BT\nverdict: unanalyzable\nlive_event_count: unknown\nfirst_seen: unknown\nlast_seen: unknown\nsentry_status: unknown\n```",
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(v.LiveEventCount).NotTo(Equal(deepverdict.EventCount{Count: 0, Known: true}))
		Expect(v.LiveEventCount).To(Equal(deepverdict.EventCount{Count: 0, Known: false}))
	})

	It("keeps a measured zero distinguishable from not-evaluable", func() {
		// The mirror case: 0 must stay a known measurement, or the tolerance
		// swallows legitimate values in the other direction.
		v, err := deepverdict.Parse(
			context.Background(),
			"## Verdict\n\n```yaml\nsentry_issue_id: NUKE-PROD-BT\nverdict: noise\nlive_event_count: 0\nfirst_seen: 2026-09-05T12:28:01Z\nlast_seen: 2026-09-05T12:28:01Z\nsentry_status: unresolved\n```",
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(v.LiveEventCount.Known).To(BeTrue())
		Expect(v.LiveEventCount.Count).To(Equal(0))
	})
})
