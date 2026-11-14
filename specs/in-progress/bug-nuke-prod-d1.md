---
status: approved
approved: 2026-11-14T11:01:10.660817109Z
kind: bug
---

## Summary

Sentry issue `NUKE-PROD-D1` is a real bug in `bborbe/agent-sentry-issue-analyzer` at `pkg/identity/resolver.go:152`, filed automatically by the sentry-fix-agent from the High/High real bug deep verdict — that verdict is the approval gate, so this spec is approved rather than drafted.

## Problem

(*sentryIdentityResolver).Resolve treats any non-200 as fatal (pkg/identity/resolver.go:152); a transient Sentry 500 on the model-authored reference NUKE-DEV-G0 aborts the deep run after analysis but before the verdict is written, with no retry or 4xx/5xx split.

## Reproduction

The implicated source location from the deep verdict is `pkg/identity/resolver.go:152` in `bborbe/agent-sentry-issue-analyzer` (resolution rule: `candidate position 1 (bborbe/agent-sentry-issue-analyzer)`), Sentry issue `NUKE-PROD-D1`.

## Recommended Fix

Split 404 (fail-loud) from 429/5xx (bounded retry with backoff), prefer the task's authoritative frontmatter identity over a live lookup of the model-written string, capture the non-200 response body, and add an HTTP client timeout.
