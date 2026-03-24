# Bundle Layout

`deck prepare` writes a self-contained workspace under the current directory. `deck bundle build` archives that workspace into a single tarball you carry into the site.

The bundle is the unit of offline handoff. Everything the workflow needs to run on the target machine should be inside it — no implicit fetch at execution time, no reach-back to external services.

## Canonical bundle inputs

`deck bundle build` archives the following workspace paths:

- `deck`: a launcher script written to the workspace root during `prepare`
- `workflows/`: scenario, component, and variable files used at the site
- `outputs/bin/`: platform-specific runtime binaries selected during `prepare`
- `outputs/packages/`: OS or Kubernetes packages fetched during `prepare`
- `outputs/images/`: container image archives fetched during `prepare`
- `outputs/files/`: supporting files copied or downloaded during `prepare`
- `.deck/manifest.json`: integrity manifest used by `bundle verify`

`bundle build` does not archive arbitrary extra root-level paths by default. If a workflow needs additional content at the site, place it under `workflows/` or produce it under `outputs/` during `prepare` so it travels with the canonical bundle.

## Example bundle contents

A typical Kubernetes control-plane bundle might contain:

```
deck
.deck/manifest.json
workflows/scenarios/apply.yaml
workflows/prepare.yaml
workflows/vars.yaml
outputs/bin/linux/amd64/deck
outputs/packages/kubernetes-1.29.tar.gz
outputs/images/pause-3.9.tar
outputs/images/coredns-1.11.tar
outputs/files/kubeadm.conf
```

The operator unpacks this on the target node, then runs `./deck apply`. The launcher selects the matching runtime binary from `outputs/bin/<os>/<arch>/deck` when that platform is included in the bundle.

## Core rule

If the site needs it to run the workflow, place it in the canonical bundle inputs rather than assume it already exists on the target machine.
