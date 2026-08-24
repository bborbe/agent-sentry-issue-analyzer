// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package prompts provides embedded per-phase prompt fragments for the
// Sentry analyzer agent. Each active phase (planning, execution) gets its
// own domain module; output-format.md is the shared JSON output contract.
package prompts

import (
	_ "embed"

	claudelib "github.com/bborbe/agent/claude"
)

//go:embed planning.md
var planning string

//go:embed execution.md
var execution string

//go:embed deep-planning.md
var deepPlanning string

//go:embed deep-execution.md
var deepExecution string

//go:embed output-format.md
var outputFormat string

// BuildPlanningInstructions assembles the triage planning-phase prompt: the
// domain planning module plus the shared output-format contract. Used by the
// sentry-issue-analyzer task type (the daily-batch triage agent).
func BuildPlanningInstructions() claudelib.Instructions {
	return claudelib.Instructions{
		{Name: "planning", Content: planning},
		{Name: "output-format", Content: outputFormat},
	}
}

// BuildExecutionInstructions assembles the triage execution-phase prompt: the
// 6-verdict rubric + noise disqualifiers, plus the shared output-format
// contract. Used by the sentry-issue-analyzer task type.
func BuildExecutionInstructions() claudelib.Instructions {
	return claudelib.Instructions{
		{Name: "execution", Content: execution},
		{Name: "output-format", Content: outputFormat},
	}
}

// BuildDeepPlanningInstructions assembles the deep planning-phase prompt: the
// deep root-cause module (LIVE state + read-only clone) plus the shared
// output-format contract. Used by the sentry-deep-analyzer task type.
func BuildDeepPlanningInstructions() claudelib.Instructions {
	return claudelib.Instructions{
		{Name: "deep-planning", Content: deepPlanning},
		{Name: "output-format", Content: outputFormat},
	}
}

// BuildDeepExecutionInstructions assembles the deep execution-phase prompt:
// the octopus verdict schema (U/F + file:line + disqualifiers_fired) plus the
// shared output-format contract. Used by the sentry-deep-analyzer task type.
func BuildDeepExecutionInstructions() claudelib.Instructions {
	return claudelib.Instructions{
		{Name: "deep-execution", Content: deepExecution},
		{Name: "output-format", Content: outputFormat},
	}
}
