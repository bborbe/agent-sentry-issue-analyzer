// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package verdict

import (
	"context"
	"time"
)

// UrgencyBand is the operator-facing urgency of an analysed alert. It answers
// "act now, schedule it, or fix it if it recurs?" — a different question from
// Confidence, which measures how certain the classification is. `real bug` is
// one bucket covering both "act today" and "fix someday"; the band is what
// separates them without making the reader re-derive the arithmetic from the
// raw live_event_count and last_seen.
//
// The band is computed here, in code, from the same live state the
// disqualifiers read. It is never asked of the model, and it never gates
// escalation — it annotates the verdict block, and the close / escalate /
// annotate decision stays the operator's.
type UrgencyBand string

const (
	// UrgencyNow means act today: the alert is exerting enough live pressure
	// that deferring it costs more than handling it.
	UrgencyNow UrgencyBand = "now"
	// UrgencySoon means schedule it: real but not burning, so it belongs in
	// the next block of work rather than today's.
	UrgencySoon UrgencyBand = "soon"
	// UrgencySomeday means fix it if it recurs: low, stale, or both.
	UrgencySomeday UrgencyBand = "someday"
)

// Urgency bands thresholds. The `now` branch restates three of the
// disqualifiers verbatim (volumeThreshold, burstEventThreshold, burstWindow,
// plus the regressed status) so those cannot disagree with the band: an alert
// they promote is never labelled with a quieter band than the promotion
// implies.
//
// SustainedSpan is deliberately NOT restated. It promotes a verdict to
// `real bug` because a long-running alert is real, which is a claim about
// significance, not about timing — a 100-event alert last seen 60 days ago is
// genuinely `someday` even though the span disqualifier promotes it.
const (
	// soonWindow is how recent last_seen must be for a low-volume alert to
	// count as still-active rather than historical.
	soonWindow = 7 * 24 * time.Hour
	// soonMinCount is the volume at which a recently-seen alert earns `soon`.
	soonMinCount = 100
	// soonCountFloor is the volume at which a stale alert still earns `soon`:
	// a large backlog is worth scheduling however old its last event is.
	soonCountFloor = 1000
)

// UrgencyBands returns the closed band set in sorted order. It is the single
// source of truth for the vocabulary, mirroring Vocabulary: tests lock its
// size, and callers that need to enumerate the bands read it rather than
// hard-coding the three strings.
func UrgencyBands() []UrgencyBand {
	return []UrgencyBand{UrgencyNow, UrgencySoon, UrgencySomeday}
}

// EvaluateUrgency derives the band from the same live state Evaluate reads.
// It is a method on the same evaluator so both computations share one injected
// clock: the 24h burst window and the 7d recency window must agree, and two
// clocks would let them drift apart in tests.
//
// A count that could not be measured (LiveEventCount.Known == false) is
// skipped rather than compared against zero, exactly as Evaluate does — an
// unknown volume must not read as "no events".
func (e *disqualifierEvaluator) EvaluateUrgency(
	ctx context.Context,
	input DisqualifierInput,
) (UrgencyBand, error) {
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
	}
	return urgencyAt(input, e.currentDateTime.Now().Time()), nil
}

// urgencyAt derives the band against a supplied instant. EvaluateAll calls it
// with the same instant it hands the disqualifiers, so the band and the fired
// list cannot describe two different moments.
func urgencyAt(input DisqualifierInput, now time.Time) UrgencyBand {
	// A last_seen in the future — clock skew, or a bad Sentry timestamp — is
	// clamped to "just now" rather than left as a negative delta. Left alone it
	// still satisfies both recency windows, so the band would be the same, but
	// by accident rather than by intent; clamping says which reading is meant.
	recency := max(now.Sub(input.LastSeen), 0)

	// `now` — the disqualifier arithmetic verbatim: volume, active burst, or a
	// regression. Restated rather than derived from the fired list so the band
	// is meaningful for verdicts the flip exempts (`unanalyzable`), where a
	// disqualifier fires but the verdict is deliberately left alone.
	if input.LiveEventCount.Known {
		if input.LiveEventCount.Count > volumeThreshold {
			return UrgencyNow
		}
		if input.LiveEventCount.Count > burstEventThreshold &&
			recency <= burstWindow {
			return UrgencyNow
		}
	}
	if input.SentryStatus == "regressed" {
		return UrgencyNow
	}

	// `soon` — recently active at meaningful volume, or a large backlog
	// however stale. This is the branch that splits two equal-count alerts by
	// recency alone: the same 500 events seen two hours ago and sixty days ago
	// are `soon` and `someday` respectively.
	if input.LiveEventCount.Known {
		if recency <= soonWindow &&
			input.LiveEventCount.Count >= soonMinCount {
			return UrgencySoon
		}
		if input.LiveEventCount.Count >= soonCountFloor {
			return UrgencySoon
		}
	}

	return UrgencySomeday
}
