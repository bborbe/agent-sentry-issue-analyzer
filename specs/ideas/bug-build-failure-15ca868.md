---
status: idea
kind: bug
---

# Build Failure: bborbe/agent-sentry-issue-analyzer

Filed automatically by the build-fix agent for the CI episode `15ca86813e8dc15402737f28bec7ffb67b6e4709`.

## Summary

The default-branch build for `bborbe/agent-sentry-issue-analyzer` is failing; the build-fix diagnosis classified this as a code/test bug (verdict `file_spec`).

## Reproduction

Failing workflow(s): test

Episode SHA: `15ca86813e8dc15402737f28bec7ffb67b6e4709`

Log evidence:

```text
| Workflow | Job | Failed Step | Run |
|---|---|---|---|
| CI | test | Run precommit checks | [Run](https://github.com/bborbe/agent-sentry-issue-analyzer/actions/runs/32898250425) |
```

## Expected vs Actual

**Expected:** green CI on the default branch.
**Actual:** `Test file pkg/steps/watcher_test.go references undefined function prompts.BuildWatcherPlanningInstructions at lines 37, 61, and 80, causing a build failure - this is a code/test bug in the repo, not a dependency or vulnerability issue.`

## Why this is a bug

The default-branch build is the repository's quality gate; a red build blocks merges. Diagnosis: `Test file pkg/steps/watcher_test.go references undefined function prompts.BuildWatcherPlanningInstructions at lines 37, 61, and 80, causing a build failure - this is a code/test bug in the repo, not a dependency or vulnerability issue.`
