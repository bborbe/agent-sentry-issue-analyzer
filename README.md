# Agent Sentry Issue Analyzer

Analyzes Sentry issues and classifies them (severity + fixability) for the Sentry resolution pipeline. Consumes one `sentry-issue-analyzer` task per run — the sentry-watcher (separate component) creates one vault task per new Sentry alert; this agent is triggered per task and analyzes that single alert.

## Role

Planner in the Sentry resolution pipeline (see [[Agent Pipeline Concept]]). Consumes a task from Kafka via `agent-task-executor`, reads the alert (stack trace + `sentry_link`), fetches LIVE state, reads the implicated source (read-only), and writes the root-cause analysis + verdict back to the task body.

## Shape

Built on `bborbe/agent-claude` template — AI-heavy reference. Two active phases (planning → execution; no ai_review — write-verification is part of execution). The watcher creates tasks; the agent processes exactly one.

## Phases

| Phase | Step | Output |
|---|---|---|
| `planning` | Fetch LIVE Sentry state, read implicated source (read-only), root-cause analysis | `## Analysis` (file.go:line, root cause, certainty) |
| `execution` | Re-check LIVE state, apply 6-verdict rubric + noise disqualifiers | `## Verdict` YAML block |
| `done` | Terminal — verdict written back to task body | — |

## Build + Deploy

```bash
make precommit                 # lint + test
BRANCH=dev make buca           # build + upload + commit + apply k8s
kubectlquant -n dev apply -f k8s/agent-sentry-issue-analyzer-config.yaml
```

## SDK

Imports `github.com/bborbe/agent` (see [bborbe/agent](https://github.com/bborbe/agent)) for runtime contract.
