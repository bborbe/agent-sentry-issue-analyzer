// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Command run-task is the local-CLI entry point for agent-sentry-issue-analyzer.
//
// Reads a markdown task file from disk, runs the agent against it, and
// writes the updated content back to the same file. Mirrors the Kafka
// entry point (../../main.go) but uses file I/O instead of Kafka/CQRS.
//
// Used for local development and integration testing without spinning up
// a K8s Job + Kafka cluster.
package main

import (
	"context"
	"fmt"
	"os"

	agentlib "github.com/bborbe/agent"
	claudelib "github.com/bborbe/agent/claude"
	"github.com/bborbe/agent/envparse"
	"github.com/bborbe/cqrs/base"
	"github.com/bborbe/errors"
	libsentry "github.com/bborbe/sentry"
	"github.com/bborbe/service"
	"github.com/bborbe/vault-cli/pkg/domain"

	"github.com/bborbe/agent-sentry-issue-analyzer/pkg/factory"
	"github.com/bborbe/agent-sentry-issue-analyzer/pkg/preflight"
)

func main() {
	app := &application{}
	os.Exit(service.Main(context.Background(), app, &app.SentryDSN, &app.SentryProxy))
}

type application struct {
	SentryDSN      string `required:"false" arg:"sentry-dsn"       env:"SENTRY_DSN"       usage:"SentryDSN"                                        display:"length"`
	SentryProxy    string `required:"false" arg:"sentry-proxy"     env:"SENTRY_PROXY"     usage:"Sentry Proxy"                                     display:"length"`
	SentryAPIToken string `required:"true"  arg:"sentry-api-token" env:"SENTRY_API_TOKEN" usage:"Sentry REST API Bearer token (teamvault-sourced)" display:"length"`

	// Claude Code CLI configuration
	ClaudeConfigDir claudelib.ClaudeConfigDir `required:"false" arg:"claude-config-dir" env:"CLAUDE_CONFIG_DIR" usage:"Claude Code config directory"`

	// Agent directory (contains .claude/ with CLAUDE.md and commands)
	AgentDir claudelib.AgentDir `required:"false" arg:"agent-dir" env:"AGENT_DIR" usage:"Agent directory with .claude/ config" default:"agent"`

	// Allowed tools (comma-separated)
	AllowedToolsRaw string `required:"false" arg:"allowed-tools" env:"ALLOWED_TOOLS" usage:"Comma-separated list of allowed tools"`

	// Environment context passed to prompt (comma-separated KEY=VALUE pairs)
	EnvContextRaw string `required:"false" arg:"env-context" env:"ENV_CONTEXT" usage:"Comma-separated KEY=VALUE pairs for prompt context"`

	// Environment variables passed to Claude CLI process (comma-separated KEY=VALUE pairs).
	// Use this for ad-hoc / less-common env vars. The three load-bearing Anthropic provider
	// vars below have dedicated arg slots so they don't have to be packed into this string.
	ClaudeEnvRaw string `required:"false" arg:"claude-env" env:"CLAUDE_ENV" usage:"Comma-separated KEY=VALUE pairs for Claude CLI environment"`

	// Anthropic-compatible provider routing. Setting AnthropicBaseURL + AnthropicAuthToken
	// routes the claude CLI to an alt-provider (e.g. MiniMax via https://api.minimax.io/anthropic).
	// AnthropicModel drives both the `--model` CLI flag and the ANTHROPIC_MODEL env var seen by
	// the claude subprocess. Non-empty values override the same keys in ClaudeEnvRaw.
	AnthropicBaseURL   string                `required:"false" arg:"anthropic-base-url"   env:"ANTHROPIC_BASE_URL"   usage:"Anthropic-compatible API base URL"`
	AnthropicAuthToken string                `required:"false" arg:"anthropic-auth-token" env:"ANTHROPIC_AUTH_TOKEN" usage:"Bearer token for ANTHROPIC_BASE_URL"                                  display:"length"`
	AnthropicModel     claudelib.ClaudeModel `required:"false" arg:"anthropic-model"      env:"ANTHROPIC_MODEL"      usage:"Model name; also exposed to the claude subprocess as ANTHROPIC_MODEL"                  default:"sonnet"`

	// Environment
	Branch base.Branch `required:"true" arg:"branch" env:"BRANCH" usage:"branch" default:"dev"`

	// Explicit Kafka topic prefix (independent of Branch; empty means unprefixed topics)
	TopicPrefix base.TopicPrefix `required:"false" arg:"topic-prefix" env:"TOPIC_PREFIX" usage:"Explicit Kafka topic prefix; empty means unprefixed topics"`

	// Phase to run (defaults to execution; framework requires explicit phase)
	Phase domain.TaskPhase `required:"false" arg:"phase" env:"PHASE" usage:"Agent phase: planning | execution | ai_review" default:"execution"`

	// Task file for local development
	TaskFilePath string `required:"true" arg:"task-file" env:"TASK_FILE" usage:"Path to the markdown task file"`
}

func (a *application) Run(ctx context.Context, _ libsentry.Client) error {
	taskContent, err := os.ReadFile(
		a.TaskFilePath,
	) // #nosec G304 -- filePath from trusted CLI input
	if err != nil {
		return errors.Wrap(ctx, err, "read task file: "+a.TaskFilePath)
	}

	deliverer := factory.CreateFileResultDeliverer(a.TaskFilePath)

	claudeEnv := envparse.KeyValuePairs(a.ClaudeEnvRaw)
	if claudeEnv == nil {
		claudeEnv = map[string]string{}
	}
	if a.AnthropicBaseURL != "" {
		claudeEnv["ANTHROPIC_BASE_URL"] = a.AnthropicBaseURL
	}
	if a.AnthropicAuthToken != "" {
		claudeEnv["ANTHROPIC_AUTH_TOKEN"] = a.AnthropicAuthToken
	}
	if a.AnthropicModel != "" {
		claudeEnv["ANTHROPIC_MODEL"] = a.AnthropicModel.String()
	}

	allowedTools := claudelib.ParseAllowedTools(a.AllowedToolsRaw)
	if err := preflight.ValidateSentryTools(ctx, allowedTools, a.SentryAPIToken); err != nil {
		return errors.Wrap(ctx, err, "sentry preflight")
	}
	if err := preflight.ValidateRepoCloneTools(ctx, allowedTools); err != nil {
		return errors.Wrap(ctx, err, "repo-clone preflight")
	}

	agent := factory.CreateAgent(
		a.ClaudeConfigDir,
		a.AgentDir,
		allowedTools,
		a.AnthropicModel,
		claudeEnv,
		envparse.KeyValuePairs(a.EnvContextRaw),
	)

	result, err := agent.Run(ctx, a.Phase, string(taskContent), deliverer)
	if err != nil {
		return errors.Wrap(ctx, err, "agent run failed")
	}
	if result == nil {
		// agent.Run returns (nil, nil) when every step in the phase skipped
		// (ShouldRun=false, e.g. the phase's output section already exists from
		// a prior run) or the phase has no registered steps. Deliver a Failed
		// result so the task reaches a terminal state, then exit 0 — a no-op
		// completion, never a controller retry (mirrors agent-lib unsupportedPhase).
		return a.deliverNilResult(ctx, deliverer)
	}
	return agentlib.PrintResult(ctx, result)
}

// deliverNilResult publishes a Failed result for a phase whose steps all
// skipped or were never registered (agent.Run returned (nil, nil)), naming
// the phase so the failure is diagnosable in the task body. Returns nil on
// success so the process exits 0 and the Job never re-enters the controller
// retry loop; a deliver failure is wrapped and returned so the controller
// retries (bounded).
func (a *application) deliverNilResult(
	ctx context.Context,
	deliverer agentlib.ResultDeliverer,
) error {
	failedResult := &agentlib.Result{
		Status: agentlib.AgentStatusFailed,
		Message: fmt.Sprintf(
			"agent run returned nil result (all steps skipped for phase %s)",
			a.Phase,
		),
	}
	if err := deliverer.DeliverResult(ctx, agentlib.AgentResultInfo{
		Status:  failedResult.Status,
		Message: failedResult.Message,
	}); err != nil {
		return errors.Wrapf(ctx, err, "deliver nil-result failure")
	}
	return nil
}
