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

var _ = Describe("DisqualifierEvaluator", func() {
	var (
		ctx context.Context
	)

	// fixedNow is captured once at spec-definition time. The DescribeTable
	// entries build their timestamps from it AND the evaluator's injected
	// clock returns it, so the two always agree — a `now` set in BeforeEach
	// would not, because Entry args are evaluated before BeforeEach runs.
	fixedNow := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)

	BeforeEach(func() {
		ctx = context.Background()
	})

	newEvaluator := func() verdict.DisqualifierEvaluator {
		return verdict.NewDisqualifierEvaluator(
			libtime.CurrentDateTimeGetterFunc(func() libtime.DateTime {
				return libtime.DateTime(fixedNow)
			}),
		)
	}

	DescribeTable(
		"boundary cases from the 2026-09-05 failures",
		func(input verdict.DisqualifierInput, expected []verdict.Disqualifier) {
			fired, err := newEvaluator().Evaluate(ctx, input)
			Expect(err).NotTo(HaveOccurred())
			Expect(fired).To(ConsistOf(expected))
		},
		Entry("rate 1.01/day → sustained span fires", verdict.DisqualifierInput{
			LiveEventCount: 101,
			FirstSeen:      fixedNow.Add(-100 * 24 * time.Hour),
			LastSeen:       fixedNow,
		}, []verdict.Disqualifier{verdict.DisqualifierSustainedSpan}),
		Entry("rate 0.99/day → stays noise", verdict.DisqualifierInput{
			LiveEventCount: 99,
			FirstSeen:      fixedNow.Add(-100 * 24 * time.Hour),
			LastSeen:       fixedNow,
		}, nil),
		Entry("count ≥ 100 with rate < 1/day (NUKE-DEV-A4 branch)", verdict.DisqualifierInput{
			LiveEventCount: 193,
			FirstSeen:      fixedNow.Add(-301 * 24 * time.Hour),
			LastSeen:       fixedNow,
		}, []verdict.Disqualifier{verdict.DisqualifierSustainedSpan}),
		Entry(
			"active burst at the 1000 boundary — exactly 1000 does not fire",
			verdict.DisqualifierInput{
				LiveEventCount: 1000,
				FirstSeen:      fixedNow.Add(-10 * 24 * time.Hour),
				LastSeen:       fixedNow.Add(-time.Hour),
			},
			nil,
		),
		Entry("active burst > 1000 within 24h fires", verdict.DisqualifierInput{
			LiveEventCount: 1001,
			FirstSeen:      fixedNow.Add(-10 * 24 * time.Hour),
			LastSeen:       fixedNow.Add(-time.Hour),
		}, []verdict.Disqualifier{verdict.DisqualifierActiveBurst}),
		Entry(
			"active burst > 1000 but last_seen older than 24h does not fire",
			verdict.DisqualifierInput{
				LiveEventCount: 1001,
				FirstSeen:      fixedNow.Add(-10 * 24 * time.Hour),
				LastSeen:       fixedNow.Add(-25 * time.Hour),
			},
			nil,
		),
		Entry("span exactly 30 days does not fire", verdict.DisqualifierInput{
			LiveEventCount: 100,
			FirstSeen:      fixedNow.Add(-30 * 24 * time.Hour),
			LastSeen:       fixedNow,
		}, nil),
		Entry("span 31 days with count ≥ 100 fires", verdict.DisqualifierInput{
			LiveEventCount: 100,
			FirstSeen:      fixedNow.Add(-31 * 24 * time.Hour),
			LastSeen:       fixedNow,
		}, []verdict.Disqualifier{verdict.DisqualifierSustainedSpan}),
		Entry(
			"no-disqualifier case stays noise (rate ~0.8/day, count 50)",
			verdict.DisqualifierInput{
				LiveEventCount: 50,
				FirstSeen:      fixedNow.Add(-62 * 24 * time.Hour),
				LastSeen:       fixedNow.Add(-24 * time.Hour),
			},
			nil,
		),
		Entry("NUKE-DEV-94 shutdown race stays noise (84 ev / 325d)", verdict.DisqualifierInput{
			LiveEventCount: 84,
			FirstSeen:      fixedNow.Add(-325 * 24 * time.Hour),
			LastSeen:       fixedNow,
		}, nil),
		Entry("NUKE-DEV-CG shutdown race stays noise (49 ev / 222d)", verdict.DisqualifierInput{
			LiveEventCount: 49,
			FirstSeen:      fixedNow.Add(-222 * 24 * time.Hour),
			LastSeen:       fixedNow,
		}, nil),
		Entry("volume > 10000 fires", verdict.DisqualifierInput{
			LiveEventCount: 10001,
		}, []verdict.Disqualifier{verdict.DisqualifierVolume}),
		Entry("regressed status fires", verdict.DisqualifierInput{
			SentryStatus: "regressed",
		}, []verdict.Disqualifier{verdict.DisqualifierRegressed}),
		Entry("sustained span + active burst both fire (NUKE-DEV-3A)", verdict.DisqualifierInput{
			LiveEventCount: 1262,
			FirstSeen:      fixedNow.Add(-875 * 24 * time.Hour),
			LastSeen:       fixedNow.Add(-2 * time.Hour),
		}, []verdict.Disqualifier{verdict.DisqualifierActiveBurst, verdict.DisqualifierSustainedSpan}),
	)
})
