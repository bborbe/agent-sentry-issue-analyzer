// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package verdict_test

import (
	"context"
	"time"

	libtime "github.com/bborbe/time"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/agent-sentry-issue-analyzer/pkg/verdict"
)

var _ = Describe("Urgency", func() {
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)

	evaluator := verdict.NewDisqualifierEvaluator(
		libtime.CurrentDateTimeGetterFunc(func() libtime.DateTime {
			return libtime.DateTime(now)
		}),
	)

	evaluate := func(
		count int,
		known bool,
		lastSeen time.Time,
		status string,
	) verdict.UrgencyBand {
		band, err := evaluator.EvaluateUrgency(
			context.Background(),
			verdict.DisqualifierInput{
				LiveEventCount: verdict.EventCount{Count: count, Known: known},
				FirstSeen:      now.Add(-60 * 24 * time.Hour),
				LastSeen:       lastSeen,
				SentryStatus:   status,
			},
		)
		Expect(err).NotTo(HaveOccurred())
		return band
	}

	It("exposes a closed set of exactly three bands", func() {
		Expect(verdict.UrgencyBands()).To(HaveLen(3))
		Expect(verdict.UrgencyBands()).To(ConsistOf(
			verdict.UrgencyNow,
			verdict.UrgencySoon,
			verdict.UrgencySomeday,
		))
	})

	DescribeTable("computes the band from live state",
		func(count int, known bool, lastSeen time.Time, status string, expected verdict.UrgencyBand) {
			Expect(evaluate(count, known, lastSeen, status)).To(Equal(expected))
		},
		Entry("volume above the disqualifier threshold is now",
			20000, true, now.Add(-26*time.Hour), "unresolved", verdict.UrgencyNow),
		Entry("active burst within 24h above 1000 is now",
			2000, true, now.Add(-2*time.Hour), "unresolved", verdict.UrgencyNow),
		Entry("a regression is now regardless of volume",
			5, true, now.Add(-60*24*time.Hour), "regressed", verdict.UrgencyNow),
		Entry("recent low volume above 100 is soon",
			500, true, now.Add(-2*time.Hour), "unresolved", verdict.UrgencySoon),
		Entry("a large backlog is soon however stale",
			5000, true, now.Add(-60*24*time.Hour), "unresolved", verdict.UrgencySoon),
		Entry("a small stale alert is someday",
			5, true, now.Add(-60*24*time.Hour), "unresolved", verdict.UrgencySomeday),
		Entry("an unknown count is not read as zero events",
			0, false, now.Add(-2*time.Hour), "unresolved", verdict.UrgencySomeday),
	)

	// The two pairs SC2 turns on. Each pair differs in exactly one input, so a
	// band derived from the other input alone cannot split it.
	Describe("the controlled pairs", func() {
		It("splits two alerts differing only in live_event_count", func() {
			lastSeen := now.Add(-26 * time.Hour)
			Expect(evaluate(20000, true, lastSeen, "unresolved")).To(Equal(verdict.UrgencyNow))
			Expect(evaluate(5, true, lastSeen, "unresolved")).To(Equal(verdict.UrgencySomeday))
		})

		It("splits two alerts with equal live_event_count differing only in last_seen", func() {
			Expect(evaluate(500, true, now.Add(-2*time.Hour), "unresolved")).
				To(Equal(verdict.UrgencySoon))
			Expect(evaluate(500, true, now.Add(-60*24*time.Hour), "unresolved")).
				To(Equal(verdict.UrgencySomeday))
		})
	})
})
