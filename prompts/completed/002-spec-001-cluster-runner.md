---
status: completed
spec: [001-cluster-build-arm]
summary: Copied the three hack/ cluster build runner files verbatim from bborbe/agent-task-executor, switched both Dockerfile builds from -mod=vendor to -mod=mod, and recorded the change under CHANGELOG Unreleased.
execution_id: 5e82dc9b-7baf-5c38-8ac9-ef04e16a0999-exec-002-spec-001-cluster-runner
dark-factory-version: dev
created: "2026-11-08T10:43:00Z"
queued: "2026-11-08T11:00:11Z"
started: "2026-11-08T11:12:51Z"
completed: "2026-11-08T11:16:18Z"
branch: dark-factory/cluster-build-arm
---

<summary>
- The repo gains the three runner files the cluster build arm needs, copied verbatim from the sibling agent repository.
- The copied runner validates every substituted value, refuses an existing Job, checks the cloned commit epoch, and reports the Job's own logs.
- The image build resolves Go dependencies in module mode, so a fresh clone with no `vendor/` directory builds successfully.
- The base images still resolve from the public registry inside the cluster because the Job template passes `DOCKER_REGISTRY=docker.io`.
- No new configuration knobs are added; the runner's own defaults cover the values this repo does not pass.
- No Go code changes and no new scenario; existing tests must still pass.
</summary>

<objective>
Add the three `hack/` files the cluster arm invokes and switch the shared Dockerfile from vendor mode to module mode, so the cluster Job can build the image from a fresh clone that has no `vendor/` directory. The local arm runs the same Dockerfile and keeps its existing command.
</objective>

<context>
Read the approved spec first: `specs/in-progress/001-cluster-build-arm.md` — especially Desired Behavior 2 and 6, Constraints (the `hack/` copy source and the `DOCKER_REGISTRY=docker.io` rule), Failure Modes, and Security / Abuse Cases.

Read before editing:
- `Dockerfile` (the two `-mod=vendor` build lines)
- `Makefile.docker` (prompt 1 added the cluster recipe that invokes `hack/build-cluster.sh`)
- `.dockerignore` (currently only `/data`; `vendor/` is not excluded from the build context, but a fresh clone has none)

Reference source for the three files: `https://github.com/bborbe/agent-task-executor`, `origin/master` (public). The files are:
- `hack/build-cluster.sh`
- `hack/cluster-alias.sh`
- `hack/build-job.yaml`

Relevant coding plugin docs (read in the YOLO container at these paths):
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-dockerfile-guide.md`
- `/home/node/.claude/plugins/marketplaces/coding/docs/go-precommit.md`

Prompt 1 must already be applied: `Makefile.docker` contains the cluster recipe that calls `hack/build-cluster.sh`.
</context>

<requirements>
1. Create `hack/` and copy exactly three files from the sibling repository at its default branch, unmodified. From the repo root:

```bash
tmp="$(mktemp -d)"
git clone --depth=1 https://github.com/bborbe/agent-task-executor "$tmp"
mkdir -p hack
cp "$tmp/hack/build-cluster.sh" "$tmp/hack/cluster-alias.sh" "$tmp/hack/build-job.yaml" hack/
chmod +x hack/build-cluster.sh hack/cluster-alias.sh
rm -rf "$tmp"
```

   - Do NOT copy `hack/boilerplate.go.txt` or `hack/update-codegen.sh`.
   - Do NOT edit, reformat, reindent or "improve" any of the three files. They carry the character whitelist, the embedded-newline rejection, the existing-Job refusal, the commit-epoch cross-check, and the `automountServiceAccountToken: false` / security-context split. Copying them verbatim is the requirement.
   - The runner defaults `NAMESPACE=build`, `PUSH=true`, `TIMEOUT=3600`, `DOCKERFILE=Dockerfile` and `DOCKER_REGISTRY=docker.io`; this repo's dispatch deliberately does not pass those, so do not add defaults to the dispatch or the runner.
   - `cluster-alias.sh` is required even though it only defines variables and the `kc()` helper: `build-cluster.sh` sources it and fails at that line without it.

2. Edit `Dockerfile`: change both `-mod=vendor` occurrences to `-mod=mod`. The two lines become:

```dockerfile
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -mod=mod -ldflags "-s" -a -installsuffix cgo -o /main
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -mod=mod -ldflags "-s" -a -installsuffix cgo -o /create-tasks ./cmd/create-tasks
```

   - Change nothing else in the Dockerfile. In particular, leave `ARG DOCKER_REGISTRY=docker.prod.nuke.benjamin-borbe.de:443` as the default; the cluster Job overrides it with `DOCKER_REGISTRY=docker.io` at build time so base images resolve publicly. Do not change that override back to the mirror.
   - Do not remove or rewrite the `check-go-mod` target in `Makefile.docker`; the local arm keeps its `go mod vendor` preparation, and `-mod=mod` makes the build ignore any `vendor/` that preparation leaves behind.

3. Do not modify `Makefile.docker`, `Makefile.env`, `.gitignore`, `README.md` or `CHANGELOG.md` in this prompt. Prompt 1 owns the dispatch; prompt 3 owns the repo hygiene and docs.

4. No Go source files change. Do not add tests for the shell scripts; their behaviour is exercised by the operator's real cluster build (spec verification ladder) and by the dry-run checks in prompt 1.
</requirements>

<constraints>
- The three `hack/` files are copied from `agent-task-executor` at `origin/master`, unmodified.
- The Job template already passes `DOCKER_REGISTRY=docker.io` as a build argument; do not change that back to the private mirror.
- `IMAGE`, `SERVICE`, the registry and the tag must each satisfy the runner's character whitelist; no value may carry a newline. Do not bypass or weaken that whitelist.
- The repository URL and the push-secret name remain literals in the dispatch, not configurable inputs.
- The image name stays unprefixed: `agent-sentry-issue-analyzer`. Do NOT add a `bborbe/` prefix.
- No new knobs beyond `BUILD_TARGET`, `IMAGE` and `VERSION`.
- Do NOT commit — dark-factory handles git.
- Existing tests must still pass.
- No new scenario.
</constraints>

<verification>
Run these from the repo root.

```bash
set -e

# The three runner files exist and parse.
test -f hack/build-cluster.sh
test -f hack/cluster-alias.sh
test -f hack/build-job.yaml
test -x hack/build-cluster.sh
test -x hack/cluster-alias.sh
bash -n hack/build-cluster.sh
bash -n hack/cluster-alias.sh

# hack/ is not ignored.
if git check-ignore -v --no-index hack/build-cluster.sh > /tmp/ign.txt 2>&1; then
	echo "FAIL: hack/build-cluster.sh is ignored"; cat /tmp/ign.txt; exit 1
fi

# Module mode, not vendor mode.
grep -q -- '-mod=mod' Dockerfile
! grep -q -- '-mod=vendor' Dockerfile
test "$(grep -c -- '-mod=mod' Dockerfile)" = "2"

# Prompt 1's dispatch still points at the runner.
test "$(grep -cE '^[[:space:]]*hack/build-cluster\.sh' Makefile.docker)" -ge 1

# The copied files are byte-identical to upstream.
tmp="$(mktemp -d)"
git clone --depth=1 https://github.com/bborbe/agent-task-executor "$tmp"
diff -u "$tmp/hack/build-cluster.sh" hack/build-cluster.sh
diff -u "$tmp/hack/cluster-alias.sh" hack/cluster-alias.sh
diff -u "$tmp/hack/build-job.yaml" hack/build-job.yaml
rm -rf "$tmp"

make precommit
make test
```
</verification>
