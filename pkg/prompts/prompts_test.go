// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package prompts_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/bborbe/agent-sentry-issue-analyzer/pkg/prompts"
)

var _ = Describe("BuildPlanningInstructions (triage)", func() {
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

	It("planning prompt contains the token-REST live-state script invocation", func() {
		instrs := prompts.BuildPlanningInstructions()
		Expect(instrs[0].Content).To(ContainSubstring("scripts/sentry-read.sh"))
	})

	It("planning prompt contains the read-only repo clone invocation", func() {
		instrs := prompts.BuildPlanningInstructions()
		Expect(instrs[0].Content).To(ContainSubstring("scripts/repo-clone.sh clone"))
		Expect(instrs[0].Content).To(ContainSubstring("scripts/repo-clone.sh log"))
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

var _ = Describe("BuildExecutionInstructions (triage)", func() {
	It("returns exactly 2 instructions", func() {
		instrs := prompts.BuildExecutionInstructions()
		Expect(instrs).To(HaveLen(2))
	})

	It("first instruction is execution", func() {
		instrs := prompts.BuildExecutionInstructions()
		Expect(instrs[0].Name).To(Equal("execution"))
		Expect(instrs[0].Content).NotTo(BeEmpty())
	})

	It("execution prompt contains the noise disqualifiers", func() {
		instrs := prompts.BuildExecutionInstructions()
		Expect(instrs[0].Content).To(ContainSubstring("live_event_count > 10000"))
		Expect(instrs[0].Content).To(ContainSubstring("regressed"))
		Expect(instrs[0].Content).To(ContainSubstring("Do NOT use simple `<50 events = noise`"))
	})

	It("execution prompt instructs live-state re-fetch before the verdict", func() {
		instrs := prompts.BuildExecutionInstructions()
		Expect(instrs[0].Content).To(ContainSubstring("scripts/sentry-read.sh"))
	})

	It("execution prompt uses the triage 6-verdict rubric", func() {
		instrs := prompts.BuildExecutionInstructions()
		for _, v := range []string{"already-tracked", "regression", "real bug", "noise", "duplicate", "not-a-defect"} {
			Expect(instrs[0].Content).To(ContainSubstring("`" + v + "`"))
		}
	})

	It("execution prompt defines the triage verdict YAML keys", func() {
		instrs := prompts.BuildExecutionInstructions()
		for _, k := range []string{"sentry_issue_id", "verdict", "confidence", "reason", "live_event_count", "sentry_status"} {
			Expect(instrs[0].Content).To(ContainSubstring(k))
		}
	})

	It("execution prompt writes the verdict into the ## Verdict section", func() {
		instrs := prompts.BuildExecutionInstructions()
		Expect(instrs[0].Content).To(ContainSubstring("## Verdict"))
	})
})

var _ = Describe("BuildDeepPlanningInstructions", func() {
	It("returns exactly 2 instructions", func() {
		instrs := prompts.BuildDeepPlanningInstructions()
		Expect(instrs).To(HaveLen(2))
	})

	It("first instruction is deep-planning", func() {
		instrs := prompts.BuildDeepPlanningInstructions()
		Expect(instrs[0].Name).To(Equal("deep-planning"))
		Expect(instrs[0].Content).NotTo(BeEmpty())
	})

	It("second instruction is output-format", func() {
		instrs := prompts.BuildDeepPlanningInstructions()
		Expect(instrs[1].Name).To(Equal("output-format"))
	})

	It("deep planning prompt contains the token-REST live-state script + clone", func() {
		instrs := prompts.BuildDeepPlanningInstructions()
		Expect(instrs[0].Content).To(ContainSubstring("scripts/sentry-read.sh"))
		Expect(instrs[0].Content).To(ContainSubstring("scripts/repo-clone.sh clone"))
	})

	It("deep planning prompt writes the ## Context section", func() {
		instrs := prompts.BuildDeepPlanningInstructions()
		Expect(instrs[0].Content).To(ContainSubstring("## Context"))
	})
})

var _ = Describe("BuildDeepExecutionInstructions", func() {
	It("returns exactly 2 instructions", func() {
		instrs := prompts.BuildDeepExecutionInstructions()
		Expect(instrs).To(HaveLen(2))
	})

	It("first instruction is deep-execution", func() {
		instrs := prompts.BuildDeepExecutionInstructions()
		Expect(instrs[0].Name).To(Equal("deep-execution"))
		Expect(instrs[0].Content).NotTo(BeEmpty())
	})

	It("deep execution prompt uses the octopus verdict vocabulary", func() {
		instrs := prompts.BuildDeepExecutionInstructions()
		for _, v := range []string{"real bug", "noise", "duplicate", "closed-fixed-in-prod", "not-a-defect", "track"} {
			Expect(instrs[0].Content).To(ContainSubstring("`" + v + "`"))
		}
	})

	It("deep execution prompt defines the octopus verdict YAML keys", func() {
		instrs := prompts.BuildDeepExecutionInstructions()
		for _, k := range []string{"sentry_issue_id", "verdict", "understanding", "fix_certainty", "root_cause", "recommended_fix", "file:line", "disqualifiers_fired", "live_event_count"} {
			Expect(instrs[0].Content).To(ContainSubstring(k))
		}
	})

	It("deep execution prompt writes the verdict into the ## Verdict section", func() {
		instrs := prompts.BuildDeepExecutionInstructions()
		Expect(instrs[0].Content).To(ContainSubstring("## Verdict"))
	})
})
