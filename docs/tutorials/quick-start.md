# Quick Start

This tutorial shows the default `deck` path:

1. create a workspace
2. express the procedure as a workflow
3. validate it
4. build the bundle
5. run it locally

That small loop is the point of the tool.

## 1. Create a workspace

```bash
deck init --out ./demo
```

This creates:

- `./demo/workflows/pack.yaml`
- `./demo/workflows/apply.yaml`
- `./demo/workflows/vars.yaml`

## 2. Add or edit steps

`deck init` starts with empty workflow files. Put preparation work in `pack.yaml` and target-machine work in `apply.yaml`.

Prefer typed steps first. The goal is to keep the procedure readable when it grows.

Minimal example:

```yaml
role: apply
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

Use `vars.yaml` or inline `vars` to keep site-specific values out of the steps themselves.

## 3. Validate before you package

```bash
deck validate --file ./demo/workflows/apply.yaml
deck validate --file ./demo/workflows/pack.yaml
```

Validation checks the workflow structure and the schema for each supported step kind.

## 4. Build an offline bundle

Run `pack` from a directory that contains `workflows/pack.yaml`, `workflows/apply.yaml`, and `workflows/vars.yaml`.

```bash
cd ./demo
deck pack --out ./bundle.tar
```

The resulting bundle is designed to be the thing you carry into the site.

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
