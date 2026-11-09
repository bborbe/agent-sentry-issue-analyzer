// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package verdictrecord retains a task's "## Verdict" and "## Fix Result"
// sections across a re-dispatch.
//
// The problem it solves: cmd/create-tasks renders a fresh task body for every
// active Sentry alert. agent-task-controller posts that create with
// ReopenTerminal, and vault-api's reopenTerminalTask REPLACES the task file's
// frontmatter and body with the request's own (it does not merge — that is
// deliberate, see task_content.go). So a re-dispatch of an alert that a
// previous run already judged silently discards the earlier verdict, and the
// evidence SC10/SC11 are measured on exists only in git history.
//
// The repair lives on the producer side: before the fresh body is emitted, the
// prior sections are read back, persisted to a durable per-issue record, and
// spliced into the new body. vault-api's replace-on-reopen stays untouched, so
// no other consumer of the reopen mode changes behaviour.
package verdictrecord

import "strings"

const (
	// VerdictHeading is the section the analyzer writes its judgement into.
	VerdictHeading = "## Verdict"
	// FixResultHeading is the section the fix lane writes its outcome into.
	FixResultHeading = "## Fix Result"

	levelTwoPrefix = "## "
)

// Section returns the markdown section that starts at heading, verbatim and
// including the heading line, up to (but not including) the next level-two
// heading. It returns "" when the body carries no such section.
//
// The match is exact on the trimmed heading line: "## Verdict" does not match
// "### Verdict" (a subsection), and "## Verdicts" does not match "## Verdict".
// Level-three headings inside the section are kept — the analyzer's verdict
// block carries fenced YAML, not subsections, but a fix result may.
func Section(body, heading string) string {
	if body == "" || heading == "" {
		return ""
	}
	lines := strings.Split(body, "\n")
	start := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == heading {
			start = i
			break
		}
	}
	if start < 0 {
		return ""
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if strings.HasPrefix(trimmed, levelTwoPrefix) && trimmed != heading {
			end = i
			break
		}
	}
	return strings.TrimRight(strings.Join(lines[start:end], "\n"), "\n")
}

// Retain returns body with the retained sections appended.
//
// It returns body byte-identical when there is nothing to retain, so a first
// dispatch — the common case — is untouched by this package. When sections are
// retained they are appended after the fresh body, which keeps the fresh
// alert context at the top and the earlier judgement below it, and keeps the
// splice byte-stable so a second re-dispatch reproduces the same file.
func Retain(body, verdict, fixResult string) string {
	retained := make([]string, 0, 2)
	for _, section := range []string{verdict, fixResult} {
		if strings.TrimSpace(section) != "" {
			retained = append(retained, strings.TrimRight(section, "\n"))
		}
	}
	if len(retained) == 0 {
		return body
	}
	base := strings.TrimRight(body, "\n")
	if base == "" {
		return strings.Join(retained, "\n\n") + "\n"
	}
	return base + "\n\n" + strings.Join(retained, "\n\n") + "\n"
}

// Extract returns the two sections this package retains, in body order.
func Extract(body string) (verdict, fixResult string) {
	return Section(body, VerdictHeading), Section(body, FixResultHeading)
}
