# Changelog

All notable changes to this project will be documented in this file.

## v0.21.1

- fix: forward the six egress proxy variables (`HTTP_PROXY`, `HTTPS_PROXY`, `NO_PROXY` and their lowercase forms) to the Claude child, so `scripts/sentry-read.sh` and `scripts/repo-clone.sh` reach `bborbe.sentry.io` and `github.com` through the egress proxy instead of dialling direct and being refused by the default-deny sandbox. Measured on prod (`sentry-analyzer-agent-12266ddb-20261027230200`) and dev (`sentry-analyzer-agent-4a8fc0a9-20261027225450`): the pod env carries the proxy (the chart's `egressProxy.injectAgentEnv`), but `bborbe/agent`'s `buildSubprocessEnv` allowlist stripped it from the Claude CLI child, so every `curl`/`git` the model ran itself failed with `curl 7: could not connect to bborbe.sentry.io:443` and the run parked `needs_input`. The six keys are forwarded from the pod env via `forwardProxyEnv`; `NO_PROXY` rides a dedicated loop rather than `CLAUDE_ENV` because that bag splits on commas and would keep only the first entry, sending in-cluster calls through the proxy. Test asserts all six pass through when set, are omitted when unset, and an unrelated secret-shaped var does not.

## v0.21.0

- feat: Give the verdict block an urgency band separate from diagnostic confidence. `real bug` was one bucket covering both "act today" and "fix someday", while `confidence` measures certainty rather than urgency, so the only thing separating the two was the raw `live_event_count` and `last_seen` — which every reader re-derived by hand, using the same arithmetic the disqualifier evaluator already performs in code. The block now carries `urgency: now | soon | someday`, computed from the same three inputs the disqualifiers read (`live_event_count`, `last_seen`, `sentry_status`): `now` restates the disqualifier arithmetic (volume, active burst, regression) so the band and the flip cannot disagree, `soon` catches a recently-active alert or a large backlog however stale, and `someday` is the rest. The band is stamped on every verdict that has live-state dates to derive it from — promoted or model-declared alike, since a model-declared `real bug` needs it as much as a promoted noise spike, which is the case the bucket conflated. A derived-key alert (the model writes `first_seen: unavailable`) carries no band, because there is no live state to derive one from; there the binary-owned keys are stripped rather than left as the model wrote them. A disqualifier-forced verdict also gains `promoted: disqualifier-fired (<names>); urgency <band>`: `reason` and `disqualifiers_fired` already recorded which disqualifier fired, but neither said what it meant for the operator, so a promoted alert and a declared defect still read the same. The model never chooses the band — the rubric documents it as binary-computed, and a value the model writes is overwritten on both write paths. The deep path re-renders too, and keeps its `file:line` through `verdict.RestoreUnmodeledLines`, the same call the analyzability guard already made: skipping the re-render there would have discarded the band on every non-promoted deep verdict, which is the final verdict for a real-bug task. `RestoreUnmodeledLines` now refuses to carry back binary-owned keys (`urgency`, `promoted`), so a model that writes one anyway cannot reinstate it past the binary's own value. The band and the fired disqualifiers are computed by one `EvaluateAll` call against a single clock reading — sharing the injected getter is not sharing the instant, and two reads could straddle the 24h or 7d boundary and let the two results describe different moments of the same alert.

## v0.20.1

- test: **cover the two gaps the #124 review named as MAJOR, both of which guard the migration's own safety argument.** `pkg/preflight` had no spec for `ValidateCollectorTools` while every sibling `Validate*` in that package has one, so the required-env rename could land unnoticed — which is exactly what happened when `GIT_REST_URL` became `VAULT_API_URL`. The new spec pins the requirement, the failure text, the sorted missing-piece list, the cancelled-context path, and that a **narrower subcommand scope still satisfies the grant** — the `HasPrefix` contract every sibling validator's spec asserts and this one did not. ⚠️ **It does not prove the caller wires the right field:** an earlier case here claimed to pin the `GIT_REST_URL` → `VAULT_API_URL` rename, but the validator only ever sees the URL *value*, never a variable name, so that assertion could never fail. It was replaced rather than kept, and the rename is pinned by the missing-value case alone — proving the caller needs a `main.go`-level test. `scripts/vault-list.sh`'s response-shape guards had no test either, although they are the mechanism that keeps a wrong-shaped answer from reading as an empty listing. The new `scripts/test-vault-list.sh` drives the **real script** against a mock HTTP server, because the failure it prevents lives in how the script composes curl, the parser and `pipefail` rather than in a function it could unit-test: an error envelope, a missing `items`, a non-list `items`, a non-JSON body and an unreachable API must each fail, while a genuinely empty `items` list must still succeed with no output — the caller cannot tell those two apart by counting rows, and treating the first as the second is what would republish every alert as a duplicate on every run. Wired into `make test` beside the two existing script tests.

## v0.20.0

- feat: List the tracked alert tasks through the typed vault API instead of a git-rest filename glob. `vault-list.sh` asked for `* Tasks/Analyze Sentry issue *.md`, which baked the vault's layout into this image: the vault renumbered `24 Tasks/` → `25 Tasks/` on 2026-09-13 and the script kept matching the old name, which is what the zero-match guard exists to catch. It now asks what a task **is**, in one multi-value `task_type` query covering the three types the tracked pages carry. **One value would not do:** the pages split 1,726 / 118 / 53 across three types, so a single-value filter returns 1,726 of 1,897 and leaves 171 looking untracked — and the caller's own guard only catches an *empty* answer, so it would re-create those 171 as duplicate tasks on every run. **Proven against the dev instance, compared as sets rather than by status code:** every genuine glob page comes back (1,897, the 1,898th being a manual page the glob matched by coincidence and which carries no `task_type`), **plus 32 real tasks of those types whose titles the glob never matched** — 28 `Sentry has stopped retaining events`, 3 `Daily Sentry Triage`, 1 `Verify trace read on prod`. The filename pattern was under-reporting the population all along. New `--vault-api-url` / `VAULT_API_URL`, forwarded into the subprocess env and now required by the collector's preflight, so a pod missing it fails before the run rather than mid-way. A wrong-shaped API response — an error envelope, a renamed key — fails the listing rather than reading as an empty one.

## v0.19.5

- chore: update github.com/bborbe/agent to v0.114.0, github.com/bborbe/kafka to v1.28.5, github.com/bborbe/log to v1.7.5, github.com/bborbe/service to v1.11.1, github.com/bborbe/time to v1.27.18

## v0.19.4

- chore: update Go to 1.27.3 and github.com/bborbe/agent to v0.111.0, github.com/bborbe/kafka to v1.28.4, github.com/bborbe/sentry to v1.10.5, github.com/bborbe/vault-cli to v0.177.0

## v0.19.3

- chore: update github.com/bborbe/kafka to v1.28.3, github.com/bborbe/maintainer to v0.50.12, github.com/bborbe/time to v1.27.17, github.com/bborbe/vault-cli to v0.174.0

## v0.19.2

- fix: `SentryTraceReader` now decodes the Sentry API's camelCase stack-frame flags. `pkg/fixagent` tagged the frame fields `json:"in_app"` / `json:"abs_path"`, but `/organizations/<org>/issues/<id>/events/latest/` serializes them as `inApp` / `absPath`; `encoding/json` matches a tag case-insensitively but not across an underscore, so every live frame decoded with `InApp: false` and `hasFirstPartyFrame` was always false — the frame-path branch was unreachable in production while every fixture, written in the protocol's snake_case spelling, stayed green. Observed 2026-10-19: issue `NUKE-DEV-F8` carries six `inApp=true` frames and the v0.18.0 run (`BuildGitCommit 1f0cca8`) reported `has_first_party_frame: false`, writing `rule: "candidate position 1 (bborbe/trading)"`. Both spellings are now decoded (`inApp`/`in_app`, `absPath`/`abs_path`), so the existing protocol-shaped fixtures keep passing. `trace_reader_test.go` gains camelCase specs for the first-party, all-third-party and path-less cases; they fail on the snake_case-only tags (verified red against `origin/master`).

## v0.19.1

- fix: the Jira preflight is now opt-in per deployment. `ValidateJiraTools` runs only when the deployment has signalled intent — the `Bash(scripts/jira-read.sh:*)` tool is granted, or any one of `JIRA_CLOUD_ID` / `JIRA_EMAIL` / `JIRA_API_TOKEN` is set — so a lane that analyzes alerts with no BRO ticket behind it (the nuke lane, which files into the personal vault) starts without a credential it never uses. Once intent is signalled the full set is still required: a partial configuration is a misconfiguration rather than an unused capability, and it fails at startup naming the missing piece.

## v0.19.0

- feat: the execution prompt can now answer the Jira half of the `already-tracked` verdict. A new `scripts/jira-read.sh` performs one read-only JQL search through the Atlassian API gateway (`/ex/jira/<cloudId>/rest/api/3/search/jql` — a scoped token sent to the site URL answers 401), printing `returned=<n>` plus one `<KEY>\t<statusCategory>\t<status>\t<summary>` line per matching issue; `pkg/preflight` now refuses to start a run whose `ALLOWED_TOOLS` lacks `Bash(scripts/jira-read.sh:*)` or whose `JIRA_CLOUD_ID` / `JIRA_EMAIL` / `JIRA_API_TOKEN` are unset, because an unbacked "open Jira ticket" is exactly the judgment the verdict must stop resting on. The execution prompt names the script, requires the matching KEY in `reason:`, and forbids inferring the verdict from a Jira key already present in the alert payload.

## v0.18.0

- feat: the fix agent now files an approved `kind: bug` spec at `specs/in-progress/bug-<issue-slug>.md`, with the `approved:` timestamp taken from the injected clock, so the emitted artifact is one the dark-factory watcher consumes instead of a `status: draft` spec at the old `specs/` path.
- feat: the fix agent now opens (or reuses) one open draft pull request from `sentry-fix/<issue-slug>` to the repository's default branch after filing the spec, and records its URL as `pr_url` in the task's `## Fix Result`; a failed filing creates no pull request, and a re-run of the same verdict creates neither a second spec nor a second pull request.
- docs: add `docs/fix-lane.md` describing the watcher's four-condition contract, the fix lane end to end, and the per-repository `.dark-factory.yaml` requirement, referenced from `README.md`.

## v0.17.7

- fix: the fix agent can file a spec at all. `GitHubSpecWriter` derived the destination branch `sentry-fix/<issue-id>` and then called `CreateFile` with it, but nothing ever created that branch — so every filing failed `404 Branch sentry-fix/<id> not found` and no spec had ever been filed. Measured on prod 2026-10-18, two consecutive runs of the same task (`sentry-fix-agent-3b3ce9fd-20261018204314`, `…204418`): each produced a correct resolution block and then failed at the write with the 404. The writer now creates the branch from the default branch's head before filing, reuses it when it already exists, and treats a concurrent-create `422` as success. Dev's separate failure (`403 Resource not accessible by integration`, App `4931539`) is a permission issue in that environment, not this code path.

## v0.17.6

- fix: the fix agent can no longer write a `resolved from frame path` rule for an alert whose trace carries no first-party frame. The fix-planning prompt told the model to record `resolved from frame path <path>` whenever the analyzer's `implicated_repo` hint was present — regardless of how that repo had been resolved — and its worked example showed the same form. A frame-less alert therefore produced a false claim: measured 2026-10-18T18:33Z on prod Job `sentry-fix-agent-3b3ce9fd-20261018183343` (build `v0.17.3-3-g6d86fca`), issue `bab89662fe73a8bf4a0014d6f78791b3`, where the analyzer had resolved `candidate position 1 (bborbe/agent-task-executor)` and the fix agent wrote `resolved from frame path pkg/k8s_connector.go`, so `VerifyResolutionMechanism` skipped the run and no spec was filed. The trace is now read before the resolution and `has_first_party_frame` is carried into the resolver's task content; the prompt states the condition explicitly (`resolved from frame path <path>` ONLY when it is `true`, otherwise `candidate position N (<repo>)`) and the worked example shows the candidate form. `VerifyResolutionMechanism` is unchanged and remains the backstop.

## v0.17.5

- fix: the fix agent now carries the analyzer's already-resolved repo (`implicated_repo`, `owner/name`) from the task's `## Analysis` section into its own resolution, and records a still-unmappable resolution as a `status: skipped` outcome instead of failing the Job. A repo-relative citation such as `pkg/deepverdict/deepverdict.go:109` matched no candidate row, so the resolver returned an empty repo and every retry failed identically; the carried-forward hint lets the resolver place it, and a genuinely unmappable path is now a deterministic skip (`fixagent.ErrUnmappable`).

- fix: the deep verdict parser now repairs an unquoted colon in a prose value instead of failing the whole block. `deepverdict.Parse` called `yaml.Unmarshal` directly, so a model-written `root_cause: … foo: bar …` was illegal YAML and took the block down — step `sentry-deep-execution-fix-handoff` failed with `parse verdict block: yaml: line 5: mapping values are not allowed in this context`, the Job burned a retry and fired a pod-failed alert, and with retries exhausted the deep verdict never reached the fix step. The repair `pkg/verdict` already applied is now `verdict.QuoteProseValues`, parameterised by the schema's prose and boundary field sets so both parsers share one implementation, and `deepverdict.Parse` retries once through it before reporting the block's original error. The boundary match is by longest known key rather than by character class, so `file:line` — the key the downstream fix spec cannot work without — is recognised whole instead of being truncated at its first colon and folded into the preceding `recommended_fix`.

## v0.17.4

- chore: update github.com/bborbe/agent to v0.103.1, github.com/bborbe/kafka to v1.28.1

## v0.17.3

- chore: update Go to 1.27.2 and github.com/bborbe/agent to v0.103.0, github.com/bborbe/cqrs to v0.6.12, github.com/bborbe/errors to v1.6.3, github.com/bborbe/kafka to v1.28.0, github.com/bborbe/log to v1.7.4, github.com/bborbe/maintainer to v0.50.11, github.com/bborbe/sentry to v1.10.4, github.com/bborbe/service to v1.11.0, github.com/bborbe/time to v1.27.16, github.com/bborbe/vault-cli to v0.170.1, github.com/onsi/ginkgo/v2 to v2.33.1, github.com/onsi/gomega to v1.44.0, github.com/prometheus/client_golang to v1.25.0

- fix: a verdict may no longer contradict its own analysis. `unanalyzable` means *"this alert could not be analysed at all"* and retires the issue from the fix queue, but the model also used it for the narrower methodological condition *"no first-party `in_app` frame / repo unresolvable"* — so a self-describing exception could carry a full `root_cause` and `recommended_fix` **beside** `verdict: unanalyzable`. Both disagreements in the one-week verdict-quality review (agreement 8/10, bar 90%) were this one mechanism: row 4 (`NUKE-PROD-AJ`, `dataError: run failed: consume failed: partition consumer messages channel closed`) named `bborbe/kafka` `kafka_consumer-offset.go:496` and a fix while labelled `unanalyzable`, and the same issue one day earlier read `real bug` with the same root cause. The execution prompt now forbids `unanalyzable` whenever the block's own `root_cause` or `recommended_fix` names a cause or a fix, and `verdict.ApplyAnalyzabilityGuard` enforces it in code on both verdict-writing paths (triage reassign + deep disqualifier guard) ahead of the disqualifier override: a contradictory block is rewritten to `real bug`, with `confidence` taken from its own `fix_certainty` (defaulting to `low`) and the guard recorded in `reason`. A genuine `unanalyzable` — placeholder analysis fields, no resolvable repo — is left exactly as the classifier wrote it. The rewrite carries any field the triage schema does not model through untouched (`verdict.RestoreUnmodeledLines`), so the deep verdict's `file:line` survives and the High/High fix handoff still resolves.

## v0.17.2

- fix: a `404` from the Sentry trace lookup no longer fails the sentry-fix Job. Measured 2026-10-18 on nukeprod `prod`, ten `sentry-fix-agent-*` Jobs over two task ids (`5dab9980`, `cce2a68b`): every pod exited `Error/1` after the model had already produced its resolution, with `fix-agent: read trace: sentry trace: fetch issue <id>: unexpected status 404` as the last line, the Job hit `BackoffLimitExceeded`, and the executor reported the generic `pod_crash_no_stdout`. The id in those verdicts is a **derived key** (a bare 32-char hex hash) that Sentry never serves as an issue id, so the lookup could only ever 404 — a deterministic outcome that retrying cannot change. Two changes. The reader now returns the sentinel `fixagent.ErrIssueNotFound` for a 404, and the fix step records it as a `status: skipped` no-op in `## Fix Result` instead of returning an error; a transport failure or any other non-200 still fails loudly so a genuinely transient read is retried. And the trace lookup now prefers the task frontmatter's `short_id` when the verdict's identity is a derived key — the derived key names no Sentry issue, while `short_id` carries the real id once the collector or analyzer resolved it (`b334dfcd…` → `7196177490`, short id `NUKE-PROD-9B`), so a fixable alert is no longer skipped for want of a lookable id.

## v0.17.1

- fix: a verdict's `sentry_issue_id` is now resolved in code to the short id Sentry returns for the issue, so the written identity no longer depends on which source the model happened to read. Both verdict-writing paths — the triage reassign step and the deep disqualifier guard — look the reference up through the shared `identity.Resolver` and rewrite the identity line in place, leaving every other line of the `## Verdict` section byte-identical so the deep verdict's `file:line`, `understanding` and `fix_certainty` survive for the High/High fix handoff. An in-place re-analysis therefore no longer flips the file between the short form and the numeric group id and thereby drops it out of the identity population the review lookups read. An alert with no Sentry issue behind it (a derived key — `event-` prefixed or a bare 32-character hex hash) keeps its key verbatim and issues no lookup. When the identity cannot be resolved the run fails loudly and nothing is written; the model's raw form is never substituted.

## v0.17.0

- feat: add a Sentry identity resolver (`pkg/identity`) and `verdict.RewriteIdentityLine`. The resolver turns whatever identity spelling a verdict carries into the canonical short id Sentry itself returns, so a verdict's identity is decided by code rather than by the model; a derived-key alert (`event-…`, or a bare 32-char hex hash) denotes a Kafka-pipeline alert with no issue behind it and is returned unchanged with no request. `RewriteIdentityLine` replaces the identity *value* of an existing `## Verdict` block and nothing else — matching both the YAML and the fenced-JSON spelling, preserving `file:line` and every other field, and deliberately avoiding a full re-render, which would drop `file:line` from deep verdicts and break the fix handoff. **Not yet wired to a write path** — the two call sites land in the follow-up.

## v0.16.8

- fix: a **derived-key** Sentry alert now resolves the ordered candidate list positionally instead of being answered `unanalyzable`. v0.16.7 made a *frame-less* alert resolve positionally, but the derived-key branch — six blocks across `planning.md` (`:26`, `:81`), `deep-planning.md` (`:7`) and `execution.md` (`:22`, `:36`, `:38`) — still told the model there was *"no repo to clone"* / *"no repo clone is attempted without a trace"*, so that branch pre-empted the list exactly as the original guard had. Measured 2026-10-17 on dev build `8161cfc` (v0.16.7), task `event-de2d57eece1c438dba9960664e66eaaf`: the run returned `verdict: unanalyzable` with reason *"unmapped project 4505975198449664"* — a project whose numeric id the candidate list does carry — and `grep -c 'candidate position'` read 0. A missing trace is not a missing repo: with no frames to match, the project's ordered repo list is resolved positionally, and the unmapped condition is recorded only when the project has no entry in that list at all. `unanalyzable` is therefore now disallowed whenever the planning phase resolved a repo **at all** — from the frame path or positionally. `docs/repo-mapping.md` is reconciled to match.

## v0.16.7

- fix: the repo resolver's ordered candidate list is now actually resolvable for a **frame-less** alert. v0.16.6 made that shape *reach* the list, but the list's success test — *"cloning each candidate until one contains the implicated frames"* — can never be satisfied when there are no frames at all, so the model correctly declined to walk it and the alert still ended `unanalyzable`. Measured 2026-10-16 on dev build `a2e0d24`, task `event-de2d57eece1c438dba9960664e66eaaf`: the analysis enumerated all four candidates verbatim and then wrote *"None were attempted because there is no first-party frame path to match"*. With no frames to match, the list is now resolved **positionally**: the first candidate for the alert's Sentry project is the owning repo, recorded as `candidate position 1 (<repo>)`, with no clone spent hunting for a frame match that cannot exist. The condition is recorded, and the alert ends `unanalyzable`, only when the project has no candidate row at all. Reconciled across all three `go:embed`-ed prompt blocks and `docs/repo-mapping.md`.

## v0.16.6

- fix: a frame-less Sentry alert — one with no exception entry at all (stack trace unavailable) — now falls through to the repo resolver's ordered candidate list instead of being answered with *"clone nothing and do not escalate"*. The guard pre-empted the list at `planning.md:42` / `deep-planning.md:27` before the list at `:44` / `:29` was ever consulted, so the list's own target class was unreachable and every frame-less event from a mapped project returned a null verdict — measured 2026-10-16 on dev build `6717274` as `grep -c 'candidate position'` = 0. The condition is now recorded, and the alert ends `unanalyzable`, only when the alert's Sentry project has no candidate row or no candidate contains the implicated frames; the frame-carrying path is unchanged. `fix-planning.md` carried no such guard at all and gains one, so all three `go:embed`-ed prompt blocks agree; `docs/repo-mapping.md` is reconciled to match.

## v0.16.5

- fix: the repo resolver's candidate list is now keyed by the **numeric** Sentry project id as well as the slug, so a frame-less event from a mapped project resolves instead of escalating `unmapped project` and self-closing as terminal `unanalyzable`. The alert payload carries `project: "4505975198449664"` / `"4505397462827008"`, never the slug `nuke-dev` / `nuke-prod` the list was written in, so the list could not match a key it was never keyed on — the mismatch dates from 2026-09-09 and covers the majority class (422 dev-id + 466 prod-id vault tasks). Both ids are named in the lead-in of all three prompt blocks and in the candidate table; the slug-only entry is the same defect one project further out.

## v0.16.4
- fix: the repo resolver's ordered candidate list gains `bborbe/agent-task-executor`, so a frame path under that repo resolves instead of failing `resolution unmappable — matches no repo in candidate list`. The list is keyed by Sentry project slug, and this repo holds the `configs.agent.benjamin-borbe.de` CRD setup in `pkg/k8s_connector.go` that project `4505975198449664` (`nuke-dev`) escalates on — the fix lane's first task from that project died there on 2026-10-15. Added to all three prompt blocks and to `docs/repo-mapping.md`, ahead of `bborbe/nuke`, which stays last.

## v0.16.3

- fix: the sentry-fix resolver falls back to the streamed `Partial` text when the final `Result` carries no fenced yaml block. The model can emit the resolution block mid-run and then end on a follow-up turn reacting to a background clone finishing, so `Result` held only "my final resolution above stands" and the run failed with `no fenced yaml block in resolution output`.

## v0.16.2

- fix: `scripts/repo-clone.sh` refuses a repo reference whose owner or name segment is `.` or `..`, and verifies the resolved clone destination stays inside `REPO_CLONE_DIR` before any `rm -rf` or `chmod -R` acts on it. The reference charset `[A-Za-z0-9._-]` admits `.` and `..`, so `https://github.com/../..` passed validation and resolved the delete target one level above the clone dir — into the agent root, which holds `.claude/` and `scripts/` — while `https://github.com/x/..` resolved it to the clone dir itself. The clone URL is chosen by the model from Sentry alert content, so the path was steerable by alert-borne text. Containment resolves symlinks, so a symlinked component under the clone dir is caught too, and the guard runs before the first destructive step so a refusal leaves nothing written. Covered by `scripts/test-repo-clone-containment.sh`, now wired into `make test`.

## v0.16.1

- fix: the deep-to-fix handoff now declares its reassignment. `sentry-deep-execution-fix-handoff` set `assignee`, `phase` and `task_type` together, but agent-task-controller's operator-owned-assignee guard discarded the assignee — it keeps the on-disk value over a differing incoming one, on the premise that an agent authors that key from its spawn-time snapshot — so the task reached the executor still addressed to `sentry-analyzer-agent` and failed its `task_type` filter, stranding every High/High verdict one step past the handoff. The step now also sets `assignee_reassigned_by` to its own name, the fresh declaration the guard honours. The key spelling is a frozen cross-repo contract: renaming it here reverts every handoff to a discard, with no error and no log line.

## v0.16.0

- feat: `sentry-analyzer-agent` exports `agent_job_verdicts_total`, a `verdict`-labelled counter, so the unanalyzable-verdict rate is observable instead of invisible to Prometheus. The counter cannot ride the existing `agent_job_*` group: the pushgateway replaces a group's metric families on PUT, so a counter there is overwritten by the next run and never accumulates — and the `POST` route does not rescue it either, since POST preserves sibling families but still overwrites by family name. Each run therefore pushes to its own group (a process-derived `run` label, one value per process), making the runs coexist as distinct series readable as `sum by (verdict) (last_over_time(agent_job_verdicts_total[<window>]))`. The shared pusher is deliberately left unchanged: `AgentJobFailureLoop` counts distinct PUTs onto one `agent_job_last_run_timestamp_seconds` series, so a per-run label there would leave every series with a single sample, read 0 changes, and silence the alert. The counter is incremented by the triage reassign step and the deep disqualifier guard, in both cases *after* the disqualifier override, so the counted value is the one the task file's `## Verdict` section carries; a run that writes no verdict counts nothing. All seven verdict values are pre-initialized to 0, without which a never-seen verdict has no series and a ratio over the counter reads no-data rather than 0.

## v0.15.12

- fix: bump `osv-scanner` to v2.6.0 and `golang.org/x/net` to v0.60.0 so the Linux vulnerability gates stop failing. v2.3.1 pins `golang.org/x/tools` v0.38.0, whose SSA builder aborts with `unexpected expr: *ast.KeyValueExpr` on the promoted-field composite-literal key Go 1.27 permits in the Linux stdlib, so a repo on the old pin passes locally on darwin and fails only in Linux CI. `x/net` v0.58.0 carries `GO-2026-6603/6610/6611/6612/6617`, which fail both `vulncheck` and `trivy`.

## v0.15.11

- fix: the analyzer's assignee is read from the lane's `ASSIGNEE` env instead of a hardcoded literal, so renaming an agent is a values-file edit rather than a code change plus rebuild. `pkg/factory` carried `const assigneeSentryAnalyzerAgent = "sentry-analyzer-agent"` — a third copy of a value already present in `cmd/create-tasks`'s `default:` tag and in nuke's lane values, with a source comment instructing the reader to keep the two in sync. That is a dispatch key: `agent-task-executor` matches it by exact string and silently drops unknown names (`skipped_unknown_assignee`), so a rename in one place files the task, claims nothing, and errors nowhere. Both binaries (`main.go`, `cmd/run-task`) now take a required `Assignee` field via the `env:"ASSIGNEE"` tag and thread it through `CreateAgentProvider` → `CreateAgent`/`CreateAgentFromRunner` → `NewReassignExecutionStep`. `cmd/create-tasks` drops its `default:` and flips to `required:"true"`, so an unset value fails the Job loudly instead of stamping `assignee: ""` — which would reproduce the exact silent break this removes. `Run` outgrew the funlen budget with the extra argument, so provider assembly moved to a `buildProvider` helper alongside the existing `buildClaudeEnv` / `buildGithubClient`. Tests pin the wiring rather than a coincidental literal: `reassign_wiring_test.go` passes a `testAssignee` into the factory and asserts the reassigned frontmatter against it, and `main_test.go` asserts `required:"true"` with an empty `default` rather than the default's value.

## v0.15.10

- fix: `SentryTraceReader` now calls the org-scoped issue route, so the fix agent can read the trace of the issue it just resolved. Every fix run on prod died at `fix-agent: read trace: sentry trace: fetch issue NUKE-PROD-BX: unexpected status 404` (observed 2026-09-14, v0.15.9, pod `sentry-fix-agent-4c46f7bb-20260914204900-428vz`), because the reader built `…/api/0/issues/<id>/events/latest/` while the agent carries the *short* issue id. Probed against the live API with the prod token: that bare route serves a numeric id only (`/issues/7672007265/…` → 200) and 404s a short one (`/issues/NUKE-PROD-BX/…` → 404), whereas the org-scoped route accepts both (`/organizations/bborbe/issues/NUKE-PROD-BX/…` → 200). The token was never the cause — the same token returns 200 on three of the four probed routes. `NewSentryTraceReader` therefore takes an `org` (empty falls back to the new `DefaultSentryOrg`, mirroring `SENTRY_ORG` in `scripts/sentry-read.sh`) and builds `/organizations/<org>/issues/<id>/events/latest/`, keeping the existing short id rather than plumbing the numeric `issue_url` through the verdict struct. `trace_reader_test.go` gains a regression whose test server reproduces the real routing — 404 on anything not org-scoped — and its pre-existing assertion of the buggy bare path is corrected.

- fix: `SentryTraceReader` now percent-escapes both URL path segments it interpolates. `sentryIssueID` is a yaml field read straight off the model's verdict (`sentry_issue_id`, validated only for non-emptiness), so a value carrying `/`, `..` or `?` could retarget the request at a different endpoint; `url.PathEscape` closes that. The local `url` variable is renamed `requestURL` to free the `net/url` import name.

## v0.15.9

- fix: the `bborbe/trading` candidate entry in the analyzer and fix planning prompts now names the Go Capital.com broker services under `capitalcom/`, so a `capitalcom/…` frame path resolves to `bborbe/trading` instead of being invented into `bborbe/capitalcom`. Observed 2026-09-14 on `NUKE-DEV-EY`/`EZ`: the frame path `capitalcom/marketdata/fetcher/pkg/market-data-websocket.go` starts with `capitalcom/`, the model read that leading segment as the repo name, cloned `bborbe/capitalcom` — a repo that does not exist — and reported `resolved from frame path …` with `fresh: false`, because the candidate entry described `bborbe/trading` as holding only `mt5/connector/` and so gave it no reason to try it. The same text is amended in `planning.md`, `deep-planning.md` and `fix-planning.md` (identical modulo list indentation), the `docs/repo-mapping.md` table row follows it, and `bborbe/capitalcom` joins that doc's list of names that must never be guessed. `pkg/prompts/prompts_test.go` gains the guard this invariant never had: the three candidate lists must be identical once indentation is stripped — the existing assertions checked substrings and relative order only, so a list amended in one file stayed green.

## v0.15.8

- fix: the fix agent no longer files a `kind: bug` spec built on an unverified resolution mechanism. `fixagent.VerifyResolutionMechanism` checks a `resolved from frame path <path>` claim against the issue's actual trace — read over the Sentry API by the new `SentryTraceReader`, never from the model's prose about it — and `fixStep.Run` records a skipped no-op instead of building the spec when the trace carries no first-party frame. Observed 2026-09-13 on `sentry-analyzer-agent-4c46f7bb-20260913181909-4v9jj`: the path claimed as a frame path was the triage phase's root-cause conclusion, on an alert whose own body says it has no frames, and because nothing checked the claim it would have travelled into the filed spec's `## Reproduction` as `resolution rule:`. Three deliberate boundaries: a candidate-list claim is never rejected (it asserts no trace evidence, so a trace cannot contradict it); a *read failure* errors rather than reporting "no frame", so only a readable trace can reject a resolution; and the refusal is a recorded no-op rather than a returned error, because the falsehood is deterministic and an error would make the controller retry a run that can only fail identically.

## v0.15.7

- fix: the deep analyzer no longer rejects its own verdict when the live event count carries a word instead of a number, mirroring the v0.15.1 triage fix. `pkg/deepverdict.Verdict.LiveEventCount` becomes `EventCount{Count, Known}`, whose `UnmarshalYAML` accepts any non-numeric token and reports `Known: false` rather than a coerced `0` — zero is a real measurement meaning "no events", so coercing would present a fabricated volume to the downstream rubric. A derived-key alert (no live state to fetch) previously died at the `sentry-deep-execution-fix-handoff` step with `fix-handoff: parse verdict: cannot unmarshal !!str ... into int`, discarding an analysis that was already complete; the disqualifier guard parses the same block with the already-tolerant triage parser, which is why the failure surfaced only at the handoff. The recorded production block is committed as a step-level regression test driving the real deep path.
- fix: the deep execution prompt now states what to write for a derived-key alert's live-state fields, naming the literal `unavailable` for `live_event_count`, `first_seen` and `last_seen`. The prompt's derived-key paragraph previously stopped at `sentry_status: unknown` and its required output example showed numbers only, so the emitter drifted between spellings for the three fields the tolerant deep verdict type now accepts — the parser tolerated the paraphrase, but nothing pinned the token at its source. The example block keeps `live_event_count: 142` and every existing key, carrying the not-evaluable alternative in its comments so the block the model copies presents both forms.

## v0.15.6

- test: the step-level regression test now carries the colon block, not only the sentinel-era one. `verdict.Parse` coverage was the gap this closes: the reassign step is where the Job dies, and its end-to-end test asserted through the step machinery against a schema-valid block alone — so the production block that killed eleven Jobs on 2026-09-13 (`yaml: line 13: mapping values are not allowed in this context`) never reached the layer that actually failed. Red against the `v0.15.4` verdict package, where it reproduces the production fatal line verbatim (`step "sentry-execution-reassign" Run: reassign: parse verdict: verdict parse errors: parse verdict block: …`), green against `v0.15.5`. The sibling entry passes in both, so the case discriminates rather than merely being present.

## v0.15.5

- fix: a verdict whose free-text prose carries an unquoted `: ` no longer kills the Job. `verdict.Parse` retries a block that fails to unmarshal with its `reason` / `root_cause` / `recommended_fix` values re-quoted as scalars, so arbitrary model prose parses instead of being read as a nested mapping. Eleven prod Jobs died on 2026-09-13 with `parse verdict block: yaml: line 13: mapping values are not allowed in this context`, discarding an otherwise ordinary `real bug` verdict over the literal text `tls: failed to send closeNotify`. Quoting the whole value — not escaping the one observed colon — is what makes the repair hold for any prose; a block broken some other way keeps its original error.

## v0.15.4

- fix: the collector no longer hardcodes the vault's tasks directory. `scripts/sentry-create-tasks.sh` globbed `24 Tasks/Analyze Sentry issue *.md`, and the vault renumbered `24 Tasks/` → `25 Tasks/` on 2026-09-13 — so the glob matched a directory that still existed and still held one stale file. Nothing errored: `existing` came back empty, every fetched alert looked untracked (so `expected_new` equalled `fetched`), and the landing clause fired on **every non-empty run**. The prod collector reported `failed` on healthy traffic — `fetched=11 published=11 landed=0`. That is the mirror of the false `done` this script exists to prevent: the same trust in a count nobody observed, one directory away. Both call sites now share `TASK_GLOB="* Tasks/Analyze Sentry issue *.md"`, which follows the numbered tasks directory wherever it moves, so the vault layout is no longer baked into the image and a future renumber cannot repeat this. A zero-match guard makes the remaining failure mode loud instead of silent: a pattern that matches nothing while alerts were fetched is a *wrong pattern*, not a zero-landing, so the run reports `unobserved` — which can prove nothing — rather than `failed`, which would claim a landing failure it never witnessed. The glob's semantics were checked against git-rest's `filepath.Match(pattern, line)` over the full `git ls-files` path, where `*` does not cross `/`, so the leading `* Tasks/` matches the directory segment.

## v0.15.3

- test: both production verdict blocks now run through the reassign step, not only through `verdict.Parse`. The step-level regression test covered the 2026-09-13 `unavailable` block alone, so the 2026-09-12 `unknown` block — the exact input this crash was filed on — never reached `applyDisqualifiers`' date guard. One block was never enough: the int unmarshal and the date parse are separate failure modes, which is why a fix to the int alone left the Job still exiting 1.

## v0.15.2

- fix: the collector's status rule now treats a failed publish as a failure. `published` was captured by the result-line regex and then discarded — `parseCollectorResult` read only `expected_new`/`landed`/`observed` — so a run that fetched 11 alerts and published none reported `done` whenever `expected_new` happened to be 0, making a dead publish indistinguishable from a quiet day. `pkg/steps/collector.go` now parses `fetched`/`published` and fails the run with its own message when `fetched > 0 && published == 0`. The clause is checked **before** the landing clause, because a landing failure is its consequence: when nothing reached Kafka, "no task files landed" is true but points the reader at the controller instead of at the publish. `fetched > 0` keeps a genuinely empty fetch a healthy quiet day — nothing to publish is not the same as failing to publish it. `scripts/sentry-create-tasks.sh` applies the same clause to its own `status=` token so the emitted line agrees with the gate, and now carries a per-reason `failure_reason` so the publish failure and the landing failure report distinct stderr messages. Also corrects the quiet-day unit fixture, which read `published=0` while asserting a day where "every fetched alert was already tracked": a real quiet day publishes every fetched alert and lands none (the controller owns dedup), so it reads `published=<fetched>`. The old fixture was the failed-publish shape and passed only because the rule ignored `published` — it would have kept the new branch green on a no-op. `scripts/sentry-create-tasks.sh` gains `CREATE_TASKS_KAFKA_BROKERS` (test-only, defaults to `KAFKA_BROKERS`) so a publish failure is constructible without killing the agent's own producer; setting `KAFKA_BROKERS` cannot do this, because the agent reads it at startup and dies with `create sync producer failed` before the script is reached. Verified on a live dev run (probe run #3, image `BuildGitCommit 782f79e` asserted before the result was read): publish refused on the injected broker, `published=0`, producer stayed up, gate reported `failed` with no `NextPhase`. Note the two clauses overlap by design — the script clause is the live path, the Go clause is defence-in-depth that fires only when the model misreports.

## v0.15.1

- fix: the analyzer no longer crashes parsing its own verdict when the live-state fields carry a word instead of a number. The execution prompt tells the model to leave `live_event_count` / `first_seen` / `last_seen` unavailable for derived-key alerts, and the model renders that instruction as a token — observed as `unknown` on 2026-09-12 and `unavailable` on 2026-09-13, because it echoes the prompt's own adjective. `live_event_count` was a bare `int`, so the unmarshal failed outright; `first_seen`/`last_seen` are `string`, so they parsed fine and only died later in the disqualifier guard's date parse. Both failure modes are fixed: `LiveEventCount` becomes `EventCount{Count, Known}`, whose `UnmarshalYAML` accepts **any** non-numeric token (an enumerated set fails on the next paraphrase) and maps it to `Known: false` rather than a coerced `0` — zero is a real measurement meaning "no events", so coercing would present a fabricated volume to the disqualifier arithmetic. The evaluator gates each count-based comparison on `Known`, so `Regressed` still fires when the count is unknown but the status is known, and `sustainedSpanFires` returns false when the count is not evaluable since both its branches derive from it. `applyDisqualifiers` returns early on a non-date token instead of wrapping it as an error. The prompt now names the literal token rather than the adjective that caused the drift. This was the largest single alert source on the fleet — 18 `Error`-state pods driving 34 `K8sPodStatus` alerts.
## v0.15.0

- feat: log the build identity at startup — `BuildGitVersion` / `BuildGitCommit` / `BuildDate` are now declared on the `application` struct, so the argument framework prints them on every run. The image already carried `BUILD_GIT_COMMIT` and `BUILD_DATE` as `ENV` (the Dockerfile has exported all three from the start) but nothing ever read them, so a Job pod's log said nothing about which build it was running. That is the entire deploy-verification problem for these agents: they run from the mutable `:dev`/`:prod` tags, and they are Jobs rather than Deployments, so the pod exists only for the duration of one task and there is no long-lived object to inspect between runs. A digest therefore answers "same or different?" but never "which commit?". Now `kubectl logs <job-pod> | grep BuildGitCommit` is per-run provenance that outlives the pod. Also passes `BUILD_GIT_VERSION` as a build-arg from `git describe --tags --always --dirty` in `Makefile.docker` — the Dockerfile declared it but no caller ever supplied it, so every image reported the literal default `dev`. The three fields are `string`, not `libtime.DateTime`: `BUILD_DATE` defaults to `unknown`, and the argument framework treats an unmarshal failure as fatal, so a typed field would refuse to start the agent rather than report an unknown build date.

## v0.14.3

- fix: the two base phase prompts state their output as a **response contract** instead of a file write — `pkg/prompts/planning.md` Step 4 and `pkg/prompts/execution.md` `## Output` now say the final response MUST contain the analysis / fenced YAML verdict block, that the framework places the entire response under the task's `## Analysis` / `## Verdict` section, and that the `<output-format>` JSON envelope comes last; both carry the `deep-execution.md` prohibition verbatim (`Do NOT try to write a task file (there is no file path in this environment)`), and the retired `into the task body under` imperative is gone from both. `pkg/prompts/prompts_test.go` locks each prompt (guard present, retired phrase absent, guard placed inside the section it governs) so the guard cannot be deleted or appended-out-of-place without turning the suite red. The job environment has no task-file path by design — the executor passes none and the controller owns the write-back — but on 2026-09-02 a prod run on the base path formed a verdict, hunted a hallucinated `/agent/tasks/<uuid>.md`, emitted `## Failure` instead of the verdict, and discarded a complete analysis; the deep prompts were fixed for exactly this on 2026-08-25 and the base ones were left behind. Text-only: no prompt file added or removed, no builder, runtime, controller, executor, config, or flag change.

## v0.14.2

- fix: `scripts/repo-clone.sh` clones into `/agent/repos` instead of `/tmp/repos`, so the deep-analysis phase can actually read the repo it just cloned. Claude's tool sandbox is its `WorkingDirectory`, which is `AGENT_DIR` — the relative default `"agent"` resolved against the runtime image's cwd `/`, i.e. `/agent`. `/tmp/repos` sits outside that root, so every run cloned successfully and was then refused by Read/Glob/Bash (`tools cannot access /tmp/repos/... (scoped to /agent only)`) and escalated — the work was wasted at the last step. Widening the sandbox was not an option: `WorkingDirectory` is the only knob `lib/claude` exposes, and that package is never-modify, so the clone path is the side that had to move. The clone goes in a dedicated `repos/` subdir rather than `/agent` itself, because the agent root also holds the agent's own `.claude/` config and `scripts/`, and cloned repo content is untrusted. Nothing else moves with it: the emitted `clone_path`/`head_sha`/`default_branch` keys, `chmod -R a-w`, and `normalize_repo`'s charset guard are unchanged, and no prompt or preflight check hardcoded the old location.

## v0.14.1

- fix: the collector reports a per-alert task creation count **observed after** publishing instead of echoing the fetched count. `scripts/sentry-create-tasks.sh` no longer `exec`s `/create-tasks` — it regains a return path, captures the publish result, then polls the vault (`scripts/vault-list.sh`, new — git-rest `?glob=`) for the per-alert task files that actually landed, and emits one machine-readable line: `sentry-create-tasks-result: fetched=<N> published=<n> expected_new=<k> landed=<m> status=<done|failed>`. `cmd/create-tasks` now prints `create-tasks-result: published= failed= total=` on stdout (previously the count existed only as a `glog.V(2)` line). A new wrapper step in `pkg/steps/collector.go` parses that line out of `## Analysis` and returns `AgentStatusFailed` with **no** `NextPhase` when new alerts were expected but zero task files landed — the controller owns unassign + `## Failure`. Before this, `NextPhase: "done"` was unconditional and the `## Analysis` section's mere presence terminated the task, which is why the 2026-08-26/27 outage reported success for ~20 hours while zero per-alert tasks landed. Dedup stays the designed idempotency: zero created with zero new alerts expected (a quiet day) is still `done`, so the gate does not false-alarm. The rule is applied in Go, not read from the script's `status=` token, so a bug in the script's wording cannot re-open the false-green path. `pkg/preflight` now requires `Bash(scripts/vault-list.sh:*)` + `GIT_REST_URL` for the collector step (GIT_REST_URL is mandatory — a run that cannot observe its own creation phase is the defect), and `main.go` forwards `GIT_REST_URL`/`GATEWAY_SECRET` into the subprocess env (`buildSubprocessEnv` strips non-allowlisted vars). `collector-planning.md` Step 4 no longer asks the model to compute the created count; it copies the script's line verbatim.
- fix: the collector's vault observation no longer masks its own failure. The first dev e2e run exposed it — `vault-list.sh` was called under `|| true`, so an auth/network failure was indistinguishable from an observed zero: the script reported a confident `landed=0` and the gate fired a **false failure** on an otherwise healthy run. Both call sites now check the exit code, `vault-list.sh` uses `curl -sfS` so errors surface instead of vanishing, the result line gains `observed=<true|false>` (and `status` gains `unobserved`), and `pkg/steps/collector.go` fails the run on `observed=false` with its own distinct message. `observed=false` is not an observed zero — a run that could not read the vault proves nothing and must not report success, or an auth failure becomes a silent green. Found by running the gate on dev: the mechanism worked, the observation lied.
- fix: the collector no longer reports success when it produced no observation at all. The dev e2e caught a run that did literally nothing being recorded `phase: done` / `status: completed`: the script never executed, because the model invoked `bash scripts/sentry-create-tasks.sh` — which does **not** match the `Bash(scripts/sentry-create-tasks.sh:*)` grant (the rule prefix-matches the whole command), so it hit an approval gate an unattended run cannot grant. The step previously treated a missing result line as "no evidence, stay done"; it now returns `AgentStatusFailed` with no `NextPhase`, because a run that cannot observe its own creation phase has not done its job. `collector-planning.md` Steps 1–2 now state the exact invocation form and warn that the `bash ` prefix defeats the grant. This is the 2026-08-26/27 false-green signature reproduced inside the task built to kill it — absence of a result line is evidence of failure, not neutrality.
- fix: the collector step now reads the script's result line from a **file**, not from the model's prose. The 2026-09-12 dev run exposed the last prompt-dependent seam: `sentry-create-tasks.sh` computed the counts correctly, but the model wrote a confident prose JSON summary and never copied the result line, so `## Analysis` carried no parseable line and a run that *had* observed nothing was indistinguishable from one that never ran. The script now also writes its line to `/tmp/sentry-create-tasks-result`, and `pkg/steps/collector.go` reads that path — clearing it *before* the run so a stale file can never be mistaken for this run's observation. The script already computes every count itself, so routing them through the model added a failure mode without adding information: a summary is the model's *claim*, the file is the script's *measurement*, and the measurement wins.

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
