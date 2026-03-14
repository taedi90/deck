# Workflow Model

`deck` uses a small YAML workflow model so larger procedures stay reviewable.

The goal is not to invent a giant DSL. The goal is to give air-gapped operational work a clearer structure than a growing Bash script.

## Why the model looks this way

- procedures need visible structure when they get long
- operators should review intent, not reverse-engineer shell
- DevOps users should feel at home with YAML, stages, and manifest-like documents
- common host changes should use typed steps instead of ad hoc command blocks

## Top-level fields

- `role`: required, either `prepare` or `apply`
- `version`: currently `v1alpha1`
- `vars`: optional variable map
- `varImports`: optional external variable imports
- `imports`: optional workflow imports
- `artifacts`: declarative prepare artifact inventory for `role: prepare`
- `steps`: top-level step list
- `phases`: named phase list for more structured execution

The schema allows one execution mode at a time:

- `artifacts` for declarative prepare workflows
- top-level `steps`
- named `phases`
- imported workflow fragments that resolve to one of those modes

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

## Prefer typed steps

Typed steps are the center of the model.

They make the workflow easier to scan, easier to validate, and easier to evolve than shell-heavy procedures.

Supported step kinds include:

- `CheckHost`
- `DownloadPackages`
- `DownloadImages`
- `DownloadFile`
- `InstallPackages`
- `EditFile`
- `CopyFile`
- `Sysctl`
- `Service`
- `EnsureDir`
- `InstallFile`
- `RepoConfig`
- `ContainerdConfig`
- `Swap`
- `KernelModule`
- `RunCommand`
- `VerifyImages`
- `KubeadmInit`
- `KubeadmJoin`

## Prepare semantics

`role: prepare` can use top-level `artifacts` to declare artifact inventory instead of writing repeated download steps.

- `artifacts.files[*].items[*].output.path` is relative to the `files/` bundle root, so use `bin/kubeadm`, not `files/bin/kubeadm`
- `artifacts.images` declares image groups and lets the engine choose bundle tar layout
- `artifacts.packages` declares package groups per target OS family, release, and arch
- internally, `deck` still plans typed actions, but the authoring model stays inventory-driven

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
