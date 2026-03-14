# Tool Schemas

This directory contains JSON Schemas for typed workflow steps.

The files are not all equal from a user-authoring point of view.

## Public apply steps

- `public/inspection.schema.json`
- `public/containerd.schema.json`
- `public/directory.schema.json`
- `public/artifacts.schema.json`
- `public/packages.schema.json`
- `public/file.schema.json`
- `public/image.schema.json`
- `public/kernel-module.schema.json`
- `public/kubeadm.schema.json`
- `public/package-cache.schema.json`
- `public/repository.schema.json`
- `public/service.schema.json`
- `public/swap.schema.json`
- `public/symlink.schema.json`
- `public/systemd-unit.schema.json`
- `public/sysctl.schema.json`
- `public/wait.schema.json`

## Advanced steps

- `advanced/file-fetch.schema.json`
- `advanced/command.schema.json`

These remain user-visible, but they are not the preferred starting point when a higher-level typed step or declarative prepare model already exists.

## Legacy/internal prepare steps

- `legacy-prepare/image-fetch.schema.json`
- `legacy-prepare/package-fetch.schema.json`

These schemas are still used for validation of legacy or internal prepare planning paths.
New `role: prepare` workflows should prefer top-level `artifacts` instead.

## Metadata

Each schema root now carries:

- `description`: what the step is for
- `x-deck-visibility`: one of `public`, `advanced`, or `legacy-prepare`

Use `../../reference/schema-reference.md` for the curated reference view.
