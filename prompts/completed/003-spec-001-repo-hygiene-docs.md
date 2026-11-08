---
status: completed
spec: [001-cluster-build-arm]
summary: Versioned the canonical dark-factory layout in .gitignore with .keep files and documented the two-arm build topology in README and CHANGELOG.
execution_id: 5e82dc9b-7baf-5c38-8ac9-ef04e16a0999-exec-003-spec-001-repo-hygiene-docs
dark-factory-version: dev
created: "2026-11-08T10:44:00Z"
queued: "2026-11-08T11:03:12Z"
started: "2026-11-08T11:16:19Z"
completed: "2026-11-08T11:21:02Z"
branch: dark-factory/cluster-build-arm
---

<summary>
- The `specs/` and `prompts/` directories are tracked by git instead of being ignored wholesale.
- Only the runtime logs and lock files under those directories stay ignored, with `.keep` files so the directories exist in a fresh clone.
- The README build section documents both build arms, the exact cluster invocation, that `buca` stays local, and why the image name is unprefixed.
- The CHANGELOG records the change under an `## Unreleased` section.
- Existing ignore entries, including `/vendor`, are preserved.
- No build logic changes; existing tests must still pass.
</summary>

<objective>
Make the repository's canonical dark-factory layout versioned and document the two-arm build topology, so a fresh clone contains the pipeline's directories and the documented build path matches reality.
</objective>

<context>
Read the approved spec first: `specs/in-progress/001-cluster-build-arm.md` — especially Desired Behavior 7 and 8, the `.gitignore` constraint (line beginning "`.gitignore` keeps every entry it has today"), the README constraint, and Acceptance Criteria 7 and 8.

Read before editing:
- `.gitignore` (current entries, including `/prompts/`, `/specs/`, `/vendor`, `/.dark-factory.log`, `/.dark-factory.lock`)
- `README.md` § *Build + Deploy* (currently lines ~47-60)
- `CHANGELOG.md` (preamble at the top, then `## v0.22.5`)
- `docs/dod.md` (requires a CHANGELOG entry under `## Unreleased`)

Relevant coding plugin docs (read in the YOLO container at these paths):
- `/home/node/.claude/plugins/marketplaces/coding/docs/changelog-guide.md`
- `/home/node/.claude/plugins/marketplaces/coding/docs/readme-guide.md`
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-precommit.md`

The canonical layout is enumerated in the spec itself; no external doc is needed.
</context>

<requirements>
1. Edit `.gitignore`. Keep every entry that exists today except remove the two wholesale ignores `/prompts/` and `/specs/`. The final file must be exactly:

```
/.update-logs/
/.claude/
/CLAUDE.md
/.mcp-*
/run-task
/cmd/run-task/run-task
/vendor
/.dark-factory.log
/.dark-factory.lock
/prompts/log
/specs/log
/prompts/in-progress/*.lock
/specs/in-progress/*.lock
```

   - `/vendor` MUST stay: `check-go-mod` would otherwise stage it on the next `git add .`.
   - Do not add `/*.lock` or any other broad pattern that would hide tracked specs or prompts.

2. Create empty `.keep` files so the directories exist in a fresh clone:

```bash
touch specs/.keep specs/in-progress/.keep specs/completed/.keep
touch prompts/.keep prompts/in-progress/.keep prompts/completed/.keep
```

   Do not add `.keep` under `specs/log` or `prompts/log`; those directories stay ignored.

3. Rewrite `README.md` § *Build + Deploy* so it documents both arms. Keep the existing statement that the repo builds and uploads the image and no longer owns cluster objects, and keep the existing note that `k8s/` was deleted and nuke owns the live objects. The section must contain:

   - A local-arm block showing `make precommit` and `BRANCH=dev make buca`, and stating that `buca` stays local-only.
   - A cluster-arm block whose fenced example contains this exact line, character for character, on one line:

     ```
     make build BUILD_TARGET=<cluster> REF=<sha> VERSION=<tag>
     ```

   - A sentence stating that the cluster arm builds from a fresh clone inside the cluster and pushes `docker.prod.nuke.benjamin-borbe.de:443/agent-sentry-issue-analyzer:<tag>`, and that `VERSION` is required.
   - A sentence recording that the image name is unprefixed (`agent-sentry-issue-analyzer`) because `bborbe/nuke`'s agent values pull exactly that repository, so a `bborbe/` prefix would push to a repository nothing deploys.

   Suggested section body (adapt surrounding prose as needed, but keep the exact cluster-invocation line and the unprefixed-name explanation):

````markdown
## Build + Deploy

This repo builds and uploads the image; it no longer owns cluster objects.

### Local arm (this machine)

```bash
make precommit                 # lint + test
BRANCH=dev make buca           # build + upload + clean (no apply)
```

The local arm tags `docker.prod.nuke.benjamin-borbe.de:443/agent-sentry-issue-analyzer:<branch>`.
`buca` stays local-only: it pushes from this machine and refuses any `BUILD_TARGET`
other than `local`.

### Cluster arm (Kubernetes Job)

```bash
make build BUILD_TARGET=<cluster> REF=<sha> VERSION=<tag>
```

The cluster arm clones `REF` inside the target cluster, builds it as a Kubernetes
Job, and pushes `docker.prod.nuke.benjamin-borbe.de:443/agent-sentry-issue-analyzer:<tag>`.
It needs no local Docker daemon and no prepared working tree. `VERSION` is
required: the arm refuses to run when the caller did not supply it.

The image name is unprefixed (`agent-sentry-issue-analyzer`) on purpose:
`bborbe/nuke`'s agent values pull exactly that repository, so a `bborbe/` prefix
would push to a repository nothing deploys.
````

   (The nested fences above are illustrative; use one code fence per example in the real README.)

4. Add a `## Unreleased` section to `CHANGELOG.md`, immediately after the preamble line `All notable changes to this project will be documented in this file.` and before `## v0.22.5`. Add one bullet:

```markdown
## Unreleased

- feat: add a cluster build arm (`make build BUILD_TARGET=<cluster> REF=<sha> VERSION=<tag>`) that builds and pushes the image inside Kubernetes from a fresh clone, leaving the local `make build` / `buca` path unchanged.
```

   - Do not insert content above `# Changelog`.
   - Do not modify the existing `## v0.22.5` section or any earlier section.
   - There must be exactly one `## Unreleased` heading.

5. Do not modify `Makefile.docker`, `Makefile.env`, `Dockerfile` or the `hack/` files in this prompt.
</requirements>

<constraints>
- `.gitignore` keeps every entry it has today, including `/vendor`, and removes only `/prompts/` and `/specs/`.
- The canonical layout adds `/.dark-factory.lock`, `/.dark-factory.log`, `/prompts/log`, `/specs/log`, `/prompts/in-progress/*.lock`, `/specs/in-progress/*.lock`, plus `.keep` files under `specs/`, `specs/in-progress/`, `specs/completed/`, `prompts/`, `prompts/in-progress/` and `prompts/completed/`.
- `README.md` § *Build + Deploy* is the in-repo home for the two-arm topology and the nuke coupling.
- The image name stays unprefixed: `agent-sentry-issue-analyzer`. Do NOT document a `bborbe/`-prefixed image.
- No new knobs beyond `BUILD_TARGET`, `IMAGE` and `VERSION`; do not document any other knob.
- Do NOT commit — dark-factory handles git.
- Existing tests must still pass.
- No new scenario.
</constraints>

<verification>
Run these from the repo root. `git check-ignore --no-index` is required because plain `git check-ignore` skips tracked paths and cannot distinguish this change from the status quo.

```bash
set -e

# .keep files exist.
for f in specs/.keep specs/in-progress/.keep specs/completed/.keep \
         prompts/.keep prompts/in-progress/.keep prompts/completed/.keep; do
	test -f "$f"
done

# Spec and prompt files are no longer ignored (untracked probe paths).
if git check-ignore -v --no-index specs/cluster-build-arm.md > /tmp/ig1.txt 2>&1; then
	echo "FAIL: specs/ is ignored"; cat /tmp/ig1.txt; exit 1
fi
if git check-ignore -v --no-index prompts/probe.md > /tmp/ig2.txt 2>&1; then
	echo "FAIL: prompts/ is ignored"; cat /tmp/ig2.txt; exit 1
fi

# Runtime logs, locks and vendor stay ignored.
git check-ignore -v --no-index specs/log/probe.log > /dev/null
git check-ignore -v --no-index prompts/log/probe.log > /dev/null
git check-ignore -v --no-index specs/in-progress/probe.lock > /dev/null
git check-ignore -v --no-index prompts/in-progress/probe.lock > /dev/null
git check-ignore -v --no-index vendor/probe > /dev/null

# Exact ignore counts.
test "$(grep -c '^/specs/$' .gitignore)" = "0"
test "$(grep -c '^/prompts/$' .gitignore)" = "0"
test "$(grep -c '^/specs/log$' .gitignore)" = "1"
test "$(grep -c '^/prompts/log$' .gitignore)" = "1"
test "$(grep -c '^/vendor$' .gitignore)" = "1"

# README documents the exact cluster invocation and the unprefixed name.
grep -q 'BUILD_TARGET=<cluster> REF=<sha> VERSION=<tag>' README.md
grep -q 'agent-sentry-issue-analyzer' README.md
grep -q 'buca' README.md

# CHANGELOG has exactly one Unreleased section.
test "$(grep -c '^## Unreleased$' CHANGELOG.md)" = "1"
grep -q 'feat:' CHANGELOG.md

make precommit
make test
```
</verification>
