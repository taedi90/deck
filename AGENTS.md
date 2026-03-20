# AGENTS.md - Project Agent Guide

## Project Overview
### Purpose
- `deck` aims to be a single-binary workflow tool for infrastructure work in air-gapped environments with no SSH, no PXE, and no BMC.

### Key Paths
- `cmd/`: CLI entry points
- `internal/`: application core implementation
- `docs/`: user-facing documentation

### Style Guide
- Do not use emojis in docs, logs, or comments.
- Keep comments concise.

### Tech Stack
- Runtime: Linux target, Vagrant VM for CI E2E
- Language: Go
- Package manager: Go modules
- Test runners: `make test`, `make test-ai`, Vagrant scenario tests
- Formatting/linting: `gofmt`, `golangci-lint`

## Testing

| Change area | RunCommands |
|---|---|
| Any Go change | `make test && make lint` |
| AI build tag (`//go:build ai`) | `make test-ai` |
| Schema gen (`cmd/schema-gen/`, `internal/schemadoc/`) | `make generate && git diff --exit-code` |
| Step kind logic or workflow YAML | `make build` then `deck lint` against files in `test/workflows/scenarios/` and `docs/user-guide/examples/` |
| CLI surface (`cmd/deck/`) | `make build && ./bin/deck --help && ./bin/deck version` |
| Full CI parity (Linux, Vagrant required) | `make build lint test test-ai` then Vagrant E2E under `test/e2e/vagrant/` |

## Coding Principles
1) Think before coding
- Do not guess. State assumptions and uncertainty explicitly, and ask with evidence when blocked.
- If multiple interpretations are possible, present options and tradeoffs first.

2) Simplicity first
- Implement only the requested scope.
- Do not add speculative abstractions, configuration, or future-facing extensions.

3) Surgical changes
- Touch only the files and code paths required for the task.
- Remove unused code only when it is a direct result of your change.

4) Goal-driven execution
- Turn requests into concrete success criteria and iterate until verification passes.
- For multi-step work, start with a `Step -> Verify` plan.
