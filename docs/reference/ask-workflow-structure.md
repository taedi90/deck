# Ask workflow structure

Generated from deck ask source-of-truth metadata.

## Invariants
- Supported roles: prepare, apply
- Supported workflow version: `v1alpha1`
- Top-level workflow modes: phases, steps
- Required top-level fields: version

## File topology
- Scenario entrypoints live under `workflows/scenarios`
- Reusable fragments live under `workflows/components`
- Shared variables live at `workflows/vars.yaml`
- Canonical prepare entrypoint: `workflows/prepare.yaml`
- Canonical apply entrypoint: `workflows/scenarios/apply.yaml`

## Examples

```yaml
version: v1alpha1
phases:
  - name: bootstrap
    steps:
      - id: check-host
        kind: CheckHost
        spec:
          checks: [os, arch, swap]
          failFast: true
```

```yaml
version: v1alpha1
steps:
  - id: run-command
    kind: Command
    spec:
      command: [echo, hello]
```
