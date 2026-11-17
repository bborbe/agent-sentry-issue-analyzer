---
status: completed
approved: "2026-11-11T22:09:12.001457977Z"
generating: "2026-11-11T22:15:24Z"
prompted: "2026-11-11T22:21:34Z"
verifying: "2026-11-11T22:35:42Z"
completed: "2026-11-11T22:35:47Z"
---

## Summary

Sentry issue `NUKE-PROD-C3` is a real bug in `bborbe/agent-sentry-issue-analyzer` at `scripts/sentry-create-tasks.sh:99`, filed automatically by the sentry-fix-agent from the High/High real bug deep verdict — that verdict is the approval gate, so this spec is approved rather than drafted.

## Problem

The sentry-heartbeat event is a deliberate level:error liveness probe (6-hourly emitter), but the poller producer scripts/sentry-create-tasks.sh:99 queries is:unresolved with no heartbeat exclusion, so the daily-refreshed synthetic issue files an analyzer task every day.

## Reproduction

The implicated source location from the deep verdict is `scripts/sentry-create-tasks.sh:99` in `bborbe/agent-sentry-issue-analyzer` (resolution rule: `candidate position 1 (bborbe/agent-sentry-issue-analyzer)`), Sentry issue `NUKE-PROD-C3`.

## Recommended Fix

Add a narrow exclusion for the exact sentry-heartbeat title (or source:sentry-heartbeat tag) in scripts/sentry-create-tasks.sh, mirroring sentry-alert-consumer v0.2.0, plus a fixture test asserting the heartbeat is not published while a normal alert still is.
