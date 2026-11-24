// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package steps

import "testing"

// These are plain Go tests, not Ginkgo specs, on purpose: the subject is the
// unexported parseWeeklyResult, so the test must live in package steps, and the
// package already has a Ginkgo suite in steps_test. A second RunSpecs in the
// same directory fails with "calling RunSpecs more than once", and a spec in a
// package with no TestXxx compiles without ever running — which reads as a pass.
//
// They are split into one function per case rather than one function with
// subtests, because a single table-driven body tripped gocognit at 25.

// weeklyLine builds a well-formed result line for the fixtures.
func weeklyLine(reviewed, expected, emitted int, reused, observed string) string {
	return "sentry-weekly-result: reviewed_set_count=" + itoa(reviewed) +
		" expected_count=" + itoa(expected) +
		" emitted=" + itoa(emitted) +
		" route_reused=" + reused +
		" observed=" + observed +
		" status=ok"
}

// mustParse fails the test when no observation is parsed.
func mustParse(t *testing.T, content string) weeklyResult {
	t.Helper()
	got, ok := parseWeeklyResult(content)
	if !ok {
		t.Fatalf("expected an observation from %q, got none", content)
	}
	return got
}

func TestParseWeeklyResultComplete(t *testing.T) {
	got := mustParse(t, weeklyLine(7, 7, 1, "false", "true"))

	if got.reviewed != 7 || got.expected != 7 || got.emitted != 1 {
		t.Fatalf("counts = %d/%d/%d, want 7/7/1", got.reviewed, got.expected, got.emitted)
	}
	if got.routeReused {
		t.Fatal("route_reused = true, want false")
	}
	if !got.observed {
		t.Fatal("observed = false, want true")
	}
}

func TestParseWeeklyResultRouteReuse(t *testing.T) {
	got := mustParse(t, weeklyLine(3, 3, 1, "true", "true"))

	if !got.routeReused {
		t.Fatal("route_reused = false, want true")
	}
}

func TestParseWeeklyResultUnobserved(t *testing.T) {
	// An unverifiable run must never read as an observed zero.
	got := mustParse(t, weeklyLine(0, 0, 0, "false", "false"))

	if got.observed {
		t.Fatal("observed = true, want false — an unverifiable run must not read as success")
	}
}

func TestParseWeeklyResultEmbedded(t *testing.T) {
	content := "## Weekly Roll-Up\n\nsome model prose\n\n" +
		weeklyLine(5, 5, 1, "false", "true") + "\n"

	got := mustParse(t, content)

	if got.reviewed != 5 {
		t.Fatalf("reviewed = %d, want 5", got.reviewed)
	}
}

func TestParseWeeklyResultAbsent(t *testing.T) {
	// Absence is failure, not a quiet run: a run that produced no line did not
	// do its work.
	if _, ok := parseWeeklyResult("## Weekly Roll-Up\n\nnothing else\n"); ok {
		t.Fatal("expected no observation for content without a result line")
	}
}

func TestParseWeeklyResultMalformed(t *testing.T) {
	malformed := "sentry-weekly-result: reviewed_set_count=many expected_count=5 " +
		"emitted=1 route_reused=false observed=true status=ok"

	if _, ok := parseWeeklyResult(malformed); ok {
		t.Fatal("expected no observation for a non-numeric count")
	}
}

// itoa keeps the fixtures readable without pulling strconv into the test.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := ""
	for n > 0 {
		digits = string(rune('0'+n%10)) + digits
		n /= 10
	}
	return digits
}
