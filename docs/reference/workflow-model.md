# Workflow Model

`deck` uses a YAML workflow model so larger procedures stay reviewable. The goal is not to invent a DSL — it is to give air-gapped operational work a clearer structure than a growing shell script, where typed steps express intent and named phases show the operator what the procedure is doing before they read every detail.

## Top-level fields

- `role`: required, either `prepare` or `apply`
- `version`: currently `v1alpha1`
- `vars`: optional variable map
- `artifacts`: declarative prepare artifact inventory for `role: prepare`
- `steps`: top-level step list
- `phases`: named phase list for more structured execution

The schema allows one execution mode at a time:

- `artifacts` for declarative prepare workflows
- top-level `steps`
- named `phases`

Phase imports resolve from `workflows/components/`. Write component-relative paths such as `k8s/prereq.yaml`, not `../components/k8s/prereq.yaml`.

`workflows/components/` files are step fragments. They contain only `steps:` and may reference shared `vars.*`, but shared defaults should stay in `workflows/vars.yaml` or the importing scenario `vars:` block.

## Minimal workflow

```yaml
role: apply
version: v1alpha1
steps:
  - id: prepare-state-dir
    kind: Directory
    spec:
      path: /var/lib/deck
      mode: "0755"
```

## Minimal prepare workflow

```yaml
role: prepare
version: v1alpha1
artifacts:
  files:
    - group: binaries
      items:
        - id: kubeadm
          source:
            url: https://example.local/kubeadm
          output:
            path: bin/kubeadm
```

## Step shape

Every step is centered on:

- `id`
- `apiVersion`
- `kind`
- `spec`

Optional execution controls:

- `when`: conditional execution expression
- `retry`: retry count
- `timeout`: duration string such as `30s` or `5m`
- `register`: export step outputs into later runtime values

## Phases

Use phases when the procedure has natural boundaries.

`artifacts` is the preferred prepare authoring mode. Use `steps` or `phases` for `apply`, or for older prepare workflows that have not been migrated yet.

That keeps large workflows readable and lets the operator see the intended order without reading every command detail.

Typical examples:

- `prepare`
- `install`
- `verify`
- `cleanup`

Named phases keep large workflows readable and let the operator see the intended order without reading every command detail.

## Prefer typed steps

Typed steps are the center of the model. They make the workflow easier to scan, easier to validate, and easier to evolve than shell-heavy procedures.

Supported step kinds:

- `Artifacts`
- `Command`
- `Containerd`
- `Directory`
- `File`
- `Image`
- `Checks`
- `KernelModule`
- `Kubeadm`
- `PackageCache`
- `Packages`
- `Repository`
- `Sysctl`
- `Service`
- `Swap`
- `Symlink`
- `SystemdUnit`
- `Wait`

## Prepare semantics

`role: prepare` can use top-level `artifacts` to declare artifact inventory instead of writing repeated download steps.

- `artifacts.files[*].items[*].output.path` is relative to the `files/` bundle root, so use `bin/kubeadm`, not `files/bin/kubeadm`
- `artifacts.images` declares image groups and lets the engine choose bundle tar layout
- `artifacts.packages` declares package groups per target OS family, release, and arch
- internally, `deck` still plans typed actions, but the authoring model stays inventory-driven

## When to use Command

Use `Command` when no supported step kind fits yet. It is the escape hatch, not the ideal authoring path. If a workflow leans heavily on `Command`, the procedure may still be too close to raw shell.

## Validation model

`deck lint` checks:

- the top-level workflow schema
- the schema for each referenced step kind
- reserved runtime keys and workflow compatibility rules

Validating before transport is one of the main reasons to use a workflow model instead of passing around shell files.

## Related references

- `../concepts/why-deck.md`
- `schema-reference.md`
- `bundle-layout.md`
- `../../schemas/deck-workflow.schema.json`
