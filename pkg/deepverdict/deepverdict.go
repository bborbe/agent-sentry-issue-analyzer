// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package deepverdict defines the structured verdict schema the deep
// analyzer's execution phase writes into the ## Verdict section of the task
// body. One verdict per task — the deep analyzer runs on ONE task flagged
// `real bug` by the triage agent and emits this deep verdict; never batch.
//
// The schema is the octopus-analyse-bugs verdict YAML minus the Jira fields
// (parent_bug, analyze_subtask, duplicate_of, fixed_commit) plus file:line —
// the deep analyzer hands a machine-actionable fix-spec downstream, so the
// file:line the fix-PR agent needs is required, not optional.
package deepverdict

import (
	"context"
	"strings"

	"github.com/bborbe/errors"
	"gopkg.in/yaml.v3"
)

// Verdict is the machine-readable deep classification of the single Sentry
// alert. The execution-phase Claude prompt emits one fenced YAML block into
// the ## Verdict section with EXACTLY these keys (see
// pkg/prompts/deep-execution.md). Unknown verdicts or missing required fields
// fail validation.
type Verdict struct {
	SentryIssueID      string   `yaml:"sentry_issue_id"`
	Verdict            string   `yaml:"verdict"`
	Understanding      string   `yaml:"understanding"`
	FixCertainty       string   `yaml:"fix_certainty"`
	RootCause          string   `yaml:"root_cause"`
	RecommendedFix     string   `yaml:"recommended_fix"`
	FileLine           string   `yaml:"file:line"`
	DisqualifiersFired []string `yaml:"disqualifiers_fired"`
	LiveEventCount     int      `yaml:"live_event_count"`
}

// Valid verdict vocabulary (octopus-analyse-bugs, mirrored verbatim from the
// source of truth; the downstream fix-PR trigger keys on U=High AND F=High).
var validVerdicts = map[string]bool{
	"real bug":             true,
	"noise":                true,
	"duplicate":            true,
	"closed-fixed-in-prod": true,
	"not-a-defect":         true,
	"track":                true,
}

// validCertainty is the certainty vocabulary shared by understanding and
// fix_certainty (octopus U/F rubric).
var validCertainty = map[string]bool{
	"High":   true,
	"Medium": true,
	"Low":    true,
}

// Parse extracts the verdict YAML block from the ## Verdict section of the
// given markdown content. Returns the parsed verdict, or zero Verdict + nil
// when no verdict block is present.
func Parse(ctx context.Context, content string) (Verdict, error) {
	section, err := extractVerdictSection(ctx, content)
	if err != nil {
		return Verdict{}, err
	}
	if section == "" {
		return Verdict{}, nil
	}

	var v Verdict
	var errs []string
	blocks, err := fencedYAMLBlocks(ctx, section)
	if err != nil {
		return v, err
	}
	for _, block := range blocks {
		var parsed Verdict
		if err := yaml.Unmarshal([]byte(block), &parsed); err != nil {
			errs = append(errs, errors.Wrapf(ctx, err, "parse verdict block").Error())
			continue
		}
		if parsed.Verdict == "" {
			continue
		}
		v = parsed
		break
	}
	if len(errs) > 0 {
		return v, errors.Errorf(ctx, "verdict parse errors: %s", strings.Join(errs, "; "))
	}
	return v, nil
}

// Validate checks the verdict against the schema. Returns an error for
// unknown verdicts, missing required fields, invalid certainty, or a real-bug
// verdict without the file:line the downstream fix-PR agent needs.
func Validate(ctx context.Context, v Verdict) error {
	if v.SentryIssueID == "" {
		return errors.New(ctx, "verdict missing required field sentry_issue_id")
	}
	if !validVerdicts[v.Verdict] {
		return errors.Errorf(
			ctx,
			"unknown verdict %q (valid: real bug, noise, duplicate, closed-fixed-in-prod, not-a-defect, track)",
			v.Verdict,
		)
	}
	if v.Verdict != "real bug" {
		return nil
	}
	return validateRealBug(ctx, v)
}

// validateRealBug enforces the fields a real-bug verdict must carry for the
// downstream fix-PR agent: High/Medium/Low U+F certainty plus the machine-
// actionable root_cause / recommended_fix / file:line triple.
func validateRealBug(ctx context.Context, v Verdict) error {
	if !validCertainty[v.Understanding] {
		return errors.Errorf(
			ctx,
			"invalid understanding %q (valid: High, Medium, Low)",
			v.Understanding,
		)
	}
	if !validCertainty[v.FixCertainty] {
		return errors.Errorf(
			ctx,
			"invalid fix_certainty %q (valid: High, Medium, Low)",
			v.FixCertainty,
		)
	}
	if v.FileLine == "" {
		return errors.New(ctx, "real-bug verdict missing required field file:line")
	}
	if v.RootCause == "" {
		return errors.New(ctx, "real-bug verdict missing required field root_cause")
	}
	if v.RecommendedFix == "" {
		return errors.New(ctx, "real-bug verdict missing required field recommended_fix")
	}
	return nil
}

// extractVerdictSection returns the body of the ## Verdict section.
func extractVerdictSection(ctx context.Context, content string) (string, error) {
	lines := strings.Split(content, "\n")
	inSection := false
	var out []string
	for _, line := range lines {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## ") {
			if trimmed == "## Verdict" {
				inSection = true
				continue
			}
			if inSection {
				break
			}
		}
		if inSection {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n"), nil
}

// fencedYAMLBlocks extracts the bodies of all ```yaml fenced code blocks in
// content.
func fencedYAMLBlocks(ctx context.Context, content string) ([]string, error) {
	var blocks []string
	lines := strings.Split(content, "\n")
	var current []string
	inBlock := false
	for _, line := range lines {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		trimmed := strings.TrimSpace(line)
		if !inBlock && strings.HasPrefix(trimmed, "```") {
			lang := strings.TrimPrefix(trimmed, "```")
			if strings.TrimSpace(lang) == "yaml" {
				inBlock = true
				current = nil
			}
			continue
		}
		if inBlock && strings.HasPrefix(trimmed, "```") {
			inBlock = false
			blocks = append(blocks, strings.Join(current, "\n"))
			current = nil
			continue
		}
		if inBlock {
			current = append(current, line)
		}
	}
	if inBlock {
		blocks = append(blocks, strings.Join(current, "\n"))
	}
	return blocks, nil
}
