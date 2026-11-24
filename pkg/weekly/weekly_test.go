// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package weekly_test

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/agent-sentry-issue-analyzer/pkg/weekly"
)

var _ = Describe("RouteFor", func() {
	// oneArm asserts the single disposition of one reviewed issue. Each case
	// names the opposite outcome a wrong implementation would produce, so a
	// blanket route cannot satisfy the set.
	oneArm := func(issue weekly.Issue, expected weekly.Route) {
		Expect(weekly.RouteFor(issue)).To(Equal(expected))
	}

	It("routes noise to nothing — no proposal, no item", func() {
		oneArm(weekly.Issue{Verdict: "noise"}, weekly.RouteNothing)
	})

	It("routes noise to nothing even when a disqualifier fired and no PR exists", func() {
		oneArm(
			weekly.Issue{Verdict: "noise", DisqualifiersFired: []string{"volume"}},
			weekly.RouteNothing,
		)
	})

	It("routes a real bug with a fired disqualifier and no PR to the Jira proposal", func() {
		oneArm(
			weekly.Issue{
				Verdict:            "real bug",
				DisqualifiersFired: []string{"volume"},
			},
			weekly.RouteJiraProposal,
		)
	})

	It("routes a real bug with a merged fix PR to an Obsidian item, never a proposal", func() {
		oneArm(
			weekly.Issue{
				Verdict:            "real bug",
				DisqualifiersFired: []string{"volume"},
				PRURL:              "https://github.com/bborbe/x/pull/1",
			},
			weekly.RouteObsidianItem,
		)
	})

	It("routes a real bug with no disqualifier to an Obsidian item", func() {
		oneArm(weekly.Issue{Verdict: "real bug"}, weekly.RouteObsidianItem)
	})

	It("routes an unanalyzable verdict to an Obsidian item", func() {
		oneArm(weekly.Issue{Verdict: "unanalyzable"}, weekly.RouteObsidianItem)
	})

	It("keeps the regression arm alive for the day the guard changes", func() {
		oneArm(
			weekly.Issue{
				Verdict:            "regression",
				DisqualifiersFired: []string{"volume"},
			},
			weekly.RouteJiraProposal,
		)
	})
})

var _ = Describe("IsDormant", func() {
	now := time.Date(2026, time.November, 23, 12, 0, 0, 0, time.UTC)

	It("calls an issue dormant at exactly the threshold", func() {
		issue := weekly.Issue{LastSeen: now.Add(-weekly.DormantAfter)}
		Expect(weekly.IsDormant(issue, now)).To(BeTrue())
	})

	It("does not call an issue dormant one second inside the threshold", func() {
		issue := weekly.Issue{LastSeen: now.Add(-weekly.DormantAfter + time.Second)}
		Expect(weekly.IsDormant(issue, now)).To(BeFalse())
	})
})

var _ = Describe("WeekToken", func() {
	It("renders the ISO week of a Monday and of the Sunday that closes it as one token", func() {
		monday := time.Date(2026, time.November, 23, 0, 0, 0, 0, time.UTC)
		sunday := time.Date(2026, time.November, 29, 23, 59, 59, 0, time.UTC)

		Expect(weekly.WeekToken(monday)).To(Equal("2026-W48"))
		Expect(weekly.WeekToken(sunday)).To(Equal(weekly.WeekToken(monday)))
	})

	It("renders a different token across a week boundary", func() {
		Expect(weekly.WeekToken(time.Date(2026, time.November, 22, 0, 0, 0, 0, time.UTC))).
			To(Equal("2026-W47"))
		Expect(weekly.WeekToken(time.Date(2026, time.November, 30, 0, 0, 0, 0, time.UTC))).
			To(Equal("2026-W49"))
	})

	It("renders the same token for the same instant in different locations", func() {
		utc := time.Date(2026, time.November, 23, 12, 0, 0, 0, time.UTC)
		plusOne := utc.In(time.FixedZone("CET", 3600))

		Expect(weekly.WeekToken(plusOne)).To(Equal(weekly.WeekToken(utc)))
	})
})

var _ = Describe("TaskIdentifier", func() {
	It("is stable for the same stage and week, so a second run is a no-op", func() {
		Expect(weekly.TaskIdentifier("dev", "2026-W48")).
			To(Equal(weekly.TaskIdentifier("dev", "2026-W48")))
	})

	It("differs across a week boundary", func() {
		Expect(weekly.TaskIdentifier("dev", "2026-W48")).
			NotTo(Equal(weekly.TaskIdentifier("dev", "2026-W49")))
	})

	It("differs across stages in the same week", func() {
		Expect(weekly.TaskIdentifier("dev", "2026-W48")).
			NotTo(Equal(weekly.TaskIdentifier("prod", "2026-W48")))
	})

	It("never collides with a per-alert task id", func() {
		Expect(weekly.TaskIdentifier("dev", "2026-W48").String()).
			NotTo(Equal("1b06ab0c-eede-481f-b6ee-e91b0708fba9"))
	})
})

var _ = Describe("Title", func() {
	It("carries both the stage and the week token", func() {
		Expect(weekly.Title("prod", "2026-W48")).
			To(Equal("Sentry Weekly Roll-Up prod 2026-W48"))
	})
})
