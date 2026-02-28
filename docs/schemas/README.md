# deck JSON Schemas (v1)

- `deck-workflow.schema.json`: top-level workflow DSL schema
- `deck-tooldefinition.schema.json`: tool definition manifest schema (`requires`, `outputContract`, `idempotency`, `failurePolicy` 포함)
- `tools/*.schema.json`: per-tool step schema

Validation flow:

1. Validate workflow shape with `deck-workflow.schema.json`.
2. Validate each step with the matching `tools/<kind>.schema.json`.

Step common fields include `id`, `apiVersion`, `kind`, `spec`, optional `when/retry/timeout/register`.

Current tool schemas:

- `check-host.schema.json`
- `download-packages.schema.json`
- `download-k8s-packages.schema.json`
- `download-images.schema.json`
- `download-file.schema.json`
- `install-packages.schema.json`
- `write-file.schema.json`
- `edit-file.schema.json`
- `copy-file.schema.json`
- `sysctl.schema.json`
- `modprobe.schema.json`
- `run-command.schema.json`
- `kubeadm-init.schema.json`
- `kubeadm-join.schema.json`
