// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package verdict

import (
	"context"
	"strings"
)

// proseFields are the verdict keys whose value is unbounded model prose. The
// other nine keys hold enumerated, numeric or timestamp values the model copies
// from the live state, so they cannot carry arbitrary text — which is why only
// these three need repairing and why a fix scoped to them still generalises.
var proseFields = []string{"reason", "root_cause", "recommended_fix"}

// verdictFields is every top-level key the schema defines, in the order the
// execution prompt emits them. A prose value ends at the next line that starts
// one of these. The full set is required, not just proseFields: a wrapped prose
// line that happens to begin `tls: failed …` must be folded into the value,
// while a real field boundary must end it. A key dropped from this list fails
// TestVerdict's boundary case; a new field on the Verdict struct has to be added
// here by hand, and forgetting one would let a wrapped prose line beginning with
// the new key be swallowed into the value above it.
var verdictFields = []string{
	"sentry_issue_id",
	"verdict",
	"confidence",
	"reason",
	"live_event_count",
	"first_seen",
	"last_seen",
	"sentry_status",
	"disqualifiers_fired",
	"understanding",
	"fix_certainty",
	"root_cause",
	"recommended_fix",
}

// quoteProseValues rewrites each prose field's value as a double-quoted scalar,
// folding the continuation lines that follow it. It is the tolerant half of
// Parse: a verdict block whose prose carries an unquoted `: ` is illegal YAML —
// the parser reads the text after the colon as a nested mapping key and rejects
// the entire block, discarding an otherwise valid verdict (11 prod Jobs on
// 2026-09-13 died this way on `tls: failed to send closeNotify`).
//
// Quoting the whole value rather than escaping the one offending colon is what
// makes the repair general: the value becomes a quoted scalar, so any prose at
// all parses — further colons, quotes, brackets, leading dashes. It is applied
// only after the block has already failed to parse, so a well-formed block is
// never rewritten.
func quoteProseValues(ctx context.Context, block string) (string, error) {
	lines := strings.Split(block, "\n")
	out := make([]string, 0, len(lines))
	for i := 0; i < len(lines); i++ {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}
		key, value, ok := splitTopLevelKey(lines[i])
		if !ok || !contains(proseFields, key) {
			out = append(out, lines[i])
			continue
		}
		parts := []string{value}
		for i+1 < len(lines) {
			if next, _, isKey := splitTopLevelKey(lines[i+1]); isKey &&
				contains(verdictFields, next) {
				break
			}
			if strings.TrimSpace(lines[i+1]) == "" {
				break
			}
			parts = append(parts, strings.TrimSpace(lines[i+1]))
			i++
		}
		out = append(out, key+": "+quoteScalar(strings.Join(parts, " ")))
	}
	return strings.Join(out, "\n"), nil
}

// splitTopLevelKey splits a `key: value` line that starts a top-level mapping
// entry. Indented lines never match, so a continuation line and a list item
// under some other key are both left alone. The key charset is [A-Za-z0-9_]:
// anything else (a JSON block's `{"sentry_issue_id":`) is not a schema key and
// must not be treated as one.
func splitTopLevelKey(line string) (key, value string, ok bool) {
	if line == "" || line[0] == ' ' || line[0] == '\t' {
		return "", "", false
	}
	idx := strings.Index(line, ":")
	if idx <= 0 {
		return "", "", false
	}
	key = line[:idx]
	for _, r := range key {
		if !isKeyRune(r) {
			return "", "", false
		}
	}
	return key, strings.TrimSpace(line[idx+1:]), true
}

// isKeyRune reports whether r can appear in a verdict schema key.
func isKeyRune(r rune) bool {
	return r == '_' ||
		(r >= 'a' && r <= 'z') ||
		(r >= 'A' && r <= 'Z') ||
		(r >= '0' && r <= '9')
}

// quoteScalar renders value as a double-quoted YAML scalar. An already quoted
// value is passed through untouched so the repair cannot double-encode it; the
// remaining two escapes are the only characters that can terminate or corrupt a
// double-quoted scalar.
func quoteScalar(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return `""`
	}
	if len(value) >= 2 && strings.HasPrefix(value, `"`) && strings.HasSuffix(value, `"`) {
		return value
	}
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `"`, `\"`)
	return `"` + value + `"`
}

// contains reports whether values holds want.
func contains(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}
