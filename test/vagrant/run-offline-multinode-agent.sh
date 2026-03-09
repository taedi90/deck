#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
VAGRANT_DIR="${ROOT_DIR}/test/vagrant"
LIBVIRT_ENV_HELPER="${VAGRANT_DIR}/libvirt-env.sh"
BUILD_BINARIES_HELPER="${VAGRANT_DIR}/build-deck-binaries.sh"
VM_SCENARIO_SCRIPT="${VAGRANT_DIR}/run-offline-multinode-vm.sh"

TS="$(date +%Y%m%d-%H%M%S)"
ART_DIR_REL="${DECK_VAGRANT_ART_DIR:-test/artifacts/offline-multinode-local}"
ART_DIR_ABS="${ROOT_DIR}/${ART_DIR_REL}"
CHECKPOINT_DIR=""
PREPARED_BUNDLE_REL="test/artifacts/cache/offline-multinode-prepared-bundle"
PREPARED_BUNDLE_ABS="${ROOT_DIR}/${PREPARED_BUNDLE_REL}"
PREPARED_BUNDLE_STAMP="${PREPARED_BUNDLE_ABS}/.deck-cache-key"
PREPARED_BUNDLE_WORK_REL="test/artifacts/cache/offline-multinode-prepared-bundle-work"
PREPARED_BUNDLE_WORK_ABS="${ROOT_DIR}/${PREPARED_BUNDLE_WORK_REL}"
PREPARED_BUNDLE_PACK_ROOT="${PREPARED_BUNDLE_WORK_ABS}/host-pack"
PREPARED_BUNDLE_WORKFLOW_DIR="${PREPARED_BUNDLE_PACK_ROOT}/workflows"
PREPARED_BUNDLE_FRAGMENT_DIR="${PREPARED_BUNDLE_WORKFLOW_DIR}/offline-multinode"
PREPARED_BUNDLE_TAR="${PREPARED_BUNDLE_WORK_ABS}/prepared-bundle.tar"
PREPARED_BUNDLE_STAGE_ABS="${PREPARED_BUNDLE_WORK_ABS}/prepared-bundle.stage"
RSYNC_STAGE_REL="test/artifacts/cache/offline-multinode-rsync-root"
RSYNC_STAGE_ABS="${ROOT_DIR}/${RSYNC_STAGE_REL}"
RSYNC_STAGE_STAGE_ABS="${ROOT_DIR}/test/artifacts/cache/offline-multinode-rsync-root.stage"

DECK_VAGRANT_PROVIDER="libvirt"
DECK_VAGRANT_SYNC_TYPE="${DECK_VAGRANT_SYNC_TYPE:-rsync}"
DECK_VAGRANT_BOX_CONTROL_PLANE="${DECK_VAGRANT_BOX_CONTROL_PLANE:-${DECK_VAGRANT_BOX:-generic/ubuntu2204}}"
DECK_VAGRANT_BOX_WORKER="${DECK_VAGRANT_BOX_WORKER:-bento/ubuntu-24.04}"
DECK_VAGRANT_BOX_WORKER_2="${DECK_VAGRANT_BOX_WORKER_2:-generic/rocky9}"
DECK_VAGRANT_BOX="${DECK_VAGRANT_BOX:-${DECK_VAGRANT_BOX_CONTROL_PLANE}}"
DECK_VAGRANT_VM_PREFIX="${DECK_VAGRANT_VM_PREFIX:-deck-offline-multinode-local}"
DECK_VAGRANT_SKIP_CLEANUP="${DECK_VAGRANT_SKIP_CLEANUP:-1}"
DECK_VAGRANT_SKIP_COLLECT="${DECK_VAGRANT_SKIP_COLLECT:-0}"
DECK_VAGRANT_COLLECT_PARALLEL="${DECK_VAGRANT_COLLECT_PARALLEL:-3}"

STEP=""
FROM_STEP=""
TO_STEP=""
RESUME="${DECK_VAGRANT_RESUME:-1}"
IN_VAGRANT_DIR=0
LIBVIRT_ENV_INITIALIZED=0
FRESH=0

STEPS=(
  prepare-host
  up-vms
  start-server
  enqueue-install
  wait-install
  enqueue-join
  assert-cluster
  collect
  cleanup
)

usage() {
  cat <<'EOF'
Usage: test/vagrant/run-offline-multinode-agent.sh [options]

Options:
  --step <name>       Run only one step.
  --from-step <name>  Start from step.
  --to-step <name>    End at step.
  --resume            Skip completed checkpoints and continue.
  --fresh             Recreate VMs and rerun from a clean local state.
  --art-dir <path>    Reuse artifact directory (absolute or workspace-relative).
  --skip-cleanup      Keep VMs after scenario for debugging.
  --cleanup           Destroy VMs at the end of the run.
  --skip-collect      Do not fetch artifacts back from VMs.

Steps:
  prepare-host, up-vms, start-server, enqueue-install,
  wait-install, enqueue-join, assert-cluster, collect, cleanup
EOF
}

resolve_host_build_context() {
  HOST_BIN="${ROOT_DIR}/test/artifacts/bin/deck-host"
  HOST_BACKEND_RUNTIME="podman"
  HOST_ARCH="amd64"

  if ! command -v podman >/dev/null 2>&1 && command -v docker >/dev/null 2>&1; then
    HOST_BACKEND_RUNTIME="docker"
  fi
  case "$(uname -m)" in
    x86_64)
      HOST_ARCH="amd64"
      ;;
    aarch64|arm64)
      HOST_ARCH="arm64"
      ;;
  esac
}

step_index() {
  local step_name="$1"
  local i
  for i in "${!STEPS[@]}"; do
    if [[ "${STEPS[$i]}" == "${step_name}" ]]; then
      echo "${i}"
      return 0
    fi
  done
  return 1
}

normalize_art_dir() {
  local path="$1"
  if [[ -z "${path}" ]]; then
    return 0
  fi
  if [[ "${path}" = /* ]]; then
    ART_DIR_ABS="${path}"
    ART_DIR_REL="${path#${ROOT_DIR}/}"
  else
    ART_DIR_REL="${path}"
    ART_DIR_ABS="${ROOT_DIR}/${path}"
  fi
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --step)
      STEP="${2:?--step requires value}"
      shift 2
      ;;
    --from-step)
      FROM_STEP="${2:?--from-step requires value}"
      shift 2
      ;;
    --to-step)
      TO_STEP="${2:?--to-step requires value}"
      shift 2
      ;;
    --resume)
      RESUME=1
      shift
      ;;
    --fresh)
      FRESH=1
      RESUME=0
      DECK_VAGRANT_SKIP_CLEANUP="0"
      shift
      ;;
    --art-dir)
      normalize_art_dir "${2:?--art-dir requires value}"
      shift 2
      ;;
    --skip-cleanup)
      DECK_VAGRANT_SKIP_CLEANUP="1"
      shift
      ;;
    --cleanup)
      DECK_VAGRANT_SKIP_CLEANUP="0"
      shift
      ;;
    --skip-collect)
      DECK_VAGRANT_SKIP_COLLECT="1"
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

if [[ -n "${STEP}" ]]; then
  FROM_STEP="${STEP}"
  TO_STEP="${STEP}"
fi

CHECKPOINT_DIR="${ART_DIR_ABS}/checkpoints"
STATE_ENV_PATH="${CHECKPOINT_DIR}/state.env"

if [[ -f "${STATE_ENV_PATH}" ]]; then
  sync_type_live="${DECK_VAGRANT_SYNC_TYPE}"
  # shellcheck disable=SC1090
  source "${STATE_ENV_PATH}"
  DECK_VAGRANT_SYNC_TYPE="${sync_type_live}"
fi

prepare_local_run_state() {
  if [[ ${FRESH} -eq 1 ]]; then
    rm -rf "${ART_DIR_ABS}"
    return 0
  fi
  if [[ ${RESUME} -eq 1 && -z "${STEP}" && -z "${FROM_STEP}" && -z "${TO_STEP}" && -f "${CHECKPOINT_DIR}/cleanup.done" ]]; then
    local step_name=""
    for step_name in start-server enqueue-install wait-install enqueue-join assert-cluster collect cleanup; do
      rm -f "${CHECKPOINT_DIR}/${step_name}.done"
    done
    FROM_STEP="start-server"
  fi
}

resolve_step_range() {
  local from_idx=0
  local to_idx=$((${#STEPS[@]} - 1))
  if [[ -n "${FROM_STEP}" ]]; then
    from_idx="$(step_index "${FROM_STEP}")" || { echo "[deck] unknown from-step: ${FROM_STEP}"; exit 1; }
  fi
  if [[ -n "${TO_STEP}" ]]; then
    to_idx="$(step_index "${TO_STEP}")" || { echo "[deck] unknown to-step: ${TO_STEP}"; exit 1; }
  fi
  if (( from_idx > to_idx )); then
    echo "[deck] from-step must be before to-step"
    exit 1
  fi
  STEP_FROM_INDEX="${from_idx}"
  STEP_TO_INDEX="${to_idx}"
}

prepare_local_run_state
resolve_step_range

ensure_libvirt_environment() {
  if [[ "${LIBVIRT_ENV_INITIALIZED}" == "1" ]]; then
    return 0
  fi
  source "${LIBVIRT_ENV_HELPER}"
  prepare_libvirt_environment
  LIBVIRT_ENV_INITIALIZED=1
}

compute_prepared_bundle_cache_key() {
  local host_bin="$1"
  local workflow_root="${ROOT_DIR}/test/vagrant/workflows/offline-multinode"
  local helper_root="${ROOT_DIR}/test/vagrant"
  local backend_runtime="$2"
  local arch="$3"
  python3 - <<'PY' "${ROOT_DIR}" "${host_bin}" "${workflow_root}" "${helper_root}" "${backend_runtime}" "${arch}"
import hashlib
from pathlib import Path
import sys

root_dir = Path(sys.argv[1])
host_bin = Path(sys.argv[2])
workflow_root = Path(sys.argv[3])
helper_root = Path(sys.argv[4])
backend_runtime = sys.argv[5]
arch = sys.argv[6]

paths = [host_bin]
paths.extend(sorted(p for p in workflow_root.rglob('*') if p.is_file()))
paths.extend(sorted(helper_root.glob('*.sh')))

digest = hashlib.sha256()
digest.update(f'backendRuntime={backend_runtime}\n'.encode())
digest.update(f'arch={arch}\n'.encode())
digest.update(b'kubernetesVersion=v1.30.1\n')
seen = set()
for path in paths:
    if path in seen:
        continue
    seen.add(path)
    digest.update(path.relative_to(root_dir).as_posix().encode())
    digest.update(b'\0')
    digest.update(path.read_bytes())
    digest.update(b'\0')
print(digest.hexdigest())
PY
}

prepare_shared_bundle_cache() {
  local host_bin="$1"
  local backend_runtime="$2"
  local arch="$3"
  local cache_key=""

  cache_key="$(compute_prepared_bundle_cache_key "${host_bin}" "${backend_runtime}" "${arch}")"
  if [[ -f "${PREPARED_BUNDLE_STAMP}" ]] && [[ -f "${PREPARED_BUNDLE_ABS}/.deck/manifest.json" ]] && [[ "$(cat "${PREPARED_BUNDLE_STAMP}" 2>/dev/null || true)" == "${cache_key}" ]]; then
    echo "[deck] reusing shared prepared bundle cache"
    return 0
  fi

  echo "[deck] rebuilding shared prepared bundle cache"
  rm -rf "${PREPARED_BUNDLE_WORK_ABS}" "${PREPARED_BUNDLE_STAGE_ABS}"
  mkdir -p "${PREPARED_BUNDLE_WORKFLOW_DIR}" "${PREPARED_BUNDLE_FRAGMENT_DIR}"
  cp -a "${ROOT_DIR}/test/vagrant/workflows/offline-multinode/." "${PREPARED_BUNDLE_FRAGMENT_DIR}/"
  cp "${ROOT_DIR}/test/vagrant/workflows/offline-multinode/profile/prepare.yaml" "${PREPARED_BUNDLE_WORKFLOW_DIR}/pack.yaml"
  cat > "${PREPARED_BUNDLE_WORKFLOW_DIR}/apply.yaml" <<'EOF'
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
  printf '{}\n' > "${PREPARED_BUNDLE_WORKFLOW_DIR}/vars.yaml"
  (cd "${PREPARED_BUNDLE_PACK_ROOT}" && "${host_bin}" pack --out "${PREPARED_BUNDLE_TAR}" \
    --var "kubernetesVersion=v1.30.1" \
    --var "arch=${arch}" \
    --var "backendRuntime=${backend_runtime}")

  mkdir -p "${PREPARED_BUNDLE_STAGE_ABS}"
  tar -xf "${PREPARED_BUNDLE_TAR}" -C "${PREPARED_BUNDLE_STAGE_ABS}" --strip-components=1
  printf '%s\n' "${cache_key}" > "${PREPARED_BUNDLE_STAGE_ABS}/.deck-cache-key"

  rm -rf "${PREPARED_BUNDLE_ABS}"
  mkdir -p "$(dirname "${PREPARED_BUNDLE_ABS}")"
  mv "${PREPARED_BUNDLE_STAGE_ABS}" "${PREPARED_BUNDLE_ABS}"
}

prepare_rsync_stage_root() {
  local deck_bin_source="${ROOT_DIR}/test/artifacts/bin/deck-linux-${HOST_ARCH}"
  rm -rf "${RSYNC_STAGE_STAGE_ABS}" "${RSYNC_STAGE_ABS}"
  mkdir -p "${RSYNC_STAGE_STAGE_ABS}/test/vagrant" "${RSYNC_STAGE_STAGE_ABS}/test/artifacts/bin"
  cp "${VM_SCENARIO_SCRIPT}" "${RSYNC_STAGE_STAGE_ABS}/test/vagrant/run-offline-multinode-vm.sh"
  cp -a "${ROOT_DIR}/test/vagrant/workflows" "${RSYNC_STAGE_STAGE_ABS}/test/vagrant/"
  cp "${deck_bin_source}" "${RSYNC_STAGE_STAGE_ABS}/test/artifacts/bin/deck-linux-${HOST_ARCH}"
  if [[ -f "${ROOT_DIR}/test/artifacts/bin/deck-linux-amd64" && "${HOST_ARCH}" != "amd64" ]]; then
    cp "${ROOT_DIR}/test/artifacts/bin/deck-linux-amd64" "${RSYNC_STAGE_STAGE_ABS}/test/artifacts/bin/deck-linux-amd64"
  fi
  if [[ -f "${ROOT_DIR}/test/artifacts/bin/deck-linux-arm64" && "${HOST_ARCH}" != "arm64" ]]; then
    cp "${ROOT_DIR}/test/artifacts/bin/deck-linux-arm64" "${RSYNC_STAGE_STAGE_ABS}/test/artifacts/bin/deck-linux-arm64"
  fi
  if [[ -d "${PREPARED_BUNDLE_ABS}" ]]; then
    mkdir -p "${RSYNC_STAGE_STAGE_ABS}/$(dirname "${PREPARED_BUNDLE_REL}")"
    cp -a "${PREPARED_BUNDLE_ABS}" "${RSYNC_STAGE_STAGE_ABS}/${PREPARED_BUNDLE_REL}"
  fi
  mv "${RSYNC_STAGE_STAGE_ABS}" "${RSYNC_STAGE_ABS}"
}

ensure_rsync_sync_source() {
  resolve_host_build_context
  if [[ ! -x "${HOST_BIN}" ]] || [[ ! -f "${ROOT_DIR}/test/artifacts/bin/deck-linux-${HOST_ARCH}" ]]; then
    "${BUILD_BINARIES_HELPER}" "${ROOT_DIR}"
  fi
  prepare_shared_bundle_cache "${HOST_BIN}" "${HOST_BACKEND_RUNTIME}" "${HOST_ARCH}"
  prepare_rsync_stage_root
}

fetch_vm_artifacts() {
  local node="$1"
  local bundle_tgz="${ART_DIR_ABS}/vm-artifacts-${node}.tgz"

  DECK_VAGRANT_BOX="${DECK_VAGRANT_BOX}" DECK_VAGRANT_PROVIDER="${DECK_VAGRANT_PROVIDER}" DECK_VAGRANT_VM_PREFIX="${DECK_VAGRANT_VM_PREFIX}" \
    vagrant ssh "${node}" -c "if [[ -d /workspace/${ART_DIR_REL} ]]; then tar -czf - -C /workspace ${ART_DIR_REL}; fi" > "${bundle_tgz}" 2>/dev/null || true

  if [[ -s "${bundle_tgz}" ]]; then
    tar -xzf "${bundle_tgz}" -C "${ROOT_DIR}" || true
  fi
}

should_fetch_vm_artifacts() {
  if [[ "${DECK_VAGRANT_SKIP_COLLECT}" == "1" ]]; then
    return 1
  fi
  if [[ -f "${ART_DIR_ABS}/offline-multinode-pass.txt" && -f "${ART_DIR_ABS}/cluster-nodes.txt" && "${DECK_VAGRANT_SYNC_TYPE}" != "rsync" ]]; then
    echo "[deck] artifacts already visible on host via shared workspace; skipping VM fetch"
    return 1
  fi
  return 0
}

fetch_vm_artifacts_parallel() {
  local -a nodes=(control-plane worker worker-2)
  local -a pids=()
  local node=""
  local pid=""
  local rc=0
  for node in "${nodes[@]}"; do
    fetch_vm_artifacts "${node}" &
    pids+=("$!")
  done
  for pid in "${pids[@]}"; do
    if ! wait "${pid}"; then
      rc=1
    fi
  done
  return ${rc}
}

fetch_vm_artifacts_serial() {
  fetch_vm_artifacts "control-plane"
  fetch_vm_artifacts "worker"
  fetch_vm_artifacts "worker-2"
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
  local result=1

  pushd "${VAGRANT_DIR}" >/dev/null
  for ((i=1; i<=attempts; i++)); do
    set +e
    DECK_VAGRANT_BOX="${DECK_VAGRANT_BOX}" DECK_VAGRANT_PROVIDER="${DECK_VAGRANT_PROVIDER}" DECK_VAGRANT_VM_PREFIX="${DECK_VAGRANT_VM_PREFIX}" \
      vagrant ssh "${node}" -c "${cmd}"
    rc=$?
    set -e
    if [[ ${rc} -eq 0 ]]; then
      result=0
      break
    fi
    sleep "${delay_sec}"
  done
  popd >/dev/null
  return ${result}
}

load_state_env() {
  if [[ -f "${STATE_ENV_PATH}" ]]; then
    local sync_type_live="${DECK_VAGRANT_SYNC_TYPE}"
    # shellcheck disable=SC1090
    source "${STATE_ENV_PATH}"
    DECK_VAGRANT_SYNC_TYPE="${sync_type_live}"
  fi
}

save_state_env() {
  mkdir -p "${CHECKPOINT_DIR}"
  cat > "${STATE_ENV_PATH}" <<EOF
DECK_VAGRANT_VM_PREFIX=${DECK_VAGRANT_VM_PREFIX}
SERVER_IP=${SERVER_IP:-}
SERVER_URL=${SERVER_URL:-}
PREPARED_BUNDLE_REL=${PREPARED_BUNDLE_REL:-}
EOF
}

mark_done() {
  local step_name="$1"
  mkdir -p "${CHECKPOINT_DIR}"
  date -u +"%Y-%m-%dT%H:%M:%SZ" > "${CHECKPOINT_DIR}/${step_name}.done"
}

cleanup() {
  set +e
  if [[ "${IN_VAGRANT_DIR}" == "1" ]]; then
    popd >/dev/null || true
    IN_VAGRANT_DIR=0
  fi
}

trap cleanup EXIT INT TERM

export VAGRANT_DEFAULT_PROVIDER="libvirt"
export DECK_VAGRANT_MANAGEMENT_NETWORK_NAME="${DECK_VAGRANT_MANAGEMENT_NETWORK_NAME:-default}"
export DECK_VAGRANT_MANAGEMENT_NETWORK_ADDRESS="${DECK_VAGRANT_MANAGEMENT_NETWORK_ADDRESS:-192.168.122.0/24}"
export DECK_VAGRANT_IP_ADDRESS_TIMEOUT="${DECK_VAGRANT_IP_ADDRESS_TIMEOUT:-300}"
export DECK_VAGRANT_LIBVIRT_IP_COMMAND="${DECK_VAGRANT_LIBVIRT_IP_COMMAND:-}"
export DECK_VAGRANT_QEMU_USE_AGENT="${DECK_VAGRANT_QEMU_USE_AGENT:-0}"
export DECK_VAGRANT_MGMT_ATTACH="${DECK_VAGRANT_MGMT_ATTACH:-1}"
export DECK_VAGRANT_SYNC_TYPE
export DECK_VAGRANT_BOX_CONTROL_PLANE
export DECK_VAGRANT_BOX_WORKER
export DECK_VAGRANT_BOX_WORKER_2

validate_box_provider() {
  local box
  for box in "${DECK_VAGRANT_BOX_CONTROL_PLANE}" "${DECK_VAGRANT_BOX_WORKER}" "${DECK_VAGRANT_BOX_WORKER_2}"; do
    if [[ "${box}" == "manrala/ubuntu24" ]]; then
      echo "[deck] box/provider mismatch: ${box} does not support libvirt"
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
    echo "${status_out}"
    exit 1
  fi
}

step_prepare_host() {
  mkdir -p "${ART_DIR_ABS}"
  ensure_libvirt_environment
  "${BUILD_BINARIES_HELPER}" "${ROOT_DIR}"
  resolve_host_build_context
  prepare_shared_bundle_cache "${HOST_BIN}" "${HOST_BACKEND_RUNTIME}" "${HOST_ARCH}"
  if [[ "${DECK_VAGRANT_SYNC_TYPE}" == "rsync" ]]; then
    prepare_rsync_stage_root
  fi
  load_state_env
  save_state_env
}

step_up_vms() {
  local up_rc=0
  local sync_source_env="${DECK_VAGRANT_SYNC_SOURCE:-${ROOT_DIR}}"
  ensure_libvirt_environment
  if [[ "${DECK_VAGRANT_SYNC_TYPE}" == "rsync" ]]; then
    ensure_rsync_sync_source
    sync_source_env="${RSYNC_STAGE_ABS}"
  fi
  pushd "${VAGRANT_DIR}" >/dev/null
  IN_VAGRANT_DIR=1
  if [[ "${DECK_VAGRANT_SKIP_CLEANUP}" != "1" ]]; then
    DECK_VAGRANT_BOX="${DECK_VAGRANT_BOX}" DECK_VAGRANT_PROVIDER="${DECK_VAGRANT_PROVIDER}" DECK_VAGRANT_VM_PREFIX="${DECK_VAGRANT_VM_PREFIX}" vagrant destroy -f || true
    delete_stale_volume "control-plane"
    delete_stale_volume "worker"
    delete_stale_volume "worker-2"
  fi
  set +e
  DECK_VAGRANT_BOX="${DECK_VAGRANT_BOX}" DECK_VAGRANT_PROVIDER="${DECK_VAGRANT_PROVIDER}" DECK_VAGRANT_VM_PREFIX="${DECK_VAGRANT_VM_PREFIX}" DECK_VAGRANT_SYNC_TYPE="${DECK_VAGRANT_SYNC_TYPE}" DECK_VAGRANT_SYNC_SOURCE="${sync_source_env}" vagrant up control-plane worker worker-2 --provider "${DECK_VAGRANT_PROVIDER}"
  up_rc=$?
  set -e
  if [[ ${up_rc} -ne 0 && "${DECK_VAGRANT_SYNC_TYPE}" == "9p" ]]; then
    echo "[deck] 9p shared folders are unavailable on this host; retrying with rsync"
    DECK_VAGRANT_SYNC_TYPE="rsync"
    export DECK_VAGRANT_SYNC_TYPE
    ensure_rsync_sync_source
    DECK_VAGRANT_BOX="${DECK_VAGRANT_BOX}" DECK_VAGRANT_PROVIDER="${DECK_VAGRANT_PROVIDER}" DECK_VAGRANT_VM_PREFIX="${DECK_VAGRANT_VM_PREFIX}" DECK_VAGRANT_SYNC_TYPE="${DECK_VAGRANT_SYNC_TYPE}" vagrant destroy -f >/dev/null 2>&1 || true
    delete_stale_volume "control-plane"
    delete_stale_volume "worker"
    delete_stale_volume "worker-2"
    DECK_VAGRANT_BOX="${DECK_VAGRANT_BOX}" DECK_VAGRANT_PROVIDER="${DECK_VAGRANT_PROVIDER}" DECK_VAGRANT_VM_PREFIX="${DECK_VAGRANT_VM_PREFIX}" DECK_VAGRANT_SYNC_TYPE="${DECK_VAGRANT_SYNC_TYPE}" DECK_VAGRANT_SYNC_SOURCE="${RSYNC_STAGE_ABS}" vagrant up control-plane worker worker-2 --provider "${DECK_VAGRANT_PROVIDER}"
  elif [[ ${up_rc} -ne 0 ]]; then
    exit ${up_rc}
  fi
  SERVER_IP="$(DECK_VAGRANT_BOX="${DECK_VAGRANT_BOX}" DECK_VAGRANT_PROVIDER="${DECK_VAGRANT_PROVIDER}" DECK_VAGRANT_VM_PREFIX="${DECK_VAGRANT_VM_PREFIX}" vagrant ssh-config control-plane 2>/dev/null | awk '/^[[:space:]]*HostName[[:space:]]+/ {print $2; exit}')"
  if [[ -z "${SERVER_IP}" ]]; then
    echo "[deck] failed to resolve control-plane IPv4 address"
    exit 1
  fi
  SERVER_URL="http://${SERVER_IP}:18080"
  cat > "${ART_DIR_ABS}/vm-ips.txt" <<EOF
control-plane=${SERVER_IP}
EOF
  DECK_VAGRANT_BOX="${DECK_VAGRANT_BOX}" DECK_VAGRANT_PROVIDER="${DECK_VAGRANT_PROVIDER}" DECK_VAGRANT_VM_PREFIX="${DECK_VAGRANT_VM_PREFIX}" vagrant status > "${ART_DIR_ABS}/vagrant-status.txt"
  save_state_env
  popd >/dev/null
  IN_VAGRANT_DIR=0
}

control_plane_action() {
  local action="$1"
  load_state_env
  run_vagrant_ssh "control-plane" "ART_DIR_REL=${ART_DIR_REL} SERVER_URL=${SERVER_URL} DECK_KUBEADM_ADVERTISE_ADDRESS=${SERVER_IP} DECK_OFFLINE_RELEASE_CONTROL_PLANE=ubuntu2204 DECK_OFFLINE_RELEASE_WORKER=ubuntu2404 DECK_OFFLINE_RELEASE_WORKER_2=rocky9 DECK_PREPARED_BUNDLE_REL=${PREPARED_BUNDLE_REL:-} bash /workspace/test/vagrant/run-offline-multinode-vm.sh control-plane ${action}"
}

step_start_server() { control_plane_action "start-server"; }
step_enqueue_install() { control_plane_action "apply-control-plane"; }
step_wait_install() { control_plane_action "verify-install"; }
step_enqueue_join() {
  load_state_env
  run_vagrant_ssh "worker" "ART_DIR_REL=${ART_DIR_REL} SERVER_URL=${SERVER_URL} DECK_OFFLINE_RELEASE=ubuntu2404 bash /workspace/test/vagrant/run-offline-multinode-vm.sh worker apply-worker"
  run_vagrant_ssh "worker-2" "ART_DIR_REL=${ART_DIR_REL} SERVER_URL=${SERVER_URL} DECK_OFFLINE_RELEASE=rocky9 bash /workspace/test/vagrant/run-offline-multinode-vm.sh worker-2 apply-worker"
}
step_assert_cluster() { control_plane_action "assert-cluster"; }

step_collect() {
  if should_fetch_vm_artifacts; then
    pushd "${VAGRANT_DIR}" >/dev/null
    IN_VAGRANT_DIR=1
    if [[ "${DECK_VAGRANT_COLLECT_PARALLEL}" -gt 1 ]]; then
      fetch_vm_artifacts_parallel || true
    fi
    if [[ ! -f "${ART_DIR_ABS}/offline-multinode-pass.txt" || ! -f "${ART_DIR_ABS}/cluster-nodes.txt" ]]; then
      fetch_vm_artifacts_serial
    fi
    popd >/dev/null
    IN_VAGRANT_DIR=0
  else
    echo "[deck] collect fetch skipped"
  fi

  if [[ ! -f "${ART_DIR_ABS}/offline-multinode-pass.txt" ]]; then
    echo "[deck] PASS marker missing: ${ART_DIR_ABS}/offline-multinode-pass.txt"
    exit 1
  fi
  if [[ ! -f "${ART_DIR_ABS}/cluster-nodes.txt" ]]; then
    echo "[deck] missing nodes report: ${ART_DIR_ABS}/cluster-nodes.txt"
    exit 1
  fi
}

step_cleanup() {
  pushd "${VAGRANT_DIR}" >/dev/null
  IN_VAGRANT_DIR=1
  if [[ "${DECK_VAGRANT_SKIP_CLEANUP}" != "1" ]]; then
    DECK_VAGRANT_BOX="${DECK_VAGRANT_BOX}" DECK_VAGRANT_PROVIDER="${DECK_VAGRANT_PROVIDER}" DECK_VAGRANT_VM_PREFIX="${DECK_VAGRANT_VM_PREFIX}" vagrant destroy -f || true
  else
    echo "[deck] skip cleanup enabled (DECK_VAGRANT_SKIP_CLEANUP=1): keeping VMs"
  fi
  popd >/dev/null
  IN_VAGRANT_DIR=0
}

run_step() {
  local step_name="$1"
  local idx="$(step_index "${step_name}")"
  local done_marker="${CHECKPOINT_DIR}/${step_name}.done"
  local err_file="${ART_DIR_ABS}/error-${step_name}.log"
  local step_log="${ART_DIR_ABS}/step-${step_name}.log"
  if (( idx < STEP_FROM_INDEX || idx > STEP_TO_INDEX )); then
    return 0
  fi
  if [[ ${RESUME} -eq 1 && -f "${done_marker}" ]]; then
    echo "[deck] step=${step_name} skip(resume)"
    return 0
  fi
  echo "[deck] step=${step_name} start"
  rm -f "${step_log}"
  rm -f "${err_file}"
  if ! "step_${step_name//-/_}" > >(tee "${step_log}") 2> >(tee "${err_file}" >&2); then
    echo "[deck] step failed: ${step_name}"
    echo "last_completed=$(ls "${CHECKPOINT_DIR}"/*.done 2>/dev/null | sed 's#.*/##; s#.done$##' | tr '\n' ',' | sed 's/,$//')" >> "${ART_DIR_ABS}/run-summary.txt"
    exit 1
  fi
  mark_done "${step_name}"
  echo "[deck] step=${step_name} done"
}

for p in "${VAGRANT_DIR}/Vagrantfile" "${VM_SCENARIO_SCRIPT}" "${LIBVIRT_ENV_HELPER}" "${BUILD_BINARIES_HELPER}"; do
  if [[ ! -e "${p}" ]]; then
    echo "[deck] missing required path: ${p}"
    exit 1
  fi
done

validate_box_provider
ensure_libvirt_environment
check_provider_available
mkdir -p "${ART_DIR_ABS}" "${CHECKPOINT_DIR}"

run_step prepare-host
run_step up-vms
run_step start-server
run_step enqueue-install
run_step wait-install
run_step enqueue-join
run_step assert-cluster
run_step collect
run_step cleanup

trap - EXIT INT TERM
echo "[deck] offline-multinode artifacts: ${ART_DIR_ABS}"
