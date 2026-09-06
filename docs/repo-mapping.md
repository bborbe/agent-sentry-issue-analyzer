# Repo mapping — Sentry project to candidate source repos

Which source repo holds the code behind a Sentry alert. This document is the operator-facing home of the mapping; the runtime artifact is the resolution block inside `pkg/prompts/planning.md` and `pkg/prompts/deep-planning.md`, which is compiled into the binary via `go:embed`. Changing this file alone changes nothing at runtime — see "Extending the mapping" below.

## Resolution order

1. **Frame path first.** If any application stack frame carries a repo-relative path (`mt5/connector/mt5linux.py`, `pkg/kafka/consumer.go`), the repo is resolved from that frame path. This is the primary mechanism and nothing below overrides it.
2. **Drop third-party frames.** Frames from a third-party library are absent from every `bborbe` repo by design. `rpyc` internals (`netref.py`, `protocol.py`, `channel.py`, `stream.py`, `classic.py`, `factory.py`) are the worked example — `mt5/connector/mt5linux.py` imports `rpyc`, so its frames show up in traces that are otherwise application code. They are excluded from resolution, never hunted for.
3. **Candidate list as fallback.** Only when no application frame carries a repo path, the candidates below are cloned in order until one contains the implicated frames.

There is deliberately no single canonical repo per Sentry project. A single project legitimately spans several repos, so a one-repo model cannot be correct for it.

## Candidate table

| Sentry project | Order | Candidate repo | Holds | Visibility |
|---|---|---|---|---|
| `nuke-dev`, `nuke-prod` | 1 | `bborbe/trading` | Python MT5 connector under `mt5/connector/` — `runner.py`, `kafka.py`, `command.py`, `account_fetcher.py`, `mt5linux.py` | private (cloned with `GIT_CLONE_TOKEN`) |
| `nuke-dev`, `nuke-prod` | 2 | `bborbe/kafka` | Go Sarama client, consumer/producer configuration | public |
| `nuke-dev`, `nuke-prod` | 3 | `bborbe/nuke` | Helm charts, YAML, deployment config only — no application source. Last, and only for infrastructure-shaped frames | private |

`bborbe/nuke` is retained rather than removed because some `nuke-*` alerts are genuine infrastructure problems for which it is the correct repo. Demoting it to last stops it shadowing the application repos, which was the actual defect.

Repo names that appear in past agent output but do **not** exist, and must never be cloned or guessed: `bborbe/trading-bot` (hallucinated) and `bborbe/nuke-dev` (a project-named variant the old prompt suggested). Never guess a project-named repo variant.

## Extending the mapping

An escalation matching `candidates tried` or `unmapped project` is the signal that this table is incomplete. (Grep the task body for those strings, not for a section name — the planning prompts write only `## Analysis`; there is no `## Failure` section.) To extend it:

1. Identify the missing repo from the escalation's frame names.
2. Add it to the ordered candidate list in **both** `pkg/prompts/planning.md` (inside `### Step 3: Read the implicated source code`) and `pkg/prompts/deep-planning.md` (inside `### Step 2: Clone the implicated repo read-only`). The two files must stay byte-identical in that block.
3. Add the same row to the candidate table above, keeping the doc order and the prompt order identical.
4. Add a `## Unreleased` entry to `CHANGELOG.md`, run `make precommit`, and release — the prompts are `go:embed`ed, so an operator cannot change the mapping by editing config or a CRD. A release plus redeploy is the only path.
5. Verify the deployed tag carries it: `grep -c '<repo>' pkg/prompts/*.md` returns `>= 1` for both prompt files.

A repo that is renamed or archived surfaces the same way — the escalation names the stale repo, and the stale name is the grep key for finding it here and in the prompts.
