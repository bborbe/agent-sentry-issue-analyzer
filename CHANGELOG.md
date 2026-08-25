# Changelog

All notable changes to this project will be documented in this file.

## Unreleased

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
