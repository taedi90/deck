# Tool Schemas

This directory contains JSON Schemas for typed workflow steps.

The files are not all equal from a user-authoring point of view.

## Public apply steps

- `public/check-host.schema.json`
- `public/containerd-config.schema.json`
- `public/copy-file.schema.json`
- `public/edit-file.schema.json`
- `public/ensure-dir.schema.json`
- `public/install-artifacts.schema.json`
- `public/install-file.schema.json`
- `public/install-packages.schema.json`
- `public/kernel-module.schema.json`
- `public/kubeadm-init.schema.json`
- `public/kubeadm-join.schema.json`
- `public/kubeadm-reset.schema.json`
- `public/package-cache.schema.json`
- `public/repo-config.schema.json`
- `public/service.schema.json`
- `public/swap.schema.json`
- `public/symlink.schema.json`
- `public/systemd-unit.schema.json`
- `public/sysctl.schema.json`
- `public/verify-images.schema.json`
- `public/wait-path.schema.json`

## Advanced steps

- `advanced/download-file.schema.json`
- `advanced/run-command.schema.json`

These remain user-visible, but they are not the preferred starting point when a higher-level typed step or declarative prepare model already exists.

## Legacy/internal prepare steps

- `legacy-prepare/download-images.schema.json`
- `legacy-prepare/download-packages.schema.json`

These schemas are still used for validation of legacy or internal prepare planning paths.
New `role: prepare` workflows should prefer top-level `artifacts` instead.

## Metadata

Each schema root now carries:

- `description`: what the step is for
- `x-deck-visibility`: one of `public`, `advanced`, or `legacy-prepare`

Use `../../reference/schema-reference.md` for the curated reference view.
