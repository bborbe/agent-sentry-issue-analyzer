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

	DescribeTable(
		"computes the band from live state",
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

		// Boundaries. These are where the restated disqualifier arithmetic can
		// drift silently, so each edge is pinned on both sides.
		Entry("volume just above the now threshold is now",
			10001, true, now.Add(-26*time.Hour), "unresolved", verdict.UrgencyNow),
		Entry("volume exactly at the now threshold is not now — the branch is strict >",
			10000, true, now.Add(-26*time.Hour), "unresolved", verdict.UrgencySoon),
		Entry("burst at exactly the 24h edge is still now",
			2000, true, now.Add(-24*time.Hour), "unresolved", verdict.UrgencyNow),
		Entry("one second past the 24h edge is no longer a burst, but the backlog floor holds",
			2000, true, now.Add(-24*time.Hour-time.Second), "unresolved", verdict.UrgencySoon),
		Entry("recency at exactly the 7d edge is soon",
			500, true, now.Add(-7*24*time.Hour), "unresolved", verdict.UrgencySoon),
		Entry("one second past the 7d edge is someday",
			500, true, now.Add(-7*24*time.Hour-time.Second), "unresolved", verdict.UrgencySomeday),
		Entry("count exactly at soonMinCount, recent, is soon",
			100, true, now.Add(-2*time.Hour), "unresolved", verdict.UrgencySoon),
		Entry("one below soonMinCount, recent, is someday",
			99, true, now.Add(-2*time.Hour), "unresolved", verdict.UrgencySomeday),
		Entry("count exactly at the backlog floor, stale, is soon",
			1000, true, now.Add(-60*24*time.Hour), "unresolved", verdict.UrgencySoon),
		Entry("one below the backlog floor, stale, is someday",
			999, true, now.Add(-60*24*time.Hour), "unresolved", verdict.UrgencySomeday),
		Entry("a regression with an unmeasurable count is still now",
			0, false, now.Add(-60*24*time.Hour), "regressed", verdict.UrgencyNow),
		Entry("a last_seen in the future reads as just now, not as a negative delta",
			500, true, now.Add(48*time.Hour), "unresolved", verdict.UrgencySoon),
	)

	// Sharing the getter is not sharing the instant: two reads inside one
	// applyDisqualifiers call can straddle the 24h or 7d boundary, leaving the
	// band and the fired list describing different moments of the same alert.
	Describe("a single evaluation instant", func() {
		It("reads the clock exactly once for both results", func() {
			calls := 0
			single := verdict.NewDisqualifierEvaluator(
				libtime.CurrentDateTimeGetterFunc(func() libtime.DateTime {
					calls++
					return libtime.DateTime(now)
				}),
			)

			evaluation, err := single.EvaluateAll(
				context.Background(),
				verdict.DisqualifierInput{
					LiveEventCount: verdict.EventCount{Count: 500, Known: true},
					FirstSeen:      now.Add(-60 * 24 * time.Hour),
					LastSeen:       now.Add(-2 * time.Hour),
					SentryStatus:   "unresolved",
				},
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(calls).To(Equal(1))
			Expect(evaluation.Urgency).To(Equal(verdict.UrgencySoon))
		})

		It("derives both results from that one instant", func() {
			// The getter would return a 400-day-later instant on a second read.
			// If either result were computed from it, the band would collapse to
			// someday and the burst would not fire.
			calls := 0
			straddling := verdict.NewDisqualifierEvaluator(
				libtime.CurrentDateTimeGetterFunc(func() libtime.DateTime {
					calls++
					if calls == 1 {
						return libtime.DateTime(now)
					}
					return libtime.DateTime(now.Add(400 * 24 * time.Hour))
				}),
			)

			evaluation, err := straddling.EvaluateAll(
				context.Background(),
				verdict.DisqualifierInput{
					LiveEventCount: verdict.EventCount{Count: 2000, Known: true},
					FirstSeen:      now.Add(-60 * 24 * time.Hour),
					LastSeen:       now.Add(-2 * time.Hour),
					SentryStatus:   "unresolved",
				},
			)
			Expect(err).NotTo(HaveOccurred())
			Expect(calls).To(Equal(1))
			Expect(evaluation.Urgency).To(Equal(verdict.UrgencyNow))
			Expect(evaluation.Disqualifiers).To(ContainElement(verdict.DisqualifierActiveBurst))
		})
	})

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
