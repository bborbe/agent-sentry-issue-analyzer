---
status: prompted
approved: "2026-11-08T10:31:18Z"
generating: "2026-11-08T10:34:51Z"
prompted: "2026-11-08T10:49:37Z"
branch: dark-factory/cluster-build-arm
---

## Summary

- This repo's image can currently be produced on one machine only: the Mac.
- Its four sibling agent repos can also build their image inside a Kubernetes cluster, unattended, from any committed ref.
- Add that second build path here as an arm beside the existing local one, leaving the local command and tag as they are.
- The cluster arm builds a named commit as a Kubernetes Job and pushes the result to the same image repository the local arm pushes to, so a release no longer needs this machine.
- Three supporting edits make the arm reachable: the image must build from a fresh clone, the branch guard must stop blocking a cluster build, and the repo must track the directories the pipeline writes into.

## Problem

`agent-sentry-issue-analyzer` is the one agent repo whose image cannot be produced without a human at this Mac. Its four siblings each carry a cluster build arm; this repo carries only the local `docker build` path. On 2026-11-06 its `:prod` image had to be built by hand, and nothing else could have produced it.

The arm is not a straight copy. Four properties of this repo make a naive paste fail at runtime rather than at review:

- **The image name is unprefixed, and the deployment depends on that.** `Makefile` declares `SERVICE = agent-sentry-issue-analyzer`, and `bborbe/nuke` pulls exactly that path — `agent/values-dev.yaml:1526,1605,1689` and `agent/values-prod.yaml:660,733,823` carry `image: agent-sentry-issue-analyzer`, rendered against `registry: docker.prod.nuke.benjamin-borbe.de:443`. The four siblings declare `IMAGE ?= bborbe/agent-<name>` and nuke pulls `bborbe/agent-<name>`, so copying the sibling `bborbe/` prefix here would push to a repository nothing deploys.
- **`common.env` pins `DOCKER_REGISTRY` to the private mirror.** The siblings ship no `common.env`. Because the registry is always a prefix, `$(DOCKER_REGISTRY)/$(SERVICE)` is a private-registry path and can never fall into the Docker Hub `library` namespace — the hazard the siblings' `bborbe/` prefix guards against does not exist here.
- **It has no version concept.** The local arm tags by branch, and nothing in the repo defines `VERSION`.
- **`Makefile.env` refuses the build goals on any branch that is not `dev` or `prod`** — at parse time, before any target runs. On a feature branch `make -n build` dies with `Invalid BRANCH 'feat-cluster-build-arm'`, so the guard blocks a cluster build as firmly as it blocks a local one.

Two further properties only surface once the build runs from a clone rather than from a developer's working tree:

- The Dockerfile builds with `-mod=vendor`, but `vendor/` is gitignored and absent from `origin/master`. A Job that clones the repo has no `vendor/` directory, so the build fails before it starts.
- `.gitignore` excludes `specs/` and `prompts/` outright, where dark-factory's canonical layout tracks both directories and ignores only their runtime logs.

## Goal

`make build` on this repo produces the image locally exactly as it does today, and `make build BUILD_TARGET=<cluster> REF=<sha> VERSION=<tag>` produces it inside that cluster as a Kubernetes Job, pushing `docker.prod.nuke.benjamin-borbe.de:443/agent-sentry-issue-analyzer:<tag>` — the repository the fleet already pulls. Both arms work from the same checkout, and the cluster arm works from a fresh clone with no local preparation.

## Non-goals

- Changing the local arm's command or its tag. Both stay as they are. The shared Dockerfile does move to module resolution, because the cluster arm cannot build from a clone otherwise; the local arm runs the same Dockerfile and is unaffected by the change in how dependencies are fetched.
- Renaming the image to the siblings' `bborbe/`-prefixed form. That is a deploy-side rename across six nuke values lines and is its own change; this spec keeps the name the deployment already consumes.
- Touching the four sibling agent repos — they already carry their arms.
- Building at release or tag time. This spec adds the capability; *when* a build is triggered is a separate concern.
- Extracting the shared build runner into a common upstream template. The files are copied, not shared.
- Extra build knobs beyond `BUILD_TARGET`, `IMAGE` and `VERSION`. The copy source also carries `ALLOW_DEFAULT_VERSION`, `BUILD_TIMEOUT`, `BUILD_REPO_URL`, `BUILD_DOCKERFILE` and `BUILD_PUSH_SECRET`. `ALLOW_DEFAULT_VERSION` in particular would reintroduce an opt-out on the refusal this spec ships, so it is not copied. The repository URL and the push-secret name are not knobs either: the dispatch passes both as literals, because a configurable clone source on a build that mounts a push credential is a hazard with no use case here.
- Refreshing the local clone's commit drift.

## Acceptance Criteria

- [ ] **The local arm's command and tag are unchanged, and its branch guard still fires** — with no `BUILD_TARGET` set, `make -n build BRANCH=dev` prints the `docker build` recipe and no reference to `hack/build-cluster.sh`; a real `make build BRANCH=dev` exits 0 and `docker image inspect docker.prod.nuke.benjamin-borbe.de:443/agent-sentry-issue-analyzer:dev` exits 0; and `make -n build` with no `BRANCH` still fails with `Invalid BRANCH`. Evidence: the three commands' output and exit codes, plus the inspected image id.
- [ ] **Post-Deploy (Rung-2):** the cluster arm builds and pushes from a fresh clone — `make build BUILD_TARGET=sun REF=<full 40-character sha> VERSION=<scratch-tag>` exits 0, and `crane digest docker.prod.nuke.benjamin-borbe.de:443/agent-sentry-issue-analyzer:<scratch-tag>` resolves to a digest. Evidence: the exit code, the Job name, the build pod's `spec.nodeName`, the resolved digest, and `docker image inspect` on the same reference failing on this Mac with `No such image` — a stopped daemon or a malformed reference does not satisfy this.
  - `deploy_check:` `kubectl --context default --kubeconfig ~/.kube/sun -n build get secret buildkit-push -o jsonpath='{.metadata.name}'`
  - `deploy_target:` `buildkit-push`
- [ ] **The cluster arm pushes to the repository the deployment consumes** — the dispatch prints the composed reference `docker.prod.nuke.benjamin-borbe.de:443/agent-sentry-issue-analyzer:<tag>` on one line before it calls the runner, so `make -n build BUILD_TARGET=sun REF=<sha> VERSION=probe` shows that exact string. Evidence: the printed line, and the digest above resolving at exactly that repository.
- [ ] **Post-Deploy (Rung-2):** the cluster arm refuses a tag it did not receive from the caller — `make build BUILD_TARGET=sun REF=<sha>` with no `VERSION` on the command line exits non-zero and prints a message naming `VERSION`, before any Job is applied. Evidence: the exit code, the message, and `kubectl --context default --kubeconfig ~/.kube/sun -n build get jobs` showing no new Job for that run.
  - `deploy_check:` `kubectl --context default --kubeconfig ~/.kube/sun -n build get secret buildkit-push -o jsonpath='{.metadata.name}'`
  - `deploy_target:` `buildkit-push`
- [ ] **`hack/` holds the three files and the cluster branch invokes the runner** — at the recorded head SHA, `git ls-tree --name-only HEAD hack/` lists `build-cluster.sh`, `cluster-alias.sh` and `build-job.yaml`, and `grep -cE '^[[:space:]]*hack/build-cluster\.sh' Makefile.docker` returns ≥ 1 on an invocation line rather than a comment. Evidence: the three names and the count.
- [ ] **`upload` / `clean` / `buca` stay local, refused at parse time** — `make -n buca BRANCH=dev` prints `docker push` and `docker rmi`; `make -n buca BRANCH=dev BUILD_TARGET=sun REF=<sha> VERSION=probe` exits non-zero and prints a message naming those targets as local-only. The refusal is a parse-time `$(error …)`, so it fires under `-n` where no recipe runs. Evidence: both outputs and exit codes.
- [ ] **The spec and prompt directories are tracked by `.gitignore`, not merely by a forced add** — `git check-ignore -v --no-index specs/cluster-build-arm.md` exits 1 and `git check-ignore -v --no-index prompts/probe.md` exits 1, on an untracked path as well as a tracked one; `grep -c '^/specs/$' .gitignore` returns 0 and `grep -c '^/specs/log$' .gitignore` returns 1. Evidence: both exit codes and both counts — `git check-ignore` skips tracked paths by default, which is why `--no-index` is required and why the plain form cannot distinguish this change from the status quo.
- [ ] **The documented build path matches reality** — `README.md` § *Build + Deploy* shows the exact cluster invocation `make build BUILD_TARGET=<cluster> REF=<sha> VERSION=<tag>`, states that `buca` remains local, and records that the image name is unprefixed because `bborbe/nuke`'s agent values pull it; `CHANGELOG.md` carries a `## Unreleased` section with a bullet for this change. Evidence: `grep -c 'BUILD_TARGET=<cluster> REF=<sha> VERSION=<tag>' README.md` ≥ 1, `grep -c 'agent-sentry-issue-analyzer' README.md` ≥ 1 in that section, and `grep -c '^## Unreleased$' CHANGELOG.md` = 1.

## Verification

### Container-executable (runs inside the YOLO container at prompt time)

- `make precommit` — lint, format and vet clean
- `make test` — unit and integration suites pass
- `bash -n hack/build-cluster.sh && bash -n hack/cluster-alias.sh` — the copied runners parse
- `grep -c '^BUILD_TARGET' Makefile.docker` returns ≥ 1
- `grep -cE '^[[:space:]]*hack/build-cluster\.sh' Makefile.docker` returns ≥ 1
- `git ls-tree --name-only HEAD hack/` lists the three expected files
- `git check-ignore -v --no-index specs/cluster-build-arm.md` exits 1; `git check-ignore -v --no-index prompts/probe.md` exits 1
- `grep -c '^/specs/$' .gitignore` returns 0; `grep -c '^/specs/log$' .gitignore` returns 1
- `make -n build BRANCH=dev` prints a `docker build` recipe and no `hack/build-cluster.sh`
- `make -n build BUILD_TARGET=sun REF=<sha> VERSION=probe` prints `hack/build-cluster.sh` and the composed `docker.prod.nuke.benjamin-borbe.de:443/agent-sentry-issue-analyzer:probe` line
- `make -n build` with no `BRANCH` still fails with `Invalid BRANCH`
- `grep -rn 'BUILD_TARGET' --exclude-dir=vendor .` — every reference in the repo names the new invocation; no stale build documentation is left behind

### Operator-executable (runs on the host after PR merge, spec verification ladder)

- `make build BUILD_TARGET=sun REF=<sha> VERSION=<scratch-tag>` — one real cluster build, Job name and digest recorded
- `crane digest docker.prod.nuke.benjamin-borbe.de:443/agent-sentry-issue-analyzer:<scratch-tag>` — the push landed
- `make build BRANCH=dev` then `docker image inspect docker.prod.nuke.benjamin-borbe.de:443/agent-sentry-issue-analyzer:dev` — the local arm still works

## Desired Behavior

1. **A build target selects the arm.** `BUILD_TARGET` defaults to `local`. With `local`, `make build` behaves exactly as it does today: it prepares the module cache, it runs `docker build` on this machine, and it tags `$(DOCKER_REGISTRY)/$(SERVICE):$(BRANCH)`.
2. **Any other value routes to a cluster Job.** With `BUILD_TARGET` set to a cluster alias or kubectl context, `make build` prints the composed image reference, then renders the Job template and applies it through the vendored runner, passing the repository URL and the push-secret name as literals. It waits for the Job and reports success or failure with the Job's own log lines.
3. **The cluster arm needs a caller-supplied tag.** The cluster arm refuses to run unless `VERSION` came from the caller — the command line, or the environment. It refuses in particular when `VERSION` resolves from the file or is undefined, so a tag nobody chose can never be pushed. The refusal names the variable and happens before any Job is created.
4. **Both arms publish to one repository.** The cluster arm uses `$(DOCKER_REGISTRY)/$(IMAGE):$(VERSION)` with `IMAGE ?= $(SERVICE)` — the same repository the local arm pushes and the fleet pulls, differing only in tag, and defined once so the two cannot drift.
5. **The branch guard covers the local arm only.** `Makefile.env` still refuses the `build` / `upload` / `clean` / `buca` goals on a branch that is not `dev` or `prod` when `BUILD_TARGET` is `local`; it does not refuse them for a cluster build, which needs no branch at all. `upload`, `clean` and `buca` remain local-only and are refused at parse time when given a cluster `BUILD_TARGET`.
6. **The cluster build works from a clone.** The image build resolves its Go dependencies in module mode, so a Job that clones the repo with no `vendor/` directory builds the same way a developer's checkout does.
7. **The pipeline's own directories are versioned.** `specs/` and `prompts/` are no longer ignored wholesale; the canonical dark-factory layout tracks both, ignoring only their runtime logs, with `.keep` files so the directories exist in a fresh clone.
8. **The documented build path matches reality.** The README's build section shows both arms, says `buca` stays local, and records why the image name is unprefixed. The CHANGELOG records the change.

## Constraints

- The local arm's command and its tag must not change, and its `check-go-mod` preparation stays on the local branch only. The cluster arm builds a named ref, which need not be the local `HEAD`, so preparing the local working tree there would be meaningless.
- The `Makefile.env` guard is narrowed only by `BUILD_TARGET`: a bare `make build` on a feature branch must still fail. Narrowing it any further removes the protection that keeps `:dev` and `:prod` from being overwritten by a branch build.
- The local-only refusal for `upload` / `clean` / `buca` is a `$(error …)` keyed on the goal list inside the non-local branch, not a recipe-level `exit 1`. A recipe-level refusal is invisible to `make -n`, so the AC could not observe it.
- `VERSION` has no definition anywhere in this repo today, so the refusal must fire when `$(origin VERSION)` is `file` **or** `undefined`. A guard that tests only for `file` never fires here and would leave the caller with the runner's generic `TAG is required` instead.
- The three `hack/` files are copied from `agent-task-executor` at `origin/master`, unmodified: `hack/build-cluster.sh`, `hack/cluster-alias.sh` and `hack/build-job.yaml`. That repository is public, so a `git clone --depth=1 https://github.com/bborbe/agent-task-executor` from inside the container reaches them; the two other files under its `hack/` are not copied. `cluster-alias.sh` is required — the runner sources it and fails at that line without it.
- The Job template already passes `DOCKER_REGISTRY=docker.io` as a build argument, so the Dockerfile's base images resolve from the public registry inside the cluster rather than from the private mirror. Do not change that back to the mirror.
- `IMAGE`, `SERVICE`, the registry and the tag must each satisfy the runner's character whitelist; no value may carry a newline.
- `.gitignore` keeps every entry it has today — including `/vendor`, which `check-go-mod` would otherwise stage on the next `git add .` — removes only `/prompts/` and `/specs/`, and adds the canonical layout from `docs/init-project.md` § Step 5: `/.dark-factory.lock`, `/.dark-factory.log`, `/prompts/log`, `/specs/log`, `/prompts/in-progress/*.lock`, `/specs/in-progress/*.lock`, plus `.keep` files under `specs/`, `specs/in-progress/`, `specs/completed/`, `prompts/`, `prompts/in-progress/` and `prompts/completed/`.
- `README.md` § *Build + Deploy* is the in-repo home for the two-arm topology and the nuke coupling; the spec dies after implementation, the README does not.
- No new scenario. The behaviour is reachable by checks on the rendered invocation plus one real cluster build.

## Assumptions

These must hold before the cluster AC can pass; none is created by this spec.

- The `build` namespace and the `buildkit-push` dockerconfigjson secret exist in the target cluster.
- The repo is public, so the Job's anonymous `git fetch` succeeds; the init container carries no clone credential.
- `crane` is installed on the host running the ACs.
- `REF` exists in the local repository **and on `origin`**: the runner derives `SOURCE_DATE_EPOCH` from the local repository, and the Job's init container fetches the ref from the remote. A commit that exists only locally fails at the clone.
- The build pod can reach the Go module proxy.
- `kubectl` is configured for the target cluster through the alias files `hack/cluster-alias.sh` resolves.

## Failure Modes

| Trigger | Expected behavior | Recovery |
|---|---|---|
| `BUILD_TARGET` names an unknown cluster or context | The runner fails before applying anything and lists the accepted values | Re-run with a listed target; no Job exists to clean up |
| `VERSION` omitted on the cluster arm | Refusal before the Job is created, naming `VERSION` | Re-run passing `VERSION=<scratch-tag>` |
| `REF` is a short SHA, or is not in the local repository | The runner dies resolving `SOURCE_DATE_EPOCH` | `git fetch`, then re-run with the full 40-character SHA |
| `REF` exists locally but was never pushed | The clone init container fails: the ref does not exist on `origin` | Push the branch or commit, then re-run |
| The clone's commit epoch differs from the locally derived one | The build container refuses before `buildctl` runs and names both epochs | `git fetch`, then re-run |
| A Job with the derived name already exists in `build` | The runner refuses and names the existing Job rather than overwriting it | Delete the finished Job, then re-run |
| The runner is interrupted after `apply` | The Job keeps running in the cluster while the local invocation exits; the next run then hits the existing-Job refusal | Inspect `kubectl -n build get jobs`, read the pod's log, delete the Job once finished |
| The build pod cannot reach the module proxy | The Job fails; the runner prints the last 100 log lines and the Job name | Inspect the Job logs; the local arm is unaffected |
| The build pod is OOM-killed or throttled | The Job reports `failed`; the pod status carries `OOMKilled` or a CPU-throttling event | Raise the template's limits in the same change, or build locally for that one image |
| The registry throttles or rejects the push | The build fails at the push step with the registry's own error | Re-check the `build` namespace secret with `kubectl -n build get secret buildkit-push -o jsonpath='{.metadata.name}'`, then re-run |
| A caller-supplied `VERSION` names an already-released tag | No guard — the push replaces that tag irreversibly | Re-push the image from the released tag, then cut a new release |
| The local arm regresses | `make build BRANCH=dev` fails or tags differently | Revert the arm; the local path is the regression gate |

## Security / Abuse Cases

- The runner substitutes caller-supplied values into a manifest and into embedded shell, so every value is checked against a character whitelist and rejected for embedded newlines before rendering. Do not add a value path that bypasses that check.
- The repository URL and the push-secret name are literals in the dispatch, not configurable inputs. The whitelist constrains the *shape* of a URL, not *which* repository is cloned and built with a mounted push credential; a configurable clone source on this build would let a caller point the credential-bearing Job at another repo.
- The build Job mounts a push credential from a `build`-namespace secret and runs with `automountServiceAccountToken: false`. The credential is scoped to pushing an image; it grants nothing else.
- The build container runs as an unprivileged user with the sandbox profiles relaxed because rootless BuildKit requires it. That relaxation is deliberate and must not be copied to the clone init container.
- `VERSION` is caller-controlled and becomes an image tag and a Job name. The tag is whitelisted and the Job name is sanitized to a valid DNS name, truncated, and stripped of trailing separators.

## Suggested Decomposition

Prompts should be generated in this order — each row is a single prompt with a clear scope.

| # | Prompt focus | Covers DBs | Covers ACs | Depends on |
|---|---|---|---|---|
| 1 | `Makefile.docker` dispatch: the `BUILD_TARGET` split, `IMAGE`/`VERSION`, the composed-reference echo, local-only `upload`/`clean`/`buca`, and the `Makefile.env` guard narrowed by `BUILD_TARGET` | 1, 2, 3, 4, 5 | 1, 3, 4, 6 | — |
| 2 | The build path itself: the three `hack/` files and the module-mode image build | 2, 6 | 2, 5 | prompt 1 (the dispatch calls the runner) |
| 3 | Repo hygiene and docs: the `.gitignore` layout with `.keep` files, the README build section, the CHANGELOG bullet | 7, 8 | 7, 8 | — |

Rationale: prompt 1 establishes the dispatch and the guard that makes any of it reachable; prompt 2 supplies the runner the dispatch calls; prompt 3 is independent and touches no build logic.

## Do-Nothing Option

Leaving this repo without a cluster arm keeps one agent image tied to the Mac. Every weekly rebuild, every release cut while the operator is away from this machine, and every future unattended pipeline needs a human at the keyboard for this one repo — while its four siblings build themselves. The cost is not the one build; it is that the automation has a hole shaped exactly like the repo nobody is watching.
