# Workflow Model

`deck` uses a YAML workflow model so larger procedures stay reviewable. The goal is not to invent a DSL — it is to give air-gapped operational work a clearer structure than a growing shell script, where typed steps express intent and named phases show the operator what the procedure is doing before they read every detail.

## Top-level fields

- `role`: required, either `prepare` or `apply`
- `version`: currently `v1alpha1`
- `vars`: optional variable map
- `artifacts`: declarative prepare artifact inventory for `role: prepare`
- `steps`: top-level step list
- `phases`: named phase list for more structured execution

The schema allows one execution mode at a time:

- `artifacts` for declarative prepare workflows
- top-level `steps`
- named `phases`

Phase imports resolve from `workflows/components/`. Write component-relative paths such as `k8s/prereq.yaml`, not `../components/k8s/prereq.yaml`.

`workflows/components/` files are step fragments. They contain only `steps:` and may reference shared `vars.*`, but shared defaults should stay in `workflows/vars.yaml` or the importing scenario `vars:` block.

## Variables

Variables flow from three sources, in order of precedence:

1. `vars:` block in the scenario file (highest)
2. `workflows/vars.yaml` (shared defaults)
3. runtime-registered step outputs via `register`

**`workflows/vars.yaml`** — define shared defaults once:

```yaml
osFamily: debian
clusterName: prod-k8s
```

**Scenario `vars:` block** — override or extend for the specific scenario:

```yaml
role: apply
version: v1alpha1
vars:
  clusterName: staging-k8s   # overrides vars.yaml
```

**Template interpolation** — use `{{ .vars.NAME }}` inside string fields:

```yaml
- id: write-hostname
  kind: File
  spec:
    action: write
    path: /etc/hostname
    content: "{{ .vars.clusterName }}\n"
```

**CEL expressions** — use `vars.NAME` (no braces) in `when:` conditions:

```yaml
- id: install-rhel-packages
  kind: Packages
  spec:
    action: install
    names: [kubeadm, kubelet, kubectl]
  when: vars.osFamily == "rhel"
```

## Minimal workflow

```yaml
role: apply
version: v1alpha1
steps:
  - id: prepare-state-dir
    kind: Directory
    spec:
      path: /var/lib/deck
      mode: "0755"
```

## Minimal prepare workflow

```yaml
role: prepare
version: v1alpha1
artifacts:
  files:
    - group: binaries
      items:
        - id: kubeadm
          source:
            url: https://example.local/kubeadm
          output:
            path: bin/kubeadm
```

## Step shape

Every step is centered on:

- `id`
- `apiVersion`
- `kind`
- `spec`

Optional execution controls:

- `when`: CEL expression; the step is skipped when it evaluates to false
- `retry`: retry count on failure
- `timeout`: duration string such as `30s` or `5m`
- `register`: export step outputs into later runtime values

### `when` — conditional execution

`when` takes a CEL expression. Use `vars.` to reference variables defined in `vars:` or `vars.yaml`.

```yaml
steps:
  - id: add-debian-repo
    kind: Repository
    spec:
      type: apt
      name: offline-base
      baseurl: file:///srv/offline-repo
    when: vars.osFamily == "debian"

  - id: add-rhel-repo
    kind: Repository
    spec:
      type: yum
      name: offline-base
      baseurl: file:///srv/offline-repo
    when: vars.osFamily == "rhel"
```

### `register` — capture step output

`register` maps a variable name to a step output key. The exported value is available to later steps via `vars.`.

```yaml
steps:
  - id: get-join-cmd
    kind: Kubeadm
    spec:
      action: token-create
    register:
      joinCmd: joinCommand

  - id: join-node
    kind: Kubeadm
    spec:
      action: join
      mode: real
      joinFile: "{{ .vars.joinFile }}"
      extraArgs: ["--cri-socket", "unix:///run/containerd/containerd.sock", "--ignore-preflight-errors=Swap,FileExisting-crictl,FileExisting-conntrack,FileExisting-socat"]
```

## Phases

Use phases when the procedure has natural boundaries — a host-prereqs block that must complete before a runtime block, for example. For simple apply workflows with a handful of steps, flat `steps:` is fine.

Each phase can import component fragments, include inline steps, or both.

```yaml
role: apply
version: v1alpha1
phases:
  - name: host-prereqs
    imports:
      - path: k8s/prereq.yaml
      - path: repo/offline-repo.yaml
  - name: runtime
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

`artifacts` is the preferred authoring mode for `role: prepare`. Use `steps` or `phases` for `role: apply`.

## Step kinds

Typed steps make the workflow easier to scan, validate, and evolve. Use `Command` only when no supported kind fits.

Supported kinds:

- `Artifacts`
- `Command`
- `Containerd`
- `Directory`
- `File`
- `Image`
- `Checks`
- `KernelModule`
- `Kubeadm`
- `PackageCache`
- `Packages`
- `Repository`
- `Sysctl`
- `Service`
- `Swap`
- `Symlink`
- `SystemdUnit`
- `Wait`

## Prepare semantics

`role: prepare` can use top-level `artifacts` to declare artifact inventory instead of writing repeated download steps.

- `artifacts.files[*].items[*].output.path` is relative to the `files/` bundle root, so use `bin/kubeadm`, not `files/bin/kubeadm`
- `artifacts.images` declares image groups and lets the engine choose bundle tar layout
- `artifacts.packages` declares package groups per target OS family, release, and arch
- internally, `deck` still plans typed actions, but the authoring model stays inventory-driven

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
