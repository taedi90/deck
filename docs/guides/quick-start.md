# Quick Start

This tutorial walks through the default `deck` path:

1. create a workspace
2. express the procedure as a workflow
3. lint it
4. build the bundle
5. run it locally

## 1. Create a workspace

```bash
deck init --out ./demo
```

This creates a starter layout with two entry workflows and a set of empty output source directories:

- `./demo/workflows/prepare.yaml`
- `./demo/workflows/scenarios/apply.yaml`
- `./demo/workflows/vars.yaml`
- `./demo/workflows/components/example-apply.yaml`
- `./demo/files/`
- `./demo/packages/`
- `./demo/images/`

## 2. Add or edit steps

`deck init` creates a fixed `workflows/prepare.yaml` entry workflow, an `apply.yaml` scenario under `workflows/scenarios/`, and reusable fragments under `workflows/components/`. Preparation work goes in `workflows/prepare.yaml`; work that runs on the target machine goes in `workflows/scenarios/apply.yaml`.

Prefer typed steps. They make the procedure easier to read and lint as it grows.

```yaml
version: v1alpha1
steps:
  - id: write-motd
    apiVersion: deck/v1alpha1
    kind: WriteFile
    spec:
      path: /etc/motd
      content: |
        deck maintenance session in progress
```

Use `vars.yaml` or inline `vars` to keep site-specific values out of the step definitions.

## 3. Validate before you package

```bash
deck lint
deck lint --file ./demo/workflows/scenarios/apply.yaml
```

`deck lint` checks the workflow structure and the schema for each typed step. Catching mistakes here is cheaper than discovering them inside the air gap.

## 4. Build an offline bundle

Run `prepare` from a workspace directory that contains `workflows/prepare.yaml`. `workflows/vars.yaml` and `workflows/scenarios/apply.yaml` are optional at this stage.

```bash
cd ./demo
deck prepare
deck bundle build --out ./bundle.tar
```

`prepare` writes generated artifacts under `./demo/outputs/`, writes a root `./demo/deck` launcher, and updates `./demo/.deck/manifest.json`. `bundle build` turns the current workspace into the archive you carry into the site.

## 5. Apply locally at the target site

```bash
deck apply
```

`apply` executes the workflow locally on the machine that needs the change. No SSH, no controller, no external reach-back required.

## 6. Optional: add site assistance

Some sites benefit from a temporary local server inside the air gap as a shared bundle source. Use `deck server up` for this when it solves a real problem.

That path extends the local workflow. It does not replace it.

## What to read next

- [Why deck?](../core-concepts/why-deck.md)
- [Workflow model](../reference/workflow-model.md)
- [Bundle layout](../reference/bundle-layout.md)
- [Using deck ask](ask.md)
