// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package steps

import (
	"context"
	"fmt"
	"strings"

	agentlib "github.com/bborbe/agent"
	"github.com/bborbe/errors"
	"github.com/golang/glog"

	"github.com/bborbe/agent-sentry-issue-analyzer/pkg/verdict"
)

// jiraProposalHeading is the block heading the /daily-maintenance consumer
// parses. It MUST be three hashes: verdict.extractVerdictSection breaks on any
// "## " prefix, so a two-hash heading would be swallowed into the ## Verdict
// section and the consumer would never see it.
const jiraProposalHeading = "### Jira Proposal"

// jiraProposalVerdicts are the classifications that can justify a Jira ticket.
// The seven-verdict rubric is the source; only these two are important enough
// to escalate when the fix agent could not resolve them.
var jiraProposalVerdicts = map[string]bool{
	"real bug":   true,
	"regression": true,
}

// jiraProposalStep emits a ### Jira Proposal block on an alert task exactly
// when the verdict is `real bug` or `regression`, at least one disqualifier
// fired, AND the fix agent produced no PR — never otherwise.
//
// The block is the contract with /daily-maintenance, which parses exactly three
// fields (summary, description, label) and refuses a block missing any of them.
// Its normative definition lives in the vault task "Write a Jira Proposal Only
// for Important Alerts the Fix Agent Cannot Fix".
//
// "No PR" is NOT one test, because the two qualifying verdicts reach the fix
// agent differently:
//
//   - `regression` is never handed to the fix agent (its gate is `real bug`
//     only), so it never carries a ## Fix Result at all. Absence of the section
//     is the evidence.
//   - `real bug` IS handed over when the verdict is High/High, so a missing
//     ## Fix Result means the fix agent has not run yet — not that it declined.
//     Emitting there would file a ticket for a bug that is about to be fixed.
//     So this arm requires the section to exist and carry no pr_url.
//
// A pre-seeded ## Verdict on a task that never ran the analyzer is therefore
// not enough on its own; the task must reach the state the real pipeline
// produces.
type jiraProposalStep struct {
	// inner is the step whose output this rule evaluates. The proposal is a
	// decorator, not a phase: it must run after the verdict (and, in the fix
	// chain, after ## Fix Result) rather than before it.
	inner agentlib.Step
}

// NewJiraProposalStep wraps the given step so a qualifying verdict gains a
// ### Jira Proposal block after it runs. Placement matters: in the triage chain
// only the `regression` arm can fire (a `real bug` still awaits the fix agent),
// and in the fix chain the `real bug` arm fires once ## Fix Result is written.
func NewJiraProposalStep(inner agentlib.Step) agentlib.Step {
	return &jiraProposalStep{inner: inner}
}

// Name reports the step name for logging and the reassign declaration key.
func (s *jiraProposalStep) Name() string {
	return "jira-proposal"
}

// ShouldRun is idempotent per task: a task that already carries the block was
// handled and must never be rewritten, so a resumed run cannot churn the body.
func (s *jiraProposalStep) ShouldRun(
	ctx context.Context,
	md *agentlib.Markdown,
) (bool, error) {
	if _, exists := md.FindSection(jiraProposalHeading); exists {
		return false, nil
	}
	return s.inner.ShouldRun(ctx, md)
}

// Run delegates to the wrapped step, then evaluates the proposal rule against
// the state that step produced.
func (s *jiraProposalStep) Run(
	ctx context.Context,
	md *agentlib.Markdown,
) (*agentlib.Result, error) {
	result, err := s.inner.Run(ctx, md)
	if err != nil {
		return result, err
	}
	if err := s.evaluate(ctx, md); err != nil {
		return result, err
	}
	return result, nil
}

// evaluate applies the rule and writes the block when it qualifies. A parse
// failure is returned rather than swallowed: a task whose ## Verdict cannot be
// read is a defect, and silently emitting nothing would look like a correct
// negative.
func (s *jiraProposalStep) evaluate(ctx context.Context, md *agentlib.Markdown) error {
	content, err := md.Marshal(ctx)
	if err != nil {
		return errors.Wrapf(ctx, err, "jira-proposal: marshal task")
	}
	v, err := verdict.Parse(ctx, content)
	if err != nil {
		return errors.Wrapf(ctx, err, "jira-proposal: parse verdict")
	}

	prURL, fixResultExists := fixResultPRURL(md)
	emit := proposalQualifies(v.Verdict, len(v.DisqualifiersFired), fixResultExists, prURL)

	// One decision line per run. This is the producer SC10's cluster
	// verification reads: without it, "no block" is indistinguishable from a
	// step that never executed.
	glog.Infof(
		"jira-proposal: verdict=%q disqualifiers=%d fix_result=%t pr=%t emit=%t",
		v.Verdict,
		len(v.DisqualifiersFired),
		fixResultExists,
		prURL != "",
		emit,
	)

	if !emit {
		return nil
	}

	block, err := buildJiraProposal(ctx, md, v)
	if err != nil {
		return err
	}
	md.ReplaceSection(agentlib.Section{Heading: jiraProposalHeading, Body: block})
	return nil
}

// proposalQualifies is the rule, isolated so the four arms are testable without
// a markdown document. It is deliberately not a single boolean expression: the
// `real bug` arm and the `regression` arm disagree about what "no PR" means,
// and collapsing them is how a fixable bug gets filed as a ticket.
func proposalQualifies(
	verdictLabel string,
	disqualifierCount int,
	fixResultExists bool,
	prURL string,
) bool {
	if !jiraProposalVerdicts[verdictLabel] {
		return false
	}
	if disqualifierCount == 0 {
		return false
	}
	if verdictLabel == "real bug" {
		// The fix agent has not run yet — it may still file a PR.
		if !fixResultExists {
			return false
		}
		return prURL == ""
	}
	// `regression` never reaches the fix agent, so absence of the section is
	// the evidence that no PR exists. A present section is still honoured: if
	// one somehow carries a pr_url, the bug was fixed and needs no ticket.
	return prURL == ""
}

// buildJiraProposal renders the three fields. The values are derived from the
// task's own frontmatter and verdict — never from a constant or a lookup — so
// the same rule produces a different block for every alert.
func buildJiraProposal(
	ctx context.Context,
	md *agentlib.Markdown,
	v verdict.Verdict,
) (string, error) {
	shortID := frontmatterString(md, "short_id")
	if shortID == "" {
		return "", errors.New(ctx, "jira-proposal: task frontmatter carries no short_id")
	}
	component := frontmatterString(md, "project")
	if component == "" {
		return "", errors.New(ctx, "jira-proposal: task frontmatter carries no project")
	}
	issueURL := frontmatterString(md, "issue_url")
	if issueURL == "" {
		return "", errors.New(ctx, "jira-proposal: task frontmatter carries no issue_url")
	}

	// summary: `component: short description`, symptom not fix.
	summary := component + ": " + symptom(v.Reason)
	// label: `sentry-<SHORT-ID>` — ONE token. Never lowercased, never split on
	// the prefix: a label that is merely present but spelled differently passes
	// the presence-only consumer parser and then matches nothing in Jira.
	label := "sentry-" + shortID

	lines := []string{
		"- summary: " + summary,
		"- description: " + issueURL,
		"- label: " + label,
	}
	return strings.Join(lines, "\n"), nil
}

// symptom trims the verdict's reason to one line so the summary stays a
// summary. The reason is free prose from the model; a multi-line value would
// break the one-field-per-line block the consumer parses.
func symptom(reason string) string {
	trimmed := strings.TrimSpace(strings.SplitN(reason, "\n", 2)[0])
	if trimmed == "" {
		return "unresolved error"
	}
	return trimmed
}

// fixResultPRURL reads the ## Fix Result section written by the fix step and
// returns its pr_url plus whether the section exists at all. The two are
// reported separately because absence and emptiness mean different things for
// the `real bug` arm — see proposalQualifies.
func fixResultPRURL(md *agentlib.Markdown) (string, bool) {
	section, ok := md.FindSection("## Fix Result")
	if !ok {
		return "", false
	}
	for line := range strings.SplitSeq(section.Body, "\n") {
		trimmed := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "-"))
		const key = "pr_url:"
		if after, found := strings.CutPrefix(trimmed, key); found {
			return strings.TrimSpace(after), true
		}
	}
	return "", true
}

// frontmatterString renders a generic frontmatter value as a string. The map is
// untyped, so a numeric short_id written unquoted parses as an int and a string
// assertion would silently miss it — the same trap traceIssueID documents.
func frontmatterString(md *agentlib.Markdown, key string) string {
	raw, ok := md.Frontmatter[key]
	if !ok || raw == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprintf("%v", raw))
}
