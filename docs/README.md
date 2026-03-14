# deck documentation

`deck` documentation follows the same idea as the product itself: keep the model small, explicit, and easy to scan.

`deck` is for air-gapped and operationally constrained environments where larger automation systems are a bad fit and growing Bash procedures become hard to manage.

## Start here

- New to `deck`: `tutorials/quick-start.md`
- Why the tool exists: `concepts/why-deck.md`
- Looking for workflow structure: `reference/workflow-model.md`
- Looking for commands: `reference/cli.md`
- Looking for bundle contents: `reference/bundle-layout.md`
- Looking for examples: `examples/README.md`

## Core ideas

- `deck` is not a broad automation platform.
- `deck` is a simple workflow tool for local execution.
- The workflow should stay readable when the procedure grows.
- The safest offline default is to bundle what the site needs.
- Site-assisted helpers are optional and secondary.

## Guides

- `concepts/why-deck.md`: scope, positioning, and why large Bash procedures are the real problem
- `tutorials/quick-start.md`: create, validate, prepare, and run a workflow
- `tutorials/offline-kubernetes.md`: use `deck` for offline Kubernetes-oriented maintenance work

## Reference

- `reference/workflow-model.md`: workflow shape, phases, steps, and execution controls
- `reference/cli.md`: command surface and default usage pattern
- `reference/bundle-layout.md`: bundle contract and why it matters offline
- `reference/schema-reference.md`: schema files and supported step kinds
- `reference/server-audit-log.md`: audit log format for `deck server up`

## Examples and schemas

- `examples/README.md`: example workflows to adapt to real procedures
- `schemas/README.md`: raw schema files and validation entry points
