#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
VAGRANT_DIR="${ROOT_DIR}/test/vagrant"
LIBVIRT_POOL_HELPER="${VAGRANT_DIR}/scripts/libvirt-pool.sh"
ENSURE_BINARIES_HELPER="${VAGRANT_DIR}/scripts/ensure-deck-binaries.sh"
PREPARE_CACHE_HELPER="${VAGRANT_DIR}/scripts/prepare-cache.sh"
VM_SCENARIO_SCRIPT="${VAGRANT_DIR}/scripts/run-offline-multinode-agent-vm.sh"
TS="$(date +%Y%m%d-%H%M%S)"
ART_DIR_REL=".ci/artifacts/offline-multinode-agent-${TS}"
ART_DIR_ABS="${ROOT_DIR}/${ART_DIR_REL}"
DECK_VAGRANT_PROVIDER="libvirt"
DECK_VAGRANT_BOX_CONTROL_PLANE="${DECK_VAGRANT_BOX_CONTROL_PLANE:-${DECK_VAGRANT_BOX:-generic/ubuntu2204}}"
DECK_VAGRANT_BOX_WORKER="${DECK_VAGRANT_BOX_WORKER:-bento/ubuntu-24.04}"
DECK_VAGRANT_BOX_WORKER_2="${DECK_VAGRANT_BOX_WORKER_2:-generic/rocky9}"
DECK_VAGRANT_BOX="${DECK_VAGRANT_BOX:-${DECK_VAGRANT_BOX_CONTROL_PLANE}}"
DECK_VAGRANT_VM_PREFIX="${DECK_VAGRANT_VM_PREFIX:-deck-offline-multinode-agent-${TS}-$$}"
DECK_VAGRANT_SKIP_CLEANUP="${DECK_VAGRANT_SKIP_CLEANUP:-0}"
IN_VAGRANT_DIR=0

usage() {
  cat <<'EOF'
Usage: test/vagrant/run-offline-multinode-agent.sh [--skip-cleanup]

Options:
  --skip-cleanup  Keep VMs after scenario for debugging.
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --skip-cleanup)
      DECK_VAGRANT_SKIP_CLEANUP="1"
      shift
      ;;
    --help|-h)
      usage
      exit 0
      ;;
    *)
      echo "[deck] unknown argument: $1"
      usage
      exit 1
      ;;
  esac
done

fetch_vm_artifacts() {
  local node="$1"
  local bundle_tgz="${ART_DIR_ABS}/vm-artifacts-${node}.tgz"

  DECK_VAGRANT_BOX="${DECK_VAGRANT_BOX}" DECK_VAGRANT_PROVIDER="${DECK_VAGRANT_PROVIDER}" DECK_VAGRANT_VM_PREFIX="${DECK_VAGRANT_VM_PREFIX}" \
    vagrant ssh "${node}" -c "if [[ -d /workspace/${ART_DIR_REL} ]]; then tar -czf - -C /workspace ${ART_DIR_REL}; fi" > "${bundle_tgz}" 2>/dev/null || true

  if [[ -s "${bundle_tgz}" ]]; then
    tar -xzf "${bundle_tgz}" -C "${ROOT_DIR}" || true
  fi
}

delete_stale_volume() {
  local node="$1"
  local -a candidates=(
    "${DECK_VAGRANT_VM_PREFIX}${node}.img"
    "${DECK_VAGRANT_VM_PREFIX}-${node}.img"
    "${DECK_VAGRANT_VM_PREFIX}_${node}.img"
  )
  local vol_name
  for vol_name in "${candidates[@]}"; do
    virsh -c "${DECK_LIBVIRT_URI}" vol-delete --pool "${DECK_LIBVIRT_POOL_NAME}" "${vol_name}" >/dev/null 2>&1 || true
  done
}

run_vagrant_ssh() {
  local node="$1"
  local cmd="$2"
  local attempts="${3:-4}"
  local delay_sec="${4:-5}"
  local i rc

  for ((i=1; i<=attempts; i++)); do
    set +e
    DECK_VAGRANT_BOX="${DECK_VAGRANT_BOX}" DECK_VAGRANT_PROVIDER="${DECK_VAGRANT_PROVIDER}" DECK_VAGRANT_VM_PREFIX="${DECK_VAGRANT_VM_PREFIX}" \
      vagrant ssh "${node}" -c "${cmd}"
    rc=$?
    set -e
    if [[ ${rc} -eq 0 ]]; then
      return 0
    fi
    sleep "${delay_sec}"
  done

  return 1
}

export VAGRANT_DEFAULT_PROVIDER="libvirt"
export DECK_VAGRANT_MANAGEMENT_NETWORK_NAME="${DECK_VAGRANT_MANAGEMENT_NETWORK_NAME:-default}"
export DECK_VAGRANT_MANAGEMENT_NETWORK_ADDRESS="${DECK_VAGRANT_MANAGEMENT_NETWORK_ADDRESS:-192.168.122.0/24}"
export DECK_VAGRANT_IP_ADDRESS_TIMEOUT="${DECK_VAGRANT_IP_ADDRESS_TIMEOUT:-300}"
export DECK_VAGRANT_LIBVIRT_IP_COMMAND="${DECK_VAGRANT_LIBVIRT_IP_COMMAND:-}"
export DECK_VAGRANT_QEMU_USE_AGENT="${DECK_VAGRANT_QEMU_USE_AGENT:-0}"
export DECK_VAGRANT_MGMT_ATTACH="${DECK_VAGRANT_MGMT_ATTACH:-1}"
export DECK_VAGRANT_BOX_CONTROL_PLANE
export DECK_VAGRANT_BOX_WORKER
export DECK_VAGRANT_BOX_WORKER_2

validate_box_provider() {
  local box
  for box in "${DECK_VAGRANT_BOX_CONTROL_PLANE}" "${DECK_VAGRANT_BOX_WORKER}" "${DECK_VAGRANT_BOX_WORKER_2}"; do
    if [[ "${box}" == "manrala/ubuntu24" ]]; then
      echo "[deck] box/provider mismatch: ${box} does not support libvirt"
      echo "[deck] use DECK_VAGRANT_BOX=bento/ubuntu-24.04 (or another libvirt-capable box)"
      exit 1
    fi
  done
}

check_provider_available() {
  local status_out=""
  local status_rc=0

  pushd "${VAGRANT_DIR}" >/dev/null
  set +e
  status_out="$(DECK_VAGRANT_BOX="${DECK_VAGRANT_BOX}" DECK_VAGRANT_PROVIDER="libvirt" DECK_VAGRANT_VM_PREFIX="${DECK_VAGRANT_VM_PREFIX}" vagrant status 2>&1)"
  status_rc=$?
  set -e
  popd >/dev/null

  if [[ ${status_rc} -ne 0 ]]; then
    echo "[deck] required provider 'libvirt' is unavailable"
    echo "[deck] install libvirt dependencies/plugins on this runner"
    echo "${status_out}"
    exit 1
  fi
}

cleanup() {
  set +e
  if [[ "${IN_VAGRANT_DIR}" == "1" ]]; then
    if [[ "${DECK_VAGRANT_SKIP_CLEANUP}" != "1" ]]; then
      DECK_VAGRANT_BOX="${DECK_VAGRANT_BOX}" DECK_VAGRANT_PROVIDER="${DECK_VAGRANT_PROVIDER}" DECK_VAGRANT_VM_PREFIX="${DECK_VAGRANT_VM_PREFIX}" vagrant destroy -f >/dev/null 2>&1 || true
    else
      echo "[deck] skip cleanup enabled (DECK_VAGRANT_SKIP_CLEANUP=1): keeping VMs"
    fi
    popd >/dev/null || true
    IN_VAGRANT_DIR=0
  fi
}

trap cleanup EXIT INT TERM

validate_box_provider
check_provider_available
mkdir -p "${ART_DIR_ABS}"

for p in "${VAGRANT_DIR}/Vagrantfile" "${VM_SCENARIO_SCRIPT}" "${LIBVIRT_POOL_HELPER}" "${ENSURE_BINARIES_HELPER}"; do
  if [[ ! -e "${p}" ]]; then
    echo "[deck] missing required path: ${p}"
    exit 1
  fi
done

if [[ ! -x "${PREPARE_CACHE_HELPER}" ]]; then
  echo "[deck] missing executable script: test/vagrant/scripts/prepare-cache.sh"
  exit 1
fi

if [[ ! -x "${VM_SCENARIO_SCRIPT}" ]]; then
  echo "[deck] missing executable script: test/vagrant/scripts/run-offline-multinode-agent-vm.sh"
  exit 1
fi

if [[ ! -x "${LIBVIRT_POOL_HELPER}" ]]; then
  echo "[deck] missing executable script: test/vagrant/scripts/libvirt-pool.sh"
  exit 1
fi

if [[ ! -x "${ENSURE_BINARIES_HELPER}" ]]; then
  echo "[deck] missing executable script: test/vagrant/scripts/ensure-deck-binaries.sh"
  exit 1
fi

source "${LIBVIRT_POOL_HELPER}"
prepare_libvirt_environment

"${ENSURE_BINARIES_HELPER}" "${ROOT_DIR}"

DECK_HOST_BIN="${ROOT_DIR}/.ci/artifacts/deck-host" \
DECK_VAGRANT_PROVIDER="${DECK_VAGRANT_PROVIDER}" \
DECK_VAGRANT_BOX="${DECK_VAGRANT_BOX_CONTROL_PLANE},${DECK_VAGRANT_BOX_WORKER},${DECK_VAGRANT_BOX_WORKER_2}" \
DECK_PREPARE_TEMPLATE_PATH="${ROOT_DIR}/test/vagrant/scenario-templates/offline-multinode-prepare.yaml" \
  "${PREPARE_CACHE_HELPER}" "${ROOT_DIR}" "${ART_DIR_ABS}" "offline-multinode-agent"
source "${ART_DIR_ABS}/prepare-cache.env"

pushd "${VAGRANT_DIR}" >/dev/null
IN_VAGRANT_DIR=1

if [[ "${DECK_VAGRANT_SKIP_CLEANUP}" != "1" ]]; then
  DECK_VAGRANT_BOX="${DECK_VAGRANT_BOX}" DECK_VAGRANT_PROVIDER="${DECK_VAGRANT_PROVIDER}" DECK_VAGRANT_VM_PREFIX="${DECK_VAGRANT_VM_PREFIX}" vagrant destroy -f || true
else
  echo "[deck] skip cleanup enabled (DECK_VAGRANT_SKIP_CLEANUP=1): skipping initial vagrant destroy"
fi
delete_stale_volume "control-plane"
delete_stale_volume "worker"
delete_stale_volume "worker-2"

DECK_VAGRANT_BOX="${DECK_VAGRANT_BOX}" DECK_VAGRANT_PROVIDER="${DECK_VAGRANT_PROVIDER}" DECK_VAGRANT_VM_PREFIX="${DECK_VAGRANT_VM_PREFIX}" vagrant up control-plane worker worker-2 --provider "${DECK_VAGRANT_PROVIDER}"

SERVER_IP="$(DECK_VAGRANT_BOX="${DECK_VAGRANT_BOX}" DECK_VAGRANT_PROVIDER="${DECK_VAGRANT_PROVIDER}" DECK_VAGRANT_VM_PREFIX="${DECK_VAGRANT_VM_PREFIX}" vagrant ssh-config control-plane 2>/dev/null | awk '/^[[:space:]]*HostName[[:space:]]+/ {print $2; exit}')"
if [[ -z "${SERVER_IP}" ]]; then
  echo "[deck] failed to resolve control-plane IPv4 address"
  exit 1
fi

SERVER_URL="http://${SERVER_IP}:18080"

run_vagrant_ssh "worker" "ART_DIR_REL=${ART_DIR_REL} SERVER_URL=${SERVER_URL} DECK_OFFLINE_RELEASE=ubuntu2404 bash /workspace/test/vagrant/scripts/run-offline-multinode-agent-vm.sh worker start-agent"
run_vagrant_ssh "worker-2" "ART_DIR_REL=${ART_DIR_REL} SERVER_URL=${SERVER_URL} DECK_OFFLINE_RELEASE=rocky9 bash /workspace/test/vagrant/scripts/run-offline-multinode-agent-vm.sh worker-2 start-agent"
run_vagrant_ssh "control-plane" "ART_DIR_REL=${ART_DIR_REL} SERVER_URL=${SERVER_URL} DECK_KUBEADM_ADVERTISE_ADDRESS=${SERVER_IP} DECK_OFFLINE_RELEASE_CONTROL_PLANE=ubuntu2204 DECK_OFFLINE_RELEASE_WORKER=ubuntu2404 DECK_OFFLINE_RELEASE_WORKER_2=rocky9 DECK_PREPARED_BUNDLE_REL=${DECK_PREPARED_BUNDLE_REL} DECK_PREPARE_CACHE_STATUS=${DECK_PREPARE_CACHE_STATUS} bash /workspace/test/vagrant/scripts/run-offline-multinode-agent-vm.sh control-plane orchestrate"

fetch_vm_artifacts "control-plane"
fetch_vm_artifacts "worker"
fetch_vm_artifacts "worker-2"

if [[ ! -f "${ART_DIR_ABS}/offline-multinode-agent-pass.txt" ]]; then
  echo "[deck] PASS marker missing: ${ART_DIR_ABS}/offline-multinode-agent-pass.txt"
  exit 1
fi

if [[ ! -f "${ART_DIR_ABS}/cluster-nodes.txt" ]]; then
  echo "[deck] missing nodes report: ${ART_DIR_ABS}/cluster-nodes.txt"
  exit 1
fi

python3 - <<'PY' "${ART_DIR_ABS}/cluster-nodes.txt"
import sys

path = sys.argv[1]
ready = 0
total = 0
nodes = set()
with open(path, "r", encoding="utf-8") as fp:
    for line in fp:
        line = line.strip()
        if not line or line.startswith("NAME"):
            continue
        parts = line.split()
        if len(parts) < 2:
            continue
        total += 1
        nodes.add(parts[0])
        if parts[1] == "Ready":
            ready += 1

if total != 3 or ready != 3 or "control-plane" not in nodes or "worker" not in nodes or "worker-2" not in nodes:
    raise SystemExit(f"expected 3 Ready nodes (control-plane, worker, worker-2), got total={total} ready={ready} nodes={sorted(nodes)}")
PY

DECK_VAGRANT_BOX="${DECK_VAGRANT_BOX}" DECK_VAGRANT_PROVIDER="${DECK_VAGRANT_PROVIDER}" DECK_VAGRANT_VM_PREFIX="${DECK_VAGRANT_VM_PREFIX}" vagrant status > "${ART_DIR_ABS}/vagrant-status.txt"
if [[ "${DECK_VAGRANT_SKIP_CLEANUP}" != "1" ]]; then
  DECK_VAGRANT_BOX="${DECK_VAGRANT_BOX}" DECK_VAGRANT_PROVIDER="${DECK_VAGRANT_PROVIDER}" DECK_VAGRANT_VM_PREFIX="${DECK_VAGRANT_VM_PREFIX}" vagrant destroy -f
else
  echo "[deck] skip cleanup enabled (DECK_VAGRANT_SKIP_CLEANUP=1): skipping final vagrant destroy"
fi

popd >/dev/null
IN_VAGRANT_DIR=0
trap - EXIT INT TERM

echo "[deck] offline multi-node agent artifacts: ${ART_DIR_ABS}"
