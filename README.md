# Agent Sentry Issue Analyzer

Analyzes Sentry issues and classifies them (severity + fixability) for the Sentry resolution pipeline. Consumes one `sentry-issue-analyzer` task per run — the `sentry-collector` agent step (fleet's first multi-agent workflow: collector fans out, analyzer consumes) creates one vault task per active Sentry alert; this agent is triggered per task and analyzes that single alert.

## Role

Planner in the Sentry resolution pipeline (see [[Agent Pipeline Concept]]). Consumes a task from Kafka via `agent-task-executor`, reads the alert (stack trace + `sentry_link`), fetches LIVE state, reads the implicated source (read-only), and writes the root-cause analysis + verdict back to the task body.

## Shape

Built on `bborbe/agent-claude` template — AI-heavy reference. Two active phases (planning → execution; no ai_review — write-verification is part of execution). The watcher creates tasks; the agent processes exactly one.

## Phases

| Phase | Step | Output |
|---|---|---|
| `planning` | Fetch LIVE Sentry state, read implicated source (read-only), root-cause analysis | `## Analysis` (file.go:line, root cause, certainty) |
| `execution` | Re-check LIVE state, apply 7-verdict rubric + noise disqualifiers | `## Verdict` YAML block |
| `done` | Terminal — verdict written back to task body | — |

## Fix agent (`sentry-fix`)

The pipeline's third task type (`assignee: sentry-fix-agent`). It turns a
High/High `real bug` deep verdict into a filed `kind: bug` spec: resolve the
repo behind the verdict's `file:line` (prompt-backed), confirm the citation at
the current revision, build the spec from the verdict's own words, and write it
as an approved `kind: bug` spec at `specs/in-progress/bug-<issue-slug>.md`
through the GitHub Contents API. The writer opens a draft pull request
from the `sentry-fix/<issue-slug>` branch to the repository's default branch.
Stale citations file nothing (recorded `status: stale` in `## Fix Result`), an
unmappable resolution is recorded `status: skipped` instead of failing the
Job, a repo `REPO_ALLOWLIST` does not admit is recorded `status: declined`
with the repo named (a malformed allowlist still fails the Job), API failures
fail loudly, and emission is idempotent per Sentry
issue id: one spec, one branch, one PR. `REPO_ALLOWLIST` bounds which repos it
may write into; it takes the repository-wide convention every lane shares —
comma-separated `github.com/owner/name`, `github.com/owner/*` to cover an org,
and a leading `!` to exclude (e.g.
`github.com/Seibert-Data/*,!github.com/Seibert-Data/test-dev`). Empty means
unbound (the App installation scope remains the hard wall); a non-empty list
that names no inclusion, or carries a token that is not a valid pattern, fails
closed.

See [docs/fix-lane.md](docs/fix-lane.md) for the watcher's four-condition
contract and the lane end to end.

## Build + Deploy

One image, one registry, one tag — the pipeline every mirrored agent repo uses:

```
build (local or sun)  →  docker.io/bborbe/agent-sentry-issue-analyzer:<version>
                      →  nuke MIRROR_IMAGES  →  docker.prod.nuke…/bborbe/agent-sentry-issue-analyzer:<version>
                      →  nuke/agent/values-<stage>.yaml pins that version  →  the Job pulls it
```

### Local arm (this machine)

```bash
make precommit              # lint + test
make buca                   # build + push + clean, no apply
VERSION=v0.25.0 make buca   # name the release tag explicitly
```

`make buca` composes `docker.io/bborbe/agent-sentry-issue-analyzer:$(VERSION)`.
`VERSION` defaults to `git describe --tags --always --dirty`, which names the
commit: `vX.Y.Z-<n>-g<sha>` once HEAD has moved past a release, exactly `vX.Y.Z`
only when HEAD *is* that release commit, and `-dirty` appended when the working
tree carries uncommitted changes. So a build works on any branch, a dirty tree
never lands on a clean commit tag, and only a build at a released commit re-pushes
that tag with a new digest — build a release once. There is no branch or stage
input. `buca` stays local-only: it acts on this machine's Docker daemon and
refuses any `BUILD_TARGET` other than `local`.

### Cluster arm (Kubernetes Job)

```bash
make build BUILD_TARGET=<cluster> REF=<full-sha> VERSION=<tag>
```

The cluster arm clones `REF` inside the target cluster, builds it as a Kubernetes
Job, and pushes the same Docker Hub reference. It needs no local Docker daemon
and no prepared working tree. `VERSION` is required: the arm refuses to run
without one, because it pushes.

`DOCKER_REGISTRY` (default `docker.io`) is the **base-image source**;
`PUSH_REGISTRY` (default `docker.io`) is the **push target**. They are separate
knobs so a repo whose base images resolve from the private mirror still cannot
push there.

### Deployment

`bborbe/nuke` owns the cluster objects. Bump the pin in
`agent/values-{dev,prod}.yaml` (`image: bborbe/agent-sentry-issue-analyzer`,
`tag: <version>`), add the same tag to `agent/Makefile`'s `MIRROR_IMAGES`, then
`/nuke-deploy <stage> agent`: the mirror copies the Docker Hub image into the
private registry and the apply pins it. This build never writes the private
registry.

The `k8s/` directory was deleted on 2026-09-10 — every manifest it held described
dead quant leftovers. The live sentry agents (`sentry-analyzer-agent`,
`sentry-collector-agent`) run on nukedev/nukeprod and their Config CRs, Secrets,
PVCs and quotas are managed by the `nuke` repo (`agent/values-{dev,prod}.yaml`),
which adopted the sentry pipeline from the hand-applied kubectl CRs on 2026-08-26.

## SDK

Imports `github.com/bborbe/agent` (see [bborbe/agent](https://github.com/bborbe/agent)) for runtime contract.

## License

See [LICENSE](LICENSE).
