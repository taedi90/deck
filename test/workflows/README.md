# Workflow test trees

`test/workflows/` is the canonical home for the workflow entrypoints and reusable fragments used by the Kubernetes regression layout.

## Tree

- `components/`, reusable step fragments grouped by concern
- `scenarios/`, workflow entrypoints
- `vars.yaml`, shared defaults applied to the scenario entrypoints

## Expected scenario shape

Each canonical scenario keeps scenario meaning in one entry workflow file instead of spreading it across per-scenario subdirectories:

- `scenarios/<name>.yaml`, the scenario entry workflow passed to `deck lint --file` and the scenario runner
- `components/...`, reusable step fragments imported by the scenario entrypoints
- `vars.yaml`, shared workflow defaults loaded automatically and overridden by scenario `vars:` blocks and CLI `--var`
- scenario entrypoints should split major execution stages into separate phases so readers can follow the scenario flow at a glance
- each phase imports reusable components directly from `components/`
- component files are `steps:`-only fragments and should not declare their own `role`, `version`, `vars`, or `phases`
- component files may reference `vars.*`, but shared defaults should stay concentrated in `vars.yaml`

Component imports resolve from the `components/` root, so workflows should use paths like `k8s/prereq.yaml` or `bootstrap.yaml` instead of `../components/...`.

E2E harness sidecars live outside the workflow tree:

- `test/e2e/scenario-meta/<name>.env`, VM topology and verify-stage metadata
- `test/e2e/scenario-hooks/<name>.sh`, scenario-specific VM helper hooks

`scenarios/prepare.yaml` is the shared `prepare` entrypoint used to build the prepared bundle cache for the regression scenarios.
