---
status: completed
spec: [001-cluster-build-arm]
summary: Added BUILD_TARGET dispatch in Makefile.docker with shared IMAGE, narrowed Makefile.env guards, and added a CHANGELOG Unreleased entry.
execution_id: 5e82dc9b-7baf-5c38-8ac9-ef04e16a0999-exec-001-spec-001-makefile-dispatch
dark-factory-version: dev
created: "2026-11-08T10:42:00Z"
queued: "2026-11-08T10:56:41Z"
started: "2026-11-08T10:56:42Z"
completed: "2026-11-08T11:12:50Z"
branch: dark-factory/cluster-build-arm
---

<summary>
- `make build` gains a build-target switch: the default keeps today's local Docker build exactly as it is, and any other value routes to a Kubernetes Job.
- The cluster arm prints the exact image reference it will push before it calls the runner, so the target repository is visible on one line.
- The cluster arm passes the repository URL and the push-secret name as fixed literals; there are no new configuration knobs.
- A cluster build refuses to run unless the caller supplied the tag, so a tag nobody chose can never be pushed.
- `upload`, `clean` and `buca` refuse any cluster target at parse time, so they also refuse under `make -n`.
- The existing branch guard still blocks a bare `make build` on a feature branch; it is narrowed only for cluster builds.
- The image repository is defined once and shared by both arms, so the local tag and the cluster push cannot drift.
- No Go code changes and no new scenario; existing tests must still pass.
</summary>

<objective>
Implement the `Makefile.docker` dispatch that selects a local or cluster build arm from `BUILD_TARGET`, and narrow the `Makefile.env` guards so the local arm keeps its branch protection while a cluster build needs no branch. The local arm's command and tag must remain byte-for-byte equivalent to today.
</objective>

<context>
Read the approved spec first: `specs/in-progress/001-cluster-build-arm.md` — especially Desired Behavior 1-5, Constraints, Failure Modes, Security / Abuse Cases, and the "Suggested Decomposition" row 1.

Read these files before editing:
- `Makefile` (include order and `SERVICE = agent-sentry-issue-analyzer`)
- `Makefile.docker` (current build / check-go-mod / upload / clean / buca targets)
- `Makefile.env` (current parse-time branch guard)
- `Makefile.variables` (`BRANCH`, `ROOTDIR`)
- `common.env` (`DOCKER_REGISTRY` is exported here; the registry is always a prefix)
- `dev.env`, `prod.env` (they export `BRANCH`, so a command-line `BRANCH` must win)

Relevant coding plugin docs (read in the YOLO container at these paths):
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-makefile-commands.md`
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-precommit.md`

Prompt 2 adds `hack/build-cluster.sh`. This prompt only wires the dispatch that calls it; `make -n` never executes it, so the verification here passes before prompt 2 lands.
</context>

<requirements>
1. Edit `Makefile.docker`. Add the build-target selector and the shared image name near the top, before the `build` target:

```make
# BUILD_TARGET selects where `make build` runs. `local` (the default) uses the
# local Docker daemon; any other value names a cluster alias (sun / nukedev /
# nukeprod) and builds inside that cluster as a Job rendered from
# hack/build-job.yaml.
BUILD_TARGET ?= local

# One image repository for both arms, so the local tag and the cluster push
# cannot drift. SERVICE is defined in Makefile.
IMAGE ?= $(SERVICE)
```

2. Replace the `build` target with a two-arm dispatch. The prerequisite must be conditional so `check-go-mod` runs on the local arm only:

```make
.PHONY: build
build: $(if $(filter local,$(BUILD_TARGET)),check-go-mod)
ifeq ($(BUILD_TARGET),local)
	DOCKER_BUILDKIT=1 \
	docker build \
	--pull \
	--no-cache \
	--rm=true \
	--platform=linux/amd64 \
	--build-arg DOCKER_REGISTRY=$(DOCKER_REGISTRY) \
	--build-arg BRANCH=$(BRANCH) \
	--build-arg BUILD_GIT_VERSION=$$(git describe --tags --always --dirty) \
	--build-arg BUILD_GIT_COMMIT=$$(git rev-parse --short HEAD) \
	--build-arg BUILD_DATE=$$(date -u +%Y-%m-%dT%H:%M:%SZ) \
	-t $(DOCKER_REGISTRY)/$(IMAGE):$(BRANCH) \
	-f Dockerfile .
else
	@echo "$(DOCKER_REGISTRY)/$(IMAGE):$(VERSION)"
	BUILD_TARGET='$(BUILD_TARGET)' \
	REPO_URL='https://github.com/bborbe/agent-sentry-issue-analyzer.git' \
	REF='$(REF)' \
	TAG='$(VERSION)' \
	IMAGE='$(DOCKER_REGISTRY)/$(IMAGE)' \
	PUSH='true' \
	PUSH_SECRET='buildkit-push' \
	hack/build-cluster.sh
endif
```

   - The local recipe is the current one with `$(SERVICE)` replaced by `$(IMAGE)`; because `IMAGE ?= $(SERVICE)`, the rendered tag is unchanged (`docker.prod.nuke.benjamin-borbe.de:443/agent-sentry-issue-analyzer:$(BRANCH)`).
   - The cluster recipe prints the composed reference on one line before invoking the runner. Do not wrap or split that `@echo` line.
   - Pass the repository URL and the push-secret name as the literals shown. Do NOT introduce `BUILD_REPO_URL`, `BUILD_PUSH_SECRET`, `BUILD_TIMEOUT`, `BUILD_DOCKERFILE` or `ALLOW_DEFAULT_VERSION`.
   - Do not pass `DOCKERFILE`, `TIMEOUT`, `NAMESPACE` or `DOCKER_REGISTRY`: the copied runner defaults them (`Dockerfile`, `3600`, `build`, `docker.io`), and the Job template hardcodes `DOCKER_REGISTRY=docker.io` as a build arg.
   - Do not add a `REF` guard in the Makefile; the runner's `require REF` already fails with a clear message.

3. Keep `check-go-mod` exactly as it is. It must remain reachable only from the local arm via the conditional prerequisite in step 2. Do not invoke it through `$(MAKE)` inside the cluster recipe.

4. Change `upload` and `clean` to use `$(IMAGE)` instead of `$(SERVICE)` (identical value, single definition). Leave `buca: build upload clean` unchanged.

5. Edit `Makefile.env`. Keep the existing branch guard, but make it apply only when `BUILD_TARGET` is `local`, and add two parse-time refusals in the non-local branch. Replace the whole file with:

```make
include $(ROOTDIR)/common.env
ifeq ($(BRANCH),prod)
include $(ROOTDIR)/prod.env
else ifeq ($(BRANCH),dev)
include $(ROOTDIR)/dev.env
else ifneq ($(filter build upload clean buca,$(MAKECMDGOALS)),)
# The branch guard protects the local arm only: a cluster build names the ref it
# builds and needs no branch at all.
ifeq ($(BUILD_TARGET),local)
$(error Invalid BRANCH '$(BRANCH)' — must be 'dev' or 'prod')
endif
endif

# upload, clean and buca stay on the local arm. This is a parse-time refusal,
# not a recipe-level exit, so it also fires under `make -n`.
ifneq ($(BUILD_TARGET),local)
ifneq ($(filter upload clean buca,$(MAKECMDGOALS)),)
$(error upload, clean and buca are local-only; unset BUILD_TARGET (got '$(BUILD_TARGET)'))
endif
endif

# The cluster arm pushes the tag the caller named. VERSION has no default in
# this repo, so the refusal must fire when $(origin VERSION) is `file` (a
# Makefile default) or `undefined` (nobody set it).
ifneq ($(BUILD_TARGET),local)
ifneq ($(filter build,$(MAKECMDGOALS)),)
ifneq ($(filter file undefined,$(origin VERSION)),)
$(error the cluster build arm needs an explicit VERSION: make build BUILD_TARGET=$(BUILD_TARGET) REF=<sha> VERSION=<tag>)
endif
endif
endif
```

   - `BUILD_TARGET` is defined in `Makefile.docker`, which `Makefile` includes before `Makefile.env`, so the reference resolves at parse time.
   - The VERSION refusal must fire on `$(origin VERSION)` equal to `file` OR `undefined`. It must NOT fire for `command line` or `environment` origins.
   - Both refusals must be `$(error ...)`, not recipe-level `exit 1`, so they are observable under `make -n`.
   - A bare `make build` on a branch that is neither `dev` nor `prod` must still fail with `Invalid BRANCH` when `BUILD_TARGET` is unset (default `local`).

6. Do not change `Makefile`, `Makefile.variables`, `Makefile.precommit`, `common.env`, `dev.env` or `prod.env`.
</requirements>

<constraints>
- The local arm's command and its tag must not change. `make -n build BRANCH=dev` must print the same `docker build` recipe and tag as before, and must not mention `hack/build-cluster.sh`.
- `check-go-mod` preparation stays on the local arm only.
- The branch guard is narrowed only by `BUILD_TARGET`; a bare `make build` on a feature branch must still fail.
- `upload` / `clean` / `buca` remain local-only and are refused at parse time when `BUILD_TARGET` is not `local`.
- No new knobs beyond `BUILD_TARGET`, `IMAGE` and `VERSION`. Do NOT add `ALLOW_DEFAULT_VERSION`, `BUILD_TIMEOUT`, `BUILD_REPO_URL`, `BUILD_DOCKERFILE`, `BUILD_PUSH_SECRET` or any opt-out on the VERSION refusal.
- The repository URL and the push-secret name are literals, not configurable inputs.
- The image name stays unprefixed: `agent-sentry-issue-analyzer`. Do NOT add a `bborbe/` prefix.
- Do NOT commit — dark-factory handles git.
- Existing tests must still pass.
- No new scenario.
</constraints>

<verification>
Run these from the repo root. The `env -u` prefixes make the checks independent of any `BRANCH`, `VERSION` or `BUILD_TARGET` exported by the execution environment.

```bash
set -e

# Local arm: unchanged command and tag, no cluster runner.
env -u BUILD_TARGET -u VERSION make -n build BRANCH=dev > /tmp/local.txt
grep -q 'docker build' /tmp/local.txt
grep -q -- '-t docker.prod.nuke.benjamin-borbe.de:443/agent-sentry-issue-analyzer:dev' /tmp/local.txt
! grep -q 'hack/build-cluster.sh' /tmp/local.txt

# Cluster arm: runner invoked and composed reference printed on one line.
make -n build BUILD_TARGET=sun REF=0123456789abcdef0123456789abcdef01234567 VERSION=probe > /tmp/cluster.txt
grep -q 'hack/build-cluster.sh' /tmp/cluster.txt
grep -q 'docker.prod.nuke.benjamin-borbe.de:443/agent-sentry-issue-analyzer:probe' /tmp/cluster.txt
grep -q "REPO_URL='https://github.com/bborbe/agent-sentry-issue-analyzer.git'" /tmp/cluster.txt
grep -q "PUSH_SECRET='buildkit-push'" /tmp/cluster.txt

# Cluster arm refuses an unsupplied VERSION before any recipe.
if env -u VERSION make -n build BUILD_TARGET=sun REF=0123456789abcdef0123456789abcdef01234567 > /tmp/nover.txt 2>&1; then
	echo "FAIL: expected a VERSION refusal"; exit 1
fi
grep -q 'VERSION' /tmp/nover.txt

# Bare local build on a non-dev/prod branch still refuses.
if env -u BRANCH -u BUILD_TARGET -u VERSION make -n build > /tmp/nobranch.txt 2>&1; then
	echo "FAIL: expected Invalid BRANCH"; exit 1
fi
grep -q 'Invalid BRANCH' /tmp/nobranch.txt

# buca stays local.
env -u BUILD_TARGET -u VERSION make -n buca BRANCH=dev > /tmp/buca.txt
grep -q 'docker push' /tmp/buca.txt
grep -q 'docker rmi' /tmp/buca.txt

if make -n buca BRANCH=dev BUILD_TARGET=sun REF=0123456789abcdef0123456789abcdef01234567 VERSION=probe > /tmp/bucacluster.txt 2>&1; then
	echo "FAIL: expected a local-only refusal"; exit 1
fi
grep -qi 'local-only' /tmp/bucacluster.txt

grep -c '^BUILD_TARGET' Makefile.docker    # must be >= 1
grep -c '^IMAGE ?=' Makefile.docker         # must be 1

make precommit
make test
```
</verification>
