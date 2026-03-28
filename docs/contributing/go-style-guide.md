# Go Style Guide

This document defines the Go coding conventions for `deck`.

It is based on the [Uber Go Style Guide](https://github.com/uber-go/guide/blob/master/style.md), but it is adapted to this repository's current tooling, Go version, and design goals. When this document differs from the Uber guide, follow this document.

## Goals

- Keep code easy to review in a safety-sensitive infrastructure tool.
- Prefer explicit control flow, typed boundaries, and small trust surfaces.
- Keep lint and formatting rules enforceable by tooling where practical.
- Avoid rules that add ceremony without improving readability or safety.

## Source of truth

- `gofumpt` and `gci` define formatting and import ordering.
- `golangci-lint` defines the enforced lint baseline.
- This document defines naming, error-handling, and code review conventions that are not fully enforced by tooling.

## Formatting and imports

- Run `gofumpt` formatting through the repo tooling.
- Use the repo's `gci` import order instead of generic `goimports` ordering.
- Do not manually align spacing or create formatting exceptions that fight the formatter.
- Avoid overly long lines when a split improves readability.

## Naming

- Follow idiomatic Go names: short, descriptive, and package-aware.
- Use `ErrXxx` for exported sentinel errors and `errXxx` for unexported sentinel errors.
- Use `XxxError` for custom error types.
- Do not shadow predeclared identifiers such as `error`, `string`, `len`, or `copy`.
- Include units in names when a value is not expressed as `time.Duration`, for example `timeoutSeconds` or `intervalMillis`.
- Use field tags on structs that are serialized to JSON, YAML, or similar external formats.

## Errors

- Return errors instead of panicking for normal failure paths.
- Only `main` should decide process exit.
- Add context with `fmt.Errorf("context: %w", err)` when the extra context helps the caller or operator.
- Do not log an error and then return the same error unless the log is the final handling point.
- Prefer `errors.Is` and `errors.As` for branching on specific failures.
- Keep error messages concise. Prefer `"open config: %w"` over `"failed to open config: %w"`.

## Panic and exit

- Do not use `panic` for ordinary runtime errors.
- Restrict `os.Exit` to `main`.
- Treat `panic` as reserved for impossible states, process initialization traps, or truly unrecoverable failures.
- In tests, prefer `t.Fatal`, `t.Fatalf`, or helpers that fail the test directly.

## Time and durations

- Use `time.Time` for instants.
- Use `time.Duration` for elapsed time and timeouts inside Go code.
- If an external format cannot carry `time.Duration`, keep the unit in the field name.

## Concurrency

- Prefer simple concurrency shapes that are easy to stop, wait for, and reason about.
- Do not start fire-and-forget goroutines in production code without an explicit ownership and shutdown path.
- Use unbuffered channels or channels of size `1` by default. Larger buffers require a clear reason.
- Keep synchronization primitives as implementation details rather than part of a public API.

## Atomics

- Use the Go standard library when atomic operations are required.
- Do not add `go.uber.org/atomic` to this repository.
- Prefer the typed atomics available in `sync/atomic` when they fit the use case.

## Structs and APIs

- Prefer explicit fields over embedding when embedding would leak implementation details.
- Be especially careful with embedding on exported types, even inside `internal/` packages.
- Use compile-time interface compliance assertions selectively when a type is expected to satisfy a stable interface contract.
- Omit zero-value fields in struct literals when that keeps the literal easier to scan.
- Use keyed struct literals when the type is exported or the field order is not obvious.

## Globals and initialization

- Avoid mutable global state.
- Prefer dependency injection, constructors, or small structs over package-level test seams.
- Avoid `init()` unless there is a strong, deterministic reason.
- `init()` must not hide I/O, environment reads, network access, or order-sensitive behavior.

## Slices, maps, and ownership

- Be explicit about ownership when accepting or returning slices and maps.
- Copy slices or maps at package boundaries when callers must not mutate internal state.
- Return `nil` slices when that is the natural zero value, unless an empty allocated slice improves API clarity.

## Functions and control flow

- Keep functions focused on one clear responsibility.
- Reduce nesting with early returns when it improves readability.
- Avoid unnecessary `else` blocks after `return`.
- Avoid naked returns.
- Keep variable scope as small as practical.
- Prefer `defer` for cleanup unless a measured hot path shows a real need to avoid it.

## Repository-specific guidance

- Prefer typed boundaries over raw maps or shell-oriented helper code when working in workflow, bundle, install, and server paths.
- Keep trust-boundary operations localized in helper packages such as filesystem, file mode, host filesystem, and command execution utilities.
- When a `gosec` suppression is unavoidable, keep it helper-local and document it in `.golangci.yml` rather than scattering suppressions through feature code.
- In CLI code, prefer `run`-style functions that return errors and keep process exit in `cmd/.../main.go`.
- In docs, logs, and comments, do not use emojis.
- Public workflow steps follow the comment-driven metadata contract documented in `docs/contributing/comment-driven-step-metadata.md`.

## Lint policy

Current enforced baseline comes from `.golangci.yml` and includes:

- `errcheck`
- `govet`
- `staticcheck`
- `gocritic`
- `gosec`
- `bodyclose`
- `ineffassign`
- `misspell`
- `nilerr`
- `unused`

Formatting is enforced with:

- `gofumpt`
- `gci`

Dependency vulnerability checks run separately from lint:

- `make vuln` runs pinned `govulncheck` against `./...`
- the scanner version is pinned in `Makefile` for reproducible CI behavior, while vulnerability data still comes from the latest Go vuln database at runtime
- CI treats `govulncheck` as a separate security gate instead of folding it into `golangci-lint`
- fix reachable vulnerabilities by upgrading dependencies first; if an exception is unavoidable, record the reason and revisit it explicitly

Additional style-focused linters may be added over time, but only if they improve signal quality for this repo.

## Deliberate deviations from the Uber guide

- We do not adopt `go.uber.org/atomic`; use `sync/atomic` instead.
- We keep the repository's `gci` import policy rather than Uber's generic import guidance.
- We do not adopt underscore-prefixed unexported globals as a naming rule.
- We treat rules like enum starting values and channel buffer sizes as review guidance, not absolute laws.

## Review checklist

When reviewing Go code in `deck`, check the following first:

- Does the code return errors instead of panicking or exiting?
- Is process exit confined to `main`?
- Are names clear, idiomatic, and free of builtin shadowing?
- Are serialized structs tagged explicitly?
- Are trust-boundary operations kept in the right helper layer?
- Does concurrency have a clear lifecycle and shutdown path?
- Are new globals or `init()` usage really necessary?
- If a public step changed, does it still satisfy the comment-driven metadata contract and pass `make generate && git diff --exit-code`?

## Updating this guide

- Prefer small, explicit updates tied to real code review pain or repeated lint failures.
- Keep this guide aligned with `.golangci.yml` and the actual codebase.
- If a rule is not enforced by tooling, it should still be concrete enough to review consistently.
