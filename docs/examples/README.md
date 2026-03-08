# Example Workflows

The files in `docs/examples/` are runnable or validatable workflow examples for common offline operations.

## Files

- `offline-k8s-control-plane.yaml`: kubeadm-based control-plane bootstrap example
- `offline-k8s-worker.yaml`: kubeadm worker join example
- `offline-repo-preinstall.yaml`: prepare package repository configuration on the target host
- `offline-containerd-mirror.yaml`: point containerd at an internal registry or mirror path
- `offline-verify-images.yaml`: verify required images exist in the local runtime
- `vagrant-smoke-install.yaml`: Vagrant-oriented smoke workflow

## Validation

Use `deck validate` for schema-level checks:

```bash
deck validate --file docs/examples/offline-k8s-control-plane.yaml
```

`cases.tsv` remains the lightweight example index used by repository maintainers.
