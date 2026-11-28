---
status: completed
created: "2026-11-28T10:26:03Z"
queued: "2026-11-28T10:26:03Z"
completed: "2026-11-28T10:32:53Z"
pr-url: https://github.com/bborbe/agent-sentry-issue-analyzer/pull/163
---

<summary>
- The agent's GitHub client now retries a rate-limit response instead of returning the error
- A 429 or a rate-limiting 403 from api.github.com is retried with the wait the response asks for, up to five times
- A wait longer than one second is deferred, so the caller sees the real rate-limit response promptly
- Each retry emits one log line naming the attempt, the status, the class and the wait
- The wrapping is a small testable function, because the client it wraps is built by an external package
</summary>

<objective>
Route this agent's GitHub client through the shared rate-limit-aware retry transport, so a throttled response delays the agent instead of ending its work item.
</objective>

<context>
Read CLAUDE.md for project conventions.

The only GitHub client in this repo is built at `main.go` in `buildGithubClient` (~line 396), which calls `githubapp.NewClient(ctx, githubapp.Config{...})` at ~402 and passes the result to `github.NewClient(github.WithHTTPClient(appHTTPClient))`. The import is `github.com/bborbe/maintainer/githubapp`.

⚠️ **`githubapp.NewClient` returns `&http.Client{Transport: transport}` — it sets no `Timeout`.** Verified against `maintainer@v0.51.2/githubapp/githubapp.go:69`. So there is no client deadline for a retry sleep to exceed on this row; the one-second bound below is chosen for responsiveness, not to stay inside a deadline. Do not add a `Timeout` here — that would be a behaviour change outside this task.

The transport it returns is a `ghinstallation` transport wrapping `http.DefaultTransport`, so the retry round tripper must sit **outside** it: each retry re-enters the auth transport, which refreshes the installation token as needed.

`github.com/bborbe/http` is pinned at v1.26.33 as an indirect dependency. Its `NewRoundTripperRetryWithBackoff(roundTripper, RetryBackoffOptions)` retries a 403/429 from the hosts in `RetryHosts`, sleeping the `Retry-After` duration (and the `X-RateLimit-Reset` delta for primary exhaustion), up to `MaxAttempts` (default 5), emitting `retry attempt=<n> status=<status> class=<class> wait=<duration>` per attempt.
</context>

<requirements>
1. In `main.go`, add one unexported helper that takes the retry hosts, so a test can point it at a stub server:

   ```go
   // wrapRetryTransport returns client with its transport wrapped in the shared
   // rate-limit retry round tripper. A nil transport becomes http.DefaultTransport
   // first, so the wrap cannot panic on the first request. MaxWait bounds one
   // wait, not the call: MaxAttempts (default 5) multiplies it, and one second
   // keeps the worst case short.
   func wrapRetryTransport(client *http.Client, hosts []string) *http.Client {
       if client == nil {
           client = &http.Client{}
       }
       base := client.Transport
       if base == nil {
           base = http.DefaultTransport
       }
       client.Transport = libhttp.NewRoundTripperRetryWithBackoff(
           base,
           libhttp.RetryBackoffOptions{
               RetryHosts: hosts,
               MaxWait:    1 * time.Second,
           },
       )
       return client
   }
   ```

   Import it as `libhttp "github.com/bborbe/http"`, adding `time` / `net/http` if missing.

2. In `buildGithubClient`, immediately after the `githubapp.NewClient` error check and before `github.NewClient(...)`, call `appHTTPClient = wrapRetryTransport(appHTTPClient, []string{"api.github.com"})`. Change nothing else in that function.

3. Do not add a `Timeout` to the client, do not change the `ghinstallation` transport, and do not alter the App-auth config or the early `nil` return when App credentials are absent.

4. Raise the dependency to the released tag: `go get github.com/bborbe/http@v1.28.1` then `go mod tidy`. **v1.28.1 is required** — v1.28.0 mislabels a 429 carrying positive `X-RateLimit-Remaining` with no `Retry-After` as `unclassified`, and that is the label the retry line carries. Confirm `go.mod` lists `github.com/bborbe/http v1.28.1` in the direct `require` block.

5. Add a test for `wrapRetryTransport` in a new `main_test.go` (or the existing main test file if one exists), in package `main`, using the same Ginkgo v2 / Gomega style as the repo. Pass `[]string{"127.0.0.1"}` as the hosts so the stub server is actually retried:
   a. **Retry fires.** Stub `httptest.Server` returns `429` with `Retry-After: 1` on the first request and `200` on the second; wrap a client with it; issue one GET; assert **exactly two requests** reached the server and no error.
   b. **Nil transport is safe.** Wrap `&http.Client{}` and assert the result's `Transport` is non-nil and is not `http.DefaultTransport` — a wrap that left a nil transport would panic on first use.
   c. **Total budget holds.** Stub returns `429` with `Retry-After: 4` every time; assert the request returns an error containing `429`, that **exactly one** request reached the server, and that elapsed time is under five seconds.

6. Update `CHANGELOG.md` with one `## Unreleased` bullet describing the retry wiring. If no `## Unreleased` section exists, create one directly above the newest released `## vX.Y.Z` heading.

7. Before finishing, re-run `<verification>` and confirm it passes; then walk each numbered requirement against the change and report any that is unmet.
</requirements>

<constraints>
- Do NOT commit — dark-factory handles git
- Existing tests must pass unchanged
- Only `main.go`, its test file, `go.mod`, `go.sum` and `CHANGELOG.md` change
- Do not add a `Timeout` to the client and do not change the auth transport
- Add no dependency beyond the `bborbe/http` bump
</constraints>

<verification>
Run `ROOTDIR=/workspace make precommit` -- must pass.
</verification>
