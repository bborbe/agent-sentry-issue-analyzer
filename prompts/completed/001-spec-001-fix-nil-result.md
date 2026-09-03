---
status: completed
spec: [001-sentry-collector-nil-result]
summary: Both agent entry points (main.go, cmd/run-task/main.go) now deliver a Failed result naming the phase when a phase has no runnable steps, returning nil (exit 0) instead of an error so the Job stops crash-retrying and the task reaches a terminal state; regression specs in both entry-point suites assert Status=Failed, phase name in message, and no error, with deliverNilResult at 100% coverage
execution_id: agent-sentry-nil-result-exec-001-spec-001-fix-nil-result
dark-factory-version: dev
created: "2026-09-03T19:30:00Z"
queued: "2026-09-03T19:45:50Z"
started: "2026-09-03T19:45:51Z"
completed: "2026-09-03T19:57:01Z"
branch: dark-factory/sentry-collector-nil-result
---

# Deliver a Failed result instead of erroring when a phase has no runnable steps

<summary>
- A phase whose steps are all skipped (or that has no registered steps) no longer crashes the Job — both entry points now deliver a `Failed` result to the task instead of returning an error
- The delivered result's message names the phase, so the failure is diagnosable in the task body
- The process exits 0 on this path, so the Job-controller stops re-creating the pod and the task reaches a terminal state (no retry loop)
- The Kafka entry point keeps recording `AgentStatusFailed` + duration metrics exactly as it does today
- The local-CLI entry point behaves identically and stays metrics-free
- The pushgateway deferred push (if configured) still fires on this path
- A transient deliver failure is wrapped and returned, so the controller still retries (bounded) instead of the error being swallowed
- Regression tests in both entry-point test suites assert the delivered status is `Failed`, the message contains the phase name, and no error is returned
- Non-nil results continue to route through result printing unchanged
</summary>

<objective>
Make a phase with no runnable steps a clean, terminal completion: both entry points deliver a `Failed` result naming the phase via the existing deliverer and return without error (exit 0), so the Job stops crash-retrying and the task reaches a terminal state. This mirrors the agent lib's `unsupportedPhase` pattern instead of the current error-return that drives the Job-controller retry loop (spec 001).
</objective>

<context>
Read `/home/node/.claude/CLAUDE.md` (YOLO container conventions) before making changes.

Read the coding plugin docs before implementing:
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-error-wrapping-guide.md` — all error wrapping uses `errors.Wrapf(ctx, err, ...)` from `github.com/bborbe/errors`; never `fmt.Errorf` in production code
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-testing-guide.md` — Ginkgo v2 + Gomega + Counterfeiter mocks at interface boundaries
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-doc-best-practices.md` — doc comment on the new method
- `/home/node/.claude/plugins/marketplaces/coding/docs/definition-of-done.md` — coverage rules (new code >= 80%)
- `/workspace/docs/dod.md` — project definition of done

Read these project files fully before changing them:
- `main.go` — the `application.Run` method (the nil-result branch is the `if result == nil {` block inside `Run`), the `createDeliverer` method, and the `application` struct (`Phase domain.TaskPhase` field)
- `cmd/run-task/main.go` — its `Run` method and identical nil-result branch
- `main_test.go` and `cmd/run-task/main_test.go` — the existing Ginkgo suite scaffolding you will extend

Out of scope: `cmd/create-tasks/main.go` never calls `agent.Run` (it publishes create-task commands from an alerts file), so it has no nil-result guard and needs no change — the spec's constraint fixes only the two agent-running entry points.

Canonical pattern to mirror — the agent lib's `unsupportedPhase` (source: `$(go env GOMODCACHE)/github.com/bborbe/agent@v0.84.1/agent_agent.go`, reproduced verbatim):

```go
// unsupportedPhase publishes a Failed result with a clear message.
func (a *Agent) unsupportedPhase(
	ctx context.Context,
	phaseName domain.TaskPhase,
	deliverer ResultDeliverer,
) (*Result, error) {
	display := string(phaseName)
	if display == "" {
		display = "(empty)"
	}
	result := &Result{
		Status:  AgentStatusFailed,
		Message: fmt.Sprintf("unsupported entry phase: %s", display),
	}
	if err := deliverer.DeliverResult(ctx, AgentResultInfo{
		Status:  result.Status,
		Message: result.Message,
	}); err != nil {
		return result, errors.Wrapf(ctx, err, "deliver unsupported-phase")
	}
	return result, nil
}
```

The delivery contract (source: `agent_result-deliverer.go` and `agent_status.go` in the same module, reproduced verbatim):

```go
// ResultDeliverer publishes an agent step result back to the task controller.
type ResultDeliverer interface {
	DeliverResult(ctx context.Context, result AgentResultInfo) error
}

type AgentResultInfo struct {
	Status         AgentStatus
	Output         string // body content (typically heading + fenced JSON)
	Message        string // human-readable status; used by failure/needs_input paths
	NextPhase      string
	ContinueToNext bool
}

const AgentStatusFailed AgentStatus = "failed"
```

Current nil-result branch in `main.go` (inside `Run`, after `agent.Run(...)`):

```go
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
```

Current nil-result branch in `cmd/run-task/main.go` (inside `Run`):

```go
	if result == nil {
		// agent.Run returns (nil, nil) when every step in the phase skipped
		// (ShouldRun=false, e.g. the phase's output section already exists from
		// a prior run) — treat as a no-op completion rather than panicking.
		return errors.Errorf(
			ctx,
			"agent run returned nil result (all steps skipped for phase %s)",
			a.Phase,
		)
	}
```
</context>

<requirements>
1. `main.go` — add the `"fmt"` standard-library import to the import block (the stdlib group currently contains `"context"`, `"os"`, `"time"`).

2. `main.go` — replace the nil-result branch shown in `<context>` with:

```go
	if result == nil {
		// agent.Run returns (nil, nil) when every step in the phase skipped
		// (ShouldRun=false, e.g. the phase's output section already exists from
		// a prior run) or the phase has no registered steps. Deliver a Failed
		// result so the task reaches a terminal state, then exit 0 — a no-op
		// completion, never a controller retry (mirrors agent-lib unsupportedPhase).
		jobMetrics.RecordRun(agentlib.AgentStatusFailed)
		jobMetrics.RecordDuration(time.Since(start))
		return a.deliverNilResult(ctx, deliverer)
	}
```

Keep the two `jobMetrics.RecordRun(...)` / `jobMetrics.RecordDuration(...)` calls exactly where they are — they must still record `AgentStatusFailed` + duration on this path. Do NOT touch the pushgateway `defer pusher.PushContext(ctx)` block at the top of `Run` — it is a `defer` and already fires on every return path, including this one; it must stay non-fatal (`Warningf` on error).

3. `main.go` — add this method to `application` (place it immediately after the `Run` method):

```go
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
		Status:  agentlib.AgentStatusFailed,
		Message: fmt.Sprintf("agent run returned nil result (all steps skipped for phase %s)", a.Phase),
	}
	if err := deliverer.DeliverResult(ctx, agentlib.AgentResultInfo{
		Status:  failedResult.Status,
		Message: failedResult.Message,
	}); err != nil {
		return errors.Wrapf(ctx, err, "deliver nil-result failure")
	}
	return nil
}
```

This uses only existing primitives: `agentlib.AgentStatusFailed`, `agentlib.Result` (fields `Status` + `Message`), `agentlib.AgentResultInfo`, the already-imported `errors` package (`github.com/bborbe/errors`), and the `fmt` package added in step 1. Keep the error-wrap message exactly `"deliver nil-result failure"` — the spec's Failure Modes table documents operators grepping Job logs for `deliver ... failure`.

4. `cmd/run-task/main.go` — add the `"fmt"` standard-library import to the import block (the stdlib group currently contains `"context"`, `"os"`).

5. `cmd/run-task/main.go` — replace the nil-result branch shown in `<context>` with:

```go
	if result == nil {
		// agent.Run returns (nil, nil) when every step in the phase skipped
		// (ShouldRun=false, e.g. the phase's output section already exists from
		// a prior run) or the phase has no registered steps. Deliver a Failed
		// result so the task reaches a terminal state, then exit 0 — a no-op
		// completion, never a controller retry (mirrors agent-lib unsupportedPhase).
		return a.deliverNilResult(ctx, deliverer)
	}
```

6. `cmd/run-task/main.go` — add the identical `deliverNilResult` method to `application` (immediately after `Run`), same body as step 3. This entry point has no metrics — the method carries none, exactly as written.

7. `main_test.go` — change the package declaration from `package main_test` to `package main`. This internal package is a deliberate, documented deviation from the external-test-package convention in `go-testing-guide.md` — required to reach the unexported `application.deliverNilResult` without a dependency-injection refactor of `Run`; no enabled linter rejects it. The existing suite scaffolding — `TestSuite`, the `//go:generate` line, and the `Compiles` spec — stays as-is. Add the imports: `"context"`, `agentlib "github.com/bborbe/agent"`, `agentmocks "github.com/bborbe/agent/mocks"`, `"github.com/bborbe/errors"`, `"github.com/bborbe/vault-cli/pkg/domain"`. Add this `Describe` block to the suite:

```go
var _ = Describe("application", func() {
	Describe("deliverNilResult", func() {
		var (
			ctx       context.Context
			deliverer *agentmocks.AgentResultDeliverer
		)

		BeforeEach(func() {
			ctx = context.Background()
			deliverer = &agentmocks.AgentResultDeliverer{}
		})

		It("delivers a Failed result naming the phase and returns nil", func() {
			app := &application{Phase: domain.TaskPhase("planning")}
			err := app.deliverNilResult(ctx, deliverer)
			Expect(err).NotTo(HaveOccurred())
			Expect(deliverer.DeliverResultCallCount()).To(Equal(1))
			_, info := deliverer.DeliverResultArgsForCall(0)
			Expect(info.Status).To(Equal(agentlib.AgentStatusFailed))
			Expect(info.Message).To(ContainSubstring("planning"))
		})

		It("wraps and returns a deliver failure", func() {
			deliverer.DeliverResultReturns(errors.New(ctx, "simulated deliver failure"))
			app := &application{Phase: domain.TaskPhase("planning")}
			err := app.deliverNilResult(ctx, deliverer)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("deliver nil-result failure"))
			Expect(deliverer.DeliverResultCallCount()).To(Equal(1))
		})
	})
})
```

`agentmocks.AgentResultDeliverer` is the counterfeiter fake shipped by `github.com/bborbe/agent/mocks` — the same external-mock pattern this repo already uses with `github.com/bborbe/kafka/mocks`. Do not add any `//counterfeiter:generate` directive; this repo has none and `make precommit`'s `generate` target only regenerates the doc-only root `mocks/mocks.go`.

8. `cmd/run-task/main_test.go` — apply the identical changes as step 7: package `main_test` → `main`, add the same imports, add the same `Describe("application")` / `deliverNilResult` block. The existing `TestSuite` scaffolding stays as-is.

9. Before finishing: run `make test` from the repo root (this repo has a single go.mod; `make test` runs `go test -mod=mod -cover ./...` across all packages, which includes both entry-point suites). It must pass. Then walk each spec acceptance criterion against the change:
   - `main.go` nil-result path delivers a `Failed` result via the deliverer whose message names the phase, and returns without error (exit 0)
   - `cmd/run-task/main.go` nil-result path behaves identically
   - Regression tests in `main_test.go` and `cmd/run-task/main_test.go` assert delivered `Status == Failed`, message contains the phase name, and no error returned
   - Job metrics (`AgentStatusFailed` + duration) still recorded in `main.go`; pushgateway deferred push not bypassed; `cmd/run-task` stays metrics-free
</requirements>

<constraints>
- Do NOT change the agent lib `github.com/bborbe/agent` — the fix lives in this repo's two entry points only
- Do not re-architect the phase model; the collector's `NextPhase: "done"` behavior stays as-is
- Keep the pushgateway push non-fatal (`Warningf`, never fail the Job on push error)
- Do NOT commit — dark-factory handles git
- Existing tests must still pass
- Non-nil results must continue to route through `jobMetrics.RecordRun(result.Status)` + `agentlib.PrintResult(ctx, result)` unchanged (spec Failure Modes, row 2)
- A failed deliver must return the wrapped error (Job exits non-zero, controller retries bounded) — never swallow it (spec Failure Modes, row 1)
- No operator-side steps belong in this prompt — the container agent has no operator
</constraints>

<verification>
Run `make test` from the repo root. Must exit 0.

Coverage of the changed code (new method must be fully covered by both the success and the error specs added in steps 7-8):
```
go test -mod=mod -coverprofile=/tmp/cover-main.out . && go tool cover -func=/tmp/cover-main.out
go test -mod=mod -coverprofile=/tmp/cover-runtask.out ./cmd/run-task && go tool cover -func=/tmp/cover-runtask.out
```
Confirm `deliverNilResult` shows 100% coverage in both packages.
</verification>
