# Changelog

## Unreleased

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
