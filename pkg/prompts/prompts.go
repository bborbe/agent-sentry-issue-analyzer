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

//go:embed output-format.md
var outputFormat string

// BuildPlanningInstructions assembles the planning-phase prompt: the domain
// planning module plus the shared output-format contract.
func BuildPlanningInstructions() claudelib.Instructions {
	return claudelib.Instructions{
		{Name: "planning", Content: planning},
		{Name: "output-format", Content: outputFormat},
	}
}

// BuildExecutionInstructions assembles the execution-phase prompt: the
// domain execution module plus the shared output-format contract.
func BuildExecutionInstructions() claudelib.Instructions {
	return claudelib.Instructions{
		{Name: "execution", Content: execution},
		{Name: "output-format", Content: outputFormat},
	}
}
