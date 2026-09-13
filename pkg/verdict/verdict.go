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
	"sort"
	"strings"

	"github.com/bborbe/errors"
	"gopkg.in/yaml.v3"
)

// NotEvaluable is the single token rendered for a live-state field the agent
// could not determine. The execution prompt leaves these fields unavailable for
// derived-key alerts, and the model renders that instruction as a word rather
// than a number — the spelling drifts with the prompt's own wording, so
// UnmarshalYAML accepts any non-numeric token and only MarshalYAML pins one.
const NotEvaluable = "unavailable"

// EventCount is a live-event count that may be explicitly not-evaluable.
//
// Zero is a real measurement and NotEvaluable is not zero. Coercing the token
// to 0 would present "volume unknown" to the disqualifier arithmetic as "no
// events" — a fabricated measurement the rubric then reasons from. Known
// carries that distinction through to the evaluator, which skips every
// count-based comparison when it is false.
type EventCount struct {
	Count int
	Known bool
}

// UnmarshalYAML accepts either a number or any non-numeric token. The token set
// is deliberately open: it is the prompt's own vocabulary rendered by the
// model, observed as both "unknown" (2026-09-12) and "unavailable"
// (2026-09-13), and an enumerated set would fail on the next paraphrase.
func (c *EventCount) UnmarshalYAML(value *yaml.Node) error {
	var n int
	if err := value.Decode(&n); err == nil {
		c.Count, c.Known = n, true
		return nil
	}
	c.Count, c.Known = 0, false
	return nil
}

// MarshalYAML round-trips a not-evaluable count back to the sentinel token, so
// a verdict that parsed from a word does not re-render as a fabricated 0.
func (c EventCount) MarshalYAML() (interface{}, error) {
	if !c.Known {
		return NotEvaluable, nil
	}
	return c.Count, nil
}

// Verdict is the machine-readable classification of the single Sentry alert.
//
// The execution-phase Claude prompt emits one fenced YAML block into the
// ## Verdict section with EXACTLY these keys (see pkg/prompts/execution.md).
// Unknown verdicts or missing required fields fail validation.
type Verdict struct {
	SentryIssueID      string     `yaml:"sentry_issue_id"`
	Verdict            string     `yaml:"verdict"`
	Confidence         string     `yaml:"confidence"`
	Reason             string     `yaml:"reason"`
	LiveEventCount     EventCount `yaml:"live_event_count"`
	FirstSeen          string     `yaml:"first_seen"`
	LastSeen           string     `yaml:"last_seen"`
	SentryStatus       string     `yaml:"sentry_status"`
	DisqualifiersFired []string   `yaml:"disqualifiers_fired"`
	Understanding      string     `yaml:"understanding"`
	FixCertainty       string     `yaml:"fix_certainty"`
	RootCause          string     `yaml:"root_cause"`
	RecommendedFix     string     `yaml:"recommended_fix"`
}

// Valid verdict vocabulary (the 7-verdict rubric, mirrored verbatim from
// octopus-check-sentry / Sentry Triage Guide).
var validVerdicts = map[string]bool{
	"already-tracked": true,
	"regression":      true,
	"real bug":        true,
	"noise":           true,
	"duplicate":       true,
	"not-a-defect":    true,
	"unanalyzable":    true,
}

// Vocabulary returns the valid triage verdict keys in sorted order. It is
// the single source of truth for the vocabulary: Validate renders its
// unknown-verdict message from it, and the package's external test locks
// its size and contents so a new verdict cannot be added without a
// validated case.
func Vocabulary() []string {
	keys := make([]string, 0, len(validVerdicts))
	for key := range validVerdicts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
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
			"unknown verdict %q (valid: %s)",
			v.Verdict,
			strings.Join(Vocabulary(), ", "),
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
func lastJSONBlock(ctx context.Context, s string) (string, bool) {
	end := strings.LastIndex(s, "}")
	if end < 0 {
		return "", false
	}
	depth := 0
	for i := end; i >= 0; i-- {
		select {
		case <-ctx.Done():
			return "", false
		default:
		}
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

// Render marshals a verdict back into the fenced YAML block the execution
// phase writes under ## Verdict — the same shape Parse accepts. Used by the
// reassign step to rewrite the verdict section when code forces `real bug`
// from a computed disqualifier.
func Render(ctx context.Context, v Verdict) (string, error) {
	block, err := yaml.Marshal(v)
	if err != nil {
		return "", errors.Wrapf(ctx, err, "marshal verdict")
	}
	return "```yaml\n" + string(block) + "```", nil
}
