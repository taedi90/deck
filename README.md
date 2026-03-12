# deck

[Korean README](./README.ko.md) | [Documentation](./docs/README.md)

`deck` is a simple workflow tool for air-gapped and operationally constrained environments.

It exists for the cases where larger automation platforms are a bad fit and growing Bash procedures become hard to read, review, and maintain. `deck` keeps the model small: write a workflow, validate it, bundle what the site needs, carry it in, and run it locally.

## Visuals

![deck terminal demo](docs/assets/deck-cli.gif)

## Why deck exists

- **Air-gapped by design**: built for sites with no SSH, no PXE, no BMC, and no internet assumptions.
- **Small tool, bounded problem**: not a new general-purpose automation platform.
- **Structured over sprawling shell**: large procedures stay readable as steps and phases instead of turning into long Bash files.
- **Developer-friendly shape**: YAML workflows feel familiar to people who already work with CI/CD pipelines and Kubernetes manifests.
- **Bundle-first execution**: package the workflow, artifacts, and `deck` binary together before the maintenance session.

## What deck is for

- Repeatable maintenance procedures that must run locally on the target host or node.
- Offline preparation and transfer of packages, images, files, and workflow inputs.
- Teams that want a simple workflow engine instead of a larger controller, agent system, or runtime stack.
- Situations where Bash started simple but the procedure is now too large to review comfortably.

## What deck is not for

- Remote orchestration across always-connected infrastructure.
- General replacement for Terraform, Pulumi, Ansible, Chef, Puppet, or other broad platforms.
- Long-lived control planes, fleet agents, or policy engines.
- Cloud-first workflows where live APIs and central controllers are normal assumptions.

## Why not just use Bash

- Small scripts are fast. Large procedures are not.
- Bash tends to hide operator intent inside command details.
- Review quality drops when a procedure becomes one long shell file.
- Reuse, validation, and step-level reasoning get weak quickly.

`deck` does not try to eliminate shell completely. It gives the procedure a clearer structure and keeps `RunCommand` as the escape hatch, not the default authoring model.

## Core flow

1. Write or adapt YAML workflows for the maintenance session.
2. Validate the workflow shape before transport or execution.
3. Build a self-contained bundle with the workflow, artifacts, and `deck` binary.
4. Carry the bundle into the site through the approved path.
5. Run `deck` locally on the machine that needs the change.

## Minimal workflow

Prefer typed steps for common host changes. Keep `RunCommand` for the cases where no supported step kind fits yet.

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

## Install

Requirements:

- Go 1.23+
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

```bash
deck init --out ./demo
deck validate --file ./demo/workflows/apply.yaml
deck validate --file ./demo/workflows/pack.yaml

cd ./demo
deck pack --out ./bundle.tar
deck apply
```

Start with `docs/tutorials/quick-start.md` for the guided path.

## Learn more

- Docs home: `docs/README.md`
- Why deck: `docs/concepts/why-deck.md`
- Workflow model: `docs/reference/workflow-model.md`
- CLI reference: `docs/reference/cli.md`
- Example workflows: `docs/examples/README.md`

## Scope and non-goals

- `deck` focuses on simple, local, bundle-first execution in disconnected environments.
- Site-assisted use is explicit and additive. It does not replace the local operator path.
- `deck` is not a remote orchestration framework.
- `deck` is not optimized for broad online infrastructure automation.

## Contributing and validation

Before sending changes, run the checks that match your work:

```bash
go test ./...
go run ./cmd/deck validate --file <workflow.yaml>
go run ./cmd/deck validate --file test/workflows/k8s-control-plane-bootstrap/profile/control-plane.yaml
go run ./cmd/deck validate --file test/workflows/k8s-worker-join/profile/worker.yaml
go run ./cmd/deck validate --file test/workflows/k8s-node-reset/profile/node-reset.yaml

# linux host with libvirt-backed vagrant
bash test/e2e/vagrant/run-scenario.sh --scenario k8s-control-plane-bootstrap
bash test/e2e/vagrant/run-scenario.sh --scenario k8s-worker-join
bash test/e2e/vagrant/run-scenario.sh --scenario k8s-node-reset
```

The maintained Kubernetes regression layout now lives under `test/workflows/`, grouped by `k8s-control-plane-bootstrap`, `k8s-worker-join`, and `k8s-node-reset`, with the shared fragments in `test/workflows/_shared/k8s/`.
Local Vagrant runs keep machine state in `test/vagrant/.vagrant/`, reuse scenario caches under `test/artifacts/cache/bundles/<scenario>/`, and reuse run artifacts under `test/artifacts/runs/<scenario>/<run-id>/` unless you choose a different `--art-dir` or run with `--fresh`.
The legacy `test/vagrant/run-offline-multinode-agent.sh` entrypoint still exists as a temporary compatibility shim during migration.
## License

Apache-2.0. See `LICENSE`.
