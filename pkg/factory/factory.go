// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package factory wires concrete dependencies for the agent-sentry-issue-analyzer binary.
//
// All factory functions follow the Create* prefix convention and contain
// zero business logic — they compose constructors with config.
package factory

import (
	agentlib "github.com/bborbe/agent"
	claudelib "github.com/bborbe/agent/claude"
	delivery "github.com/bborbe/agent/delivery"
	healthcheck "github.com/bborbe/agent/healthcheck"
	"github.com/bborbe/cqrs/base"
	libkafka "github.com/bborbe/kafka"
	libtime "github.com/bborbe/time"
	"github.com/bborbe/vault-cli/pkg/domain"

	"github.com/bborbe/agent-sentry-issue-analyzer/pkg/prompts"
	"github.com/bborbe/agent-sentry-issue-analyzer/pkg/steps"
)

const serviceName = "agent-sentry-issue-analyzer"

// taskTypeSentryIssueAnalyzer is the agent-lib TaskType literal for the
// triage agent's domain task. No constant exists in agent-lib for this value,
// so we cast it locally (mirrors github-update-go-agent). Keep the literal
// exactly "sentry-issue-analyzer" — the collector emits it verbatim in task
// frontmatter and the Config CR taskTypes list must match.
var taskTypeSentryIssueAnalyzer = agentlib.TaskType("sentry-issue-analyzer")

// taskTypeSentryDeepAnalyzer is the agent-lib TaskType literal for the deep
// analyzer's domain task. The triage execution phase reassigns a task to this
// type (assignee + task_type + phase: planning) on a `real bug` verdict, and
// the executor routes the task by its assignee to the live
// sentry-analyzer-agent Config CR, whose taskTypes list includes
// sentry-deep-analyzer.
var taskTypeSentryDeepAnalyzer = agentlib.TaskType("sentry-deep-analyzer")

// assigneeSentryAnalyzerAgent is the `assignee` of the live agent Config CR
// that handles deep analysis. It is a PLAIN string, not an agentlib.TaskType:
// assignee and task_type are different namespaces and must never share one
// constant. The dedicated sentry-deep-analyzer Config CR was deleted on
// 2026-08-26 when the sentry pipeline consolidated from 4 Config CRs to 2;
// the surviving sentry-analyzer-agent CR lists sentry-deep-analyzer in its
// taskTypes. agent-task-executor resolves an agent by exact assignee string
// and silently drops unknown names (skipped_unknown_assignee), so a wrong
// value here strands the task with no error anywhere. Keep this literal in
// sync with cmd/create-tasks/main.go's Assignee default.
const assigneeSentryAnalyzerAgent = "sentry-analyzer-agent"

// taskTypeSentryCollector is the agent-lib TaskType literal for the collector
// step's domain task. The daily recurring trigger creates one task of this
// type; the agent fetches the day's active unresolved Sentry alerts and
// publishes one per-alert task per (short-id, date) for the triage agent.
// Keep the literal exactly "sentry-collector" — the Config CR taskTypes list
// must match.
var taskTypeSentryCollector = agentlib.TaskType("sentry-collector")

// CreateClaudeRunner constructs a ClaudeRunner pre-configured with tools,
// model, working directory, and CLI environment.
func CreateClaudeRunner(
	claudeConfigDir claudelib.ClaudeConfigDir,
	agentDir claudelib.AgentDir,
	allowedTools claudelib.AllowedTools,
	model claudelib.ClaudeModel,
	env map[string]string,
) claudelib.ClaudeRunner {
	return claudelib.NewClaudeRunner(claudelib.ClaudeRunnerConfig{
		ClaudeConfigDir:  claudeConfigDir,
		AllowedTools:     allowedTools,
		Model:            model,
		WorkingDirectory: agentDir,
		Env:              env,
	})
}

// CreateKafkaResultDeliverer creates a ResultDeliverer that publishes task
// updates to Kafka via CQRS commands. Uses the passthrough content generator
// — the agent framework's StepRunner already produces the full marshaled
// task in result.Output; the deliverer publishes it as-is and overrides
// status/phase frontmatter based on the result Status.
func CreateKafkaResultDeliverer(
	syncProducer libkafka.SyncProducer,
	topicPrefix base.TopicPrefix,
	taskID agentlib.TaskIdentifier,
	originalContent string,
	currentDateTime libtime.CurrentDateTimeGetter,
) agentlib.ResultDeliverer {
	return delivery.NewKafkaResultDeliverer(
		syncProducer,
		topicPrefix,
		taskID,
		originalContent,
		delivery.NewPassthroughContentGenerator(),
		currentDateTime,
	)
}

// CreateFileResultDeliverer creates a ResultDeliverer that writes the agent's
// output back to a markdown file (local CLI mode). Uses the passthrough
// content generator (same rationale as Kafka).
func CreateFileResultDeliverer(filePath string) agentlib.ResultDeliverer {
	return delivery.NewFileResultDeliverer(
		delivery.NewPassthroughContentGenerator(),
		filePath,
	)
}

// CreateAgent assembles the 2-phase Sentry analyzer agent:
//
//   - planning  → claude.NewAgentStep (planning prompt + MCP tools): fetch
//     LIVE state for the single alert, read implicated source (read-only),
//     write ## Analysis
//   - execution → claude.NewAgentStep (execution prompt): apply the 6-verdict
//     rubric + noise disqualifiers, write ## Verdict back to the task body
//
// No ai_review phase — write-verification is part of execution (two-active-phase
// pattern, per the spec). The collector creates one task per new Sentry alert;
// this agent processes exactly one task per run.
func CreateAgent(
	claudeConfigDir claudelib.ClaudeConfigDir,
	agentDir claudelib.AgentDir,
	allowedTools claudelib.AllowedTools,
	model claudelib.ClaudeModel,
	claudeEnv map[string]string,
	envContext map[string]string,
) *agentlib.Agent {
	return CreateAgentFromRunner(
		CreateClaudeRunner(claudeConfigDir, agentDir, allowedTools, model, claudeEnv),
		envContext,
	)
}

// CreateAgentFromRunner builds the triage agent given a pre-constructed
// ClaudeRunner. Used by CreateAgentProvider to share one runner across the
// domain agent and the healthcheck-Claude liveness agent.
//
// The triage execution step is wrapped in the real-bug reassign trigger: on a
// `verdict: real bug` it reassigns the SAME task to sentry-analyzer-agent with
// task_type: sentry-deep-analyzer instead of closing it.
func CreateAgentFromRunner(
	runner claudelib.ClaudeRunner,
	envContext map[string]string,
) *agentlib.Agent {
	planning := steps.NewPlanningStep(runner, prompts.BuildPlanningInstructions(), envContext)
	execution := steps.NewReassignExecutionStep(
		steps.NewExecutionStep(runner, prompts.BuildExecutionInstructions(), envContext),
		assigneeSentryAnalyzerAgent,
		string(taskTypeSentryDeepAnalyzer),
	)
	return agentlib.NewAgent(
		agentlib.NewPhase("planning", planning),
		agentlib.NewPhase(domain.TaskPhaseExecution, execution),
	)
}

// CreateDeepAgentFromRunner builds the deep analyzer agent given a
// pre-constructed ClaudeRunner. The deep agent is a separate task type
// (sentry-deep-analyzer) with its own octopus verdict schema and deep
// planning/execution prompts — it runs on ONE task flagged `real bug` by the
// triage agent, never batch.
func CreateDeepAgentFromRunner(
	runner claudelib.ClaudeRunner,
	envContext map[string]string,
) *agentlib.Agent {
	planning := steps.NewDeepPlanningStep(
		runner,
		prompts.BuildDeepPlanningInstructions(),
		envContext,
	)
	execution := steps.NewDeepExecutionStep(
		runner,
		prompts.BuildDeepExecutionInstructions(),
		envContext,
	)
	return agentlib.NewAgent(
		agentlib.NewPhase("planning", planning),
		agentlib.NewPhase(domain.TaskPhaseExecution, execution),
	)
}

// CreateCollectorAgentFromRunner builds the collector agent given a
// pre-constructed ClaudeRunner. The collector has a single planning phase — it
// fetches the day's active unresolved Sentry alerts and publishes one
// per-alert task per (short-id, date) so the triage agent processes each. It
// replaces the retired standalone sentry-watcher service.
func CreateCollectorAgentFromRunner(
	runner claudelib.ClaudeRunner,
	envContext map[string]string,
) *agentlib.Agent {
	planning := steps.NewCollectorPlanningStep(
		runner,
		prompts.BuildCollectorPlanningInstructions(),
		envContext,
	)
	return agentlib.NewAgent(
		agentlib.NewPhase("planning", planning),
	)
}

// CreateAgentProvider wires the per-task-type dispatch table for agent-sentry-issue-analyzer.
// Returns lib.AgentProvider — main.go calls Get(ctx, taskType) to select the
// appropriate *Agent. Pure plumbing; no conditional, no error.
//
// taskTypeSentryIssueAnalyzer and TaskTypeLLM (legacy alias) both route to the
// triage agent. taskTypeSentryDeepAnalyzer routes to the deep analyzer (its
// own prompts + octopus verdict). taskTypeSentryCollector routes to the
// collector agent (single planning phase, fetches the day's alerts + publishes
// per-alert tasks). TaskTypeHealthcheck and TaskTypeOAuthProbe (transition
// alias) both route to the shared healthcheck-Claude liveness agent, reusing
// the same ClaudeRunner.
func CreateAgentProvider(
	claudeConfigDir claudelib.ClaudeConfigDir,
	agentDir claudelib.AgentDir,
	allowedTools claudelib.AllowedTools,
	model claudelib.ClaudeModel,
	claudeEnv map[string]string,
	envContext map[string]string,
) agentlib.AgentProvider {
	runner := CreateClaudeRunner(claudeConfigDir, agentDir, allowedTools, model, claudeEnv)
	domainAgent := CreateAgentFromRunner(runner, envContext)
	deepAgent := CreateDeepAgentFromRunner(runner, envContext)
	collectorAgent := CreateCollectorAgentFromRunner(runner, envContext)
	livenessAgent := healthcheck.NewAgent(healthcheck.NewClaudeStep(runner))
	return agentlib.NewAgentProvider(serviceName, map[agentlib.TaskType]*agentlib.Agent{
		taskTypeSentryIssueAnalyzer:  domainAgent,
		taskTypeSentryDeepAnalyzer:   deepAgent,
		taskTypeSentryCollector:      collectorAgent,
		agentlib.TaskTypeLLM:         domainAgent,
		agentlib.TaskTypeHealthcheck: livenessAgent,
		agentlib.TaskTypeOAuthProbe:  livenessAgent,
	})
}
