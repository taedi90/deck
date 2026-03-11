# Workflow Model

`deck` uses a small YAML workflow model so larger procedures stay reviewable.

The goal is not to invent a giant DSL. The goal is to give air-gapped operational work a clearer structure than a growing Bash script.

## Why the model looks this way

- procedures need visible structure when they get long
- operators should review intent, not reverse-engineer shell
- DevOps users should feel at home with YAML, stages, and manifest-like documents
- common host changes should use typed steps instead of ad hoc command blocks

## Top-level fields

- `role`: required, either `pack` or `apply`
- `version`: currently `v1alpha1`
- `vars`: optional variable map
- `varImports`: optional external variable imports
- `imports`: optional workflow imports
- `steps`: top-level step list
- `phases`: named phase list for more structured execution

The schema allows either top-level `steps`, named `phases`, or imported workflow fragments.

## Minimal workflow

```yaml
role: apply
version: v1alpha1
steps:
  - id: prepare-state-dir
    apiVersion: deck/v1alpha1
    kind: EnsureDir
    spec:
      path: /var/lib/deck
      mode: "0755"
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

That keeps large workflows readable and lets the operator see the intended order without reading every command detail.

Typical examples:

- `prepare`
- `install`
- `verify`
- `cleanup`

## Prefer typed steps

Typed steps are the center of the model.

They make the workflow easier to scan, easier to validate, and easier to evolve than shell-heavy procedures.

Supported step kinds include:

- `CheckHost`
- `DownloadPackages`
- `DownloadK8sPackages`
- `DownloadImages`
- `DownloadFile`
- `InstallPackages`
- `WriteFile`
- `EditFile`
- `CopyFile`
- `Sysctl`
- `Modprobe`
- `Service`
- `EnsureDir`
- `InstallFile`
- `TemplateFile`
- `RepoConfig`
- `ContainerdConfig`
- `Swap`
- `KernelModule`
- `SysctlApply`
- `RunCommand`
- `VerifyImages`
- `KubeadmInit`
- `KubeadmJoin`

## When to use RunCommand

Use `RunCommand` when no supported step kind fits yet.

That is the escape hatch, not the ideal authoring path. If a workflow leans heavily on `RunCommand`, the procedure may still be too close to raw shell.

## Validation model

`deck validate` checks:

- the top-level workflow schema
- the schema for each referenced step kind
- reserved runtime keys and workflow compatibility rules

This is one of the main reasons to use a workflow model instead of passing around shell files.

## Related references

- `../concepts/why-deck.md`
- `schema-reference.md`
- `bundle-layout.md`
- `../schemas/deck-workflow.schema.json`
