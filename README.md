<table width="100%"><tr><td valign="middle">

# deck

[Korean README](./README.ko.md) | [Documentation](./docs/README.md)

`deck` is a workflow tool built for air-gapped and operationally constrained environments. Write a YAML workflow, lint it, bundle everything the site needs, carry it in, and run it locally on the target machine.

</td><td align="right" valign="middle">
<img src="assets/logo.png" alt="deck logo" width="150" />
</td></tr></table>

## Why deck exists

Operational procedures for disconnected sites — Kubernetes bootstraps, package installs, host configuration — often start as shell scripts and grow until they are too large to review confidently. `deck` gives those procedures a cleaner shape: typed steps with visible intent, lint validation before transport, and a self-contained bundle that travels with the workflow.

It is built for sites with no SSH-driven orchestration, no internet access, and a local human in the loop. If your environment looks more like a cloud deployment with live APIs and central controllers, deck is probably not the right fit.

## Core flow

1. Write or adapt YAML workflows for the maintenance session.
2. Validate the workflow shape before transport or execution.
3. Build a self-contained bundle with the workflow, artifacts, and `deck` binary.
4. Carry the bundle into the site through the approved path.
5. Run `deck` locally on the machine that needs the change.

## Minimal workflow

Prefer typed steps for common host changes. Keep `Command` for the cases where no supported step kind fits yet.

```yaml
role: apply
version: v1alpha1
steps:
  - id: write-repo-config
    apiVersion: deck/v1alpha1
    kind: File
    spec:
      action: install
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

# generate shell completion
go run ./cmd/deck completion bash

# install the binary
go install ./cmd/deck

# verify
deck --help
```

## Quick Start

```bash
deck init --out ./demo
deck lint
deck lint --file ./demo/workflows/scenarios/apply.yaml

cd ./demo
deck prepare
deck bundle build --out ./bundle.tar
deck apply
```

Start with `docs/tutorials/quick-start.md` for the guided walkthrough.

## Shell completion

```bash
deck completion bash
deck completion zsh
deck completion fish
deck completion powershell
```

Completion output goes to stdout. Help is shown only when requested with `--help`. Command and flag errors are written to stderr without automatic usage output.

## Learn more

- Docs home: `docs/README.md`
- Why deck: `docs/concepts/why-deck.md`
- Workflow model: `docs/reference/workflow-model.md`
- CLI reference: `docs/reference/cli.md`
- Example workflows: `docs/examples/README.md`

## Contributing and validation

Before sending changes, run the checks that match your work:

```bash
go build -o ./deck ./cmd/deck
./deck --help
./deck completion bash
./deck completion zsh
./deck completion fish
./deck completion powershell
go test ./...
./deck lint
./deck lint --file docs/examples/vagrant-smoke-install.yaml
./deck lint --file test/workflows/scenarios/control-plane-bootstrap.yaml
./deck lint --file test/workflows/scenarios/worker-join.yaml
./deck lint --file test/workflows/scenarios/node-reset.yaml

# linux host with libvirt-backed vagrant
bash test/e2e/vagrant/run-scenario.sh --scenario k8s-control-plane-bootstrap
bash test/e2e/vagrant/run-scenario.sh --scenario k8s-worker-join
bash test/e2e/vagrant/run-scenario.sh --scenario k8s-node-reset
```

The maintained Kubernetes regression layout lives under `test/workflows/`, with scenario entrypoints in `test/workflows/scenarios/`, reusable fragments in `test/workflows/components/`, and shared defaults in `test/workflows/vars.yaml`.
Local Vagrant runs keep machine state in `test/vagrant/.vagrant/`, reuse scenario caches under `test/artifacts/cache/bundles/<scenario>/`, and reuse run artifacts under `test/artifacts/runs/<scenario>/<run-id>/` unless you choose a different `--art-dir` or run with `--fresh`.
## License

Apache-2.0. See `LICENSE`.
