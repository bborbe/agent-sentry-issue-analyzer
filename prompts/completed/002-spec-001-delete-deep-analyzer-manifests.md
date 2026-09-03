---
status: completed
spec: [001-bug-deep-reassign-targets-deleted-config]
summary: 'Deleted k8s/sentry-deep-analyzer-config.yaml and k8s/sentry-deep-analyzer-config-prod.yaml so make buca stops re-applying the retired sentry-deep-analyzer Config CR, and appended the corresponding fix: bullet to CHANGELOG.md under the existing ## Unreleased section'
execution_id: agent-sentry-deep-assignee-exec-002-spec-001-delete-deep-analyzer-manifests
dark-factory-version: v0.193.0
created: "2026-09-03T19:29:44Z"
queued: "2026-09-03T19:44:07Z"
started: "2026-09-03T19:55:27Z"
completed: "2026-09-03T20:03:39Z"
---

<summary>
- The repo stops shipping two Kubernetes manifests that define an agent which no longer exists on any cluster that runs work.
- Those manifests documented the retired routing as deliberate design, which is why a reader of this repo alone would conclude the buggy code was correct.
- The deploy step applies every YAML file it finds in the manifest directory, so keeping these files means every deploy silently re-creates the dead agent — this closes that loop.
- Only the two deep-analyzer manifests are removed; the collector and triage manifests stay exactly as they are.
- No Go code, no prompts, no environment files, and no secrets are touched.
- A CHANGELOG entry records the removal under `## Unreleased`.
- The dedicated GitHub App the removed manifests referenced is deliberately left in place; retiring it is separate work.
</summary>

<objective>
Delete `k8s/sentry-deep-analyzer-config.yaml` and `k8s/sentry-deep-analyzer-config-prod.yaml` so the repo stops shipping — and `make buca` stops re-applying — Config CRs for an agent that was retired in the 2026-08-26 sentry pipeline consolidation.
</objective>

<context>
This repo has no `CLAUDE.md`. Read these before making changes:

- `specs/in-progress/001-bug-deep-reassign-targets-deleted-config.md` — the full spec. Desired Behavior 3 and the Constraints section govern this prompt.
- `k8s/sentry-deep-analyzer-config.yaml` and `k8s/sentry-deep-analyzer-config-prod.yaml` — the two files to delete. Their header comments state `The triage execution phase reassigns a task to this assignee on a 'verdict: real bug', and the executor routes it here`, which is the pre-consolidation design the spec is correcting.
- `Makefile.k8s` — the reason deletion matters. Both the `prod` and `dev` branches run `find . -maxdepth 1 -name "*.yaml" | while read -r file; do ... | kubectlquant apply -f -; done` from `k8s/`. Every manifest in that directory is applied on every `make buca`, so leaving either file on disk keeps re-creating the dead Config CR.
- `k8s/` directory listing — 12 files today, 10 after this change.

Reference guides:

- `/home/node/.claude/plugins/marketplaces/coding/docs/k8s-manifest-guide.md` — manifest conventions.
- `/home/node/.claude/plugins/marketplaces/coding/docs/changelog-guide.md` — CHANGELOG entry format.
</context>

<requirements>

1. Delete both files with plain `rm` (NOT `git rm` — see the constraints):

   ```
   rm k8s/sentry-deep-analyzer-config.yaml
   rm k8s/sentry-deep-analyzer-config-prod.yaml
   ```

   If either `rm` reports the file does not exist, stop and report it rather than continuing silently — the spec's reproduction confirmed both files are present, so a missing file means the working tree is not what this prompt assumes.

2. Do not create, rename, or edit any replacement manifest. There is no successor file. The surviving `sentry-analyzer-agent` Config CR is Helm-managed out of this repo (`bborbe/nuke`, `agent/values-dev.yaml` / `agent/values-prod.yaml`) and already lists `sentry-deep-analyzer` in its `taskTypes`.

3. Leave every other file under `k8s/` byte-for-byte unchanged. After the deletions, these ten files must still be present:

   ```
   k8s/Makefile
   k8s/agent-sentry-issue-analyzer-config.yaml
   k8s/agent-sentry-issue-analyzer-pvc.yaml
   k8s/agent-sentry-issue-analyzer-secret.yaml
   k8s/agent-sentry-issue-analyzer.yaml
   k8s/priorityclass.yaml
   k8s/resource-quota-dev.yaml
   k8s/resource-quota-prod.yaml
   k8s/sentry-collector-config-prod.yaml
   k8s/sentry-collector-config.yaml
   ```

4. Append a new bullet to the EXISTING `## Unreleased` section of `CHANGELOG.md`. Do not create a second `## Unreleased` heading, do not rename any released `## vX.Y.Z` heading, and do not edit or reorder any existing bullet. Append at the end of the `## Unreleased` bullet list. Suggested wording:

   ```
   - fix: drop `k8s/sentry-deep-analyzer-config.yaml` and `k8s/sentry-deep-analyzer-config-prod.yaml` — the `sentry-deep-analyzer` Config CR was retired on 2026-08-26 when the sentry pipeline consolidated from 4 Config CRs to 2, but both manifests stayed in the repo documenting the pre-consolidation routing as deliberate design, and `Makefile.k8s` globs every `*.yaml` under `k8s/`, so each `make buca` re-created the dead CR on the quant cluster. Deep analysis stays a task type on the surviving `sentry-analyzer-agent`; nothing replaces these files. The deep analyzer's dedicated GitHub App (`APP_ID 4710983` / `INSTALLATION_ID 156399284`) is intentionally left in place — retiring it and its TeamVault PEM is tracked separately.
   ```

5. Do not remove the deep analyzer's GitHub App credentials from `dev.env`, `prod.env`, or `k8s/agent-sentry-issue-analyzer-secret.yaml`. Deleting the two manifests removes this repo's only record of **two** GitHub App pairs, which differ in consequence (verified against the live clusters, not inferred from the files): `APP_ID 4710983` / `INSTALLATION_ID 156399284` (dev, `k8s/sentry-deep-analyzer-config.yaml:51-52`) is genuinely orphaned; `APP_ID 4710998` / `INSTALLATION_ID 156399409` (prod, `k8s/sentry-deep-analyzer-config-prod.yaml:51-52`) is **still in active use** — the live `sentry-analyzer-agent` CR returns exactly that pair on both nuke dev and nuke prod, so deleting the manifest removes the in-repo copy, not the App. Cloning is unaffected: the surviving CR is Helm-managed out of `bborbe/nuke` and carries that App plus `Bash(scripts/repo-clone.sh:*)` in `ALLOWED_TOOLS`. Retiring App 4710983 and its TeamVault PEM is out of scope for this spec. Do NOT attempt to verify the surviving CR's App from inside the container — it is not in this repo.

6. Run `make precommit` and confirm it exits 0. Nothing under `k8s/` feeds the Go build, so this is a regression gate rather than a change verification — if it fails, the failure is unrelated to this deletion and must be reported, not worked around.

</requirements>

<constraints>
- Delete ONLY `k8s/sentry-deep-analyzer-config.yaml` and `k8s/sentry-deep-analyzer-config-prod.yaml`. Both, not one — leaving the prod variant on disk keeps `Makefile.k8s` re-applying it on every `BRANCH=prod make buca`.
- The other stale manifests are explicitly out of scope: `k8s/agent-sentry-issue-analyzer-config.yaml` declares `assignee: sentry-issue-analyzer` and `k8s/sentry-collector-config.yaml` declares `assignee: sentry-collector`, neither of which has a live CR. Do NOT touch them in this spec.
- Do not modify anything under `pkg/`, `cmd/`, `prompts/`, `scripts/`, or `main.go`. In particular `pkg/verdict/` and `pkg/deepverdict/` must remain untouched — the spec verifies this as a negative invariant.
- Do not restore or re-create the `sentry-deep-analyzer` Config CR anywhere, in any form. The consolidation to two agents is intentional and verified against both live clusters.
- Do not change `Makefile.k8s`, `Makefile.docker`, or any other Makefile. The glob-and-apply behavior is not the defect; the presence of the files is.
- Do NOT run any `git` command — no `git rm`, no `git status`, no `git diff`. This checkout is a linked git worktree whose `.git` is a *file* (`gitdir: .../agent-sentry-issue-analyzer/.git/worktrees/agent-sentry-deep-assignee`) resolving outside the container mount — note `.dark-factory.yaml` does NOT set `hideGit`, so no guidance fragment is injected and the worktree layout is the operative reason; a `git` invocation fails on tooling rather than behavior and would report a false pass. Use plain `rm` and filesystem checks.
- Do NOT run `make buca`, `make apply`, `make build`, `docker`, or any `kubectl*` wrapper. The container has no Docker socket and no cluster credentials. Deploying the deletion is separate work performed outside this prompt.
- Do NOT commit — dark-factory handles git.
- Existing tests must still pass.
</constraints>

<verification>
Run each of these. All must hold.

1. `test "$(ls k8s/ | grep -c sentry-deep-analyzer)" = "0" && echo deep-manifests-gone` — must print `deep-manifests-gone` and exit 0 (the bare `grep -c` pipeline exits 1 on success, which inverts under `set -e`); the count form returns `2` unmodified and `1` if only one file is deleted, catching both failure shapes. Note: `grep -c` exits 1 when it matches nothing, and that non-zero exit alongside the printed `0` IS the pass signal here. Do not rewrite this as `ls k8s/sentry-deep-analyzer-config.yaml k8s/sentry-deep-analyzer-config-prod.yaml`: `ls` exits 1 when *any* operand is missing, so deleting only one file would look like a pass.

2. `ls k8s/ | wc -l` — must print `10` (12 before the change).

3. `test -f k8s/agent-sentry-issue-analyzer-config.yaml && test -f k8s/agent-sentry-issue-analyzer-secret.yaml && test -f k8s/sentry-collector-config.yaml && test -f k8s/sentry-collector-config-prod.yaml && echo untouched-siblings-ok` — must print `untouched-siblings-ok`.

4. `test -d pkg/verdict && test -d pkg/deepverdict && test -f k8s/agent-sentry-issue-analyzer-config.yaml && echo negative-invariant-paths-present` — must print `negative-invariant-paths-present`. The `test -d` guards exist so a mistyped path cannot pass silently.

5. `grep -c 'sentry-deep-analyzer' pkg/factory/factory.go` — must print `1` or more. The task type survives; only the manifests are gone.

6. `grep -n '^## Unreleased' -A 10 CHANGELOG.md` — the output must show your new bullet under `## Unreleased`, and must show exactly one `## Unreleased` heading.

7. `make precommit` — must exit 0.
</verification>
