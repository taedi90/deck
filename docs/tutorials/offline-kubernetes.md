# Offline Kubernetes Tutorial

This tutorial shows how `deck` fits a Kubernetes maintenance session in the environment it is built for: no internet, no SSH-driven orchestration, and a local operator path.

The reason to use `deck` here is not only offline transport. It is also that Kubernetes host-prep and bootstrap procedures grow quickly, and raw shell becomes hard to review.

## Goal

Build a portable bundle outside the site, move it into the air gap, then run Kubernetes-oriented workflows locally on the nodes that need the change.

## 1. Start from the shipped examples

Relevant examples in this repository:

- `../examples/offline-k8s-control-plane.yaml`
- `../examples/offline-k8s-worker.yaml`
- `../examples/offline-repo-preinstall.yaml`
- `../examples/offline-containerd-mirror.yaml`
- `../examples/offline-verify-images.yaml`

These examples are meant to be read and adapted, not treated as opaque automation blobs.

## 2. Keep the two jobs separate

- `pack` gathers what the site needs before transport
- `apply` executes the procedure locally on the node

The mental model is:

```text
prepare artifacts -> pack bundle -> transfer bundle -> run locally on each node
```

## 3. Model the procedure clearly

Use steps and phases to show the operator what the procedure is doing.

Typical boundaries in Kubernetes workflows:

- host preparation
- package or image setup
- runtime configuration
- kubeadm bootstrap or join
- verification

Prefer typed steps where possible. Keep `RunCommand` for the edges that are not modeled yet.

## 4. Prepare the bundle in the connected environment

Author a `pack` workflow that gathers the packages, container images, files, and templates your site needs.

Then build the bundle:

```bash
deck pack --out ./bundle.tar
```

The bundle can include `packages/`, `images/`, `files/`, `workflows/`, the `deck` binary, and `.deck/manifest.json` checksums.

## 5. Move the bundle into the offline site

Transfer `bundle.tar` through the approved path for your environment: removable media, controlled gateway, or another site-approved handoff.

`deck` assumes this step is out-of-band. It does not require a remote control service.

## 6. Run workflows locally on the target nodes

At the offline site, execute on the target machine itself:

```bash
deck apply
```

Use the control-plane and worker examples as starting points for kubeadm-based bootstrap and follow-on maintenance.

## 7. Add site assistance only when it solves a local problem

Some sites want a temporary shared source for bundle contents or a local place to collect session status. That can help when multiple nodes need the same release inside the same air gap.

Keep that choice explicit and secondary. The operator workflow still centers on local `deck` execution on each node.

## 8. Validate before transport and execution

```bash
deck validate --file ./workflows/pack.yaml
deck validate --file ./workflows/apply.yaml
```

For planning and diagnostics, also review:

- `../reference/workflow-model.md`
- `../reference/schema-reference.md`
- `../reference/server-audit-log.md`
