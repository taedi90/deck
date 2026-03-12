# Schema Reference

`deck` validates both the workflow shape and each supported step kind through JSON Schema files in `docs/schemas/`.

## Entry points

- `../schemas/deck-workflow.schema.json`: top-level workflow schema
- `../schemas/deck-tooldefinition.schema.json`: tool definition schema
- `../schemas/tools/*.schema.json`: per-step-kind schemas

## Workflow schema highlights

The workflow schema currently enforces:

- required `role` and `version`
- `role` must be `pack` or `apply`
- either `steps`, `phases`, or `imports` must be present
- a step must include `id`, `kind`, and `spec`
- optional `when`, `retry`, `timeout`, and `register`

## Supported step schemas

- `check-host.schema.json`
- `containerd-config.schema.json`
- `copy-file.schema.json`
- `download-packages.schema.json`
- `download-k8s-packages.schema.json`
- `download-images.schema.json`
- `download-file.schema.json`
- `ensure-dir.schema.json`
- `install-file.schema.json`
- `install-packages.schema.json`
- `edit-file.schema.json`
- `kernel-module.schema.json`
- `modprobe.schema.json`
- `kubeadm-init.schema.json`
- `kubeadm-join.schema.json`
- `repo-config.schema.json`
- `run-command.schema.json`
- `service.schema.json`
- `swap.schema.json`
- `sysctl.schema.json`
- `sysctl-apply.schema.json`
- `template-file.schema.json`
- `verify-images.schema.json`
- `write-file.schema.json`

## Validation flow

1. Validate the workflow structure.
2. Validate each step against its matching tool schema.
3. Keep documentation and workflow examples aligned with the shipped schemas before packaging or applying.

## Command

```bash
deck validate --file ./workflows/apply.yaml
```
