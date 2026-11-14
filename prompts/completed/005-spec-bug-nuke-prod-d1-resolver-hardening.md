---
status: completed
spec: [bug-nuke-prod-d1]
summary: 'Hardened the Sentry identity resolver: 404 fails loudly with no retry, 429/5xx get two bounded backoff retries with body capture, and every lookup uses a timeout-carrying client without mutating the caller''s client.'
execution_id: bcb42633-e0f4-5ab3-a54f-92cc21862955-exec-005-spec-bug-nuke-prod-d1-resolver-hardening
dark-factory-version: dev
created: "2026-11-14T12:00:00Z"
queued: "2026-11-14T11:23:00Z"
started: "2026-11-14T11:23:01Z"
completed: "2026-11-14T11:40:42Z"
---

# Harden the Sentry identity resolver: 404 vs 429/5xx, bounded retry, body capture, timeout

<summary>
- A single transient Sentry 500 no longer kills a deep run after analysis but before its verdict is written.
- A 404 still fails loudly and immediately, with no retry: a missing issue is a fact about the issue, not the attempt.
- 429 and 5xx responses are retried a bounded number of times with exponential backoff, and the retry wait honors context cancellation.
- A failed lookup now carries the non-200 response body (truncated) in its error, so the cause is visible without a redeploy.
- Every identity lookup runs through an HTTP client with a timeout, even when the caller hands the resolver a client that has none; the caller's client is never mutated.
- Derived keys, reference validation, path escaping, canonical short-id validation and token secrecy behave exactly as they do today.
- Existing tests keep passing; new tests pin the retry split, the retry count, the body capture and the timeout.

</summary>

<objective>
Make `(*sentryIdentityResolver).Resolve` in `pkg/identity/resolver.go` distinguish a deterministic 404 from a transient 429/5xx, retry only the latter a bounded number of times with backoff, capture the non-200 response body in the error, and guarantee a non-zero HTTP client timeout. The identity resolution rule itself does not change: the canonical value is still the `shortId` Sentry returns, and a resolution failure still fails the run.
</objective>

<context>
Read the approved spec first: `specs/in-progress/bug-nuke-prod-d1.md`. The bug is Sentry issue `NUKE-PROD-D1`: a transient 500 on an identity lookup aborted a deep run after analysis but before the verdict was written.

Read these files before changing anything:
- `pkg/identity/resolver.go` — the whole file. Pay attention to:
  - `func NewSentryIdentityResolver(client *http.Client, baseURL string, org string, token string) Resolver`
  - `type sentryIdentityResolver struct { client *http.Client; baseURL string; org string; token string }`
  - `func (r *sentryIdentityResolver) Resolve(ctx context.Context, reference string) (string, error)` — the non-200 check is the current `if resp.StatusCode != http.StatusOK` block.
  - `validateReference(ctx context.Context, reference string) error` and `IsDerivedKey(reference string) bool` — reuse them unchanged.
- `pkg/identity/resolver_test.go` — the existing Ginkgo suite. The `DescribeTable("fails loudly on a non-200 response", ...)` table currently includes `http.StatusTooManyRequests`; that entry must move to the retry tests because 429 is now retried.
- `pkg/identity/resolver_suite_test.go` — the Ginkgo bootstrap (`package identity_test`).
- `pkg/factory/factory.go` — the three `identity.NewSentryIdentityResolver(...)` call sites (inside `CreateAgent` and twice inside `CreateAgentProvider`). The constructor signature does NOT change, so no factory edit is expected. Confirm this rather than assuming it.
- `pkg/fixagent/trace_reader.go` — the project's existing example of splitting 404 from every other non-200 (`ErrIssueNotFound`). Do not copy its sentinel; the identity resolver does not need one. Use it only for the shape of the status split and the `errors.Wrapf` idiom.

Coding guides (read them in the YOLO container at these in-container paths):
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-error-wrapping-guide.md`
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-context-cancellation-in-loops.md`
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-testing-guide.md`
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-precommit.md`

Project conventions: `docs/dod.md`. There is no `CLAUDE.md` in this repo.

Verified library facts:
- `github.com/bborbe/errors` is imported as `errors` and provides `errors.Errorf(ctx, format, args...)`, `errors.Wrapf(ctx, err, format, args...)`, `errors.Wrap(ctx, err, msg)` and `errors.New(ctx, msg)`. Use those, not `fmt.Errorf`.
- The current resolver imports `context`, `encoding/json`, `net/http`, `net/url`, `regexp`, `strings`, `github.com/bborbe/errors`, `github.com/golang/glog`. This prompt adds `io` and `time`.
- `http.Client.Timeout` is a `time.Duration`; zero means no timeout.
- `http.DefaultClient` is a shared pointer. Do not assign to `http.DefaultClient.Timeout`.
</context>

<requirements>
1. In `pkg/identity/resolver.go`, add these unexported constants near the top of the file (after `DefaultSentryOrg`):
   ```go
   const (
       // defaultHTTPClientTimeout bounds one identity lookup, including connect,
       // redirects and body read, when the caller supplies no timeout of its own.
       defaultHTTPClientTimeout = 10 * time.Second
       // retryBaseDelay is the first backoff wait; each subsequent retry doubles it.
       retryBaseDelay = 250 * time.Millisecond
       // errorBodyLimit caps how much of a non-200 body reaches the error.
       errorBodyLimit = 512
   )
   ```

2. Add a `retryDelays []time.Duration` field to `sentryIdentityResolver`. It holds the waits between attempts, so the number of retries is `len(retryDelays)`. The constructor sets it to `[]time.Duration{retryBaseDelay, 2 * retryBaseDelay}` (two retries, three attempts total). A test may construct the struct with a shorter slice to keep the suite fast.

3. In `NewSentryIdentityResolver`, keep the existing `baseURL`/`org` fallbacks and change only the client handling:
   - `client == nil` → `client = &http.Client{Timeout: defaultHTTPClientTimeout}`.
   - `client.Timeout == 0` → shallow-copy the client (`cloned := *client`), set `cloned.Timeout = defaultHTTPClientTimeout`, and use `&cloned`. Do NOT mutate the caller's client and do NOT assign to `http.DefaultClient`.
   - `client.Timeout != 0` → use the client as given, preserving the caller's explicit timeout.
   Then construct `&sentryIdentityResolver{client: client, baseURL: baseURL, org: org, token: token, retryDelays: []time.Duration{retryBaseDelay, 2 * retryBaseDelay}}`.

4. Keep the top of `Resolve` unchanged: the derived-key early return (validated first, then returned verbatim with no request) and `validateReference(ctx, reference)` for every other reference. Replace only the request/response block with a call to a new method:
   ```go
   issue, err := r.fetchIssue(ctx, reference)
   if err != nil {
       return "", err
   }
   return issue.ShortID, nil
   ```
   The short-id validation of `issue.ShortID` moves into `fetchOnce` (step 6) so the successful path keeps rejecting an unsafe resolved identity exactly as it does today.

5. Add `func (r *sentryIdentityResolver) fetchIssue(ctx context.Context, reference string) (sentryIssue, error)`:
   - Build `requestURL` exactly as today: `r.baseURL + "/organizations/" + url.PathEscape(r.org) + "/issues/" + url.PathEscape(reference) + "/"`.
   - Loop `for attempt := 0; attempt <= len(r.retryDelays); attempt++`.
   - Before every attempt after the first, wait `r.retryDelays[attempt-1]` through `waitForRetry(ctx, delay)`; on error return `errors.Wrapf(ctx, err, "identity: retry wait for %s", reference)`.
   - Call `r.fetchOnce(ctx, reference, requestURL)`.
   - On `err == nil`, return the issue.
   - On `retryable == false`, return the error immediately.
   - On `retryable == true`, remember the error, log one line at `glog.V(1).Infof("identity: retrying issue %s after error: %v", reference, err)`, and continue.
   - After the loop return the last error. The loop runs at least once, so the last error is never nil on this path.

6. Add `func (r *sentryIdentityResolver) fetchOnce(ctx context.Context, reference string, requestURL string) (sentryIssue, bool, error)`:
   - Build the request with `http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)`; on error return `sentryIssue{}, false, errors.Wrapf(ctx, err, "identity: build request %s", reference)`.
   - Set `req.Header.Set("Authorization", "Bearer "+r.token)`.
   - `resp, err := r.client.Do(req)`; on error return `sentryIssue{}, false, errors.Wrapf(ctx, err, "identity: fetch issue %s", reference)`. Transport errors are not retried — the spec scopes retry to 429/5xx.
   - `defer func() { _ = resp.Body.Close() }()`.
   - Status handling with a `switch`:
     - `http.StatusOK`: fall through to decoding.
     - `http.StatusNotFound`: return `sentryIssue{}, false, errors.Errorf(ctx, "identity: fetch issue %s: not found (status %d) body=%q", reference, resp.StatusCode, readErrorBody(resp.Body))`. This is the fail-loud, no-retry case; the body is still captured.
     - `resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= http.StatusInternalServerError`: return `sentryIssue{}, true, errors.Errorf(ctx, "identity: fetch issue %s: status %d body=%q", reference, resp.StatusCode, readErrorBody(resp.Body))`.
     - default: return `sentryIssue{}, false, errors.Errorf(ctx, "identity: fetch issue %s: unexpected status %d body=%q", reference, resp.StatusCode, readErrorBody(resp.Body))`.
   - On 200, keep the existing audit line `glog.V(2).Infof("identity: fetched issue %s status=%d", reference, resp.StatusCode)` (token deliberately absent), then decode `sentryIssue` with `json.NewDecoder(resp.Body).Decode`, reject an empty `ShortID`, and call `validateReference(ctx, issue.ShortID)` with the existing error message `"identity: issue %s: resolved short id %q is not a safe identity"`. Return `sentryIssue{}, false, ...` on each of those failures: a 200 with a bad body is not retried.

7. Add these two helpers in the same file:
   ```go
   // waitForRetry sleeps for delay or returns early when ctx is cancelled.
   func waitForRetry(ctx context.Context, delay time.Duration) error {
       timer := time.NewTimer(delay)
       defer timer.Stop()
       select {
       case <-ctx.Done():
           return ctx.Err()
       case <-timer.C:
           return nil
       }
   }

   // readErrorBody returns at most errorBodyLimit bytes of a non-200 body,
   // trimmed. A read error yields the empty string; the status is still reported.
   func readErrorBody(body io.Reader) string {
       data, err := io.ReadAll(io.LimitReader(body, errorBodyLimit))
       if err != nil {
           return ""
       }
       return strings.TrimSpace(string(data))
   }
   ```

8. Update the `Resolve` doc comment and the `sentryIdentityResolver` doc comments to state the new rule: a 404 fails loudly with no retry, 429/5xx are retried with bounded backoff, every other non-200 fails loudly, and the client carries a timeout. Do not claim the token is logged anywhere; it is not.

9. Update `pkg/identity/resolver_test.go`:
   - Change `DescribeTable("fails loudly on a non-200 response", ...)` so it only covers non-retryable statuses: `Entry("bad request", http.StatusBadRequest)` and `Entry("not found", http.StatusNotFound)`. Assert `requests` is `1` for each entry (add that assertion inside the table body), and keep the existing assertions that the error names the reference and the status and that the result is empty.
   - Add an `It("captures the non-200 response body in the error", ...)`: the handler writes `{"detail":"sentry is down"}` with `http.StatusBadRequest`; assert the error contains `sentry is down`, `requests` is `1`, and the result is empty.
   - Leave every other existing test unchanged, including the derived-key zero-request tests, the token-leak test, the escaping tests and the nil-client test.

10. Add `pkg/identity/resolver_internal_test.go` with `package identity` (internal, so it can build the struct with zero retry delays and inspect the unexported client). Use the standard `testing` package, not Ginkgo. Include the project's copyright header. Cover:
    - A helper that starts an `httptest.Server`, counts requests, and returns `&sentryIdentityResolver{client: server.Client(), baseURL: server.URL, org: "bborbe", token: "test-token", retryDelays: []time.Duration{0, 0}}`.
    - A 404 is not retried: handler returns 404 with body `{"detail":"no such issue"}`, `Resolve` errors, attempts == 1, error contains `404` and `no such issue`.
    - A 400 is not retried: handler returns 400, `Resolve` errors, attempts == 1.
    - A 429 retries and then succeeds: handler returns 429 twice then `{"shortId":"NUKE-DEV-FB"}`; `Resolve` returns `NUKE-DEV-FB`, attempts == 3.
    - A 500 retries and then succeeds: handler returns 500 twice then `{"shortId":"NUKE-DEV-FB"}`; `Resolve` returns `NUKE-DEV-FB`, attempts == 3.
    - A persistent 500 exhausts the retries: handler always returns 500 with body `{"detail":"boom"}`; `Resolve` errors, attempts == 3, error contains `500` and `boom`.
    - `NewSentryIdentityResolver(&http.Client{}, "", "", "token")` yields a `*sentryIdentityResolver` whose `client.Timeout` equals `defaultHTTPClientTimeout`, and the caller's `*http.Client` still has `Timeout == 0` (not mutated).
    - `NewSentryIdentityResolver(&http.Client{Timeout: 3 * time.Second}, "", "", "token")` preserves `3 * time.Second`.
    - `NewSentryIdentityResolver(nil, "", "", "token")` yields a non-nil client with `Timeout == defaultHTTPClientTimeout`.
    - `waitForRetry` returns `context.Canceled` for an already-cancelled context and a one-hour delay (use `stderrors "errors"` and `stderrors.Is`).

11. Do not add an exported sentinel error, a config knob, a flag or an environment variable. The retry count and delays are internal constants. Do not change the `Resolver` interface or regenerate mocks.

12. Add a `## Unreleased` section to `CHANGELOG.md` if one does not already exist. Place it immediately after the preamble line `All notable changes to this project will be documented in this file.` and directly above the newest `## vX.Y.Z` heading (currently `## v0.25.5`). Add exactly one bullet for the resolver half of this fix:
   ```markdown
   - fix: keep a transient Sentry failure from aborting an identity lookup. `(*sentryIdentityResolver).Resolve` treated every non-200 as fatal, so a single 500 on `NUKE-PROD-D1` killed the deep run after analysis but before its verdict was written. A 404 still fails loudly and is never retried; 429 and 5xx now get two bounded backoff retries, the non-200 response body is captured in the error, and the resolver always carries a 10s HTTP client timeout.
   ```
   Do not create a second `## Unreleased` heading and do not modify any released section. If prompt 2 has already added an `## Unreleased` section and a bullet, append this bullet to that section instead of creating a new one.
</requirements>

<constraints>
- Do NOT commit — dark-factory handles git.
- Existing tests must still pass. Update only the one non-200 table entry that now retries; do not delete the rest of the suite.
- The `Resolver` interface (`Resolve(ctx context.Context, reference string) (string, error)`) is unchanged.
- `NewSentryIdentityResolver(client *http.Client, baseURL string, org string, token string) Resolver` keeps its exact signature. No factory call site changes.
- Derived keys (`IsDerivedKey`) still return verbatim with zero HTTP requests.
- Reference validation, `url.PathEscape` on both segments, the `shortId` empty check, the safe-identity check on the resolved short id, and the token-never-in-errors property are unchanged.
- Only 404 is treated as a deterministic not-found. Only 429 and 5xx are retried. 400/401/403 and every other non-200 fail immediately.
- Transport errors (`client.Do` returning an error) are not retried; they fail loudly on the first attempt.
- Do NOT mutate `http.DefaultClient` or any caller-supplied client. Copy before setting a timeout.
- Only `pkg/identity/resolver.go`, `pkg/identity/resolver_test.go`, `pkg/identity/resolver_internal_test.go` (new) and `CHANGELOG.md` change. Do NOT change `pkg/factory/factory.go`, `pkg/fixagent/`, `pkg/steps/` or the prompts. This prompt is the resolver only.
- Do not run `go mod vendor`; do not change `go.mod` / `go.sum` unless `make precommit` fails on a dependency vulnerability with a known fixed version.
- Use `github.com/bborbe/errors`, never `fmt.Errorf` / `github.com/pkg/errors`.
- Add the copyright header to every new file.

Open question for the reviewer (do not block on it): the spec does not name a retry count or backoff schedule. This prompt picks two retries at 250ms and 500ms (three attempts, 750ms total). Adjust the constants in step 1 if the lane needs a longer window; the tests only assert attempt counts, not wall-clock duration.
</constraints>

<verification>
Run from the repo root. `ROOTDIR=/workspace` is required because the worktree workflow masks `.git` and `Makefile.variables` otherwise cannot derive the root.

```bash
set -e
set -o pipefail
ROOTDIR=/workspace make precommit

# The status split exists.
grep -q 'http.StatusNotFound' pkg/identity/resolver.go
grep -q 'http.StatusTooManyRequests' pkg/identity/resolver.go
grep -q 'http.StatusInternalServerError' pkg/identity/resolver.go

# Retry, body capture and timeout are present.
grep -q 'retryDelays' pkg/identity/resolver.go
grep -q 'func waitForRetry' pkg/identity/resolver.go
grep -q 'func readErrorBody' pkg/identity/resolver.go
grep -q 'defaultHTTPClientTimeout' pkg/identity/resolver.go

# The new internal test exists and covers the constructor timeout.
test -f pkg/identity/resolver_internal_test.go
grep -q 'defaultHTTPClientTimeout' pkg/identity/resolver_internal_test.go

# The 429 entry no longer sits in the no-retry table.
! grep -q 'rate limited' pkg/identity/resolver_test.go

# The changelog records the resolver half.
grep -q 'keep a transient Sentry failure from aborting an identity lookup' CHANGELOG.md
```
</verification>
