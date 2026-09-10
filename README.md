# Agent Sentry Issue Analyzer

Analyzes Sentry issues and classifies them (severity + fixability) for the Sentry resolution pipeline. Consumes one `sentry-issue-analyzer` task per run — the `sentry-collector` agent step (fleet's first multi-agent workflow: collector fans out, analyzer consumes) creates one vault task per active Sentry alert; this agent is triggered per task and analyzes that single alert.

## Role

Planner in the Sentry resolution pipeline (see [[Agent Pipeline Concept]]). Consumes a task from Kafka via `agent-task-executor`, reads the alert (stack trace + `sentry_link`), fetches LIVE state, reads the implicated source (read-only), and writes the root-cause analysis + verdict back to the task body.

## Shape

Built on `bborbe/agent-claude` template — AI-heavy reference. Two active phases (planning → execution; no ai_review — write-verification is part of execution). The watcher creates tasks; the agent processes exactly one.

## Phases

| Phase | Step | Output |
|---|---|---|
| `planning` | Fetch LIVE Sentry state, read implicated source (read-only), root-cause analysis | `## Analysis` (file.go:line, root cause, certainty) |
| `execution` | Re-check LIVE state, apply 7-verdict rubric + noise disqualifiers | `## Verdict` YAML block |
| `done` | Terminal — verdict written back to task body | — |

## Build + Deploy

This repo builds and uploads the image; it no longer owns cluster objects.

```bash
make precommit                 # lint + test
BRANCH=dev make buca           # build + upload + clean (no apply)
```

The `k8s/` directory was deleted on 2026-09-10 — every manifest it held described
dead quant leftovers. The live sentry agents (`sentry-analyzer-agent`,
`sentry-collector-agent`) run on nukedev/nukeprod and their Config CRs, Secrets,
PVCs and quotas are managed by the `nuke` repo (`agent/values-{dev,prod}.yaml`),
which adopted the sentry pipeline from the hand-applied kubectl CRs on 2026-08-26.

## SDK

Imports `github.com/bborbe/agent` (see [bborbe/agent](https://github.com/bborbe/agent)) for runtime contract.

## License

See [LICENSE](LICENSE).
