#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="${1:?root dir required}"
TARGET_DIR="${2:?target dir required}"
SCENARIO_ID="${3:-${DECK_VAGRANT_SCENARIO:-k8s-worker-join}}"

CANONICAL_ROOT="${ROOT_DIR}/test/workflows"
CANONICAL_SCENARIO_ROOT="${CANONICAL_ROOT}/${SCENARIO_ID}"
CANONICAL_SHARED_ROOT="${CANONICAL_ROOT}/_shared"
COMPAT_ROOT="${ROOT_DIR}/test/vagrant/workflows/offline-multinode"

mkdir -p "${TARGET_DIR}"

if [[ -d "${CANONICAL_SCENARIO_ROOT}" ]]; then
  mkdir -p "${TARGET_DIR}/${SCENARIO_ID}"
  cp -a "${CANONICAL_SCENARIO_ROOT}/." "${TARGET_DIR}/${SCENARIO_ID}/"
  if [[ -d "${CANONICAL_SHARED_ROOT}" ]]; then
    mkdir -p "${TARGET_DIR}/_shared"
    cp -a "${CANONICAL_SHARED_ROOT}/." "${TARGET_DIR}/_shared/"
  fi
fi

if [[ "${SCENARIO_ID}" == "offline-multinode" ]] && [[ -d "${COMPAT_ROOT}" ]]; then
  mkdir -p "${TARGET_DIR}/offline-multinode"
  cp -a "${COMPAT_ROOT}/." "${TARGET_DIR}/offline-multinode/"
fi

cat > "${TARGET_DIR}/pack.yaml" <<'EOF'
role: pack
version: v1alpha1
vars:
  kubernetesVersion: v1.30.1
  arch: amd64
  backendRuntime: auto
phases:
  - name: prepare
    steps:
      - id: runtime-deps-ubuntu2204
        apiVersion: deck/v1alpha1
        kind: DownloadPackages
        spec:
          distro:
            family: debian
            release: ubuntu2204
            arch: "{{ .vars.arch }}"
          repo:
            type: apt-flat
            generate: true
          packages: [containerd, containernetworking-plugins]
          backend:
            mode: container
            runtime: "{{ .vars.backendRuntime }}"
            image: ubuntu:22.04
      - id: runtime-deps-ubuntu2404
        apiVersion: deck/v1alpha1
        kind: DownloadPackages
        spec:
          distro:
            family: debian
            release: ubuntu2404
            arch: "{{ .vars.arch }}"
          repo:
            type: apt-flat
            generate: true
          packages: [containerd, containernetworking-plugins]
          backend:
            mode: container
            runtime: "{{ .vars.backendRuntime }}"
            image: ubuntu:24.04
      - id: runtime-deps-rocky9
        apiVersion: deck/v1alpha1
        kind: DownloadPackages
        spec:
          distro:
            family: rhel
            release: rocky9
            arch: "{{ .vars.arch }}"
          repo:
            type: yum
            generate: true
          packages: [bash]
          backend:
            mode: container
            runtime: "{{ .vars.backendRuntime }}"
            image: rockylinux:9
      - id: image-kube-apiserver
        apiVersion: deck/v1alpha1
        kind: DownloadImages
        spec:
          images: ["registry.k8s.io/kube-apiserver:{{ .vars.kubernetesVersion }}"]
      - id: image-kube-controller-manager
        apiVersion: deck/v1alpha1
        kind: DownloadImages
        spec:
          images: ["registry.k8s.io/kube-controller-manager:{{ .vars.kubernetesVersion }}"]
      - id: image-kube-scheduler
        apiVersion: deck/v1alpha1
        kind: DownloadImages
        spec:
          images: ["registry.k8s.io/kube-scheduler:{{ .vars.kubernetesVersion }}"]
      - id: image-kube-proxy
        apiVersion: deck/v1alpha1
        kind: DownloadImages
        spec:
          images: ["registry.k8s.io/kube-proxy:{{ .vars.kubernetesVersion }}"]
      - id: image-pause
        apiVersion: deck/v1alpha1
        kind: DownloadImages
        spec:
          images: ["registry.k8s.io/pause:3.9"]
      - id: image-etcd
        apiVersion: deck/v1alpha1
        kind: DownloadImages
        spec:
          images: ["registry.k8s.io/etcd:3.5.12-0"]
      - id: image-coredns
        apiVersion: deck/v1alpha1
        kind: DownloadImages
        spec:
          images: ["registry.k8s.io/coredns/coredns:v1.11.1"]
      - id: bin-kubelet-amd64
        apiVersion: deck/v1alpha1
        kind: DownloadFile
        spec:
          source:
            url: https://dl.k8s.io/release/{{ .vars.kubernetesVersion }}/bin/linux/amd64/kubelet
          output:
            path: files/bin/linux/amd64/kubelet
            chmod: "0755"
      - id: bin-kubeadm-amd64
        apiVersion: deck/v1alpha1
        kind: DownloadFile
        spec:
          source:
            url: https://dl.k8s.io/release/{{ .vars.kubernetesVersion }}/bin/linux/amd64/kubeadm
          output:
            path: files/bin/linux/amd64/kubeadm
            chmod: "0755"
      - id: bin-kubectl-amd64
        apiVersion: deck/v1alpha1
        kind: DownloadFile
        spec:
          source:
            url: https://dl.k8s.io/release/{{ .vars.kubernetesVersion }}/bin/linux/amd64/kubectl
          output:
            path: files/bin/linux/amd64/kubectl
            chmod: "0755"
      - id: bin-crictl-amd64
        apiVersion: deck/v1alpha1
        kind: DownloadFile
        spec:
          source:
            url: https://github.com/kubernetes-sigs/cri-tools/releases/download/v1.30.0/crictl-v1.30.0-linux-amd64.tar.gz
          output:
            path: files/bin/linux/amd64/crictl.tar.gz
      - id: bin-containerd-amd64
        apiVersion: deck/v1alpha1
        kind: DownloadFile
        spec:
          source:
            url: https://github.com/containerd/containerd/releases/download/v1.7.18/containerd-1.7.18-linux-amd64.tar.gz
          output:
            path: files/bin/linux/amd64/containerd.tar.gz
      - id: bin-runc-amd64
        apiVersion: deck/v1alpha1
        kind: DownloadFile
        spec:
          source:
            url: https://github.com/opencontainers/runc/releases/download/v1.1.13/runc.amd64
          output:
            path: files/bin/linux/amd64/runc
            chmod: "0755"
      - id: bin-cni-plugins-amd64
        apiVersion: deck/v1alpha1
        kind: DownloadFile
        spec:
          source:
            url: https://github.com/containernetworking/plugins/releases/download/v1.5.1/cni-plugins-linux-amd64-v1.5.1.tgz
          output:
            path: files/bin/linux/amd64/cni-plugins.tgz
      - id: bin-kubelet-arm64
        apiVersion: deck/v1alpha1
        kind: DownloadFile
        spec:
          source:
            url: https://dl.k8s.io/release/{{ .vars.kubernetesVersion }}/bin/linux/arm64/kubelet
          output:
            path: files/bin/linux/arm64/kubelet
            chmod: "0755"
      - id: bin-kubeadm-arm64
        apiVersion: deck/v1alpha1
        kind: DownloadFile
        spec:
          source:
            url: https://dl.k8s.io/release/{{ .vars.kubernetesVersion }}/bin/linux/arm64/kubeadm
          output:
            path: files/bin/linux/arm64/kubeadm
            chmod: "0755"
      - id: bin-kubectl-arm64
        apiVersion: deck/v1alpha1
        kind: DownloadFile
        spec:
          source:
            url: https://dl.k8s.io/release/{{ .vars.kubernetesVersion }}/bin/linux/arm64/kubectl
          output:
            path: files/bin/linux/arm64/kubectl
            chmod: "0755"
      - id: bin-crictl-arm64
        apiVersion: deck/v1alpha1
        kind: DownloadFile
        spec:
          source:
            url: https://github.com/kubernetes-sigs/cri-tools/releases/download/v1.30.0/crictl-v1.30.0-linux-arm64.tar.gz
          output:
            path: files/bin/linux/arm64/crictl.tar.gz
      - id: bin-containerd-arm64
        apiVersion: deck/v1alpha1
        kind: DownloadFile
        spec:
          source:
            url: https://github.com/containerd/containerd/releases/download/v1.7.18/containerd-1.7.18-linux-arm64.tar.gz
          output:
            path: files/bin/linux/arm64/containerd.tar.gz
      - id: bin-runc-arm64
        apiVersion: deck/v1alpha1
        kind: DownloadFile
        spec:
          source:
            url: https://github.com/opencontainers/runc/releases/download/v1.1.13/runc.arm64
          output:
            path: files/bin/linux/arm64/runc
            chmod: "0755"
      - id: bin-cni-plugins-arm64
        apiVersion: deck/v1alpha1
        kind: DownloadFile
        spec:
          source:
            url: https://github.com/containernetworking/plugins/releases/download/v1.5.1/cni-plugins-linux-arm64-v1.5.1.tgz
          output:
            path: files/bin/linux/arm64/cni-plugins.tgz
EOF
cat > "${TARGET_DIR}/apply.yaml" <<'EOF'
role: apply
version: v1alpha1
phases:
  - name: install
    steps:
      - id: noop
        apiVersion: deck/v1alpha1
        kind: RunCommand
        spec:
          command: ["sh", "-c", "true"]
EOF
printf '{}\n' > "${TARGET_DIR}/vars.yaml"
