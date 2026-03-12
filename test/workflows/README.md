# Workflow test trees

`test/workflows/` is the canonical home for the workflow entrypoints and reusable fragments used by the Kubernetes regression layout.

## Tree

- `components/`, reusable workflow fragments grouped by concern
- `scenarios/`, workflow entrypoints
- `vars.yaml`, shared defaults applied to the scenario entrypoints

## Expected scenario shape

Each canonical scenario keeps scenario meaning in one entry workflow file instead of spreading it across per-scenario subdirectories:

- `scenarios/<name>.yaml`, the scenario entry workflow passed to `deck validate` and the scenario runner
- `components/...`, reusable workflow fragments imported by the scenario entrypoints
- `vars.yaml`, shared workflow defaults overridden by scenario `vars:` blocks and CLI `--var`

E2E harness sidecars live outside the workflow tree:

- `test/e2e/scenario-meta/<name>.env`, VM topology and verify-stage metadata
- `test/e2e/scenario-hooks/<name>.sh`, scenario-specific VM helper hooks

`scenarios/prepare.yaml` is the shared `pack` entrypoint used to build the prepared bundle cache for the regression scenarios.
