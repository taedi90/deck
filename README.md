# deck

[Korean README](./README.ko.md) | [Documentation](./docs/README.md)

`deck` is a single-binary tool for manual-first maintenance sessions in extreme air-gapped environments: no SSH, no PXE, no BMC, and no internet assumptions.

It helps operators prepare a self-contained bundle, carry it into the site, and run the work locally on the machine that needs the change.

## Visuals

![deck terminal demo](docs/assets/deck-cli.gif)

## What deck is for

- **Manual-first operation**: the default path is local execution by an operator at the target site.
- **Extreme air-gap focus**: `deck` is built for places where connected control loops are not available or not trusted.
- **Hermetic bundles**: `bundle.tar` carries workflows, artifacts, and the `deck` binary needed for offline work.
- **YAML-driven workflows**: operators can read, review, and adapt workflows without adding a larger control system.
- **Explicit site assistance**: if a site wants a temporary shared server or coordination point inside the air gap, that is additive and deliberate, not the base mode.

## When to use deck

- You need to prepare artifacts ahead of time, transfer them through an approved path, and execute locally at the site.
- You want a small operator workflow such as `validate -> pack -> apply`.
- You need repeatable maintenance steps for disconnected hosts, clusters, or appliances.
- You may need optional site-local assistance later, but you still want the same local execution model on each node.

## When not to use deck

- You want a remote orchestration platform, long-lived control plane, or agent-based rollout system.
- You expect always-on connectivity, live cloud APIs, or SSH-driven automation as the normal path.
- You need a general replacement for Terraform, Pulumi, Ansible, or other broad infrastructure platforms.

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

2. Edit `./demo/workflows/pack.yaml`, `./demo/workflows/apply.yaml`, and `./demo/workflows/vars.yaml` for the maintenance session you want to run.

3. Validate the workflow before packaging or applying it.

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

6. Add site-assisted execution only if you explicitly want a temporary shared bundle source or local coordination point inside the air gap. That path is secondary to the default local flow.

For a guided walkthrough, start with `docs/tutorials/quick-start.md`.

## How deck works

1. Author or adapt YAML workflows for the maintenance task.
2. `pack` gathers packages, images, files, workflows, and the `deck` binary into a bundle.
3. Transfer the bundle through an approved offline path.
4. `apply` runs locally at the target site with no SSH or remote execution dependency.
5. Optional site-assisted workflows can add a temporary local server or shared visibility, but the operator still runs `deck` directly on each target.

## Workflow Model

The workflow DSL is YAML-based and centered on step execution. A workflow declares a `role` (`pack` or `apply`) and then defines either top-level `steps` or named `phases`.

Prefer typed primitives for common host changes. Keep `RunCommand` for cases where no supported step kind fits yet.

```yaml
role: apply
version: v1alpha1
steps:
  - id: write-repo-config
    apiVersion: deck/v1alpha1
    kind: WriteFile
    spec:
      path: /etc/example.repo
      content: |
        [offline-base]
        name=offline-base
        baseurl=file:///srv/offline-repo
        enabled=1
        gpgcheck=0
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

- Core local flow: `init`, `validate`, `pack`, `apply`
- Local planning and diagnostics: `diff`, `doctor`
- Optional site-local helpers: `serve`, `list`, `health`, `logs`
- Bundle lifecycle and cache management: `bundle`, `cache`
- Compatibility and advanced steps still exist, but `RunCommand` should be a last resort in new workflows

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
- Site-assisted use is explicit and additive. It does not replace the local operator path.
- `deck` is not a remote orchestration framework.
- `deck` is not optimized for cloud-first or always-connected workflows.

## Contributing and Validation

Before sending changes, run the checks that match your work:

```bash
go test ./...
go run ./cmd/deck validate --file <workflow.yaml>

# linux host with libvirt-backed vagrant
bash test/vagrant/run-offline-multinode-agent.sh
```

Local Vagrant runs keep machine state in `test/vagrant/.vagrant/` and reuse the prepared bundle cache in `test/artifacts/cache/offline-multinode-prepared-bundle/` across runs.
The default local loop now prefers `rsync`, keeps VMs alive, and reuses `test/artifacts/offline-multinode-local/` unless you choose a different `--art-dir` or run with `--fresh`.

## License

Apache-2.0. See `LICENSE`.
