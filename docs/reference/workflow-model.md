# Workflow Model

`deck` uses a YAML workflow model so larger procedures stay reviewable. The goal is not to invent a DSL — it is to give air-gapped operational work a clearer structure than a growing shell script, where typed steps express intent and named phases show the operator what the procedure is doing before they read every detail.

## Top-level fields

- `role`: required, either `prepare` or `apply`
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
    kind: Directory
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

Use phases when the procedure has natural boundaries. Typical examples:

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
- `Inspection`
- `KernelModule`
- `Kubeadm`
- `PackageCache`
- `Packages`
- `Repository`
- `Service`
- `Swap`
- `Symlink`
- `Sysctl`
- `SystemdUnit`
- `Wait`

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
- `../schemas/deck-workflow.schema.json`
