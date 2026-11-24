// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package steps

import (
	"context"
	"os"
	"regexp"
	"strconv"

	agentlib "github.com/bborbe/agent"
	claudelib "github.com/bborbe/agent/claude"
)

// weeklyResultPath is where the weekly roll-up's script writes its
// machine-readable result line. It is deliberately NOT collectorResultPath:
// the collector clears its file at the start of every run, and sharing the path
// would let one step wipe the other's observation.
const weeklyResultPath = "/tmp/sentry-weekly-result"

// weeklyResultLine matches the result line the weekly script emits.
//
// reviewed_set_count is what the run actually processed; expected_count is the
// total the executor recorded by hand BEFORE the run (the SC2 probe). They are
// separate numbers on purpose: when they differ the review set was truncated,
// and no count the step writes about itself can hide that.
var weeklyResultLine = regexp.MustCompile(
	`sentry-weekly-result:\s+reviewed_set_count=(\d+)\s+expected_count=(\d+)\s+` +
		`emitted=(\d+)\s+route_reused=(\w+)\s+observed=(\w+)\s+status=(\w+)`,
)

// weeklyResult is the parsed observation from the weekly script.
type weeklyResult struct {
	// reviewed is how many issues the run actually reviewed.
	reviewed int
	// expected is the total the executor recorded before the run. Zero means
	// the probe was not taken, which is itself a failure: an unmeasured run
	// cannot claim it covered the week.
	expected int
	// emitted is how many Obsidian tasks the run wrote. More than one is a
	// duplicate; zero against a non-empty review set is a silent no-op.
	emitted int
	// routeReused is true when a second run in the same week reused the first
	// run's routes instead of re-deriving them.
	routeReused bool
	// observed is false when the script could not read the vault at all. That
	// is NOT an observed zero, and must never report success.
	observed bool
}

// weeklyStep wraps the weekly roll-up's Claude invocation so the task's
// terminal status is decided by what was OBSERVED to happen, not by whether the
// output section exists.
//
// This mirrors collectorStep for the same reason: an unconditional
// NextPhase "done" let a run that produced nothing report success and hid a
// ~20-hour outage. The weekly roll-up has the same failure shape — a run that
// reviews nothing, or reviews a truncated page, still writes its section.
type weeklyStep struct {
	inner agentlib.Step
}

// NewWeeklyRollupStep wraps a Claude invocation as the weekly roll-up's step.
//
// NextPhase is "done" so a successful run terminates the task; without it the
// executor re-dispatches, ShouldRun skips, and the nil result becomes
// deadline_exceeded churn.
func NewWeeklyRollupStep(
	runner claudelib.ClaudeRunner,
	instructions claudelib.Instructions,
	envContext map[string]string,
) agentlib.Step {
	return &weeklyStep{
		inner: claudelib.NewAgentStep(claudelib.AgentStepConfig{
			Name:          "sentry-weekly-rollup",
			Runner:        runner,
			Instructions:  instructions,
			EnvContext:    envContext,
			OutputSection: "## Weekly Roll-Up",
			NextPhase:     "done",
		}),
	}
}

// Name delegates to the wrapped step so logs and tool wiring keep referring to
// the same step.
func (s *weeklyStep) Name() string {
	return s.inner.Name()
}

// ShouldRun delegates to the wrapped step: the idempotent-resume guard (skip
// once the output section exists) must stay exactly as it is, so a resumed run
// cannot rewrite a week's roll-up.
func (s *weeklyStep) ShouldRun(
	ctx context.Context,
	md *agentlib.Markdown,
) (bool, error) {
	return s.inner.ShouldRun(ctx, md)
}

// Run delegates, then decides the terminal status from the script's observation.
func (s *weeklyStep) Run(
	ctx context.Context,
	md *agentlib.Markdown,
) (*agentlib.Result, error) {
	// Clear any result left by an earlier run in this container, so a stale
	// file can never be mistaken for this run's observation.
	if err := os.Remove(weeklyResultPath); err != nil && !os.IsNotExist(err) {
		return &agentlib.Result{
			Status: agentlib.AgentStatusFailed,
			Message: "weekly roll-up could not clear the previous run's result file — " +
				"refusing to report success on a possibly stale observation",
		}, nil
	}

	result, err := s.inner.Run(ctx, md)
	if err != nil {
		return result, err
	}
	// Only Done is a success claim. Failed / NeedsInput already carry the
	// controller-owned failure envelope.
	if result.Status != agentlib.AgentStatusDone {
		return result, nil
	}

	body, readErr := os.ReadFile(weeklyResultPath)
	if readErr != nil {
		return &agentlib.Result{
			Status: agentlib.AgentStatusFailed,
			Message: "weekly roll-up produced no observation — the script did not run to " +
				"completion, so it cannot report success",
		}, nil
	}
	observation, ok := parseWeeklyResult(string(body))
	if !ok {
		// No result line at all. The run's entire job is to observe the review
		// set, so a run with no observation did not do its work. Absence of
		// evidence is evidence of failure here, not neutrality.
		return &agentlib.Result{
			Status: agentlib.AgentStatusFailed,
			Message: "weekly roll-up produced no observation — the step did not run to " +
				"completion, so it cannot report success",
		}, nil
	}
	// Failure contract: Failed with NO NextPhase — the controller owns
	// unassign and ## Failure.
	if !observation.observed {
		return &agentlib.Result{
			Status: agentlib.AgentStatusFailed,
			Message: "weekly roll-up could not observe the vault — an unverifiable run must " +
				"not report success",
		}, nil
	}
	// A run that did not record its expected total has not measured its own
	// coverage, so it cannot claim the week was reviewed.
	if observation.expected == 0 {
		return &agentlib.Result{
			Status: agentlib.AgentStatusFailed,
			Message: "weekly roll-up recorded no expected review-set total — the pre-run " +
				"probe was not taken, so coverage is unmeasured",
		}, nil
	}
	// Truncation: fewer reviewed than expected is the page-1-only failure this
	// criterion exists to catch.
	if observation.reviewed < observation.expected {
		return &agentlib.Result{
			Status: agentlib.AgentStatusFailed,
			Message: "weekly roll-up reviewed " + strconv.Itoa(observation.reviewed) +
				" of " + strconv.Itoa(observation.expected) +
				" unresolved issues — the review set was truncated",
		}, nil
	}
	// A non-empty review set that emitted no task is a silent no-op: the week
	// was reviewed and nothing was written.
	if observation.reviewed > 0 && observation.emitted == 0 {
		return &agentlib.Result{
			Status: agentlib.AgentStatusFailed,
			Message: "weekly roll-up reviewed " + strconv.Itoa(observation.reviewed) +
				" issues but emitted no task — the week's work was not recorded",
		}, nil
	}
	// More than one task per run means the idempotency key did not hold.
	if observation.emitted > 1 {
		return &agentlib.Result{
			Status: agentlib.AgentStatusFailed,
			Message: "weekly roll-up emitted " + strconv.Itoa(observation.emitted) +
				" tasks for one week — the week key did not deduplicate",
		}, nil
	}

	return result, nil
}

// parseWeeklyResult parses the result line out of the script's output. It
// returns false when no line is present at all, which the caller treats as
// failure rather than as a quiet run.
func parseWeeklyResult(content string) (weeklyResult, bool) {
	match := weeklyResultLine.FindStringSubmatch(content)
	if match == nil {
		return weeklyResult{}, false
	}
	reviewed, err := strconv.Atoi(match[1])
	if err != nil {
		return weeklyResult{}, false
	}
	expected, err := strconv.Atoi(match[2])
	if err != nil {
		return weeklyResult{}, false
	}
	emitted, err := strconv.Atoi(match[3])
	if err != nil {
		return weeklyResult{}, false
	}
	return weeklyResult{
		reviewed:    reviewed,
		expected:    expected,
		emitted:     emitted,
		routeReused: match[4] == "true",
		observed:    match[5] == "true",
	}, true
}
