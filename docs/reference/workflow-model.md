# Workflow Model

`deck` uses a YAML workflow model so larger procedures stay reviewable. The goal is not to invent a DSL — it is to give air-gapped operational work a clearer structure than a growing shell script, where typed steps express intent and named phases show the operator what the procedure is doing before they read every detail.

## Top-level fields

- `version`: currently `v1alpha1`
- `vars`: optional variable map
- `steps`: top-level step list
- `phases`: named phase list for more structured execution

The schema allows one execution mode at a time:

- top-level `steps`
- named `phases`

Phase imports resolve from `workflows/components/`. Write component-relative paths such as `k8s/prereq.yaml`, not `../components/k8s/prereq.yaml`.

`workflows/components/` files are step fragments. They contain only `steps:` and may reference shared `vars.*`, but shared defaults should stay in `workflows/vars.yaml` or the importing scenario `vars:` block.

## Variables

Variables and runtime values come from distinct sources:

Static `vars` flow from two sources, in order of precedence:

1. `vars:` block in the scenario file (highest)
2. `workflows/vars.yaml` (shared defaults)

Runtime values flow separately through `register` outputs and built-in runtime facts such as `runtime.host`.

**`workflows/vars.yaml`** — define shared defaults once:

```yaml
osFamily: debian
clusterName: prod-k8s
```

**Scenario `vars:` block** — override or extend for the specific scenario:

```yaml
version: v1alpha1
vars:
  clusterName: staging-k8s   # overrides vars.yaml
```

**Template interpolation** — use `{{ .vars.NAME }}` inside string fields:

```yaml
- id: write-hostname
  kind: WriteFile
  spec:
    path: /etc/hostname
    content: "{{ .vars.clusterName }}\n"
```

**CEL expressions** — use `vars.NAME` and `runtime.NAME` (no braces) in `when:` conditions:

```yaml
- id: install-rhel-packages
  kind: InstallPackage
  spec:
    packages: [kubeadm, kubelet, kubectl]
  when: vars.osFamily == "rhel"
```

## Minimal workflow

```yaml
version: v1alpha1
steps:
  - id: prepare-state-dir
    kind: EnsureDirectory
    spec:
      path: /var/lib/deck
      mode: "0755"
```

## Minimal prepare workflow

```yaml
version: v1alpha1
steps:
  - id: fetch-kubeadm
    kind: DownloadFile
    spec:
      source:
        url: https://example.local/kubeadm
      outputPath: files/bin/kubeadm
      mode: "0755"
```

## Step shape

Every step is centered on:

- `id`
- `apiVersion`
- `kind`
- `spec`

Optional execution controls:

- `when`: CEL expression; the step is skipped when it evaluates to false
- `parallelGroup`: consecutive steps with the same group can run as one parallel batch inside a phase
- `retry`: retry count on failure
- `timeout`: duration string such as `30s` or `5m`
- `register`: export step outputs into later runtime values

### `when` — conditional execution

`when` takes a CEL expression. Use `vars.` to reference input variables defined in `vars:` or `vars.yaml`, and `runtime.` to reference step outputs registered earlier in the run.

```yaml
steps:
  - id: add-debian-repo
    kind: ConfigureRepository
    spec:
      format: deb
      repositories:
        - id: offline-base
          baseurl: file:///srv/offline-repo
          trusted: true
    when: vars.osFamily == "debian"

  - id: add-rhel-repo
    kind: ConfigureRepository
    spec:
      format: rpm
      repositories:
        - id: offline-base
          name: offline-base
          baseurl: file:///srv/offline-repo
          enabled: true
          gpgcheck: false
    when: vars.osFamily == "rhel"
```

### `register` — capture step output

`register` maps a runtime variable name to a step output key. The exported value is available to later steps via `runtime.` in CEL and `.runtime` in templates. If the step runs inside a parallel batch, the value becomes visible after the full batch succeeds.

```yaml
steps:
  - id: get-join-cmd
    kind: InitKubeadm
    spec:
      outputJoinFile: "{{ .vars.joinFile }}"
    register:
      joinFile: joinFile

  - id: join-node
    kind: JoinKubeadm
    spec:
      joinFile: "{{ .runtime.joinFile }}"
      extraArgs: ["--cri-socket", "unix:///run/containerd/containerd.sock", "--ignore-preflight-errors=Swap,FileExisting-crictl,FileExisting-conntrack,FileExisting-socat"]
```

## Phases

Use phases when the procedure has natural boundaries — a host-prereqs block that must complete before a runtime block, for example. For simple apply workflows with a handful of steps, flat `steps:` is fine.

Each phase can import component fragments, include inline steps, or both. Phases are also the persisted resume boundary for `apply`.

```yaml
version: v1alpha1
phases:
  - name: host-prereqs
    imports:
      - path: k8s/prereq.yaml
      - path: repo/offline-repo.yaml
  - name: runtime
    maxParallelism: 2
    imports:
      - path: k8s/containerd-kubelet.yaml
  - name: verify
    steps:
      - id: check-node-ready
        kind: Command
        spec:
          command: [kubectl, get, nodes]
```

Import paths are relative to `workflows/components/`. Write `k8s/prereq.yaml`, not `../components/k8s/prereq.yaml`.

Top-level `steps:` are still valid. Execution normalizes them into an implicit phase named `default`.

## Parallel batches

Use `parallelGroup` when a few consecutive steps are safe to run together.

```yaml
version: v1alpha1
phases:
  - name: packages
    maxParallelism: 2
    steps:
      - id: download-ubuntu
        kind: DownloadPackage
        parallelGroup: distro-downloads
        spec:
          packages: [containerd]
          distro:
            family: debian
            release: ubuntu2204
          repo:
            type: deb-flat
          backend:
            mode: container
            runtime: docker
            image: ubuntu:22.04

      - id: download-rhel
        kind: DownloadPackage
        parallelGroup: distro-downloads
        spec:
          packages: [containerd]
          distro:
            family: rhel
            release: rhel9
          repo:
            type: rpm
          backend:
            mode: container
            runtime: docker
            image: rockylinux:9
```

Rules for the first version:

- only consecutive steps with the same `parallelGroup` value are in the same batch
- phases still execute in order
- same-batch steps cannot consume each other's `register` outputs through `runtime.*`
- `register` outputs from a parallel batch become visible only to later batches or later phases

## Step kinds

Typed steps make the workflow easier to scan, validate, and evolve. Use `Command` only when no supported kind fits.

Supported kinds:

- `CheckHost`
- `Command`
- `WriteContainerdConfig`, `WriteContainerdRegistryHosts`
- `EnsureDirectory`
- `DownloadFile`, `WriteFile`, `CopyFile`, `EditFile`, `ExtractArchive`
- `DownloadImage`, `LoadImage`, `VerifyImage`
- `CheckCluster`
- `KernelModule`
- `InitKubeadm`, `JoinKubeadm`, `ResetKubeadm`, `UpgradeKubeadm`
- `DownloadPackage`, `InstallPackage`
- `ConfigureRepository`, `RefreshRepository`
- `ManageService`
- `Swap`
- `CreateSymlink`
- `Sysctl`
- `WriteSystemdUnit`
- `WaitForCommand`, `WaitForFile`, `WaitForMissingFile`, `WaitForService`, `WaitForTCPPort`, `WaitForMissingTCPPort`

## Prepare semantics

`prepare` uses the same step grammar as `apply`, but command context determines which kinds are valid.

- `DownloadFile` is prepare-only and writes bundle-relative outputs under the canonical `files/` root
- `DownloadImage` is prepare-only and writes prepared image archives under `images/` or an `images/...` subdirectory
- `DownloadPackage` is prepare-only and writes prepared package content under `packages/` or a `packages/...` subdirectory
- container-backed `DownloadPackage` reuses a host-owned exported artifact cache after successful exports instead of bind-mounting apt/dnf package-manager cache directories
- `workflows/prepare.yaml` is the fixed entrypoint for prepare workflows

## When to use Command

Use `Command` when no supported step kind fits yet. It is the escape hatch, not the ideal authoring path. If a workflow leans heavily on `Command`, the procedure may still be too close to raw shell.

## Validation model

`deck lint` checks:

- the top-level workflow schema
- the schema for each referenced step kind
- reserved runtime keys and workflow compatibility rules

Validating before transport is one of the main reasons to use a workflow model instead of passing around shell files.

## Related references

- `../concepts/why-deck.md`
- [Schema Reference](schema/README.md)
- `bundle-layout.md`
- `../../schemas/deck-workflow.schema.json`
