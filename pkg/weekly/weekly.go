// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package weekly decides the one disposition of every reviewed Sentry issue and
// keys the week's Obsidian work task.
//
// It is the pure half of the weekly roll-up: no I/O, no clock reads, no vault
// writes. The step that gathers the review set and emits the task calls in
// here, so the three-way routing and the week identity are testable without a
// pipeline — and so the routing cannot drift from pkg/steps/jira-proposal.go,
// whose rule this mirrors.
package weekly

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Route is the single disposition of one reviewed issue. Exactly one applies: a
// reviewed issue is never both an Obsidian item and a Jira proposal.
type Route string

const (
	// RouteJiraProposal means the issue earns a ### Jira Proposal block and no
	// Obsidian item. It mirrors the emission rule in
	// pkg/steps/jira-proposal.go: the verdict qualifies, at least one
	// disqualifier fired, and the fix agent produced no PR.
	RouteJiraProposal Route = "jira-proposal"
	// RouteObsidianItem means the issue is worth doing but is not a Jira
	// proposal — it becomes an item in the week's Obsidian task.
	RouteObsidianItem Route = "obsidian-item"
	// RouteNothing means the issue is noise: no proposal, no item.
	RouteNothing Route = "nothing"
)

// jiraProposalVerdicts mirrors pkg/steps/jira-proposal.go's map, deliberately
// as a map over a fixed verdict set rather than an if-chain so a new verdict
// cannot fall through into the proposal arm by accident.
//
// `regression` is unreachable in the wired pipeline — reassign.go relabels
// every verdict that is neither `real bug` nor `unanalyzable` to `real bug`
// once a disqualifier fires — and is kept for the same reason it is kept there:
// a guard change must not silently drop the arm.
var jiraProposalVerdicts = map[string]bool{
	"real bug":   true,
	"regression": true,
}

// Issue is the analyzer's own record of one reviewed alert, reduced to the
// fields the routing rule reads.
type Issue struct {
	// ShortID is the Sentry short id, e.g. OCTOPUS-PROD-12. It is the only
	// identifier a human can act on, so the emitted task lists it.
	ShortID string
	// Project is the Sentry project slug the issue belongs to.
	Project string
	// Title is the alert title as the collector recorded it.
	Title string
	// URL is the issue permalink.
	URL string
	// LastSeen is the last event timestamp, used to decide dormancy.
	LastSeen time.Time
	// Verdict is the analyzer's verdict string.
	Verdict string
	// DisqualifiersFired is the recorded disqualifier names. Empty means none
	// fired.
	DisqualifiersFired []string
	// PRURL is the fix agent's PR, empty when it produced none.
	PRURL string
}

// RouteFor returns the one disposition this issue earns.
func RouteFor(issue Issue) Route {
	if issue.Verdict == "noise" {
		return RouteNothing
	}
	if !jiraProposalVerdicts[issue.Verdict] {
		return RouteObsidianItem
	}
	if len(issue.DisqualifiersFired) > 0 && issue.PRURL == "" {
		return RouteJiraProposal
	}
	return RouteObsidianItem
}

// DormantAfter is the age at which an unresolved issue counts as dormant. An
// issue nobody has seen for this long is exactly the one a lastSeen-windowed
// review would miss, which is the gap this roll-up exists to close.
const DormantAfter = 14 * 24 * time.Hour

// IsDormant reports whether the issue has been silent for at least DormantAfter
// as of now. now is a parameter rather than a clock read so the caller injects
// the time it already holds.
func IsDormant(issue Issue, now time.Time) bool {
	return now.Sub(issue.LastSeen) >= DormantAfter
}

// WeekToken renders the ISO week identity of t, e.g. 2026-W48.
//
// The token, not the task, is the roll-up's idempotency key: one task per stage
// per week. It is derived from the clock the caller injects rather than from
// time.Now(), so a replayed run in the same week reproduces the same token and
// the second run is a no-op.
func WeekToken(t time.Time) string {
	year, week := t.UTC().ISOWeek()
	return fmt.Sprintf("%d-W%02d", year, week)
}

// weeklyNamespace is derived from the per-alert namespace rather than invented,
// so the relationship between the two id families is recorded and the value is
// reproducible from source.
var weeklyNamespace = uuid.NewSHA1(
	uuid.MustParse("1b06ab0c-eede-481f-b6ee-e91b0708fba9"),
	[]byte("sentry-weekly-namespace"),
)

// TaskIdentifier derives the weekly task's identifier from (stage, week).
// Same stage and week -> same id -> the controller's dedup makes a second run
// in the week a no-op; a new week -> a fresh task.
func TaskIdentifier(stage, week string) uuid.UUID {
	return uuid.NewSHA1(weeklyNamespace, []byte("sentry-weekly:"+stage+":"+week))
}

// Title renders the week's task title. The stage and the week token are both in
// the title so a reader can tell two stages' tasks apart at a glance.
func Title(stage, week string) string {
	return "Sentry Weekly Roll-Up " + stage + " " + week
}
