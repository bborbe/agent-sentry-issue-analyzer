// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package prompts_test

import (
	"strings"

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

	// IMPORTANT: the triage and deep-planning assertions below are duplicated on
	// purpose. Spec 001 AC 11 greps this file for `bborbe/trading` and
	// `bborbe/kafka` on >=2 lines (one per builder); a DescribeTable or shared
	// helper collapses them to one and silently drops the per-builder guarantee.
	It("planning prompt resolves the repo frame-path-first with an ordered candidate list", func() {
		content := prompts.BuildPlanningInstructions()[0].Content
		Expect(content).To(ContainSubstring("nuke-dev"))
		Expect(content).To(ContainSubstring("nuke-prod"))
		Expect(content).To(ContainSubstring("bborbe/trading"))
		Expect(content).To(ContainSubstring("mt5/connector"))
		Expect(content).To(ContainSubstring("bborbe/kafka"))
		Expect(content).To(ContainSubstring("docs/repo-mapping.md"))
		Expect(content).To(ContainSubstring("frame path"))
		Expect(strings.Index(content, "frame path")).
			To(BeNumerically("<", strings.Index(content, "candidate")))
	})

	It("planning prompt orders the infrastructure repo after the application repos", func() {
		content := prompts.BuildPlanningInstructions()[0].Content
		Expect(strings.Index(content, "bborbe/nuke")).
			To(BeNumerically(">", strings.Index(content, "bborbe/trading")))
	})

	It("planning prompt excludes third-party frames from repo resolution", func() {
		content := prompts.BuildPlanningInstructions()[0].Content
		Expect(content).To(ContainSubstring("third-party"))
		Expect(content).To(ContainSubstring("rpyc"))
		Expect(content).To(ContainSubstring("netref.py"))
	})

	It("planning prompt records the no-first-party-frame condition instead of escalating", func() {
		content := prompts.BuildPlanningInstructions()[0].Content
		Expect(content).NotTo(ContainSubstring("escalate saying the trace is entirely third-party"))
		Expect(content).To(ContainSubstring("no exception entry"))
		Expect(content).To(ContainSubstring("## Analysis"))
		Expect(content).NotTo(ContainSubstring("unanalyzable"))
		Expect(content).To(ContainSubstring("Do not assign a verdict"))
	})

	It(
		"planning prompt requires candidates tried and the resolution mechanism in the output",
		func() {
			content := prompts.BuildPlanningInstructions()[0].Content
			Expect(content).To(ContainSubstring("candidates tried"))
			Expect(content).To(ContainSubstring("resolved from frame path"))
			Expect(content).To(ContainSubstring("candidate position"))
		},
	)

	It("planning prompt no longer maps a Sentry project to a single canonical repo", func() {
		content := prompts.BuildPlanningInstructions()[0].Content
		Expect(content).NotTo(ContainSubstring("map to source repo"))
		Expect(content).NotTo(ContainSubstring("project-named variant"))
		Expect(content).NotTo(ContainSubstring("bborbe/trading-bot"))
		Expect(content).NotTo(ContainSubstring("bborbe/nuke-dev"))
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

	It(
		"planning prompt references the root_cause_* pointer block for file:line identification",
		func() {
			instrs := prompts.BuildPlanningInstructions()
			Expect(instrs[0].Content).To(ContainSubstring("root_cause_"))
			Expect(instrs[0].Content).To(ContainSubstring("deepest first-party"))
		},
	)

	It("planning prompt scopes investigation to first-party frames via in_app", func() {
		instrs := prompts.BuildPlanningInstructions()
		Expect(instrs[0].Content).To(ContainSubstring("in_app"))
	})
})

var _ = Describe("output-format shared contract", func() {
	It("mandates a fenced json block instead of forbidding fences", func() {
		instrs := prompts.BuildPlanningInstructions()
		Expect(instrs[1].Name).To(Equal("output-format"))
		Expect(instrs[1].Content).To(ContainSubstring("fenced code block"))
		Expect(instrs[1].Content).To(ContainSubstring("```json"))
		Expect(
			instrs[1].Content,
		).NotTo(ContainSubstring("Do NOT wrap the JSON in markdown code fences"))
	})
})

var _ = Describe("no-ID (derived-key) path", func() {
	It(
		"planning prompt instructs skipping the live fetch and classifying from the snapshot instead of refusing",
		func() {
			instrs := prompts.BuildPlanningInstructions()
			Expect(instrs[0].Content).To(ContainSubstring("Derived-key (no-ID) tasks"))
			Expect(
				instrs[0].Content,
			).To(ContainSubstring("do NOT call `sentry-read.sh` and do NOT refuse"))
			Expect(instrs[0].Content).To(ContainSubstring("outcome"))
		},
	)

	It("execution prompt handles no-ID tasks without a live-state re-fetch", func() {
		instrs := prompts.BuildExecutionInstructions()
		Expect(instrs[0].Content).To(ContainSubstring("Derived-key (no-ID) tasks"))
		Expect(instrs[0].Content).To(ContainSubstring("sentry_status: unknown"))
	})

	It("deep planning prompt handles derived-key real-bug tasks", func() {
		instrs := prompts.BuildDeepPlanningInstructions()
		Expect(instrs[0].Content).To(ContainSubstring("Derived-key (no-ID) tasks"))
		Expect(instrs[0].Content).To(ContainSubstring("no stack trace to implicate a repo"))
	})

	It("deep execution prompt handles derived-key tasks", func() {
		instrs := prompts.BuildDeepExecutionInstructions()
		Expect(instrs[0].Content).To(ContainSubstring("Derived-key (no-ID) tasks"))
		Expect(instrs[0].Content).To(ContainSubstring("sentry_status: unknown"))
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

	It("execution prompt uses the triage 7-verdict rubric", func() {
		instrs := prompts.BuildExecutionInstructions()
		for _, v := range []string{"already-tracked", "regression", "real bug", "noise", "duplicate", "not-a-defect", "unanalyzable"} {
			Expect(instrs[0].Content).To(ContainSubstring("`" + v + "`"))
		}
	})

	It("execution prompt classifies no-first-party-frame alerts as terminal unanalyzable", func() {
		content := prompts.BuildExecutionInstructions()[0].Content
		Expect(content).To(ContainSubstring("unanalyzable"))
		Expect(content).To(ContainSubstring("no exception entry"))
		Expect(content).To(ContainSubstring("in_app=0"))
		Expect(content).To(ContainSubstring("status: done"))
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

	It(
		"execution prompt disallows unanalyzable when planning resolved a repo from the frame path",
		func() {
			content := prompts.BuildExecutionInstructions()[0].Content
			Expect(content).To(ContainSubstring("resolved from frame path"))
			Expect(content).To(ContainSubstring("first-party-eligible"))
		},
	)

	It("execution prompt treats in_app=unknown as not-third-party evidence", func() {
		content := prompts.BuildExecutionInstructions()[0].Content
		Expect(content).To(ContainSubstring("in_app=unknown"))
		Expect(content).To(ContainSubstring("NOT evidence of third-party"))
	})
})

var _ = Describe("BuildCollectorPlanningInstructions", func() {
	It("returns exactly 2 instructions", func() {
		instrs := prompts.BuildCollectorPlanningInstructions()
		Expect(instrs).To(HaveLen(2))
	})

	It("first instruction is collector-planning", func() {
		instrs := prompts.BuildCollectorPlanningInstructions()
		Expect(instrs[0].Name).To(Equal("collector-planning"))
		Expect(instrs[0].Content).NotTo(BeEmpty())
	})

	It("second instruction is output-format", func() {
		instrs := prompts.BuildCollectorPlanningInstructions()
		Expect(instrs[1].Name).To(Equal("output-format"))
		Expect(instrs[1].Content).NotTo(BeEmpty())
	})

	It("collector prompt contains the sentry-create-tasks.sh invocation", func() {
		instrs := prompts.BuildCollectorPlanningInstructions()
		Expect(instrs[0].Content).To(ContainSubstring("scripts/sentry-create-tasks.sh"))
	})

	It("collector prompt writes the ## Analysis summary section", func() {
		instrs := prompts.BuildCollectorPlanningInstructions()
		Expect(instrs[0].Content).To(ContainSubstring("## Analysis"))
	})

	It("collector prompt filters is:unresolved (no resolved/regressed tasks)", func() {
		instrs := prompts.BuildCollectorPlanningInstructions()
		Expect(instrs[0].Content).To(ContainSubstring("is:unresolved"))
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

	// IMPORTANT: the triage and deep-planning assertions below are duplicated on
	// purpose. Spec 001 AC 11 greps this file for `bborbe/trading` and
	// `bborbe/kafka` on >=2 lines (one per builder); a DescribeTable or shared
	// helper collapses them to one and silently drops the per-builder guarantee.
	It(
		"deep planning prompt resolves the repo frame-path-first with an ordered candidate list",
		func() {
			content := prompts.BuildDeepPlanningInstructions()[0].Content
			Expect(content).To(ContainSubstring("nuke-dev"))
			Expect(content).To(ContainSubstring("nuke-prod"))
			Expect(content).To(ContainSubstring("bborbe/trading"))
			Expect(content).To(ContainSubstring("mt5/connector"))
			Expect(content).To(ContainSubstring("bborbe/kafka"))
			Expect(content).To(ContainSubstring("docs/repo-mapping.md"))
			Expect(content).To(ContainSubstring("frame path"))
			Expect(strings.Index(content, "frame path")).
				To(BeNumerically("<", strings.Index(content, "candidate")))
		},
	)

	It("deep planning prompt orders the infrastructure repo after the application repos", func() {
		content := prompts.BuildDeepPlanningInstructions()[0].Content
		Expect(strings.Index(content, "bborbe/nuke")).
			To(BeNumerically(">", strings.Index(content, "bborbe/trading")))
	})

	It("deep planning prompt excludes third-party frames from repo resolution", func() {
		content := prompts.BuildDeepPlanningInstructions()[0].Content
		Expect(content).To(ContainSubstring("third-party"))
		Expect(content).To(ContainSubstring("rpyc"))
		Expect(content).To(ContainSubstring("netref.py"))
	})

	It(
		"deep planning prompt records the no-first-party-frame condition instead of escalating",
		func() {
			content := prompts.BuildDeepPlanningInstructions()[0].Content
			Expect(
				content,
			).NotTo(ContainSubstring("escalate saying the trace is entirely third-party"))
			Expect(content).To(ContainSubstring("no exception entry"))
			Expect(content).To(ContainSubstring("## Analysis"))
			Expect(content).NotTo(ContainSubstring("unanalyzable"))
			Expect(content).To(ContainSubstring("Do not assign a verdict"))
		},
	)

	It(
		"deep planning prompt requires candidates tried and the resolution mechanism in the output",
		func() {
			content := prompts.BuildDeepPlanningInstructions()[0].Content
			Expect(content).To(ContainSubstring("candidates tried"))
			Expect(content).To(ContainSubstring("resolved from frame path"))
			Expect(content).To(ContainSubstring("candidate position"))
		},
	)

	It("deep planning prompt no longer maps a Sentry project to a single canonical repo", func() {
		content := prompts.BuildDeepPlanningInstructions()[0].Content
		Expect(content).NotTo(ContainSubstring("map to source repo"))
		Expect(content).NotTo(ContainSubstring("project-named variant"))
		Expect(content).NotTo(ContainSubstring("bborbe/trading-bot"))
		Expect(content).NotTo(ContainSubstring("bborbe/nuke-dev"))
	})

	It("deep planning prompt writes the ## Context section", func() {
		instrs := prompts.BuildDeepPlanningInstructions()
		Expect(instrs[0].Content).To(ContainSubstring("## Context"))
	})

	It(
		"deep planning prompt references the root_cause_* pointer block for file:line identification",
		func() {
			instrs := prompts.BuildDeepPlanningInstructions()
			Expect(instrs[0].Content).To(ContainSubstring("root_cause_"))
			Expect(instrs[0].Content).To(ContainSubstring("deepest first-party"))
		},
	)

	It("deep planning prompt scopes investigation to first-party frames via in_app", func() {
		instrs := prompts.BuildDeepPlanningInstructions()
		Expect(instrs[0].Content).To(ContainSubstring("in_app"))
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
