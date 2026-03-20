# Workspace Layout

This document describes the standard directory structure of a `deck` project. Use `deck init` to scaffold this layout automatically.

A `deck` workspace is organized into three main functional areas: **Workflows**, **Artifact**, and **Metadata**.

## EnsureDirectory Structure

```text
.
├── workflows/
│   ├── scenarios/      # Main entrypoint workflows (prepare.yaml, apply.yaml)
│   ├── components/     # Reusable step fragments (Component Fragments)
│   └── vars.yaml       # Shared variable definitions
├── files/              # Local file sources for preparation
├── packages/           # Local package sources or override lists
├── images/             # Container image lists or local tarballs
├── outputs/            # (Generated) Collected artifacts after 'deck prepare'
└── .deck/              # (Internal) Checksums, manifest, and run history
```

## Workflows (`workflows/`)

The `workflows/` directory contains all your operational logic.

### Scenarios (`workflows/scenarios/`)
Scenarios are the primary entrypoints for `deck prepare` and `deck apply`. 
- Each file here must be a complete workflow with `role`, `version`, and either `steps`, `phases`, or `artifact`.
- Typical filenames: `prepare.yaml`, `apply.yaml`, `bootstrap.yaml`.

### Components (`workflows/components/`)
Components are **Component Fragments**—reusable sets of steps that are imported into scenarios.
- Files here follow the **Component Fragment Schema**.
- They only contain a `steps:` list.
- They are imported via `phases[].imports` in a scenario.
- **Example**: `workflows/components/k8s/runtime.yaml` is imported as `k8s/runtime.yaml`.

### Variables (`workflows/vars.yaml`)
A central YAML file for shared defaults. Values defined here are available to all workflows and components via the `{{ .vars.NAME }}` syntax.

## Artifact (`files/`, `packages/`, `images/`)

These directories hold the source material for your workflows.
- During `deck prepare`, artifacts are gathered from these locations (or remote URLs) and placed into the `outputs/` directory.
- Once bundled, these sources are no longer needed; the target node only sees the final bundle content.

## Internal Metadata (`.deck/`)

This directory is managed by `deck` and should not be edited manually.
- `manifest.json`: Tracks every file in the workspace for integrity and versioning.
- `runs/`: Local execution history for the workspace.

## Related References

- [Workflow Model](workflow-model.md)
- [Bundle Layout](bundle-layout.md)
- [Component Fragment Schema](schema/component-fragment.md)
