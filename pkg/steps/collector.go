// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package steps

import (
	"context"
	"regexp"
	"strconv"

	agentlib "github.com/bborbe/agent"
	claudelib "github.com/bborbe/agent/claude"
)

// collectorResultLine matches the machine-readable result line emitted by
// scripts/sentry-create-tasks.sh. The step gates the task's terminal status on
// the observed counts in this line, not on anything the model wrote.
var collectorResultLine = regexp.MustCompile(
	`sentry-create-tasks-result:\s+fetched=(\d+)\s+published=(\d+)\s+expected_new=(\d+)\s+landed=(\d+)\s+observed=(\w+)\s+status=(\w+)`,
)

// collectorResult is the parsed observation from the collector script.
type collectorResult struct {
	expectedNew int
	landed      int
	// observed is false when the script could not read the vault at all. That is
	// NOT the same as an observed zero: an unverifiable run must not report
	// success, or the gate re-opens the false-green it exists to close.
	observed bool
}

// collectorStep wraps the collector's Claude planning step so the task's
// terminal status is decided by what was OBSERVED to land, not by whether the
// ## Analysis section exists.
//
// Before this wrapper, NextPhase "done" was unconditional: the section's mere
// presence terminated the task, so a run whose creation phase produced zero
// per-alert task files still reported success. That is what made the
// 2026-08-26/27 outage invisible for ~20 hours.
type collectorStep struct {
	inner agentlib.Step
}

// NewCollectorPlanningStep wraps a Claude invocation as the sentry-collector's
// planning-phase step. Claude runs scripts/sentry-create-tasks.sh to fetch the
// day's active unresolved Sentry alerts and publish one per-alert task per
// (short-id, date), then writes the summary under the ## Analysis section.
//
// NextPhase is "done" so a successful fan-out terminates the task: without it,
// the step is an in-place save that leaves the task planning/in_progress, the
// executor re-dispatches, ShouldRun skips (success section present), and the
// agent's nil result becomes deadline_exceeded → trigger_count churn (spec 051
// follow-up, verified live 2026-08-30 job ...220835).
//
// The wrapper keeps that terminal behaviour for a successful run and converts
// it to a failure when the script reports that new alerts were expected but
// zero task files landed.
func NewCollectorPlanningStep(
	runner claudelib.ClaudeRunner,
	instructions claudelib.Instructions,
	envContext map[string]string,
) agentlib.Step {
	return &collectorStep{
		inner: claudelib.NewAgentStep(claudelib.AgentStepConfig{
			Name:          "sentry-collector-planning",
			Runner:        runner,
			Instructions:  instructions,
			EnvContext:    envContext,
			OutputSection: "## Analysis",
			NextPhase:     "done",
		}),
	}
}

// Name delegates to the wrapped step so existing logs and tool wiring keep
// referring to the same step.
func (s *collectorStep) Name() string {
	return s.inner.Name()
}

// ShouldRun delegates to the wrapped step — the idempotent-resume guard (skip
// when ## Analysis already exists) must stay exactly as it was.
func (s *collectorStep) ShouldRun(
	ctx context.Context,
	md *agentlib.Markdown,
) (bool, error) {
	return s.inner.ShouldRun(ctx, md)
}

// Run delegates, then downgrades a Done result to Failed when the script's
// observed counts say the creation phase produced nothing while new alerts
// were expected.
func (s *collectorStep) Run(
	ctx context.Context,
	md *agentlib.Markdown,
) (*agentlib.Result, error) {
	result, err := s.inner.Run(ctx, md)
	if err != nil {
		return result, err
	}
	// Only Done is a success claim. Failed / NeedsInput already carry the
	// controller-owned failure envelope — leave them untouched.
	if result.Status != agentlib.AgentStatusDone {
		return result, nil
	}

	section, ok := md.FindSection("## Analysis")
	if !ok {
		return result, nil
	}
	observation, ok := parseCollectorResult(section.Body)
	if !ok {
		// No result line at all. The collector's entire job is to observe the
		// creation phase, so a run that produced no observation did not do its
		// work. A stalled run — e.g. the script never executed because its Bash
		// grant did not match the `bash scripts/...` form the model used — must
		// NOT report success. Absence of evidence is evidence of failure here,
		// not neutrality: reporting done on a missing line is exactly the
		// false-green this step exists to close. Observed on dev 2026-09-11: a
		// run that did nothing was recorded `status: completed`.
		return &agentlib.Result{
			Status: agentlib.AgentStatusFailed,
			Message: "collector produced no creation-count observation — the step did not " +
				"run to completion, so it cannot report success",
		}, nil
	}

	// Failure contract: Failed with NO NextPhase — the controller owns
	// unassign + ## Failure. Never signal failure through NextPhase
	// ("human_review" is reserved for a successful verdict needing a human).
	if !observation.observed {
		return &agentlib.Result{
			Status: agentlib.AgentStatusFailed,
			Message: "collector could not observe the creation phase (the vault listing " +
				"failed) — an unverifiable run must not report success",
		}, nil
	}
	if observation.expectedNew > 0 && observation.landed == 0 {
		return &agentlib.Result{
			Status: agentlib.AgentStatusFailed,
			Message: "collector reported no per-alert task files landed while new alerts " +
				"were expected — refusing to report success",
		}, nil
	}
	return result, nil
}

// parseCollectorResult extracts the script's observation from the ## Analysis
// body. Returns false when the line is absent or malformed.
//
// The rule is applied here rather than read from the line's status= token: the
// script observes the counts, the step owns the policy, so a bug in the
// script's own status wording cannot silently re-open the false-green path.
func parseCollectorResult(body string) (collectorResult, bool) {
	m := collectorResultLine.FindStringSubmatch(body)
	if m == nil {
		return collectorResult{}, false
	}
	expectedNew, err := strconv.Atoi(m[3])
	if err != nil {
		return collectorResult{}, false
	}
	landed, err := strconv.Atoi(m[4])
	if err != nil {
		return collectorResult{}, false
	}
	return collectorResult{
		expectedNew: expectedNew,
		landed:      landed,
		observed:    m[5] == "true",
	}, true
}
