// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fixagent

import (
	"context"
	"fmt"
	"strings"

	"github.com/bborbe/errors"

	"github.com/bborbe/agent-sentry-issue-analyzer/pkg/deepverdict"
)

// Spec is a fully built kind: bug spec ready to be filed into a repository.
type Spec struct {
	Repo    string // owner/name, e.g. "bborbe/trading"
	Path    string // repo-relative destination, e.g. "specs/bug-octopus-prod-1j.md"
	Content string // full markdown spec (frontmatter + body)
	IssueID string // Sentry issue id — the idempotency key
}

// specSlug lower-cases an issue id and collapses every run of non
// [a-z0-9] characters into a single '-', so a crafted issue id can never
// produce a path component that escapes the repo tree (no "..", no "/").
func specSlug(issueID string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(issueID) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevDash = false
			continue
		}
		if !prevDash {
			b.WriteByte('-')
			prevDash = true
		}
	}
	return b.String()
}

// BuildSpec assembles the kind: bug spec from the verdict's own words and
// the resolution. The frontmatter (kind, status) is written here, never
// copied from the verdict — a verdict cannot forge kind or status. The
// root_cause and recommended_fix appear VERBATIM, not summarised.
func BuildSpec(ctx context.Context, v deepverdict.Verdict, resolution Resolution) (Spec, error) {
	switch {
	case v.SentryIssueID == "":
		return Spec{}, errors.New(ctx, "fix-agent: missing verdict field sentry_issue_id")
	case v.FileLine == "":
		return Spec{}, errors.New(ctx, "fix-agent: missing verdict field file:line")
	case v.RootCause == "":
		return Spec{}, errors.New(ctx, "fix-agent: missing verdict field root_cause")
	case v.RecommendedFix == "":
		return Spec{}, errors.New(ctx, "fix-agent: missing verdict field recommended_fix")
	case resolution.Repo == "":
		return Spec{}, errors.New(ctx, "fix-agent: missing resolution field repo")
	case resolution.Rule == "":
		return Spec{}, errors.New(ctx, "fix-agent: missing resolution field rule")
	}
	slug := specSlug(v.SentryIssueID)
	path := "specs/bug-" + slug + ".md"
	content := fmt.Sprintf(
		"---\nstatus: draft\nkind: bug\n---\n\n"+
			"## Summary\n\n"+
			"Sentry issue `%s` is a real bug in `%s` at `%s`, filed automatically by the sentry-fix-agent from the deep verdict.\n\n"+
			"## Problem\n\n%s\n\n"+
			"## Reproduction\n\n"+
			"The implicated source location from the deep verdict is `%s` in `%s` (resolution rule: `%s`), Sentry issue `%s`.\n\n"+
			"## Recommended Fix\n\n%s\n",
		v.SentryIssueID, resolution.Repo, v.FileLine, v.RootCause,
		v.FileLine, resolution.Repo, resolution.Rule, v.SentryIssueID, v.RecommendedFix,
	)
	return Spec{
		Repo:    resolution.Repo,
		Path:    path,
		Content: content,
		IssueID: v.SentryIssueID,
	}, nil
}
