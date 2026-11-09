// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package verdictrecord_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/agent-sentry-issue-analyzer/pkg/verdictrecord"
)

// freshBody is the shape cmd/create-tasks renders on every dispatch.
const freshBody = "# Analyze Sentry issue OCTOPUS-DEV-4A - 2026-11-04\n" +
	"\n" +
	"Active unresolved Sentry alert (last activity 2026-11-04) in project nuke-dev.\n" +
	"- Issue: OCTOPUS-DEV-4A\n" +
	"- Project: nuke-dev\n"

// priorVerdict is what a completed run left behind, including the fenced YAML
// that carries the fields SC10/SC11 are measured on.
const priorVerdict = `## Verdict

` + "```yaml" + `
verdict: real bug
understanding: High
fix_certainty: High
` + "```"

const priorFixResult = `## Fix Result

` + "```yaml" + `
status: declined
repo: bborbe/agent-task-executor
message: fix-agent: repo bborbe/agent-task-executor not in REPO_ALLOWLIST; refusing to file
` + "```"

var _ = Describe("Section", func() {
	It("returns the section verbatim, heading included", func() {
		body := freshBody + "\n" + priorVerdict + "\n\n## Analysis\n\nnot this one\n"
		Expect(verdictrecord.Section(body, verdictrecord.VerdictHeading)).To(Equal(priorVerdict))
	})

	It("stops at the next level-two heading", func() {
		body := "## Verdict\n\nkeep\n\n## Fix Result\n\ndrop\n"
		Expect(
			verdictrecord.Section(body, verdictrecord.VerdictHeading),
		).To(Equal("## Verdict\n\nkeep"))
	})

	It("keeps level-three headings inside the section", func() {
		body := "## Fix Result\n\n### Detail\n\nkept\n\n## Next\n\ndropped\n"
		Expect(verdictrecord.Section(body, verdictrecord.FixResultHeading)).
			To(Equal("## Fix Result\n\n### Detail\n\nkept"))
	})

	It("returns empty when the body carries no such section", func() {
		Expect(verdictrecord.Section(freshBody, verdictrecord.VerdictHeading)).To(BeEmpty())
	})

	It("does not match a heading that merely shares the prefix", func() {
		body := "## Verdicts\n\nwrong\n"
		Expect(verdictrecord.Section(body, verdictrecord.VerdictHeading)).To(BeEmpty())
	})

	It("does not match a level-three heading of the same name", func() {
		body := "### Verdict\n\nwrong\n"
		Expect(verdictrecord.Section(body, verdictrecord.VerdictHeading)).To(BeEmpty())
	})

	It("returns empty for an empty body", func() {
		Expect(verdictrecord.Section("", verdictrecord.VerdictHeading)).To(BeEmpty())
	})
})

var _ = Describe("Retain", func() {
	It("returns the body byte-identical when there is nothing to retain", func() {
		Expect(verdictrecord.Retain(freshBody, "", "")).To(Equal(freshBody))
	})

	It("returns the body byte-identical when the retained sections are whitespace", func() {
		Expect(verdictrecord.Retain(freshBody, "\n\n", "  \n")).To(Equal(freshBody))
	})

	It("appends both sections after the fresh body", func() {
		got := verdictrecord.Retain(freshBody, priorVerdict, priorFixResult)
		Expect(got).To(HavePrefix(freshBody))
		Expect(got).To(ContainSubstring(priorVerdict))
		Expect(got).To(ContainSubstring(priorFixResult))
	})

	It("appends only the section that exists", func() {
		got := verdictrecord.Retain(freshBody, priorVerdict, "")
		Expect(got).To(ContainSubstring(priorVerdict))
		Expect(got).NotTo(ContainSubstring(verdictrecord.FixResultHeading))
	})

	It("is idempotent: retaining the same sections twice yields the same bytes", func() {
		once := verdictrecord.Retain(freshBody, priorVerdict, priorFixResult)
		verdict, fixResult := verdictrecord.Extract(once)
		Expect(verdictrecord.Retain(freshBody, verdict, fixResult)).To(Equal(once))
	})
})

var _ = Describe("Extract", func() {
	It("returns both sections of a body that carries them", func() {
		body := freshBody + "\n" + priorVerdict + "\n\n" + priorFixResult + "\n"
		verdict, fixResult := verdictrecord.Extract(body)
		Expect(verdict).To(Equal(priorVerdict))
		Expect(fixResult).To(Equal(priorFixResult))
	})

	It("returns two empty strings for a fresh body", func() {
		verdict, fixResult := verdictrecord.Extract(freshBody)
		Expect(verdict).To(BeEmpty())
		Expect(fixResult).To(BeEmpty())
	})

	It("round-trips: Extract then Retain reproduces the original", func() {
		body := freshBody + "\n" + priorVerdict + "\n\n" + priorFixResult + "\n"
		verdict, fixResult := verdictrecord.Extract(body)
		Expect(verdictrecord.Retain(freshBody, verdict, fixResult)).To(Equal(body))
	})
})
