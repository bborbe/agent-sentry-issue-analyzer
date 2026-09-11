// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package fixagent implements the sentry-fix agent domain: resolving the
// repository behind a deep verdict's file:line (prompt-backed, running the
// same resolution block the deep analyzer uses), building a valid kind: bug
// spec from the verdict's own words, and filing it through the GitHub
// Contents API. Resolution and writing are injectable interfaces so the
// production implementations can be stubbed in tests.
package fixagent

import (
	"context"
	"fmt"
	"strings"

	claudelib "github.com/bborbe/agent/claude"
	"github.com/bborbe/errors"
	"gopkg.in/yaml.v3"

	"github.com/bborbe/agent-sentry-issue-analyzer/pkg/deepverdict"
)

// Resolution is the outcome of resolving a deep verdict's file:line to a
// repository. Fresh is false when the cited path no longer exists at the
// current revision; StaleReason then names the stale path.
type Resolution struct {
	Repo        string `yaml:"repo"`
	Rule        string `yaml:"rule"`
	Fresh       bool   `yaml:"fresh"`
	StaleReason string `yaml:"stale_reason"`
}

//counterfeiter:generate -o mocks/repo-resolver.go --fake-name RepoResolver . RepoResolver

// RepoResolver resolves a deep verdict's file:line to the repository holding
// the source. Production is prompt-backed (PromptRepoResolver runs the same
// resolution block the deep analyzer uses); tests stub it per fixture.
type RepoResolver interface {
	Resolve(ctx context.Context, v deepverdict.Verdict) (Resolution, error)
}

// PromptRepoResolver resolves the repository by running the resolution prompt
// block through a ClaudeRunner and parsing the model's structured output.
type PromptRepoResolver struct {
	runner       claudelib.ClaudeRunner
	instructions claudelib.Instructions
}

// NewPromptRepoResolver constructs a prompt-backed RepoResolver.
func NewPromptRepoResolver(
	runner claudelib.ClaudeRunner,
	instructions claudelib.Instructions,
) RepoResolver {
	return &PromptRepoResolver{runner: runner, instructions: instructions}
}

// Resolve implements RepoResolver. It renders the verdict's citation fields
// as the task content, runs the resolution prompt, and parses the model's
// fenced YAML block. An unmappable result (empty repo) is an error.
func (r *PromptRepoResolver) Resolve(
	ctx context.Context,
	v deepverdict.Verdict,
) (Resolution, error) {
	taskContent := fmt.Sprintf(
		"sentry_issue_id: %s\nfile:line: %s\nroot_cause: %s\nrecommended_fix: %s\n",
		v.SentryIssueID, v.FileLine, v.RootCause, v.RecommendedFix,
	)
	prompt := claudelib.BuildPrompt(r.instructions.String(), nil, taskContent)
	result, err := r.runner.Run(ctx, prompt)
	if err != nil {
		return Resolution{}, errors.Wrapf(ctx, err, "fix-agent: resolution claude run")
	}
	res, err := parseResolutionBlock(ctx, result.Result)
	if err != nil {
		return Resolution{}, errors.Wrapf(ctx, err, "fix-agent: parse resolution block")
	}
	if res.Repo == "" {
		return Resolution{}, errors.Errorf(ctx, "fix-agent: resolution unmappable: %s", res.Rule)
	}
	return res, nil
}

// parseResolutionBlock extracts the first fenced yaml block from content and
// unmarshals it into a Resolution.
func parseResolutionBlock(ctx context.Context, content string) (Resolution, error) {
	block, err := firstFencedYAMLBlock(ctx, content)
	if err != nil {
		return Resolution{}, err
	}
	var res Resolution
	if err := yaml.Unmarshal([]byte(block), &res); err != nil {
		return Resolution{}, errors.Wrapf(ctx, err, "unmarshal resolution block")
	}
	return res, nil
}

// firstFencedYAMLBlock extracts the body of the first ```yaml fenced code
// block in content. The model's output must carry exactly that block: no block
// at all and an unclosed fence are both errors, not silently complete — a
// malformed resolution must not proceed to an empty repo.
func firstFencedYAMLBlock(ctx context.Context, content string) (string, error) {
	lines := strings.Split(content, "\n")
	var current []string
	inBlock := false
	sawYAMLFence := false
	for _, line := range lines {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}
		trimmed := strings.TrimSpace(line)
		if !inBlock && strings.HasPrefix(trimmed, "```") {
			lang := strings.TrimPrefix(trimmed, "```")
			if strings.TrimSpace(lang) == "yaml" {
				inBlock = true
				sawYAMLFence = true
				current = nil
			}
			continue
		}
		if inBlock && strings.HasPrefix(trimmed, "```") {
			return strings.Join(current, "\n"), nil
		}
		if inBlock {
			current = append(current, line)
		}
	}
	if inBlock {
		// Unclosed fence — the block is malformed, not silently complete.
		return "", errors.New(ctx, "unclosed yaml fence in resolution block")
	}
	if !sawYAMLFence {
		return "", errors.New(ctx, "no fenced yaml block in resolution output")
	}
	return "", nil
}
