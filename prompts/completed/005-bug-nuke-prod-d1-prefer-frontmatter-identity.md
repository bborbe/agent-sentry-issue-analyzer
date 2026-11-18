---
status: completed
spec: [bug-nuke-prod-d1]
summary: Added identity.ResolveTaskIdentity to prefer the task's short_id frontmatter over a live Sentry lookup, wired it into the reassign and disqualifier-guard verdict paths, and added unit and step-level regression tests plus an Unreleased changelog entry.
execution_id: bcb42633-e0f4-5ab3-a54f-92cc21862955-exec-005-bug-nuke-prod-d1-prefer-frontmatter-identity
dark-factory-version: dev
created: "2026-11-14T14:30:00Z"
queued: "2026-11-14T14:21:53Z"
started: "2026-11-14T14:21:54Z"
completed: "2026-11-14T14:36:17Z"
---

# Prefer the task's frontmatter identity over a live Sentry lookup

<summary>
- A verdict now takes its Sentry identity from the task's own `short_id` frontmatter when that value is present and safe, instead of looking the model-written reference up in Sentry.
- A task that already knows which Sentry issue it is about can finish writing its verdict even when Sentry is unavailable.
- The lookup remains the fallback for tasks that carry no frontmatter identity, so manual and legacy tasks keep working.
- An unsafe or malformed frontmatter value is ignored and the lookup fallback validates the model's reference, so a hostile value still cannot reach the task file.
- Both verdict-writing paths — the triage reassign step and the deep disqualifier guard — use the same rule.
- A derived-key frontmatter identity is written verbatim and issues no lookup.
- The existing behaviour for tasks without `short_id`, for numeric model references, and for unresolvable identities is unchanged.
- Unit tests cover the frontmatter-present path, the fallback path, the invalid-frontmatter path, and both step wiring paths.
- The changelog records the fix under Unreleased.
</summary>

<objective>
Prefer the task's authoritative `short_id` frontmatter over a live lookup of the model-authored `sentry_issue_id`, so a verdict still writes the correct identity when Sentry is transiently unavailable. Fall back to the existing resolver for tasks that carry no frontmatter identity.
</objective>

<context>
Read the approved spec first: `specs/in-progress/bug-nuke-prod-d1.md` — Problem and Recommended Fix.

Read `docs/dod.md` for the project's Definition of Done.

This is prompt 2 of 2. Prompt 1 must already be applied: `pkg/identity/resolver.go` already has the bounded retry loop, the body capture, and `NewSentryHTTPClient`, and `pkg/factory/factory.go` already uses the timed client. Do not redo or revert those changes.

Files to read before changing anything:
- `pkg/identity/resolver.go` — the `Resolver` interface (`Resolve(ctx context.Context, reference string) (string, error)`), `validateReference(ctx, reference string) error`, and `IsDerivedKey`. The new helper lives in this package so it can call the unexported `validateReference`.
- `pkg/steps/reassign.go` — `(*reassignExecutionStep).finalizeVerdict` calls `s.identityResolver.Resolve(ctx, v.SentryIssueID)`. Read the `identityResolver` field comment and the comment above the call.
- `pkg/steps/disqualifier-guard.go` — `(*disqualifierGuardStep).Run` calls `s.identityResolver.Resolve(ctx, v.SentryIssueID)`. Read the `identityResolver` field comment and the comment above the call.
- `pkg/steps/jira-proposal.go` — the unexported helper `frontmatterString(md *agentlib.Markdown, key string) string`. It renders a generic frontmatter value through `%v`, so an unquoted numeric `short_id` parses as an int and a string assertion would miss it. Reuse this helper; do not add a second one.
- `cmd/create-tasks/main.go` — the task producer writes `"short_id": alert.ShortID` into the task frontmatter, where `alert.ShortID` is Sentry's `shortId`. That is the authoritative value this prompt prefers.
- `pkg/steps/fix.go` — `traceIssueID(md *agentlib.Markdown, verdictID string) string` is the existing precedent for preferring the task's `short_id` over a derived verdict id. Read its comment.
- `pkg/steps/deep_identity_test.go` — Ginkgo/Gomega style. `buildTask()` parses a deep task with no `short_id`; `echoDeepIdentityResolver`, `mappingIdentityResolver`, and `recordingDeliverer` are available.
- `pkg/steps/triage_identity_test.go` — Ginkgo/Gomega style. `buildTask()` parses a triage task with no `short_id`; `echoIdentityResolver` and `mappingIdentityResolver` are available.
- `pkg/identity/resolver_test.go` — Ginkgo/Gomega style; `newResolver` and `roundTripFunc` are available.

Library facts (verified in the repo):
- `agentlib.Markdown.Frontmatter` is `agentlib.TaskFrontmatter` (a map). Values are untyped, so read them with `frontmatterString`.
- `identity.Resolver` is the interface both step structs hold. `ResolveTaskIdentity` takes that interface, so the generated mock and the real resolver both satisfy it.
- `validateReference` rejects `/`, `?`, `\`, whitespace, newline, `:`, backticks, `.`, `..`, and any string containing `..`. It accepts real short ids, numeric ids, and both derived-key shapes. It is the safety gate for the frontmatter value too.

Coding guides (read in the YOLO container at these in-container paths):
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-error-wrapping-guide.md`
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-testing-guide.md`
- `/home/node/.claude/plugins/marketplaces/coding/docs/changelog-guide.md`

<!-- OPEN QUESTION for the human reviewer: the spec says "prefer the task's authoritative frontmatter identity" but does not name the key. This prompt uses `short_id` because `cmd/create-tasks` writes Sentry's shortId there and `traceIssueID` already treats it as authoritative. `issue_url` is deliberately not parsed. -->

<!-- OPEN QUESTION for the human reviewer: a frontmatter `short_id` is assumed already canonical (the collector copies Sentry's shortId). A numeric frontmatter value is therefore written verbatim rather than canonicalised, because canonicalising it would require the live lookup this fix exists to avoid. If numeric frontmatter ids must be canonicalised, that is a separate decision. -->
</context>

<requirements>
1. In `pkg/identity/resolver.go`, add this exported helper with a doc comment:
   ```go
   // ResolveTaskIdentity returns the canonical identity a verdict should carry.
   // The task's own frontmatter identity is authoritative: when it is a safe
   // bare reference it is returned without a lookup, so a Sentry outage cannot
   // abort a run that already knows the identity. Tasks that carry no
   // frontmatter identity fall back to resolver.Resolve.
   func ResolveTaskIdentity(
       ctx context.Context,
       resolver Resolver,
       frontmatterID string,
       reference string,
   ) (string, error) {
       if id := strings.TrimSpace(frontmatterID); id != "" {
           if err := validateReference(ctx, id); err == nil {
               return id, nil
           }
       }
       return resolver.Resolve(ctx, reference)
   }
   ```
   The contract is exact: a non-empty, safe `frontmatterID` is returned verbatim (including a derived key) with no resolver call; an empty or unsafe `frontmatterID` falls through to `resolver.Resolve(ctx, reference)`. `resolver.go` already imports `strings`, `context`, and `errors`; no new imports are needed.

2. In `pkg/steps/reassign.go`, in `finalizeVerdict`, replace
   ```go
   canonical, err := s.identityResolver.Resolve(ctx, v.SentryIssueID)
   ```
   with
   ```go
   canonical, err := identity.ResolveTaskIdentity(
       ctx,
       s.identityResolver,
       frontmatterString(md, "short_id"),
       v.SentryIssueID,
   )
   ```
   Keep the surrounding `errors.Wrapf(ctx, err, "reassign: resolve identity")`, the assignment to `v.SentryIssueID`, and the `verdict.RewriteIdentityLine` call unchanged. Update the comment above the call to state the frontmatter-first rule and why: the task's `short_id` is the identity the collector recorded from Sentry, so it is authoritative and avoids a live lookup; the resolver is the fallback for tasks that carry none.

3. In `pkg/steps/disqualifier-guard.go`, in `Run`, make the same replacement using `frontmatterString(md, "short_id")`, keeping the existing `errors.Wrapf(ctx, err, "disqualifier-guard: resolve identity")` wrapping and the rest of the sequence unchanged. Update the comment the same way.

4. Update the `identityResolver` field comments in both files (and the `New*` doc comments if they describe the resolver) so they say the resolver is the fallback used when the task frontmatter carries no safe identity, rather than the only source of the identity. Do not change the field name or the constructor signatures.

5. In `pkg/identity/resolver_test.go`, add a `Describe("ResolveTaskIdentity", ...)` block with a small local fake implementing `identity.Resolver` that counts calls and records the reference it was given. Cover:
   - a non-empty safe frontmatter id (`NUKE-DEV-G0`) → returned verbatim, fake called 0 times, even when the fake is configured to return an error;
   - a derived-key frontmatter id (`684522b30d7431dfb5371ef57d0fd431`) → returned verbatim, fake called 0 times;
   - a numeric frontmatter id (`7791255664`) → returned verbatim, fake called 0 times;
   - an empty frontmatter id → the fake's result is returned and the fake is called exactly once with `reference`;
   - an unsafe frontmatter id (`../../projects/other`) → the fake is called exactly once with `reference` and its result is returned;
   - an empty frontmatter id and a fake that returns an error → the error propagates.
   Add the `github.com/bborbe/errors` import if it is not already present.

6. In `pkg/steps/deep_identity_test.go`, add a regression test for the reported bug:
   - Add a task builder that parses frontmatter including `short_id: NUKE-DEV-G0`. Do not change the existing `buildTask()`; the existing tests depend on the no-`short_id` fallback path.
   - Configure the resolver to return an error (for example `errors.New(ctx, "identity: fetch issue NUKE-DEV-G0: unexpected status 500")`). The test asserts it is called zero times.
   - The model writes `sentry_issue_id: NUKE-DEV-G0`.
   - Assert `Run` returns no error, the `## Verdict` identity line is `sentry_issue_id: NUKE-DEV-G0`, and `ResolveCallCount()` is zero.
   - Add a second case where the frontmatter says `NUKE-DEV-G0` and the model wrote `7791255664`; assert the written identity is `NUKE-DEV-G0` and the resolver is never called. This proves the frontmatter wins over a different model spelling.

7. In `pkg/steps/triage_identity_test.go`, add the same two cases for the reassign path: frontmatter present and the resolver configured to error, and frontmatter wins over a different model value. Use the existing `buildStep` pattern and add a task builder with `short_id`.

8. Do not change the existing tests that build tasks without `short_id`; they exercise the fallback and must keep passing. In particular, keep both `It("fails the run and delivers nothing when the identity cannot be resolved")` tests as they are.

9. In `CHANGELOG.md`, add this bullet under `## Unreleased` (create the section directly above the newest `## vX.Y.Z` heading if it does not exist; append as the last bullet if it does, separated by one blank line):
   ```
   - fix: prefer the task's own `short_id` frontmatter over a live Sentry lookup of the model-written `sentry_issue_id`, so a verdict still writes the authoritative identity when Sentry is transiently unavailable
   ```
   Do not create a second `## Unreleased` heading and do not touch a released section.

10. Self-check before finishing: re-run every command in `<verification>` and confirm each passes; then walk the summary bullets against the diff — frontmatter present wins with no lookup, empty or unsafe falls back, derived key is verbatim, both call sites are wired, and the existing fallback tests are untouched.
</requirements>

<constraints>
- Do NOT commit — dark-factory handles git.
- Existing tests must still pass.
- Only `pkg/identity/resolver.go`, `pkg/identity/resolver_test.go`, `pkg/steps/reassign.go`, `pkg/steps/disqualifier-guard.go`, `pkg/steps/deep_identity_test.go`, `pkg/steps/triage_identity_test.go`, and `CHANGELOG.md` change.
- Do not change the `Resolver` interface, `NewSentryIdentityResolver`'s signature, or the generated mock.
- Do not change `pkg/steps/jira-proposal.go`; reuse its `frontmatterString` helper.
- Do not prefer or parse `issue_url`; `short_id` is the authoritative frontmatter key.
- Do not remove the resolver fallback; tasks without `short_id` must keep working exactly as they do today.
- Do not write an unsafe frontmatter value: an invalid frontmatter id must fall through to the resolver, which validates the model's reference.
- Do not change the derived-key, `RewriteIdentityLine`, disqualifier, analyzability-guard, or metrics sequence.
- Preserve the security properties: the frontmatter value is `validateReference`-checked before use, and the token never appears in an error or log line.
- Use `github.com/bborbe/errors` for any new error wrapping; none should be needed for the helper.
- No new scenario: the behaviour is fully exercisable with step-level unit tests.
- Do not run `go mod vendor`. Do not change `go.mod` / `go.sum` unless `make precommit` fails on a dependency vulnerability with a known fixed version; then bump only that module with `go get <pkg>@<fixed> && go mod tidy` and add a CHANGELOG bullet for it.
</constraints>

<verification>
Run from the repo root inside the YOLO container:

```bash
set -e
set -o pipefail
ROOTDIR=/workspace make precommit

# The shared helper exists and both call sites use it.
grep -q 'func ResolveTaskIdentity' pkg/identity/resolver.go
grep -q 'ResolveTaskIdentity' pkg/steps/reassign.go
grep -q 'ResolveTaskIdentity' pkg/steps/disqualifier-guard.go

# Both call sites read the authoritative frontmatter key.
grep -q '"short_id"' pkg/steps/reassign.go
grep -q '"short_id"' pkg/steps/disqualifier-guard.go

# The helper and both step wiring paths are covered by the new tests.
grep -q 'Describe("ResolveTaskIdentity"' pkg/identity/resolver_test.go
grep -q 'NUKE-DEV-G0' pkg/steps/deep_identity_test.go
grep -q 'NUKE-DEV-G0' pkg/steps/triage_identity_test.go
```
</verification>
