# deck

[Korean README](./README.ko.md) | [Documentation](./docs/README.md)

`deck` is a single-binary infrastructure workflow tool for the worst possible air-gapped environments: no-proxy, no-SSH, no-PXE, no-BMC, and no internet assumptions.

It exists to make infrastructure changes practical in places where online-first tools stop being convenient and start being dead weight.

## Visuals

![deck terminal demo](docs/assets/deck-cli.gif)

## Principles

- **Extreme Air-gap Focus**: `deck` is optimized for fully isolated environments. If a capability assumes online-first operations or remote orchestration, it belongs to tools like Ansible or Terraform instead.
- **Hermetic & Self-contained**: `bundle.tar` is expected to contain the execution logic, required data, and the binary needed to operate at the offline site.
- **Simple is the best**: complexity is removed, not hidden. The core user journey should be understandable as `pack -> apply`. Supporting commands must justify their existence.
- **K8s friendly**: workflows are YAML-based so operators familiar with Kubernetes, Helm, and manifest-driven tooling can read and author them quickly.
- Change infrastructure with local workflows and local bundles, even when the site cannot reach the internet.
- Keep the operator flow small enough to trust: prepare artifacts, carry them in, apply them locally.

## Install

Requirements:

- Go 1.22+
- Linux target environment

```bash
# run from source
go run ./cmd/deck --help

# install the binary
go install ./cmd/deck

# verify
deck --help
```

## Quick Start

1. Create a starter workspace.

```bash
deck init --out ./demo
```

2. Edit `./demo/workflows/pack.yaml`, `./demo/workflows/apply.yaml`, and `./demo/workflows/vars.yaml`.

3. Validate a workflow before packaging or applying it.

```bash
deck validate --file ./demo/workflows/apply.yaml
```

4. Build a self-contained offline bundle.

```bash
deck pack --out ./bundle.tar
```

5. Carry the bundle into the offline site and execute locally.

```bash
deck apply
```

6. Optionally expose a prepared bundle over HTTP for a local repo server workflow.

```bash
deck serve --root ./bundle --addr :8080
deck source set --server http://127.0.0.1:8080
deck list
deck health
```

For a guided walkthrough, start with `docs/tutorials/quick-start.md`.

## How deck works

1. Author or generate YAML workflows.
2. `pack` gathers packages, images, files, workflows, and the `deck` binary into a bundle.
3. Transfer the bundle through an approved offline path.
4. `apply` runs locally at the target site with no SSH or remote control dependency.

## Workflow Model

The workflow DSL is YAML-based and centered on step execution. A workflow declares a `role` (`pack` or `apply`) and then defines either top-level `steps` or named `phases`.

```yaml
role: apply
version: v1alpha1
steps:
  - id: disable-swap
    apiVersion: deck/v1alpha1
    kind: RunCommand
    spec:
      command: ["swapoff", "-a"]
```

Common step features include:

- `when` for conditional execution
- `retry` and `timeout` for controlled retries
- `register` for passing step outputs into later steps
- JSON Schema validation for the workflow and each supported step kind

## Bundle Contract

By design, a prepared bundle can include:

- `workflows/` for offline execution inputs
- `packages/`, `images/`, and `files/` for fetched artifacts
- `deck` and `files/deck` for self-contained execution
- `.deck/manifest.json` for artifact checksum metadata

## Command Surface

- Core flow: `init`, `validate`, `pack`, `apply`
- Offline repo flow: `serve`, `source`, `list`, `health`
- Bundle lifecycle: `bundle`
- Planning and diagnostics: `diff`, `doctor`, `logs`, `cache`, `service`

## Documentation Map

- Docs home: `docs/README.md`
- Quick start tutorial: `docs/tutorials/quick-start.md`
- Offline Kubernetes tutorial: `docs/tutorials/offline-kubernetes.md`
- CLI reference: `docs/reference/cli.md`
- Workflow model: `docs/reference/workflow-model.md`
- Bundle layout: `docs/reference/bundle-layout.md`
- Schema reference: `docs/reference/schema-reference.md`
- Server audit log reference: `docs/reference/server-audit-log.md`
- Example workflows: `docs/examples/README.md`
- Raw JSON schemas: `docs/schemas/README.md`

## Scope and Non-goals

- `deck` focuses on air-gapped preparation, packaging, offline installation, and deterministic local execution.
- It does not aim to be a remote orchestration framework.
- It does not optimize for cloud-first or always-connected workflows.

## Contributing and Validation

Before sending changes, run the checks that match your work:

```bash
go test ./...
go run ./cmd/deck validate --file <workflow.yaml>
```

## License

Apache-2.0. See `LICENSE`.
