// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package prompts_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/agent-sentry-issue-analyzer/pkg/prompts"
)

var _ = Describe("BuildPlanningInstructions", func() {
	It("returns exactly 2 instructions", func() {
		instrs := prompts.BuildPlanningInstructions()
		Expect(instrs).To(HaveLen(2))
	})

	It("first instruction is planning", func() {
		instrs := prompts.BuildPlanningInstructions()
		Expect(instrs[0].Name).To(Equal("planning"))
		Expect(instrs[0].Content).NotTo(BeEmpty())
	})

	It("second instruction is output-format", func() {
		instrs := prompts.BuildPlanningInstructions()
		Expect(instrs[1].Name).To(Equal("output-format"))
		Expect(instrs[1].Content).NotTo(BeEmpty())
	})

	It("planning prompt contains the live-state tool invocation", func() {
		instrs := prompts.BuildPlanningInstructions()
		Expect(instrs[0].Content).To(ContainSubstring("mcp__sentry__get_sentry_resource"))
	})

	It("planning prompt contains the ## Analysis section heading", func() {
		instrs := prompts.BuildPlanningInstructions()
		Expect(instrs[0].Content).To(ContainSubstring("## Analysis"))
	})

	It("planning prompt instructs reading the implicated source code read-only", func() {
		instrs := prompts.BuildPlanningInstructions()
		Expect(instrs[0].Content).To(ContainSubstring("READ-ONLY source access"))
		Expect(instrs[0].Content).To(ContainSubstring("file.go:line"))
	})
})

var _ = Describe("BuildExecutionInstructions", func() {
	It("returns exactly 2 instructions", func() {
		instrs := prompts.BuildExecutionInstructions()
		Expect(instrs).To(HaveLen(2))
	})

	It("first instruction is execution", func() {
		instrs := prompts.BuildExecutionInstructions()
		Expect(instrs[0].Name).To(Equal("execution"))
		Expect(instrs[0].Content).NotTo(BeEmpty())
	})

	It("execution prompt contains the 6-verdict rubric", func() {
		instrs := prompts.BuildExecutionInstructions()
		for _, v := range []string{"already-tracked", "regression", "real bug", "noise", "duplicate", "not-a-defect"} {
			Expect(instrs[0].Content).To(ContainSubstring("`" + v + "`"))
		}
	})

	It("execution prompt contains the noise disqualifiers", func() {
		instrs := prompts.BuildExecutionInstructions()
		Expect(instrs[0].Content).To(ContainSubstring("live_event_count > 10000"))
		Expect(instrs[0].Content).To(ContainSubstring("regressed"))
		Expect(instrs[0].Content).To(ContainSubstring("Do NOT use simple `<50 events = noise`"))
	})

	It("execution prompt instructs live-state re-fetch before the verdict", func() {
		instrs := prompts.BuildExecutionInstructions()
		Expect(instrs[0].Content).To(ContainSubstring("mcp__sentry__get_sentry_resource"))
	})

	It("execution prompt defines the verdict YAML keys", func() {
		instrs := prompts.BuildExecutionInstructions()
		Expect(instrs[0].Content).To(ContainSubstring("sentry_issue_id"))
		Expect(instrs[0].Content).To(ContainSubstring("verdict"))
		Expect(instrs[0].Content).To(ContainSubstring("confidence"))
	})

	It("execution prompt writes the verdict into the ## Verdict section", func() {
		instrs := prompts.BuildExecutionInstructions()
		Expect(instrs[0].Content).To(ContainSubstring("## Verdict"))
	})
})
