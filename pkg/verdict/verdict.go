// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package verdict defines the structured verdict schema the Sentry analyzer's
// execution phase writes into the ## Verdict section of the task body. One
// verdict per task (per-alert architecture: the collector creates one task per
// new Sentry alert, this agent analyzes that single alert).
package verdict

import (
	"context"
	"strings"

	"github.com/bborbe/errors"
	"gopkg.in/yaml.v3"
)

// Verdict is the machine-readable classification of the single Sentry alert.
//
// The execution-phase Claude prompt emits one fenced YAML block into the
// ## Verdict section with EXACTLY these keys (see pkg/prompts/execution.md).
// Unknown verdicts or missing required fields fail validation.
type Verdict struct {
	SentryIssueID  string `yaml:"sentry_issue_id"`
	Verdict        string `yaml:"verdict"`
	Confidence     string `yaml:"confidence"`
	Reason         string `yaml:"reason"`
	LiveEventCount int    `yaml:"live_event_count"`
	LastSeen       string `yaml:"last_seen"`
	SentryStatus   string `yaml:"sentry_status"`
	Understanding  string `yaml:"understanding"`
	FixCertainty   string `yaml:"fix_certainty"`
	RootCause      string `yaml:"root_cause"`
	RecommendedFix string `yaml:"recommended_fix"`
}

// Valid verdict vocabulary (the 6-verdict rubric, mirrored verbatim from
// octopus-check-sentry / Sentry Triage Guide).
var validVerdicts = map[string]bool{
	"already-tracked": true,
	"regression":      true,
	"real bug":        true,
	"noise":           true,
	"duplicate":       true,
	"not-a-defect":    true,
}

// validConfidence is the confidence vocabulary for real-bug verdicts.
var validConfidence = map[string]bool{
	"high":   true,
	"medium": true,
	"low":    true,
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
// unknown verdicts, missing required fields, or invalid confidence.
func Validate(ctx context.Context, v Verdict) error {
	if v.SentryIssueID == "" {
		return errors.New(ctx, "verdict missing required field sentry_issue_id")
	}
	if !validVerdicts[v.Verdict] {
		return errors.Errorf(
			ctx,
			"unknown verdict %q (valid: already-tracked, regression, real bug, noise, duplicate, not-a-defect)",
			v.Verdict,
		)
	}
	if v.Verdict == "real bug" {
		if v.Confidence == "" {
			return errors.New(ctx, "real-bug verdict missing required field confidence")
		}
		if !validConfidence[strings.ToLower(v.Confidence)] {
			return errors.Errorf(
				ctx,
				"invalid confidence %q (valid: high, medium, low)",
				v.Confidence,
			)
		}
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
