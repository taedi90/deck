# Schema Reference

`deck` validates both the workflow shape and each supported step kind through generated JSON Schema files rooted under `schemas/`.

## Entry points

- `../../schemas/deck-workflow.schema.json`: top-level workflow schema
- `../../schemas/deck-tooldefinition.schema.json`: tool definition schema
- `../../schemas/tools/*.schema.json`: per-step-kind schemas

## Workflow schema highlights

The workflow schema currently enforces:

- required `role` and `version`
- `role` must be `prepare` or `apply`
- either `steps`, `phases`, or `imports` must be present
- a step must include `id`, `kind`, and `spec`
- optional `when`, `retry`, `timeout`, and `register`

## Supported step schemas

- `artifacts.schema.json`
- `command.schema.json`
- `containerd.schema.json`
- `directory.schema.json`
- `file.schema.json`
- `image.schema.json`
- `inspection.schema.json`
- `kernel-module.schema.json`
- `kubeadm.schema.json`
- `package-cache.schema.json`
- `packages.schema.json`
- `repository.schema.json`
- `service.schema.json`
- `swap.schema.json`
- `symlink.schema.json`
- `sysctl.schema.json`
- `systemd-unit.schema.json`
- `wait.schema.json`

## Typed step reference notes

### `Repository`

Supports both apt and yum repository definitions, plus file placement and refresh controls such as `path`, `mode`, `replaceExisting`, `disableExisting`, `backupPaths`, `cleanupPaths`, and `refreshCache`.

```yaml
- id: configure-offline-repo
  apiVersion: deck/v1alpha1
  kind: Repository
  spec:
    format: apt
    replaceExisting: true
    refreshCache:
      enabled: true
      clean: true
    repositories:
      - id: offline-repo
        baseurl: http://repo.local/apt/bookworm
        trusted: true
```

### `PackageCache`

Refreshes local package metadata with `manager`, `clean`, and `update`. Set at least one of `clean` or `update`.

```yaml
- id: refresh-apt-package-cache
  apiVersion: deck/v1alpha1
  kind: PackageCache
  spec:
    manager: apt
    clean: true
    update: true
```

### `Containerd`

Supports `path`, `configPath`, `systemdCgroup`, `createDefault`, and per-registry `registryHosts` entries with `registry`, `server`, `host`, `capabilities`, and `skipVerify`.

```yaml
- id: configure-containerd
  apiVersion: deck/v1alpha1
  kind: Containerd
  spec:
    path: /etc/containerd/config.toml
    configPath: /etc/containerd/certs.d
    systemdCgroup: true
    registryHosts:
      - registry: registry.k8s.io
        server: https://registry.k8s.io
        host: http://mirror.local:5000
        capabilities: [pull, resolve]
        skipVerify: true
```

### `Service`

Supports either a single `name` or multiple `names`, plus `daemonReload`, `ifExists`, `ignoreMissing`, `enabled`, and `state`.

```yaml
- id: disable-host-firewalls
  apiVersion: deck/v1alpha1
  kind: Service
  spec:
    names: [firewalld, ufw]
    enabled: false
    state: stopped
    ifExists: true
    ignoreMissing: true
```

### `SystemdUnit`

Writes a unit file at `path` from either `content` or `contentFromTemplate`. It also supports `mode`, `daemonReload`, and an optional `service` block with `name`, `enabled`, and `state`.

```yaml
- id: setup-kubelet-systemd
  apiVersion: deck/v1alpha1
  kind: SystemdUnit
  spec:
    path: /etc/systemd/system/kubelet.service
    mode: "0644"
    content: |
      [Unit]
      Description=kubelet

      [Service]
      ExecStart=/usr/bin/kubelet

      [Install]
      WantedBy=multi-user.target
    daemonReload: true
    service:
      name: kubelet
      enabled: true
      state: started
```

### `InstallArtifacts`

Installs or extracts per-architecture artifacts. Each entry requires `source.amd64` and `source.arm64`, optional `skipIfPresent`, and exactly one of `install` or `extract`. The step also supports shared `fetch` defaults. This exists for operator clarity instead of overloading `DownloadFile`, because the target workflows express artifact-install intent, not plain fetch/copy intent.

```yaml
- id: install-k8s-binaries
  apiVersion: deck/v1alpha1
  kind: Artifacts
  spec:
    artifacts:
      - source:
          amd64:
            url: http://{{ .vars.serverURL }}/files/bin/linux/amd64/kubelet
          arm64:
            url: http://{{ .vars.serverURL }}/files/bin/linux/arm64/kubelet
        skipIfPresent:
          path: /usr/bin/kubelet
          executable: true
        install:
          path: /usr/bin/kubelet
          mode: "0755"
```

### `Kubeadm`

Runs `kubeadm init` with either `configFile` or `configTemplate`, plus bootstrap-oriented fields such as `pullImages`, `outputJoinFile`, `kubernetesVersion`, `advertiseAddress`, `podNetworkCIDR`, `criSocket`, `ignorePreflightErrors`, `extraArgs`, `timeout`, and `skipIfAdminConfExists`.

```yaml
- id: bootstrap-init
  apiVersion: deck/v1alpha1
  kind: Kubeadm
  spec:
    mode: real
    timeout: 20m
    configTemplate: default
    pullImages: true
    outputJoinFile: /tmp/deck/join.txt
    kubernetesVersion: "{{ .vars.kubernetesVersion }}"
    advertiseAddress: auto
    podNetworkCIDR: 10.244.0.0/16
    criSocket: unix:///run/containerd/containerd.sock
```

### `KubeadmReset`

Wraps `kubeadm reset` and related cleanup with `force`, `ignoreErrors`, `stopKubelet`, `criSocket`, `removePaths`, `removeFiles`, `cleanupContainers`, `restartRuntimeService`, and `timeout`.

```yaml
- id: bootstrap-reset-preflight
  apiVersion: deck/v1alpha1
  kind: Kubeadm
  spec:
    force: true
    ignoreErrors: true
    criSocket: unix:///run/containerd/containerd.sock
    removePaths:
      - /etc/cni/net.d
      - /var/lib/etcd
    removeFiles:
      - /etc/kubernetes/admin.conf
    cleanupContainers:
      - kube-apiserver
      - etcd
    restartRuntimeService: containerd
```

### `WaitPath`

Waits for a path to become `exists` or `absent`. The step also supports `type`, `nonEmpty`, `pollInterval`, and `timeout`. Use `nonEmpty` only with `state: exists`.

```yaml
- id: wait-admin-conf
  apiVersion: deck/v1alpha1
  kind: Wait
  spec:
    path: /etc/kubernetes/admin.conf
    state: exists
    type: file
    nonEmpty: true
    pollInterval: 2s
    timeout: 5m
```

### `Symlink`

Creates or replaces a symbolic link with `path` and `target`. The step also supports `force`, `createParent`, and `requireTarget`.

```yaml
- id: symlink-runc
  apiVersion: deck/v1alpha1
  kind: Symlink
  spec:
    path: /usr/bin/runc
    target: /usr/local/sbin/runc
    force: true
    createParent: true
    requireTarget: true
```

## Validation flow

1. Validate the workflow structure.
2. Validate each step against its matching tool schema.
3. Keep documentation and workflow examples aligned with the shipped schemas before packaging or applying.

## Command

```bash
deck lint --file ./workflows/scenarios/apply.yaml
```
