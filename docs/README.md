# deck documentation

`deck` is a workflow tool for air-gapped and operationally constrained environments. Write a workflow, lint it, bundle what the site needs, and run it locally on the target machine.

## Start here

- New to `deck`: `tutorials/quick-start.md`
- Why the tool exists: `concepts/why-deck.md`
- System architecture: `concepts/architecture.md`
- Workflow structure: `reference/workflow-model.md`
- Commands: `reference/cli.md`
- Bundle contents: `reference/bundle-layout.md`
- Examples: `examples/README.md`

## Guides

- `concepts/why-deck.md`: the problem deck solves and how it approaches it
- `concepts/architecture.md`: system boundaries, execution flow, and trust model
- `development/go-style-guide.md`: Go coding conventions for the `deck` repository
- `tutorials/quick-start.md`: create, lint, prepare, and run a workflow
- `tutorials/offline-kubernetes.md`: deck for offline Kubernetes maintenance

## Reference

- `reference/workflow-model.md`: workflow shape, phases, steps, and execution controls
- `reference/cli.md`: command surface and default usage pattern
- `reference/bundle-layout.md`: bundle contract and layout
- `reference/schema-reference.md`: schema files and supported step kinds
- `reference/server-audit-log.md`: audit log format for `deck server up`

## Examples

- `examples/README.md`: example workflows to adapt to real procedures
- `../schemas/README.md`: raw schema files and validation entry points
