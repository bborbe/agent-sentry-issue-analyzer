# Repo mapping — Sentry project to candidate source repos

Which source repo holds the code behind a Sentry alert. This document is the operator-facing home of the mapping; the runtime artifact is the resolution block inside `pkg/prompts/planning.md`, `pkg/prompts/deep-planning.md` and `pkg/prompts/fix-planning.md`, which are compiled into the binary via `go:embed`. Changing this file alone changes nothing at runtime — see "Extending the mapping" below.

## Resolution order

1. **Frame path first.** If any application stack frame carries a repo-relative path (`mt5/connector/mt5linux.py`, `pkg/kafka/consumer.go`), the repo is resolved from that frame path. This is the primary mechanism and nothing below overrides it.
2. **Drop third-party frames.** Frames from a third-party library are absent from every `bborbe` repo by design. `rpyc` internals (`netref.py`, `protocol.py`, `channel.py`, `stream.py`, `classic.py`, `factory.py`) are the worked example — `mt5/connector/mt5linux.py` imports `rpyc`, so its frames show up in traces that are otherwise application code. They are excluded from resolution, never hunted for.
3. **Candidate list as fallback.** Only when no application frame carries a repo path, the candidates below are cloned in order until one contains the implicated frames.

There is deliberately no single canonical repo per Sentry project. A single project legitimately spans several repos, so a one-repo model cannot be correct for it.

## Candidate table

| Sentry project | Order | Candidate repo | Holds | Visibility |
|---|---|---|---|---|
| `nuke-dev` (`4505975198449664`), `nuke-prod` (`4505397462827008`) | 1 | `bborbe/trading` | Python MT5 connector under `mt5/connector/` — `runner.py`, `kafka.py`, `command.py`, `account_fetcher.py`, `mt5linux.py` — and the Go Capital.com broker services under `capitalcom/` (`marketdata/`, `gateway/`, `trade-executor/`, `position/`, `transaction/`, `account/`) | private (cloned with `GIT_CLONE_TOKEN`) |
| `nuke-dev` (`4505975198449664`), `nuke-prod` (`4505397462827008`) | 2 | `bborbe/kafka` | Go Sarama client, consumer/producer configuration | public |
| `nuke-dev` (`4505975198449664`), `nuke-prod` (`4505397462827008`) | 3 | `bborbe/agent-task-executor` | Go agent task executor — the `configs.agent.benjamin-borbe.de` CRD setup in `pkg/k8s_connector.go`, and its `k8s/` manifests | public |
| `nuke-dev` (`4505975198449664`), `nuke-prod` (`4505397462827008`) | 4 | `bborbe/nuke` | Helm charts, YAML, deployment config only — no application source. Last, and only for infrastructure-shaped frames | private |

`bborbe/nuke` is retained rather than removed because some `nuke-*` alerts are genuine infrastructure problems for which it is the correct repo. Demoting it to last stops it shadowing the application repos, which was the actual defect.

**The project key is numeric in the alert payload.** Both projects are addressed by their slug here and in the prompt blocks, but the per-alert task payload the analyzer receives carries only the numeric project **id** — `4505975198449664` for `nuke-dev`, `4505397462827008` for `nuke-prod`. (A synthetic sentinel `9999999` also appears on derived-key test alerts; it maps to no project and is not in any candidate list.) Both spellings are therefore listed in each prompt block's lead-in, so a frame-less event matches whichever key it carries. A project added here must have its numeric id resolved from the live project list (`GET https://bborbe.sentry.io/api/0/organizations/bborbe/projects/`) and written into all three blocks alongside the slug — a slug-only entry cannot match a numeric-keyed payload.

Repo names that appear in past agent output but do **not** exist, and must never be cloned or guessed: `bborbe/trading-bot` (hallucinated), `bborbe/nuke-dev` (a project-named variant the old prompt suggested) and `bborbe/capitalcom` (hallucinated 2026-09-14 from the leading segment of a `capitalcom/…` frame path — the tree is in `bborbe/trading`). Never guess a project-named repo variant.

## Extending the mapping

An escalation matching `candidates tried` or `unmapped project` is the signal that this table is incomplete. (Grep the task body for those strings, not for a section name — the planning prompts write only `## Analysis`; there is no `## Failure` section.) To extend it:

1. Identify the missing repo from the escalation's frame names.
2. Add it to the ordered candidate list in **all three** `pkg/prompts/planning.md` (inside `### Step 3: Read the implicated source code`), `pkg/prompts/deep-planning.md` (inside `### Step 2: Clone the implicated repo read-only`), and `pkg/prompts/fix-planning.md` (inside `### Step 1: Resolve the repository from the file:line`). The three files' **numbered candidate rows** must stay identical — same text, same order, differing only in list indentation (`fix-planning.md`'s copy is nested one level deeper). The surrounding lead-in prose is *not* identical and is not compared: `planning.md` and `deep-planning.md` open with *"Only when no application frame carries a repo path…"* while `fix-planning.md` opens with *"Only when the path maps to no repo…"* — edit each against its own text rather than copying one across.
3. Add the same row to the candidate table above, keeping the doc order and the prompt order identical.
4. Add a `## Unreleased` entry to `CHANGELOG.md`, run `make precommit`, and release — the prompts are `go:embed`ed, so an operator cannot change the mapping by editing config or a CRD. A release plus redeploy is the only path.
5. Verify the deployed tag carries it: `grep -c '<repo>' pkg/prompts/*.md` returns `>= 1` for all three prompt files, and `make test` — which compares the three lists indentation-stripped and fails when they drift apart.

A repo that is renamed or archived surfaces the same way — the escalation names the stale repo, and the stale name is the grep key for finding it here and in the prompts.
