---
status: approved
approved: 2026-10-19T16:14:08.453702474Z
kind: bug
---

## Summary

Sentry issue `NUKE-PROD-CA` is a real bug in `bborbe/agent-sentry-issue-analyzer` at `github.com/bborbe/agent-sentry-issue-analyzer/pkg/fixagent/trace_reader.go:125`, filed automatically by the sentry-fix-agent from the High/High real bug deep verdict — that verdict is the approval gate, so this spec is approved rather than drafted.

## Problem

errors.Wrapf(ctx, ErrIssueNotFound, format, args...) in trace_reader.go:125 treats ErrIssueNotFound as the printf format string instead of the underlying %w cause, breaking the errors.Is chain that fix.go:202 depends on

## Reproduction

The implicated source location from the deep verdict is `github.com/bborbe/agent-sentry-issue-analyzer/pkg/fixagent/trace_reader.go:125` in `bborbe/agent-sentry-issue-analyzer` (resolution rule: `candidate position 1 (bborbe/agent-sentry-issue-analyzer)`), Sentry issue `NUKE-PROD-CA`.

## Recommended Fix

Replace errors.Wrapf(ctx, ErrIssueNotFound, ...) with errors.Join(ErrIssueNotFound, errors.Errorf(ctx, "sentry trace: fetch issue %s: not found", sentryIssueID)) — errors.Is traverses errors.Join chains and will find ErrIssueNotFound
