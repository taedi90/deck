# Example Workflows

The files in `docs/examples/` are small starting points for real procedures.

They are meant to show how `deck` keeps operational work readable: use typed steps where possible, keep the workflow shape obvious, and treat shell as the fallback.

## How to use these examples

- start from them when you want a concrete workflow to adapt
- keep the overall structure clear before adding more details
- replace repetitive shell with typed steps when a step kind already fits
- validate the result before packaging or transport

## What not to assume

- these are not remote orchestration playbooks
- these do not require a shared server to be useful
- `Command` is not the preferred first choice just because it is flexible

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

`cases.tsv` remains the lightweight example index used by repository maintainers.
