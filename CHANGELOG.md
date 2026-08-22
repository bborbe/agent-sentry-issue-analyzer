# Changelog

All notable changes to this project will be documented in this file.

Please choose versions by [Semantic Versioning](http://semver.org/).

* MAJOR version when you make incompatible API changes,
* MINOR version when you add functionality in a backwards-compatible manner, and
* PATCH version when you make backwards-compatible bug fixes.

## Unreleased

- feat: per-alert Sentry analyzer domain logic — planning (LIVE-state fetch + read-only source analysis → `## Analysis`) and execution (6-verdict rubric + noise disqualifiers → `## Verdict`) phases replace the generic claude-template prompts; verdict YAML schema + parser + validator (`pkg/verdict`); fail-fast `mcp__sentry__*` preflight (`pkg/preflight`). Watcher creates one task per new alert; agent analyzes that single alert (same architecture as all other agents).

## v0.1.5

- chore: Bump errcheck to v1.20.0 and golangci-lint to v2.13.1 for Go 1.27 support

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
