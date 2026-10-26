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

// Urgency bands thresholds. `now` deliberately restates the disqualifier
// thresholds (volumeThreshold, burstEventThreshold, burstWindow) so the band
// and the noise-to-real-bug flip cannot disagree — an alert the code promoted
// is never labelled with a quieter band than the promotion implies.
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

	now := e.currentDateTime.Now().Time()

	// `now` — the disqualifier arithmetic verbatim: volume, active burst, or a
	// regression. Restated rather than derived from the fired list so the band
	// is meaningful for verdicts the flip exempts (`unanalyzable`), where a
	// disqualifier fires but the verdict is deliberately left alone.
	if input.LiveEventCount.Known {
		if input.LiveEventCount.Count > volumeThreshold {
			return UrgencyNow, nil
		}
		if input.LiveEventCount.Count > burstEventThreshold &&
			now.Sub(input.LastSeen) <= burstWindow {
			return UrgencyNow, nil
		}
	}
	if input.SentryStatus == "regressed" {
		return UrgencyNow, nil
	}

	// `soon` — recently active at meaningful volume, or a large backlog
	// however stale. This is the branch that splits two equal-count alerts by
	// recency alone: the same 500 events seen two hours ago and sixty days ago
	// are `soon` and `someday` respectively.
	if input.LiveEventCount.Known {
		if now.Sub(input.LastSeen) <= soonWindow &&
			input.LiveEventCount.Count >= soonMinCount {
			return UrgencySoon, nil
		}
		if input.LiveEventCount.Count >= soonCountFloor {
			return UrgencySoon, nil
		}
	}

	return UrgencySomeday, nil
}
