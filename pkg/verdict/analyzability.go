// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package verdict

import "strings"

// UnanalyzableVerdict is the verdict label meaning "this alert could not be
// analysed at all". It is terminal: the issue leaves the fix queue.
const UnanalyzableVerdict = "unanalyzable"

// RealBugVerdict is the verdict label the rubric assigns to an issue whose code
// path is identifiable and whose fix can be named.
const RealBugVerdict = "real bug"

// analysisPlaceholders are the values a model writes into root_cause /
// recommended_fix when it has nothing to say. They are matched against the
// whole value after trimming surrounding whitespace and punctuation, and — for
// the explicitly "not known" family below — as a leading phrase, so
// "unknown (no first-party frame)" still counts as no analysis.
var analysisPlaceholders = map[string]bool{
	"":               true,
	"-":              true,
	"—":              true,
	"n/a":            true,
	"na":             true,
	"none":           true,
	"unknown":        true,
	"unavailable":    true,
	"not applicable": true,
	"not available":  true,
	"not determined": true,
	"not-determined": true,
}

// analysisPlaceholderPrefixes are the leading phrases that mark a value as "no
// analysis" even when the model appends an explanation. Only the explicit
// not-known vocabulary is here: a prefix like "no" would swallow a real fix
// such as "no retry guard on the consumer loop".
var analysisPlaceholderPrefixes = []string{
	"unknown",
	"unavailable",
	"n/a",
	"not determined",
	"not-determined",
	"not available",
	"not applicable",
}

// HasAnalysis reports whether the verdict block names an actual root cause or
// recommended fix. A placeholder token ("unknown", "n/a", "none", …) is the
// absence of an analysis, not one.
func HasAnalysis(v Verdict) bool {
	return namesAnalysis(v.RootCause) || namesAnalysis(v.RecommendedFix)
}

// ContradictsAnalysis reports whether the verdict claims `unanalyzable` while
// the same block names a root cause or a recommended fix. The two answers
// cannot both be true: a block that names a cause and a fix has been analysed.
//
// This is a per-verdict correctness rule, not a rate rule. An `unanalyzable`
// label retires the issue from the fix queue, so a self-describing error
// mislabelled this way never gets fixed while it keeps burning events.
func ContradictsAnalysis(v Verdict) bool {
	return v.Verdict == UnanalyzableVerdict && HasAnalysis(v)
}

// ApplyAnalyzabilityGuard reclassifies a verdict that contradicts its own
// analysis. `unanalyzable` is reserved for alerts that genuinely cannot be
// analysed; a block carrying a root cause and a recommended fix carries
// exactly the evidence the rubric's `real bug` row asks for, so it is rewritten
// to `real bug` with the block's own fix certainty as its confidence.
//
// Returns true when the verdict was rewritten. The caller is responsible for
// re-rendering the ## Verdict section.
func ApplyAnalyzabilityGuard(v *Verdict) bool {
	if v == nil || !ContradictsAnalysis(*v) {
		return false
	}
	v.Verdict = RealBugVerdict
	confidence := strings.ToLower(strings.TrimSpace(v.FixCertainty))
	if !validConfidence[confidence] {
		// A block that contradicted itself has not earned a confidence claim of
		// its own; low is the only honest default.
		confidence = "low"
	}
	v.Confidence = confidence
	const note = "analyzability guard: `unanalyzable` contradicted the block's own " +
		"root_cause/recommended_fix, so it was reclassified as `real bug`"
	if v.Reason != "" {
		v.Reason = note + " — " + v.Reason
	} else {
		v.Reason = note
	}
	return true
}

// namesAnalysis reports whether value carries analysis rather than a
// placeholder.
func namesAnalysis(value string) bool {
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = strings.TrimRight(normalized, ".!;,: ")
	if normalized == "" {
		return false
	}
	if analysisPlaceholders[normalized] {
		return false
	}
	for _, prefix := range analysisPlaceholderPrefixes {
		if strings.HasPrefix(normalized, prefix) &&
			hasPhraseBoundary(normalized[len(prefix):]) {
			return false
		}
	}
	return true
}

// hasPhraseBoundary reports whether rest starts a new phrase rather than
// continuing the prefix word — so "unknown" and "unknown (no frames)" match the
// prefix, while "unknowns resolved" does not.
func hasPhraseBoundary(rest string) bool {
	if rest == "" {
		return true
	}
	switch rest[0] {
	case ' ', '(', ':', '-', ',':
		return true
	}
	// The em dash is multi-byte, so it cannot be a byte case above.
	return strings.HasPrefix(rest, "—")
}

// RestoreUnmodeledLines appends to rendered every top-level `key:` entry that
// original carries and rendered does not, preserving the entry's own value
// lines. It returns rendered unchanged when original carries nothing extra.
//
// A re-render marshals the triage schema, which has no `file:line` field — the
// deep verdict's one machine-actionable pointer, and what the High/High fix
// handoff resolves. Rendering a deep block therefore silently drops it. This
// restores every unmodeled entry instead, so the guard can rewrite a verdict on
// either write path without destroying a field it does not understand.
func RestoreUnmodeledLines(original, rendered string) string {
	extra := unmodeledEntries(original, rendered)
	if len(extra) == 0 {
		return rendered
	}
	lines := strings.Split(rendered, "\n")
	// Insert before the closing fence so the entries stay inside the block the
	// parser reads; append at the end when there is no fence.
	insertAt := len(lines)
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "```") {
			insertAt = i
			break
		}
	}
	out := make([]string, 0, len(lines)+len(extra))
	out = append(out, lines[:insertAt]...)
	out = append(out, extra...)
	out = append(out, lines[insertAt:]...)
	return strings.Join(out, "\n")
}

// unmodeledEntries returns the top-level entries of original whose key does not
// appear as a top-level entry in rendered.
func unmodeledEntries(original, rendered string) []string {
	present := map[string]bool{}
	for _, line := range strings.Split(rendered, "\n") {
		if key, ok := topLevelKey(line); ok {
			present[key] = true
		}
	}
	var extra []string
	var current []string
	flush := func() {
		if len(current) == 0 {
			return
		}
		if key, ok := topLevelKey(current[0]); ok && !present[key] {
			extra = append(extra, current...)
		}
		current = nil
	}
	for _, line := range strings.Split(original, "\n") {
		if _, ok := topLevelKey(line); ok {
			flush()
		}
		if len(current) > 0 || strings.TrimSpace(line) != "" {
			current = append(current, line)
		}
	}
	flush()
	return extra
}

// topLevelKey reports whether line is a top-level `key:` entry (column 0, a
// bare identifier key) and returns the key. `file:line` yields "file", which is
// consistent on both sides of the comparison — the entry is what gets carried.
func topLevelKey(line string) (string, bool) {
	if line == "" || line[0] == ' ' || line[0] == '\t' || line[0] == '#' ||
		strings.HasPrefix(line, "```") {
		return "", false
	}
	idx := strings.Index(line, ":")
	if idx <= 0 {
		return "", false
	}
	key := line[:idx]
	for _, r := range key {
		isAlnum := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
		if !isAlnum && r != '_' && r != '-' {
			return "", false
		}
	}
	return key, true
}
