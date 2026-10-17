// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package verdict_test

import (
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/agent-sentry-issue-analyzer/pkg/verdict"
)

var _ = Describe("RewriteIdentityLine", func() {
	// deepVerdictBody is the shape the deep verdict writes: the file:line and
	// the two High ratings must survive the rewrite, because the fix handoff
	// reads them.
	deepVerdictBody := "```yaml\n" +
		"sentry_issue_id: 7791255664\n" +
		"verdict: real bug\n" +
		"confidence: high\n" +
		"file:line: pkg/handler/handler.go:142\n" +
		"understanding: High\n" +
		"fix_certainty: High\n" +
		"disqualifiers_fired: []\n" +
		"```"

	It("rewrites only the identity line of a deep verdict block", func() {
		got := verdict.RewriteIdentityLine(deepVerdictBody, "NUKE-DEV-FB")

		Expect(got).To(ContainSubstring("sentry_issue_id: NUKE-DEV-FB"))
		Expect(got).To(ContainSubstring("file:line: pkg/handler/handler.go:142"))
		Expect(got).To(ContainSubstring("understanding: High"))
		Expect(got).To(ContainSubstring("fix_certainty: High"))
		Expect(got).To(ContainSubstring("disqualifiers_fired: []"))

		before := strings.Split(deepVerdictBody, "\n")
		after := strings.Split(got, "\n")
		Expect(after).To(HaveLen(len(before)))
		var changed []int
		for i := range before {
			if before[i] != after[i] {
				changed = append(changed, i)
			}
		}
		Expect(changed).To(Equal([]int{1}))
	})

	It("returns an already-canonical body unchanged", func() {
		body := "sentry_issue_id: NUKE-DEV-FB\nverdict: real bug"
		Expect(verdict.RewriteIdentityLine(body, "NUKE-DEV-FB")).To(Equal(body))
	})

	It("returns a body with no identity line unchanged", func() {
		body := "```yaml\nverdict: real bug\nconfidence: high\n```"
		Expect(verdict.RewriteIdentityLine(body, "NUKE-DEV-FB")).To(Equal(body))
	})

	It("rewrites an identity line that is the first line", func() {
		body := "sentry_issue_id: 7791255664\nverdict: real bug"
		Expect(verdict.RewriteIdentityLine(body, "NUKE-DEV-FB")).To(Equal(
			"sentry_issue_id: NUKE-DEV-FB\nverdict: real bug",
		))
	})

	It("rewrites an identity line that is the last line", func() {
		body := "verdict: real bug\nsentry_issue_id: 7791255664"
		Expect(verdict.RewriteIdentityLine(body, "NUKE-DEV-FB")).To(Equal(
			"verdict: real bug\nsentry_issue_id: NUKE-DEV-FB",
		))
	})

	It("rewrites a JSON identity value and leaves every other key byte-identical", func() {
		body := "```json\n" +
			"{\n" +
			`  "sentry_issue_id": "7791255664",` + "\n" +
			`  "verdict": "real bug"` + "\n" +
			"}\n" +
			"```"
		got := verdict.RewriteIdentityLine(body, "NUKE-DEV-FB")
		Expect(got).To(ContainSubstring(`  "sentry_issue_id": "NUKE-DEV-FB",`))
		Expect(got).To(ContainSubstring(`  "verdict": "real bug"`))
		Expect(got).To(ContainSubstring("```json"))
		Expect(got).To(ContainSubstring("```"))
	})

	It("rewrites the first top-level occurrence only", func() {
		body := "sentry_issue_id: 7791255664\nverdict: real bug\nsentry_issue_id: 111"
		Expect(verdict.RewriteIdentityLine(body, "NUKE-DEV-FB")).To(Equal(
			"sentry_issue_id: NUKE-DEV-FB\nverdict: real bug\nsentry_issue_id: 111",
		))
	})

	It("leaves an indented identity key alone", func() {
		body := "  sentry_issue_id: 7791255664\nverdict: real bug"
		Expect(verdict.RewriteIdentityLine(body, "NUKE-DEV-FB")).To(Equal(body))
	})

	It("leaves a longer key with the identity prefix alone", func() {
		body := "sentry_issue_id_extra: 7791255664\nverdict: real bug"
		Expect(verdict.RewriteIdentityLine(body, "NUKE-DEV-FB")).To(Equal(body))
	})

	DescribeTable("rewrites either accepted spelling",
		func(body string, want string) {
			Expect(verdict.RewriteIdentityLine(body, "NUKE-DEV-FB")).To(Equal(want))
		},
		Entry(
			"yaml",
			"sentry_issue_id: OCTOPUS-PROD-1J",
			"sentry_issue_id: NUKE-DEV-FB",
		),
		Entry(
			"json with trailing comma",
			`  "sentry_issue_id": "OCTOPUS-PROD-1J",`,
			`  "sentry_issue_id": "NUKE-DEV-FB",`,
		),
	)
})
