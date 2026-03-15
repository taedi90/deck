# Quick Start

This tutorial shows the default `deck` path:

1. create a workspace
2. express the procedure as a workflow
3. lint it
4. build the bundle
5. run it locally

That small loop is the point of the tool.

## 1. Create a workspace

```bash
deck init --out ./demo
```

This creates:

- `./demo/workflows/scenarios/prepare.yaml`
- `./demo/workflows/scenarios/apply.yaml`
- `./demo/workflows/vars.yaml`
- `./demo/workflows/components/example-prepare.yaml`
- `./demo/workflows/components/example-apply.yaml`
- `./demo/files/`
- `./demo/packages/`
- `./demo/images/`

## 2. Add or edit steps

`deck init` starts with fixed entry workflows under `workflows/scenarios/` and reusable fragments under `workflows/components/`. Put preparation work in `prepare.yaml` and target-machine work in `apply.yaml`.

Prefer typed steps first. The goal is to keep the procedure readable when it grows.

Minimal example:

```yaml
role: apply
version: v1alpha1
steps:
  - id: write-motd
    apiVersion: deck/v1alpha1
    kind: File
    spec:
      action: install
      path: /etc/motd
      content: |
        deck maintenance session in progress
```

Use `vars.yaml` or inline `vars` to keep site-specific values out of the steps themselves.

## 3. Validate before you package

```bash
deck lint
deck lint --file ./demo/workflows/scenarios/apply.yaml
```

Validation checks the workflow structure and the schema for each supported step kind.

## 4. Build an offline bundle

Run `prepare` from a directory that contains `workflows/scenarios/prepare.yaml`. `workflows/vars.yaml` and `workflows/scenarios/apply.yaml` are optional at this stage.

```bash
cd ./demo
deck prepare
deck bundle build --out ./bundle.tar
```

`prepare` writes generated artifacts under `./demo/outputs/`, refreshes `./demo/deck`, and updates `./demo/.deck/manifest.json`. `bundle build` turns the current workspace into the bundle you carry into the site.

## 5. Apply locally at the target site

```bash
deck apply
```

`apply` executes the workflow locally. That is the default `deck` story: make the procedure understandable, package what it needs, then run it on the machine that needs the change.

## 6. Optional: add site assistance

Use site-assisted features only when you explicitly want a temporary local server, shared bundle source, or session visibility inside the air gap.

That path extends the same local workflow. It does not replace it.

## What to read next

- `../concepts/why-deck.md`
- `../reference/workflow-model.md`
- `../reference/bundle-layout.md`
