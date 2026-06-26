# Agent Sentry Issue Analyzer

Watches Sentry issues, classifies severity, and emits structured fix-prompts for downstream agents (analyser → fix → code-review → human-review pipeline).

## Role

Planner in the Sentry resolution pipeline (see [[Agent Pipeline Concept]]). Consumes `sentry-task` from Kafka via `agent-task-executor`, runs Claude over the stack trace + repo state, emits a structured spec/prompt for the next stage.

## Shape

Built on `bborbe/agent-claude` template — AI-heavy reference (same Claude Code step reused across all phases). See [[Quick-Launch New Agents]] for the scaffolding flow that produced this repo.

## Phases

| Phase | Step | Output |
|---|---|---|
| `planning` | Analyze stack trace, classify severity (critical / important / nit), inspect linked repos | Structured analysis (JSON) |
| `ai_review` | Verdict on fixability — is this safely auto-fixable, or human-only? | `{fixable: bool, reason: string}` |
| `done` | Emit `fix-task` prompt for downstream agent if `fixable=true`; else hand off to human | New task in pipeline |

## Build + Deploy

```bash
make precommit                 # lint + test
BRANCH=dev make buca           # build + upload + commit + apply k8s
kubectlquant -n dev apply -f k8s/agent-sentry-issue-analyzer-config.yaml
```

## SDK

Imports `github.com/bborbe/agent` (see [bborbe/agent](https://github.com/bborbe/agent)) for runtime contract.
