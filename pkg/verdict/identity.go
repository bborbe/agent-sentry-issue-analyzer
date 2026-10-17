// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package verdict

import "strings"

// yamlIdentityKey is the identity key as the execution prompt emits it in a
// fenced YAML block: a top-level mapping entry at column 0.
const yamlIdentityKey = "sentry_issue_id:"

// jsonIdentityKey is the identity key as it appears in a fenced JSON block,
// which Parse accepts too because JSON is a YAML subset.
const jsonIdentityKey = `"sentry_issue_id":`

// RewriteIdentityLine replaces the value of the first top-level
// `sentry_issue_id:` line in sectionBody with canonical, leaving every other
// byte of sectionBody unchanged. It returns sectionBody unchanged when it
// carries no identity line.
//
// Both the YAML and the JSON spelling are matched: a YAML-only matcher would
// silently no-op on a JSON block and let the identity flip survive. The
// identity VALUE is the only byte that changes — re-rendering the block would
// drop the deep verdict's `file:line` and break the High/High fix handoff.
func RewriteIdentityLine(sectionBody string, canonical string) string {
	lines := strings.Split(sectionBody, "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, yamlIdentityKey) {
			lines[i] = yamlIdentityKey + " " + canonical
			return strings.Join(lines, "\n")
		}
		if trimmed := strings.TrimSpace(line); strings.HasPrefix(trimmed, jsonIdentityKey) {
			lines[i] = rewriteJSONIdentityLine(line, trimmed, canonical)
			return strings.Join(lines, "\n")
		}
	}
	return sectionBody
}

// rewriteJSONIdentityLine rewrites the identity scalar in a JSON mapping line,
// preserving the line's own leading indentation and any trailing comma.
func rewriteJSONIdentityLine(line string, trimmed string, canonical string) string {
	indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
	value := strings.TrimSpace(strings.TrimPrefix(trimmed, jsonIdentityKey))
	comma := ""
	if strings.HasSuffix(value, ",") {
		comma = ","
	}
	return indent + jsonIdentityKey + ` "` + canonical + `"` + comma
}
