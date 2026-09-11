# Changelog

All notable changes to this project will be documented in this file.

## Unreleased

- fix: the collector reports a per-alert task creation count **observed after** publishing instead of echoing the fetched count. `scripts/sentry-create-tasks.sh` no longer `exec`s `/create-tasks` — it regains a return path, captures the publish result, then polls the vault (`scripts/vault-list.sh`, new — git-rest `?glob=`) for the per-alert task files that actually landed, and emits one machine-readable line: `sentry-create-tasks-result: fetched=<N> published=<n> expected_new=<k> landed=<m> status=<done|failed>`. `cmd/create-tasks` now prints `create-tasks-result: published= failed= total=` on stdout (previously the count existed only as a `glog.V(2)` line). A new wrapper step in `pkg/steps/collector.go` parses that line out of `## Analysis` and returns `AgentStatusFailed` with **no** `NextPhase` when new alerts were expected but zero task files landed — the controller owns unassign + `## Failure`. Before this, `NextPhase: "done"` was unconditional and the `## Analysis` section's mere presence terminated the task, which is why the 2026-08-26/27 outage reported success for ~20 hours while zero per-alert tasks landed. Dedup stays the designed idempotency: zero created with zero new alerts expected (a quiet day) is still `done`, so the gate does not false-alarm. The rule is applied in Go, not read from the script's `status=` token, so a bug in the script's wording cannot re-open the false-green path. `pkg/preflight` now requires `Bash(scripts/vault-list.sh:*)` + `GIT_REST_URL` for the collector step (GIT_REST_URL is mandatory — a run that cannot observe its own creation phase is the defect), and `main.go` forwards `GIT_REST_URL`/`GATEWAY_SECRET` into the subprocess env (`buildSubprocessEnv` strips non-allowlisted vars). `collector-planning.md` Step 4 no longer asks the model to compute the created count; it copies the script's line verbatim.
- fix: the collector's vault observation no longer masks its own failure. The first dev e2e run exposed it — `vault-list.sh` was called under `|| true`, so an auth/network failure was indistinguishable from an observed zero: the script reported a confident `landed=0` and the gate fired a **false failure** on an otherwise healthy run. Both call sites now check the exit code, `vault-list.sh` uses `curl -sfS` so errors surface instead of vanishing, the result line gains `observed=<true|false>` (and `status` gains `unobserved`), and `pkg/steps/collector.go` fails the run on `observed=false` with its own distinct message. `observed=false` is not an observed zero — a run that could not read the vault proves nothing and must not report success, or an auth failure becomes a silent green. Found by running the gate on dev: the mechanism worked, the observation lied.

## v0.14.0

- feat: wire the sentry-fix agent (task_type sentry-fix, assignee sentry-fix-agent): the fix step resolves the repo via the prompt-backed resolver, confirms freshness, and files a kind: bug spec through the GitHub API — stale citations file nothing and record why, out-of-scope repos fail loudly, emission is idempotent per Sentry issue id
- test: widen the fix-handoff gate table to five distinct High/High verdicts — the positive class previously rested on one hand-picked fixture, so a gate keyed on that fixture's incidental content (issue id, root cause, file:line) would have passed; each new entry varies all three
- feat: add the sentry-fix kind: bug spec builder (verdict words verbatim, validated by dark-factory spec.Load) and the GitHub Contents API writer (traversal-safe, REPO_ALLOWLIST-bounded, idempotent per Sentry issue id, filed on a non-default branch)
- fix: align the `fix-planning.md` candidate list with `planning.md` / `deep-planning.md` — the block must match across all three prompt files, and `docs/repo-mapping.md` now states the real invariant (same text and order, differing only in list indentation) instead of claiming byte-identity the nested copy cannot satisfy
- feat: add the sentry-fix resolution prompt and an injectable prompt-backed repo resolver — file:line to repo runs the deep analyzer's resolution procedure with a freshness check at the current revision
- feat: promote a High/High real-bug deep verdict to the fix agent (assignee `sentry-fix-agent`, task_type `sentry-fix`, phase planning) instead of completing the task — the deep-to-fix handoff (`NewFixHandoffStep`) is wired outside the disqualifier guard so a guard-forced `real bug` still reaches the fix agent, keyed idempotent on the current assignee, and every other verdict completes exactly as before

## v0.13.4

- fix: pin counterfeiter at the point of use in both //go:generate directives (go run github.com/maxbrunsfeld/counterfeiter/v6@v6.12.2, no -mod=mod) and delete the superseded tools/tools.go so go run builds the tool in a temporary module — counterfeiter leaves go.mod/go.sum, make precommit becomes idempotent with respect to the module files, and the nine-dependency churn on every gate run ends

## v0.13.3

- fix: derived-key `needs_input` is an envelope status inside the `## Verdict` block, never a `## Failure` section — prod E2E (2026-09-11) showed the execution LLM still routing a thin-snapshot `needs_input` through `## Failure`, which reverts the task to planning and unassigns the analyzer

## v0.13.2

- fix: derived-key (no-ID) tasks deterministically emit an analysis + verdict (`unanalyzable` / `needs_input`) instead of a `## Failure` for a missing trace or unmapped project — E2E-observed inconsistency (one fixture escalated, the other emitted `unanalyzable` for the same structural situation)

## v0.13.1

- fix: delete the dead `k8s/` directory and drop the `apply` half of `make buca` — every manifest it held described quant leftovers with zero scheduled pods; the live sentry agents (`sentry-analyzer-agent`, `sentry-collector-agent`) run on nukedev/nukeprod and are managed by the `nuke` repo (adopted from the hand-applied kubectl CRs on 2026-08-26). `make buca` is now `build upload clean`; the image it produces is consumed by the nuke-managed agents.

## v0.13.0

- feat: no-ID (derived-key) path — classify Kafka-pipeline tasks with empty `issue_url` / derived `short_id` from the envelope snapshot instead of refusing with `## Failure`; enrichment upgrades to the live-state fetch when a numeric ID becomes available

## v0.12.2

- chore: bump github.com/bborbe/service to v1.10.14 — fixes the Sentry proxy guard so a service deployed without SENTRY_PROXY sends events direct via http.DefaultTransport instead of silently dropping all error reporting

## v0.12.1

- fix: the triage execution prompt (`pkg/prompts/execution.md`) now disallows the `unanalyzable` verdict whenever the planning-phase `## Analysis` resolved a repo from the frame path (`resolved from frame path <path>`) or cited an implicated first-party `file.go:line` — the planning resolution is authoritative and such an alert goes down the normal resolution path — and reclassifies `in_app=unknown` frames as first-party-eligible (Sentry's `in_app=None` is unclassified, not third-party evidence), so a frame whose path maps to a known `bborbe` repo can no longer be counted toward "every frame is `in_app=0`"
- fix: `scripts/sentry-read.sh` now labels every emitted frame with a truthful three-state `in_app` flag — `1` first-party / `0` explicitly third-party / `unknown` unclassified (`in_app=None` or absent in the Sentry payload is never collapsed into `0`) — so a downstream phase can no longer read "Sentry did not classify" as "Sentry says third-party", and the `root_cause_*` pointer's deepest-frame-overall fallback carries the chosen frame's actual flag (`root_cause_in_app=unknown` when unclassified). A first exception value with `stacktrace: null` now degrades to `stack_trace unavailable (no frames)` instead of the misleading `no exception entry`.
- test: lock the three-state `in_app` mapping and the `unanalyzable` guard in unit tests — `scripts/test-sentry-read.sh` gains the three-state fixture (issue 444, `in_app=None` → `in_app=unknown`, never collapsed into `0`), the all-unknown fallback fixture (issue 555, truthful `root_cause_in_app=unknown`), and the null-stacktrace fixture (issue 666, degrades to `no frames`), pins the issue-222 string-`"true"` schema drift to `in_app=unknown`, and `pkg/prompts/prompts_test.go` asserts the execution prompt's `resolved from frame path` disallow and its `in_app=unknown` not-third-party instruction

## v0.12.0

- feat: add the terminal `unanalyzable` triage verdict for traces that carry no first-party frame — either the alert has no exception entry at all (stack trace unavailable) or an exception is present but every frame is `in_app=0` (third-party library code) — emitted with `status: done` instead of the escalation the planning prompts previously instructed for an all-third-party trace, which cleared the task's assignee and handed a human exactly the non-information the agent had. `applyDisqualifiers` no longer force-flips an `unanalyzable` verdict to `real bug` / `confidence: high` when a disqualifier fires — a volume or sustained-span claim is not a claim about analysability — while still recording `disqualifiers_fired` as evidence. The deep-analysis vocabulary (`pkg/deepverdict`) is deliberately unchanged.

## v0.11.3

- chore: update `github.com/bborbe/agent` to v0.87.3 — picks up the `kafkaResultDeliverer.stampTargetVault` fix (spec 052): stub results (failed / needs_input / unsupported-phase) now carry `target_vault` echoed from the original task content, so the controller's routing guard skips them cleanly instead of scanning-and-dropping (fixes the `AgentControllerResultNotFound` alert on nukedev)

## v0.11.2

- fix: make the shared disqualifier override skip (not crash) when the verdict omits the live-state date fields, and require `first_seen`/`last_seen`/`sentry_status` in the deep-analysis verdict template. Observed live on NUKE-DEV-A4 after v0.11.1: the deep model's verdict YAML omitted `first_seen`/`last_seen` (the deep template only listed `live_event_count`), `applyDisqualifiers` unconditionally parsed the empty dates and errored (`parse first_seen ""`), failing every deep job and re-triggering the controller loop. The override now skips when either date is absent — the model's verdict stands, no crash — and the deep template now lists the three live-state fields so the code-side evaluator can compute Sustained span / Active burst / Regressed on the deep path. Ginkgo test pins the missing-dates case (no crash, verdict unchanged).

## v0.11.1

- fix: wrap the deep-analysis execution step in a disqualifier guard so a fired noise-to-real-bug disqualifier forces `real bug` even when the deep model's re-analysis overwrites the verdict. Observed on NUKE-DEV-A4: the triage step correctly forced real bug via sustained span (count 193 ≥ 100, ~300-day span), but the deep re-analysis then wrote `noise` with the same events/day prose arithmetic error the v0.10.1 fix removed from the triage path. The guard runs the shared `applyDisqualifiers` after the deep model writes `## Verdict` (the same code the reassign step uses on the triage path), rewrites the section on override, and records `disqualifiers_fired`. `CreateDeepAgentFromRunner` now takes a `CurrentDateTimeGetter`; Ginkgo tests pin the deep-path override (A4 sustained-span case → real bug) and the no-fire case (count 84 → stays noise).

## v0.11.0

- feat: `scripts/sentry-read.sh` now tags every emitted stack frame with an `in_app=<0|1>` first-party flag, emits the repo-relative Sentry `filename` (falling back to the `abs_path` basename only when no relative path exists) instead of a bare basename, and appends a `root_cause_*` pointer block naming the deepest first-party frame — falling back to the deepest frame overall with `root_cause_in_app=0` — so the analyzer can cite a repo `file:line` root cause without guessing repo names from basenames
- feat: planning and deep-planning prompts now instruct the analyzer to consume the `sentry-read.sh` `root_cause_*` pointer block directly for repo+file:line identification and to scope investigation to `in_app=1` first-party frames before considering `in_app=0` third-party/library frames

## v0.10.2

- fix: the v0.10.1 disqualifier-handoff wording in the execution prompt ("the arithmetic is done in code… a fired disqualifier overrides your verdict automatically") over-corrected the model into NOT writing the `## Verdict` YAML block — live runs emitted only the `{status,message,files}` output-format JSON with the verdict buried inside `message`, so `verdict.Parse` found no `verdict:` key, the reassign step skipped `applyDisqualifiers`, and the disqualifier evaluation never ran on live output (zero `disqualifiers_fired` evidence across the 2026-09-05 batch; A4 still showed the old "count 193 < 100" prose error). The prompt now states explicitly that the model MUST still write the complete `## Verdict` YAML block, including its signature `verdict:` classification — the block is what the binary evaluates — while the numeric thresholds remain code-decided.
- fix: repo-resolution in the analyzer planning prompts (`planning.md`, `deep-planning.md`) is frame-path-first with a per-project ordered candidate list, replacing the single canonical `nuke-dev`/`nuke-prod` -> `bborbe/nuke` mapping. `bborbe/nuke` is a Helm/YAML infrastructure repo holding no application source, so every application-code alert on those projects cloned it, found nothing, and escalated with "does not exist in bborbe/nuke" — 3 of the 6 failing per-alert tasks on 2026-09-05/06 had this single cause. The prompts now resolve the repo from the frame's own path whenever the trace carries one, fall back to the ordered candidates `bborbe/trading` (Python MT5 connector under `mt5/connector/`), `bborbe/kafka` (Go Sarama), then `bborbe/nuke` last and only for infrastructure-shaped frames, classify third-party frames (`rpyc` internals) as out of scope for resolution instead of hunting for them, drop the instruction to guess project-named variants like `nuke-dev` (a repo that does not exist), require the escalation to name every candidate tried, and require the analysis to state how the repo was resolved. `docs/repo-mapping.md` holds the candidate table plus the procedure for extending it.

## v0.10.1

- fix: compute the noise-to-real-bug disqualifiers in `pkg/verdict` instead of leaving the events/day arithmetic to the model in prose, which got it wrong and biased verdicts toward `noise` (measured 8/13 = 62% agreement on 2026-09-05; all five disagreements traced to this one defect). New `DisqualifierEvaluator` (Volume / Active burst / Regressed / Sustained span) computes the thresholds from the verdict's live-state fields (`live_event_count`, `first_seen`, `last_seen`, `sentry_status`); a fired disqualifier forces `real bug` and is recorded in `disqualifiers_fired`. The `Verdict` schema gains `first_seen` + `disqualifiers_fired`, the reassign step rewrites the `## Verdict` section on override, the execution prompt no longer asks the model to evaluate the numeric thresholds, and Ginkgo boundary tests pin the rate/count/burst/span edges.

## v0.10.0

- fix: repair the k8s apply path in `Makefile.k8s` — replace `teamvault-config-parser` with `teamvault-cli config parse`, and replace the `kubectlquant` pipe with an explicit `KUBECONFIG` + real `kubectl`. `kubectlquant` is a zsh function with no executable on PATH, so it was undefined inside the recipe's `bash -c` and `BRANCH=dev make buca` failed at apply. `--teamvault-config` is passed explicitly because teamvault-cli defaults to `seibert.json`, which does not resolve this agent's secrets. `TEAMVAULT` now defaults to `~/.config/teamvault-cli/config.json` (the previous default, `~/.teamvault.json`, does not exist).
- feat: add `applytest` target that renders manifests to stdout without touching the cluster, for verifying teamvault wiring before an apply. `Makefile.env`'s BRANCH guard now covers it.

## v0.9.5

- chore: update Go to 1.27.1 and github.com/bborbe/agent to v0.87.1, github.com/bborbe/kafka to v1.25.13, github.com/bborbe/maintainer to v0.50.6, github.com/bborbe/service to v1.10.12, github.com/bborbe/time to v1.27.13, github.com/bborbe/vault-cli to v0.122.2

## v0.9.4

- chore: update github.com/bborbe/agent to v0.86.0, github.com/bborbe/cqrs to v0.6.10, github.com/bborbe/errors to v1.6.0, github.com/bborbe/kafka to v1.25.11, github.com/bborbe/maintainer to v0.50.5, github.com/bborbe/sentry to v1.10.1, github.com/bborbe/service to v1.10.11, github.com/bborbe/time to v1.27.12, github.com/bborbe/vault-cli to v0.121.2, github.com/onsi/gomega to v1.43.0

## v0.9.3

- fix: the real-bug reassign now targets the live `sentry-analyzer-agent` Config CR instead of the deleted `sentry-deep-analyzer` one — `pkg/factory` passed a single task-type constant into both the `deepAssignee` and `deepTaskType` parameters of `NewReassignExecutionStep`, so every `verdict: real bug` since the 2026-08-26 4-CR→2-CR consolidation stamped an assignee that `agent-task-executor` cannot resolve, silently dropping the task (`skipped_unknown_assignee`) with no Job, no error, and no escalation. The assignee is now its own `assigneeSentryAnalyzerAgent` constant, `task_type` stays `sentry-deep-analyzer`, and a focused factory-level Ginkgo spec drives `CreateAgentFromRunner` end-to-end with a real-bug verdict so the two values cannot be re-conflated unnoticed (third occurrence of this bug class in this repo; the `create-tasks` sibling path was fixed twice).
- fix: drop `k8s/sentry-deep-analyzer-config.yaml` and `k8s/sentry-deep-analyzer-config-prod.yaml` — the `sentry-deep-analyzer` Config CR was retired on 2026-08-26 when the sentry pipeline consolidated from 4 Config CRs to 2, but both manifests stayed in the repo documenting the pre-consolidation routing as deliberate design, and `Makefile.k8s` globs every `*.yaml` under `k8s/`, so each `make buca` re-created the dead CR on the quant cluster. Deep analysis stays a task type on the surviving `sentry-analyzer-agent`; nothing replaces these files. The deep analyzer's dedicated GitHub App (`APP_ID 4710983` / `INSTALLATION_ID 156399284`) is intentionally left in place — retiring it and its TeamVault PEM is tracked separately.
- chore: stop tracking `.dark-factory.log` and `.dark-factory.lock`, and add both to `.gitignore` — the dark-factory daemon writes these runtime artifacts into the repo root and its own auto-commit step swept them into history (commit `3da27a2`), putting a stale lock file and a container log in the tree. Nothing reads them from git; they are regenerated on every daemon run.

## v0.9.2

- fix: nil result from `agent.Run` (phase steps all skipped or none registered) no longer crashes the Job — `main.go` and `cmd/run-task/main.go` deliver a `Failed` result naming the phase and exit 0, so the task reaches a terminal state and the controller retry loop is never entered

## v0.9.1

- fix: bump `golang.org/x/crypto` v0.55.0 -> v0.56.0, clearing `GO-2026-6354` and `GO-2026-6355` (DoS on deadlocked undecided/established channels in `golang.org/x/crypto/ssh`). `make precommit` failed at the `vulncheck` target on master, so the whole repo was unbuildable by the standard gate and the dark-factory daemon refused to start with `preflight baseline broken`. Dependency-only change: `go.mod` + `go.sum`, no source edits.

## v0.9.0

- feat: analyzer planning prompts (`planning.md`, `deep-planning.md`) now map Sentry projects `nuke-dev` / `nuke-prod` to source repo `bborbe/nuke` — when the stack trace lacks a repo path, the agent clones the mapped canonical repo before guessing project-named variants
- fix: `scripts/sentry-read.sh` now emits the latest event's stack trace frames whenever an exception entry carries them — including when none have a usable line number (as `basename in function`) — emits the distinct `stack_trace unavailable (no frames)` marker for an exception entry with zero frames, reserves `stack_trace unavailable (no exception entry)` for events with no exception entry, and neutralises control characters in emitted frame values so every frame line stays single-line

## v0.8.0

- feat: `scripts/sentry-read.sh` now fetches the latest event after the metadata block and emits up to 30 `file:line in function` stack trace frames (basename of `abs_path`/`filename`, `lineno`, `function`; first exception value only) under a `stack_trace=<N> frames` header, degrading to a single best-effort `stack_trace unavailable (<reason>)` line when the event fetch fails or the event has no exception — the event fetch never fails the run, only the metadata fetch still does
- fix: `scripts/sentry-read.sh` accepts a bare numeric issue id (e.g. `5192501045`) exactly like a full `.../issues/1234567890/` URL — previously a bare id fell through the URL extraction and errored out with `could not extract a numeric Sentry issue id`

## v0.7.4

- fix: create-tasks derives `stage` per alert instead of stamping a uniform default — `buildCreateCommand` now derives the frontmatter `stage` from the alert's `short_id` prefix (`NUKE-PROD-*` → `prod`, `NUKE-DEV-*` → `dev`) or its project slug (`nuke-prod`/`nuke-dev`), falling back to the `--stage` default only when neither matches; previously every task got the global `dev` default, silently mislabeling NUKE-PROD issues as dev for any stage-filtering consumer (2026-08-29)
- fix: restore the `create-tasks` default assignee to `sentry-analyzer-agent` — the stage-derivation struct-tag rewrite reverted it to the retired `sentry-issue-analyzer`, which `agent-task-executor` cannot resolve (silently drops the task as `skipped_unknown_assignee`), undoing the 2026-08-26 Config CR consolidation fix; pinned by a reflection test on the struct tag so a future tag rewrite cannot regress it unnoticed (2026-08-30)

## v0.7.3

- fix: sentry-collector planning step advances to `done` on success (`NextPhase: "done"`) — a successful fan-out now terminates the task so the executor stops re-dispatching; previously the empty NextPhase left the task planning/in_progress, and each re-dispatch after the success section skipped the step → nil result → `deadline_exceeded` → `trigger_count` churn (spec 051 follow-up)

## v0.7.2

- chore: update github.com/bborbe/agent to v0.84.1 (fixes `agentStep.ShouldRun` re-dispatch poisoning — a failed collector run no longer blocks re-dispatch)

## v0.7.1

- chore: update github.com/bborbe/agent to v0.83.1, github.com/bborbe/maintainer to v0.50.3

## v0.7.0

- feat: opt into `autoMerge.trivial` for mechanically-trivial update PRs

## v0.6.4

- fix: mandate fenced ```json blocks for the agent's `<output-format>` JSON envelope — `## Analysis`/`## Verdict` now render as formatted, syntax-highlighted JSON in Obsidian per-alert tasks instead of raw unreadable text (mirrors github-pr-review-agent); the verdict parser strips ```json fences and falls back to legacy unfenced raw JSON so both output shapes parse.

## v0.6.3

- fix: point the `create-tasks` default assignee at `sentry-analyzer-agent`. The 2026-08-26 consolidation of the 4 sentry Config CRs into 2 renamed the analyzer's assignee, but the fan-out kept stamping the retired `sentry-issue-analyzer` on every per-alert task. `agent-task-executor` resolves the agent Config by exact assignee string and skips unknown names silently (`skipped_unknown_assignee`), so the collector would have kept creating tasks that no agent ever picked up — a severed pipeline with no error anywhere.

## v0.6.2

- chore: update github.com/bborbe/errors to v1.5.21, github.com/bborbe/maintainer to v0.50.2, github.com/bborbe/vault-cli to v0.116.2
## v0.6.1

- fix: complete the sentry-collector rename — the rename PR left 4 dead `sentry-watcher`-named files on master (`pkg/steps/watcher.go`, `pkg/steps/watcher_test.go`, `pkg/prompts/watcher-planning.md`, `k8s/sentry-watcher-config.yaml`) because the deletions weren't committed; the stale test file referenced renamed-away symbols and broke `go vet`/`go test`. Removed; also adds the previously-uncommitted `k8s/sentry-collector-config-prod.yaml` (the prod collector Config CR used in the 2026-08-25 prod promotion).

## v0.6.0

- feat: rename the fan-out agent `sentry-watcher` → `sentry-collector` — the step's real job is collecting the day's active unresolved Sentry alerts and fanning them out into per-alert tasks (not watching); pairs `collector → analyzer` in the multi-agent workflow. Task type, Config CR (`k8s/sentry-collector-config.yaml`), step/prompt/preflight identifiers, and the Kafka producer name updated; the retired standalone Go `sentry-watcher` service references in comments/changelog are preserved as historical.
- fix: add the missing `ctx.Done()` guard to the verdict-YAML parsing loop in `pkg/verdict/verdict.go` (`Parse`), matching the existing guards in `extractVerdictSection`/`fencedYAMLBlocks` so a cancelled context can't block shutdown (review finding on the rename PR).

## v0.5.2

- fix: `create-tasks` no longer parses Sentry's `count`/`userCount` — Sentry returns them as a number OR a formatted string (e.g. `"1.2k"`), and no downstream field uses them; the strict `int64` unmarshal aborted the whole fan-out (observed in the dev e2e: 68 alerts fetched, then `json: cannot unmarshal string into Go struct field .0.count` → zero tasks created). The fields are dropped from `compactAlert`; Go ignores the script's extra JSON keys.

## v0.5.1

- fix: `sentry-create-tasks.sh` pagination loop — Sentry always returns a `rel="next"` cursor, so the loop broke only on an empty cursor and spun forever on 0-item pages once `results="false"` (observed in the dev e2e: page 1 = 68 items, then identical 0-item pages ad infinitum; the pod agent misdiagnosed it as a network failure). The loop now breaks when the next link's `results="false"` — fetch terminates after the last real page.

## v0.5.0

- feat: sentry-watcher as an agent step — new `sentry-watcher` task type + `cmd/create-tasks` publisher (one CreateTaskCommand per active unresolved Sentry alert, byte-identical task shape to the retired Go watcher: UUID5 `DeriveTaskID`, title/frontmatter/body defaults) + `scripts/sentry-create-tasks.sh` (constrained fetch + Kafka publish) + watcher planning prompt. Establishes the fleet's first multi-agent workflow: the daily recurring-task-creator task triggers the watcher agent step (fans out per-alert tasks), and the triage agent consumes each. Retires the separate Go `sentry-watcher` service.

## v0.4.0

- feat: wire the deep analyzer's dedicated GitHub App family into the deploy config — per-stage PEM teamvault keys in `dev.env`/`prod.env`, `PEM_KEY` in the agent secret (env-indirected via teamvault), and `APP_ID`/`INSTALLATION_ID` on the deep Config CRs (dev updated + new prod variant `k8s/sentry-deep-analyzer-config-prod.yaml`).

## v0.3.5

- fix: guard nil agent result in both mains — `agent.Run` returns `(nil, nil)` when every step in the phase skips (`ShouldRun=false`, e.g. the phase's output section already exists from a prior run); both `main.go` and `cmd/run-task/main.go` now return a clear error instead of panicking on `result.Status`. Caught by the dev e2e: re-triggering a completed deep task (with `## Context`/`## Verdict` still in the body) crashed the pod with SIGSEGV at `main.go:255`.

## v0.3.4

- fix: deep prompt output contract — deep-execution/deep-planning prompts no longer tell the agent to "write into the task body" (no file path exists in the container); instead the agent emits the verdict YAML block / context markdown as its response (the framework places the whole response under `## Verdict` / `## Context`) followed by the `<output-format>` JSON envelope, so `deepverdict.Parse` can read the fenced YAML. E2E on dev showed the deep-execution agent returning prose+JSON with no YAML fence (unparseable verdict).

## v0.3.3

- fix: scope the deep analyzer to `bborbe/*` repos only (Personal-vault fleet). `seibert-group` / `seibert-data` repos are OUT OF SCOPE for the nuke dev/prod agent — they belong to the dedicated octopus agent (deployed later into the octopus cluster). The planning prompt now STOPS with `needs_input` on a non-`bborbe` repo instead of attempting an out-of-scope clone; execution example ID updated from `OCTOPUS-PROD-1J` to `NUKE-PROD-77`.

## v0.3.2

- fix: forward `SENTRY_API_TOKEN` into the Claude subprocess env so `scripts/sentry-read.sh` can authenticate in-container (the main binary read it for preflight but never passed it to the claude CLI, whose Bash tool runs the script). Also mint a GitHub App installation token (dedicated App family per agent, per the fleet standard — the deep agent gets its own App, not the shared reviewer App) and expose it as `GIT_CLONE_TOKEN` so `scripts/repo-clone.sh` can clone private `bborbe` repos — caught by the deep-analyzer e2e in dev on NUKE-DEV-A7 (`bborbe/trading` is private).

## v0.3.1

- fix: ship the constrained scripts under `/agent/scripts/` so the agent's cwd-relative `scripts/...` Bash tool contract resolves in-container. The deep/triage agents run with cwd `/agent`, but the Dockerfile copied `scripts/` only to `/scripts/`, so `scripts/sentry-read.sh` and `scripts/repo-clone.sh` were unreachable and both deep phases returned `needs_input` (caught by the deep-analyzer e2e in dev on NUKE-DEV-A7).

## v0.3.0

- feat: trigger wiring — real-bug → deep analyzer. Triage execution is wrapped in a reassign step: on `verdict: real bug` it flips the SAME task's frontmatter (assignee → `sentry-deep-analyzer`, phase → `planning`, task_type → `sentry-deep-analyzer`) and returns InProgress, so the controller applies it, the scanner re-publishes, and the executor re-routes the task to the deep Config CR — strictly per-task, never batch. New `sentry-deep-analyzer` task type + Config CR (k8s/sentry-deep-analyzer-config.yaml) route the reassigned task to the deep agent, which has its own prompts (`deep-planning.md`/`deep-execution.md`) and octopus verdict schema (`pkg/deepverdict`). The shared triage prompts + `pkg/verdict` are restored to the 6-verdict triage versions (token-REST live-state fetch) so the triage task type is unchanged.
- feat: octopus verdict emission — the deep analyzer's execution phase emits the octopus-analyse-bugs verdict schema (`verdict` ∈ real bug | noise | duplicate | closed-fixed-in-prod | not-a-defect | track, plus `understanding`, `fix_certainty`, `root_cause`, `recommended_fix`, `file:line`, `disqualifiers_fired`, `live_event_count`), in `pkg/deepverdict` (kept separate from the triage's 6-verdict `pkg/verdict`). Real-bug verdicts require `file:line`, `root_cause`, `recommended_fix`, and High/Medium/Low U/F; downstream trigger keys on `understanding: High` AND `fix_certainty: High`.
- feat: read-only repo clone for the planning phase — constrained `scripts/repo-clone.sh` (clone/log subcommands) clones the implicated source repo into `REPO_CLONE_DIR` and `chmod a-w`'s the whole tree, so the agent can Read/Grep the code and resolve the root-cause `file:line` but cannot modify/commit/push. Preflight gains `ValidateRepoCloneTools` (checks `Bash(scripts/repo-clone.sh:*)`); Config CRD `ALLOWED_TOOLS` extended; Dockerfile now ships `scripts/` (`/scripts`) and installs git+python3 (python3 fixes `sentry-read.sh` JSON parsing in-container). Planning prompt's `mcp__sentry__*` refs replaced with the token-REST script + clone/log steps; execution prompt re-fetch switched to `scripts/sentry-read.sh`.
- feat: token-based Sentry access — constrained `scripts/sentry-read.sh` (Bearer-token REST fetch of a single issue's LIVE state: status, count, first/last seen, users) replaces the `mcp__sentry__*` MCP tools. Preflight now checks the `Bash(scripts/sentry-read.sh:*)` tool + `SENTRY_API_TOKEN` instead of MCP tool names; `SENTRY_API_TOKEN` arg added to both mains; Config CRD `ALLOWED_TOOLS` constrained to the script.



## v0.2.3

- chore: update github.com/bborbe/agent to v0.83.0, github.com/bborbe/sentry to v1.9.27, github.com/bborbe/vault-cli to v0.115.0

## v0.2.2

- chore: update github.com/bborbe/vault-cli to v0.114.7

## v0.1.5

- chore: Bump errcheck to v1.20.0 and golangci-lint to v2.13.1 for Go 1.27 support

Please choose versions by [Semantic Versioning](http://semver.org/).

* MAJOR version when you make incompatible API changes,
* MINOR version when you add functionality in a backwards-compatible manner, and
* PATCH version when you make backwards-compatible bug fixes.

## v0.1.4

- fix: repoint dead `docker.quant` registry to `docker.prod.nuke` in common.env, Dockerfile, k8s Config CRs, and the agent-scaffold doc template

## v0.1.3

- chore: update bborbe module dependencies — `cqrs` v0.6.6 -> v0.6.7, `time` v1.27.8 -> v1.27.9, `vault-cli` v0.111.4 -> v0.111.5, plus transitive `collection` v1.20.21, `http` v1.26.21, `k8s` v1.14.10, `kv` v1.21.10, `math` v1.3.19

## v0.1.2

- chore: bump Go toolchain to 1.26.6 and update dependencies
- chore: fix stdlib CVEs: GO-2026-5026, GO-2026-5972, GO-2026-6090, GO-2026-6218

## v0.1.1

- Bump Go toolchain to 1.26.5 and Alpine base image to 3.24
- Update bborbe module dependencies (agent, cqrs, errors, kafka, sentry, service, time, vault-cli) and transitive deps
- Add trivyignore/vulncheck exceptions for CVE-2024-27758 and GO-2026-5932

## v0.1.0

- feat: add explicit `TopicPrefix base.TopicPrefix` config field (env `TOPIC_PREFIX`) to `main.go` and `cmd/run-task/main.go`, threaded into `NewKafkaResultDeliverer` independent of `Branch`; bump `github.com/bborbe/agent` to v0.72.0 and `github.com/bborbe/cqrs` to v0.6.0

## v0.0.0

- Initial scaffold from bborbe/agent-claude template via /launch-agent on 2026-06-26
