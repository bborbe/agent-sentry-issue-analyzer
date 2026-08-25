// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Command agent-sentry-issue-analyzer is the canonical AI-heavy agent: one Claude
// invocation per phase, all logic in the prompt + allowed tools.
//
// This binary is the Kafka entry point — spawned as a K8s Job by
// task/executor with TASK_CONTENT + TASK_ID + PHASE + KAFKA_BROKERS env.
// For local CLI mode (file-based), see cmd/run-task/main.go.
//
// Reference implementation for AI-heavy agents using the agent framework
// (lib.NewAgent + claude.NewAgentStep). Other agents (trade-analysis,
// pr-reviewer) follow the same shape — copy this main.go and swap
// prompts/tools.
package main

import (
	"context"
	"os"
	"time"

	agentlib "github.com/bborbe/agent"
	claudelib "github.com/bborbe/agent/claude"
	delivery "github.com/bborbe/agent/delivery"
	"github.com/bborbe/agent/envparse"
	libmetrics "github.com/bborbe/agent/metrics"
	"github.com/bborbe/cqrs/base"
	"github.com/bborbe/errors"
	libkafka "github.com/bborbe/kafka"
	"github.com/bborbe/maintainer/githubapp"
	libsentry "github.com/bborbe/sentry"
	"github.com/bborbe/service"
	libtime "github.com/bborbe/time"
	"github.com/bborbe/vault-cli/pkg/domain"
	"github.com/golang/glog"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/push"

	"github.com/bborbe/agent-sentry-issue-analyzer/pkg/factory"
	"github.com/bborbe/agent-sentry-issue-analyzer/pkg/preflight"
)

// agentName is the identity string used for Prometheus metric grouping and logging.
const agentName = "claude-agent"

// taskTypeSentryWatcher is the watcher step's task-type literal (mirrors
// factory.taskTypeSentryWatcher). The watcher has a single planning phase and
// a different tool contract (scripts/sentry-create-tasks.sh instead of
// sentry-read.sh / repo-clone.sh), so it gets its own preflight.
const taskTypeSentryWatcher = "sentry-watcher"

func main() {
	app := &application{}
	os.Exit(service.Main(context.Background(), app, &app.SentryDSN, &app.SentryProxy))
}

type application struct {
	SentryDSN      string `required:"false" arg:"sentry-dsn"       env:"SENTRY_DSN"       usage:"SentryDSN"                                        display:"length"`
	SentryProxy    string `required:"false" arg:"sentry-proxy"     env:"SENTRY_PROXY"     usage:"Sentry Proxy"                                     display:"length"`
	SentryAPIToken string `required:"true"  arg:"sentry-api-token" env:"SENTRY_API_TOKEN" usage:"Sentry REST API Bearer token (teamvault-sourced)" display:"length"`

	// GitHub App auth for read-only cloning of private repos (repo-clone.sh).
	// The pod mints an installation access token at startup (same pattern as
	// github-pr-review-agent resolveAuth) and exposes it to the Claude
	// subprocess as GIT_CLONE_TOKEN so repo-clone.sh can clone private
	// bborbe/seibert repos. APP_ID + INSTALLATION_ID + one of PEM/PEM_KEY are
	// required for App auth.
	AppID          int64  `required:"false" arg:"app-id"          env:"APP_ID"          usage:"GitHub App ID (numeric); required for App auth"`
	InstallationID int64  `required:"false" arg:"installation-id" env:"INSTALLATION_ID" usage:"GitHub App installation ID; required for App auth"`
	PEMKey         string `required:"false" arg:"pem-key"         env:"PEM_KEY"         usage:"GitHub App private key (PEM) as env var content"   display:"length"`

	// Claude Code CLI configuration
	ClaudeConfigDir claudelib.ClaudeConfigDir `required:"false" arg:"claude-config-dir" env:"CLAUDE_CONFIG_DIR" usage:"Claude Code config directory"`

	// Agent directory (contains .claude/ with CLAUDE.md and commands)
	AgentDir claudelib.AgentDir `required:"false" arg:"agent-dir" env:"AGENT_DIR" usage:"Agent directory with .claude/ config" default:"agent"`

	// Allowed tools (comma-separated)
	AllowedToolsRaw string `required:"false" arg:"allowed-tools" env:"ALLOWED_TOOLS" usage:"Comma-separated list of allowed tools"`

	// Task content from agent pipeline
	TaskContent string `required:"true" arg:"task-content" env:"TASK_CONTENT" usage:"Raw task markdown from vault"`

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

	// Branch for Kafka result delivery
	Branch base.Branch `required:"false" arg:"branch" env:"BRANCH" usage:"branch"`

	// Explicit Kafka topic prefix (independent of Branch; empty means unprefixed topics)
	TopicPrefix base.TopicPrefix `required:"false" arg:"topic-prefix" env:"TOPIC_PREFIX" usage:"Explicit Kafka topic prefix; empty means unprefixed topics"`

	// Stage is the deployment stage (dev|prod), forwarded to the watcher script
	// as STAGE so scripts/sentry-create-tasks.sh can pass --stage to
	// /create-tasks. Empty means the watcher script's default (dev).
	Stage string `required:"false" arg:"stage" env:"STAGE" usage:"Deployment stage (dev|prod); forwarded to the watcher script"`

	// TargetVault is the Obsidian vault slug the watcher's /create-tasks
	// publishes per-alert tasks into (e.g. "personal"), forwarded to the
	// watcher script as TARGET_VAULT.
	TargetVault string `required:"false" arg:"target-vault" env:"TARGET_VAULT" usage:"Obsidian vault slug for watcher-published per-alert tasks"`

	// Phase to run (framework requires explicit phase)
	Phase domain.TaskPhase `required:"false" arg:"phase" env:"PHASE" usage:"Agent phase: planning | execution | ai_review" default:"execution"`

	// Kafka delivery (optional — only active when TASK_ID is set)
	KafkaBrokers libkafka.Brokers        `required:"false" arg:"kafka-brokers" env:"KAFKA_BROKERS" usage:"Comma separated list of Kafka brokers"`
	TaskID       agentlib.TaskIdentifier `required:"false" arg:"task-id"       env:"TASK_ID"       usage:"Agent task identifier for publishing results back to task controller"`

	PushgatewayURL string `required:"false" arg:"pushgateway-url" env:"PUSHGATEWAY_URL" usage:"Prometheus PushGateway URL"          default:"http://pushgateway:9090"`
	TaskType       string `required:"false" arg:"task-type"       env:"TASK_TYPE"       usage:"Task type label for metric grouping" default:"unknown"`
}

// createDeliverer builds the Kafka-or-Noop result deliverer. Empty taskID
// means "no Kafka" — returns a noop deliverer and an empty cleanup. Non-empty
// taskID requires non-empty brokers; the cleanup closes the SyncProducer.
// Boot-time decision, kept in main.go (per go-factory-pattern: factories
// compose, main.go interprets config + owns lifecycle).
func (a *application) createDeliverer(
	ctx context.Context,
) (agentlib.ResultDeliverer, func(), error) {
	if a.TaskID == "" {
		return delivery.NewNoopResultDeliverer(), func() {}, nil
	}
	if len(a.KafkaBrokers) == 0 {
		return nil, nil, errors.Errorf(ctx, "KAFKA_BROKERS must be set when TASK_ID is set")
	}
	syncProducer, err := libkafka.NewSyncProducerWithName(
		ctx,
		a.KafkaBrokers,
		"agent-sentry-issue-analyzer",
	)
	if err != nil {
		return nil, nil, errors.Wrap(ctx, err, "create sync producer")
	}
	cleanup := func() {
		if err := syncProducer.Close(); err != nil {
			glog.Warningf("close sync producer failed: %v", err)
		}
	}
	return factory.CreateKafkaResultDeliverer(
		syncProducer, a.TopicPrefix, a.TaskID, a.TaskContent,
		libtime.NewCurrentDateTime(),
	), cleanup, nil
}

// buildClaudeEnv assembles the environment passed to the Claude CLI subprocess.
// The claude CLI runs prompt scripts (Bash tool) in a subprocess env built from
// an allowlist + CLAUDE_CONFIG_DIR + this map, so anything a script needs (the
// Sentry token, the repo-clone GitHub token) must be forwarded here — the pod
// secret alone is not enough (buildSubprocessEnv strips non-allowlisted vars).
// Errors are wrapped for the caller to record metrics + return.
func (a *application) buildClaudeEnv(ctx context.Context) (map[string]string, error) {
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
	// The Sentry REST token: the main binary reads it for preflight, and the
	// Bash tool contract (scripts/sentry-read.sh) needs it in the subprocess env.
	if a.SentryAPIToken != "" {
		claudeEnv["SENTRY_API_TOKEN"] = a.SentryAPIToken
	}
	// The watcher step's Bash tool contract (scripts/sentry-create-tasks.sh)
	// needs the Kafka brokers + per-alert task knobs in the subprocess env —
	// the pod secret alone is not enough (buildSubprocessEnv strips
	// non-allowlisted vars). Forward only when non-empty.
	if len(a.KafkaBrokers) > 0 {
		claudeEnv["KAFKA_BROKERS"] = a.KafkaBrokers.String()
	}
	if a.TopicPrefix != "" {
		claudeEnv["TOPIC_PREFIX"] = string(a.TopicPrefix)
	}
	if a.TargetVault != "" {
		claudeEnv["TARGET_VAULT"] = a.TargetVault
	}
	if a.Stage != "" {
		claudeEnv["STAGE"] = a.Stage
	}
	// Mint a GitHub App installation token and expose it as GIT_CLONE_TOKEN so
	// scripts/repo-clone.sh can clone private bborbe/seibert repos (e.g. trading).
	// Same shared App as github-pr-review-agent. Optional: when App auth is not
	// configured, no GIT_CLONE_TOKEN is set and repo-clone.sh only clones public
	// repos.
	if a.AppID != 0 && a.InstallationID != 0 && a.PEMKey != "" {
		iat, err := githubapp.MintIAT(ctx, githubapp.Config{
			AppID:          a.AppID,
			InstallationID: a.InstallationID,
			PEM:            []byte(a.PEMKey),
		})
		if err != nil {
			return nil, errors.Wrap(ctx, err, "mint github app iat")
		}
		claudeEnv["GIT_CLONE_TOKEN"] = iat
	}
	return claudeEnv, nil
}

// validatePreflight runs the task-type-appropriate fail-fast tool/token check.
// The watcher step uses its own constrained script (scripts/sentry-create-tasks.sh)
// instead of sentry-read.sh / repo-clone.sh, so it is preflighted against its
// own tool contract; all other task types keep the triage/deep checks.
func (a *application) validatePreflight(
	ctx context.Context,
	allowedTools claudelib.AllowedTools,
) error {
	if a.TaskType == taskTypeSentryWatcher {
		return errors.Wrap(
			ctx,
			preflight.ValidateWatcherTools(ctx, allowedTools, a.SentryAPIToken),
			"sentry-watcher preflight",
		)
	}
	if err := preflight.ValidateSentryTools(ctx, allowedTools, a.SentryAPIToken); err != nil {
		return errors.Wrap(ctx, err, "sentry preflight")
	}
	if err := preflight.ValidateRepoCloneTools(ctx, allowedTools); err != nil {
		return errors.Wrap(ctx, err, "repo-clone preflight")
	}
	return nil
}

func (a *application) Run(ctx context.Context, _ libsentry.Client) error {
	registry := prometheus.NewRegistry()
	jobMetrics := libmetrics.NewJobMetrics(registry, libtime.NewCurrentDateTime())
	pusher := push.New(a.PushgatewayURL, libmetrics.BuildJobMetricsName(agentName)).
		Grouping("agent", agentName).
		Grouping("task_type", a.TaskType).
		Collector(registry)
	defer func() {
		if err := pusher.PushContext(ctx); err != nil {
			glog.Warningf("prometheus push failed: %v", err)
			return
		}
		glog.V(2).Infof("prometheus push completed")
	}()
	start := libtime.NewCurrentDateTime().Now().Time()

	glog.V(2).Infof("agent-sentry-issue-analyzer started phase=%s", a.Phase)

	deliverer, cleanup, err := a.createDeliverer(ctx)
	if err != nil {
		jobMetrics.RecordRun(agentlib.AgentStatusFailed)
		jobMetrics.RecordDuration(time.Since(start))
		return errors.Wrap(ctx, err, "create deliverer")
	}
	defer cleanup()

	claudeEnv, err := a.buildClaudeEnv(ctx)
	if err != nil {
		jobMetrics.RecordRun(agentlib.AgentStatusFailed)
		jobMetrics.RecordDuration(time.Since(start))
		return err
	}

	allowedTools := claudelib.ParseAllowedTools(a.AllowedToolsRaw)
	if err := a.validatePreflight(ctx, allowedTools); err != nil {
		jobMetrics.RecordRun(agentlib.AgentStatusFailed)
		jobMetrics.RecordDuration(time.Since(start))
		return err
	}

	provider := factory.CreateAgentProvider(
		a.ClaudeConfigDir,
		a.AgentDir,
		claudelib.ParseAllowedTools(a.AllowedToolsRaw),
		a.AnthropicModel,
		claudeEnv,
		envparse.KeyValuePairs(a.EnvContextRaw),
	)
	agent, err := provider.Get(ctx, agentlib.TaskType(a.TaskType))
	if err != nil {
		jobMetrics.RecordRun(agentlib.AgentStatusFailed)
		jobMetrics.RecordDuration(time.Since(start))
		return errors.Wrap(ctx, err, "select agent for task_type")
	}

	result, err := agent.Run(ctx, a.Phase, a.TaskContent, deliverer)
	if err != nil {
		jobMetrics.RecordRun(agentlib.AgentStatusFailed)
		jobMetrics.RecordDuration(time.Since(start))
		return errors.Wrap(ctx, err, "agent run failed")
	}
	if result == nil {
		// agent.Run returns (nil, nil) when every step in the phase skipped
		// (ShouldRun=false, e.g. the phase's output section already exists from
		// a prior run) — treat as a no-op completion rather than panicking.
		jobMetrics.RecordRun(agentlib.AgentStatusFailed)
		jobMetrics.RecordDuration(time.Since(start))
		return errors.Errorf(
			ctx,
			"agent run returned nil result (all steps skipped for phase %s)",
			a.Phase,
		)
	}
	jobMetrics.RecordRun(result.Status)
	jobMetrics.RecordDuration(time.Since(start))
	return agentlib.PrintResult(ctx, result)
}
