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
		Entry(
			"the 2026-09-12 block (`unknown`)",
			"```yaml\nverdict: unanalyzable\nconfidence: low\nlive_event_count: unknown\nfirst_seen: unknown\nlast_seen: unknown\nsentry_status: unknown\n```",
		),
		Entry(
			"the 2026-09-13 block (`unavailable`)",
			"```yaml\nverdict: unanalyzable\nconfidence: low\nlive_event_count: unavailable\nfirst_seen: unavailable\nlast_seen: unavailable\nsentry_status: unknown\n```",
		),
		Entry(
			"an unobserved paraphrase, to keep the set open",
			"```yaml\nverdict: unanalyzable\nconfidence: low\nlive_event_count: not-determined\nfirst_seen: not-determined\nlast_seen: not-determined\nsentry_status: unknown\n```",
		),
	)

	It("still reports a real zero as a known count", func() {
		// The mirror case: 0 must remain distinguishable from not-evaluable, or
		// the distinction this change exists to preserve is lost in the other
		// direction.
		v, err := verdict.Parse(
			context.Background(),
			"## Verdict\n\n```yaml\nverdict: noise\nlive_event_count: 0\nfirst_seen: 2026-09-05T12:28:01Z\nlast_seen: 2026-09-05T12:28:01Z\nsentry_status: unresolved\n```",
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(v.LiveEventCount.Known).To(BeTrue())
		Expect(v.LiveEventCount.Count).To(Equal(0))
	})
})

var _ = Describe("Parse a verdict whose free-text prose carries an unquoted colon", func() {
	// 2026-09-13, prod (nuke-k3s-prod, ns `prod`): eleven consecutive analyzer
	// Jobs died on
	//
	//	parse verdict block: yaml: line 13: mapping values are not allowed in this context
	//
	// The block was an ordinary `real bug` verdict; only its `recommended_fix`
	// (line 13) contains an unquoted `: `. yaml.v3 reads `tls: failed` as a
	// nested mapping and rejects the whole block, discarding the analysis.
	//
	// Provenance: the `recommended_fix` prose is verbatim from the pod logs
	// (pods `sentry-analyzer-agent-d084b808-20260913122102-4brvc` and
	// `-20260913121339-r6nbp`, since aged out by Job TTL) and was emitted as ONE
	// line - the task evidence wraps it for readability. The wrap shape matters:
	// split across two lines with the continuation at column 0 the block parses
	// without error and silently truncates the value at the first line, which is
	// NOT what prod did. Only the single line reproduces prod's `line 13`.
	// `live_event_count` and `first_seen` are likewise verbatim. The remaining
	// keys are reconstructed to the schema in verdict.go - they parsed fine in
	// prod and are not the failing element; their order is what puts
	// `recommended_fix` on line 13, matching the production error.
	It("parses the production block instead of rejecting it", func() {
		content := "## Verdict\n\n" + "```yaml\n" + `sentry_issue_id: NUKE-PROD-XX
verdict: real bug
confidence: high
reason: Kafka topic deployment fails when the API server connection drops
live_event_count: 2
first_seen: 2026-08-08T10:34:43Z
last_seen: 2026-09-13T12:20:00Z
sentry_status: unresolved
disqualifiers_fired: []
understanding: high
fix_certainty: high
root_cause: Sarama wraps the TLS closeNotify error in *errors.dataError, so IsNetworkError does not unwrap it
recommended_fix: Extend IsNetworkError to unwrap Sarama *errors.dataError wrapper and check underlying error, or add IsTLSCloseError classifier for the tls: failed to send closeNotify pattern
` + "```\n"

		v, err := verdict.Parse(context.Background(), content)
		Expect(err).NotTo(HaveOccurred())
		Expect(v.Verdict).To(Equal("real bug"))
		Expect(v.RecommendedFix).To(ContainSubstring("tls: failed to send closeNotify"))
		Expect(v.LiveEventCount.Count).To(Equal(2))
	})
})

var _ = Describe("Parse prose carrying an unquoted colon", func() {
	// The fix must hold for arbitrary prose, not the one observed string. Every
	// free-text field of the schema is exercised, plus a paraphrase that never
	// appeared in production - a repair keyed to `tls: failed` fails here.
	DescribeTable("accepts an unquoted colon in every free-text field",
		func(line string, field func(verdict.Verdict) string) {
			content := "## Verdict\n\n```yaml\nverdict: real bug\nconfidence: high\n" + line + "\n```\n"
			v, err := verdict.Parse(context.Background(), content)
			Expect(err).NotTo(HaveOccurred())
			Expect(field(v)).To(ContainSubstring("tls: failed to send closeNotify"))
		},
		Entry("reason",
			"reason: the producer keeps retrying on the tls: failed to send closeNotify error",
			func(v verdict.Verdict) string { return v.Reason }),
		Entry("root_cause",
			"root_cause: Sarama never classifies tls: failed to send closeNotify as retryable",
			func(v verdict.Verdict) string { return v.RootCause }),
		Entry("recommended_fix",
			"recommended_fix: add a classifier for the tls: failed to send closeNotify pattern",
			func(v verdict.Verdict) string { return v.RecommendedFix }),
	)

	It("parses a paraphrase that never appeared in production", func() {
		content := "## Verdict\n\n```yaml\nverdict: noise\nreason: the alert fires on every rollout: the counter is reset by the deploy\n```\n"
		v, err := verdict.Parse(context.Background(), content)
		Expect(err).NotTo(HaveOccurred())
		Expect(
			v.Reason,
		).To(Equal("the alert fires on every rollout: the counter is reset by the deploy"))
	})

	It("folds a wrapped prose continuation instead of truncating it", func() {
		// The model may wrap long prose with an indented continuation. That shape
		// is illegal YAML for the same reason, and the whole value must survive -
		// a repair that only re-quotes the first line would silently truncate it.
		content := "## Verdict\n\n```yaml\nverdict: real bug\nconfidence: high\nreason: first half of the story\n  and the second half with a colon: right here\n```\n"
		v, err := verdict.Parse(context.Background(), content)
		Expect(err).NotTo(HaveOccurred())
		Expect(
			v.Reason,
		).To(Equal("first half of the story and the second half with a colon: right here"))
	})

	It("ends a folded prose value at the next schema key", func() {
		// The fold must stop at a real field boundary: were the boundary line
		// swallowed into the prose, the field after it would silently lose its
		// value. This is what keeps verdictFields in step with the schema - drop a
		// key from that list and this case fails.
		content := "## Verdict\n\n```yaml\nverdict: real bug\nconfidence: high\nreason: the retry fires on every rollout: the counter resets\nroot_cause: nil check missing\n```\n"
		v, err := verdict.Parse(context.Background(), content)
		Expect(err).NotTo(HaveOccurred())
		Expect(v.Reason).To(Equal("the retry fires on every rollout: the counter resets"))
		Expect(v.RootCause).To(Equal("nil check missing"))
	})

	It("still reports malformed YAML that is not prose", func() {
		// The repair is scoped to prose values: a block broken some other way must
		// keep failing, with its own error rather than a repaired verdict.
		content := "## Verdict\n\n```yaml\nverdict: [unclosed\n```\n"
		_, err := verdict.Parse(context.Background(), content)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("parse verdict block"))
	})
})
