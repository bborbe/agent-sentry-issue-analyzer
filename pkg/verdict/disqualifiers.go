// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package verdict

import (
	"context"
	"time"

	libtime "github.com/bborbe/time"
)

// Disqualifier names a noise-to-real-bug disqualifier from the Sentry Triage
// Guide (mirrored verbatim from pkg/prompts/execution.md). Verified-absent
// resource is deliberately absent here: it needs a judgment call (is the
// sibling resource really absent?) and stays a model decision.
type Disqualifier string

const (
	// DisqualifierVolume fires when live_event_count > 10000.
	DisqualifierVolume Disqualifier = "Volume"
	// DisqualifierActiveBurst fires when last_seen is within 24h AND
	// live_event_count > 1000.
	DisqualifierActiveBurst Disqualifier = "Active burst"
	// DisqualifierRegressed fires when status == "regressed".
	DisqualifierRegressed Disqualifier = "Regressed"
	// DisqualifierSustainedSpan fires when the first-seen-to-last-seen span
	// exceeds 30 days AND (rate >= ~1 event/day OR live count >= 100).
	DisqualifierSustainedSpan Disqualifier = "Sustained span"
)

// Rubric thresholds, ported verbatim from pkg/prompts/execution.md / the
// Sentry Triage Guide. Changing a threshold is out of scope for this task —
// only the computation moves into code.
const (
	volumeThreshold     = 10000
	burstEventThreshold = 1000
	burstWindow         = 24 * time.Hour
	spanDaysThreshold   = 30
	minDailyRate        = 1.0
	minSpanCount        = 100
)

// DisqualifierInput is the live-state evidence the evaluator computes from.
// Timestamps are pre-parsed by the caller from the verdict YAML's first_seen /
// last_seen fields.
type DisqualifierInput struct {
	LiveEventCount int
	FirstSeen      time.Time
	LastSeen       time.Time
	SentryStatus   string
}

// DisqualifierEvaluator computes which disqualifiers fire against live state.
// The model supplies the live-state fields and the signature classification;
// the numeric thresholds are decided here, in code.
type DisqualifierEvaluator interface {
	Evaluate(ctx context.Context, input DisqualifierInput) ([]Disqualifier, error)
}

// NewDisqualifierEvaluator creates the evaluator with an injectable clock so
// the active-burst 24h window is deterministic in tests.
func NewDisqualifierEvaluator(currentDateTime libtime.CurrentDateTimeGetter) DisqualifierEvaluator {
	return &disqualifierEvaluator{currentDateTime: currentDateTime}
}

type disqualifierEvaluator struct {
	currentDateTime libtime.CurrentDateTimeGetter
}

func (e *disqualifierEvaluator) Evaluate(
	ctx context.Context,
	input DisqualifierInput,
) ([]Disqualifier, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	var fired []Disqualifier
	if input.LiveEventCount > volumeThreshold {
		fired = append(fired, DisqualifierVolume)
	}
	if input.LiveEventCount > burstEventThreshold &&
		e.currentDateTime.Now().Time().Sub(input.LastSeen) <= burstWindow {
		fired = append(fired, DisqualifierActiveBurst)
	}
	if input.SentryStatus == "regressed" {
		fired = append(fired, DisqualifierRegressed)
	}
	if sustainedSpanFires(input) {
		fired = append(fired, DisqualifierSustainedSpan)
	}
	return fired, nil
}

// sustainedSpanFires implements the sustained-span rule: first-seen to
// last-seen span over 30 days AND (rate >= ~1 event/day OR live count >= 100).
// The span disqualifier is about RATE, not calendar age — a long-lived
// low-rate transient (rate < ~1/day AND count < 100) stays `noise` regardless
// of how old it is.
func sustainedSpanFires(input DisqualifierInput) bool {
	if input.LastSeen.Before(input.FirstSeen) {
		return false
	}
	span := input.LastSeen.Sub(input.FirstSeen)
	if span <= spanDaysThreshold*24*time.Hour {
		return false
	}
	spanDays := span.Hours() / 24
	if spanDays <= 0 {
		return false
	}
	rate := float64(input.LiveEventCount) / spanDays
	return rate >= minDailyRate || input.LiveEventCount >= minSpanCount
}
