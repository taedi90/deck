# Example Workflows

The files in `docs/examples/` are starting points for real procedures. They show how typed steps express operational intent more clearly than equivalent shell, and how phases keep larger workflows scannable.

## How to use these examples

- Start from them when you want a concrete workflow to adapt.
- Keep the overall structure clear before adding more details.
- Replace repetitive shell with typed steps when a step kind already fits.
- Validate the result before packaging or transport.

## Files

- `offline-k8s-control-plane.yaml`: kubeadm-based control-plane bootstrap example
- `offline-k8s-worker.yaml`: kubeadm worker join example
- `offline-repo-preinstall.yaml`: prepare package repository configuration on the target host
- `offline-containerd-mirror.yaml`: point containerd at an internal registry or mirror path
- `offline-verify-images.yaml`: verify required images exist in the local runtime
- `vagrant-smoke-install.yaml`: Vagrant-oriented smoke workflow

## Validation

Use `deck lint` for schema-level checks:

```bash
deck lint --file docs/examples/offline-k8s-control-plane.yaml
```

`cases.tsv` is the lightweight example index used by repository maintainers.
