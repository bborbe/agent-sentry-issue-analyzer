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

// Parse extracts the verdict block from the ## Verdict section of the given
// markdown content. The verdict may arrive as a fenced ```yaml or ```json
// block (JSON is a YAML subset) or as legacy unfenced raw JSON; both shapes
// parse. Returns the parsed verdict, or zero Verdict + nil when no verdict
// block is present.
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
	blocks, err := fencedBlocks(ctx, section)
	if err != nil {
		return v, err
	}
	for _, block := range blocks {
		select {
		case <-ctx.Done():
			return v, ctx.Err()
		default:
		}
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
	if v.Verdict != "" {
		return v, nil
	}
	// Legacy unfenced raw JSON: the LLM may have written the verdict JSON with
	// no fence at all. JSON is a YAML subset, so the same unmarshal applies.
	parsed, err := parseUnfencedVerdict(ctx, section)
	if err != nil {
		errs = append(errs, errors.Wrapf(ctx, err, "parse unfenced verdict").Error())
	} else if parsed.Verdict != "" {
		v = parsed
	}
	if len(errs) > 0 && v.Verdict == "" {
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

// fencedBlocks extracts the bodies of all ```yaml and ```json fenced code
// blocks in content. JSON verdict blocks are extracted too because JSON is a
// YAML subset — the same yaml.Unmarshal parses both shapes.
func fencedBlocks(ctx context.Context, content string) ([]string, error) {
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
			lang := strings.TrimSpace(strings.TrimPrefix(trimmed, "```"))
			if lang == "yaml" || lang == "json" {
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

// lastJSONBlock returns the last balanced {...} substring in s, or "", false
// if none exists. Walks from the end finding the closing brace, then walks
// back tracking brace depth to find the matching open. Mirrors
// github-pr-review-agent extractVerdict's fallback for legacy unfenced output.
func lastJSONBlock(_ context.Context, s string) (string, bool) {
	end := strings.LastIndex(s, "}")
	if end < 0 {
		return "", false
	}
	depth := 0
	for i := end; i >= 0; i-- {
		switch s[i] {
		case '}':
			depth++
		case '{':
			depth--
			if depth == 0 {
				return s[i : end+1], true
			}
		}
	}
	return "", false
}

// parseUnfencedVerdict extracts a legacy unfenced raw JSON verdict from the
// section body. JSON is a YAML subset, so the same yaml.Unmarshal applies.
// Mirrors github-pr-review-agent extractVerdict's last-balanced-block
// fallback. Returns a zero Verdict when no JSON object is present.
func parseUnfencedVerdict(ctx context.Context, section string) (Verdict, error) {
	block, ok := lastJSONBlock(ctx, section)
	if !ok {
		return Verdict{}, nil
	}
	var parsed Verdict
	if err := yaml.Unmarshal([]byte(block), &parsed); err != nil {
		return Verdict{}, err
	}
	return parsed, nil
}
