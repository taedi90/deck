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
CHECKPOINT_DIR=""

DECK_VAGRANT_PROVIDER="libvirt"
DECK_VAGRANT_BOX_CONTROL_PLANE="${DECK_VAGRANT_BOX_CONTROL_PLANE:-${DECK_VAGRANT_BOX:-generic/ubuntu2204}}"
DECK_VAGRANT_BOX_WORKER="${DECK_VAGRANT_BOX_WORKER:-bento/ubuntu-24.04}"
DECK_VAGRANT_BOX_WORKER_2="${DECK_VAGRANT_BOX_WORKER_2:-generic/rocky9}"
DECK_VAGRANT_BOX="${DECK_VAGRANT_BOX:-${DECK_VAGRANT_BOX_CONTROL_PLANE}}"
DECK_VAGRANT_VM_PREFIX="${DECK_VAGRANT_VM_PREFIX:-deck-offline-multinode-agent-${TS}-$$}"
DECK_VAGRANT_SKIP_CLEANUP="${DECK_VAGRANT_SKIP_CLEANUP:-0}"

STEP=""
FROM_STEP=""
TO_STEP=""
RESUME=0
IN_VAGRANT_DIR=0

STEPS=(
  prepare-host
  up-vms
  start-agents
  start-server
  enqueue-install
  wait-install
  enqueue-join
  wait-join
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
  --art-dir <path>    Reuse artifact directory (absolute or workspace-relative).
  --skip-cleanup      Keep VMs after scenario for debugging.

Steps:
  prepare-host, up-vms, start-agents, start-server, enqueue-install,
  wait-install, enqueue-join, wait-join, assert-cluster, collect, cleanup
EOF
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
    --art-dir)
      normalize_art_dir "${2:?--art-dir requires value}"
      shift 2
      ;;
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

if [[ -n "${STEP}" ]]; then
  FROM_STEP="${STEP}"
  TO_STEP="${STEP}"
fi

if [[ ${RESUME} -eq 1 && -z "${STEP}" && -z "${FROM_STEP}" && -z "${TO_STEP}" && ! -d "${ART_DIR_ABS}" ]]; then
  latest_rel="$(ls -dt "${ROOT_DIR}"/.ci/artifacts/offline-multinode-agent-* 2>/dev/null | head -n1 | sed "s#^${ROOT_DIR}/##" || true)"
  if [[ -n "${latest_rel}" ]]; then
    ART_DIR_REL="${latest_rel}"
    ART_DIR_ABS="${ROOT_DIR}/${ART_DIR_REL}"
  fi
fi

CHECKPOINT_DIR="${ART_DIR_ABS}/checkpoints"
STATE_ENV_PATH="${CHECKPOINT_DIR}/state.env"

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

resolve_step_range

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
  if [[ -f "${ART_DIR_ABS}/prepare-cache.env" ]]; then
    # shellcheck disable=SC1090
    source "${ART_DIR_ABS}/prepare-cache.env"
  fi
  if [[ -f "${STATE_ENV_PATH}" ]]; then
    # shellcheck disable=SC1090
    source "${STATE_ENV_PATH}"
  fi
}

save_state_env() {
  mkdir -p "${CHECKPOINT_DIR}"
  cat > "${STATE_ENV_PATH}" <<EOF
DECK_VAGRANT_VM_PREFIX=${DECK_VAGRANT_VM_PREFIX}
SERVER_IP=${SERVER_IP:-}
SERVER_URL=${SERVER_URL:-}
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
  source "${LIBVIRT_POOL_HELPER}"
  prepare_libvirt_environment
  "${ENSURE_BINARIES_HELPER}" "${ROOT_DIR}"
  DECK_HOST_BIN="${ROOT_DIR}/.ci/artifacts/deck-host" \
  DECK_VAGRANT_PROVIDER="${DECK_VAGRANT_PROVIDER}" \
  DECK_VAGRANT_BOX="${DECK_VAGRANT_BOX_CONTROL_PLANE},${DECK_VAGRANT_BOX_WORKER},${DECK_VAGRANT_BOX_WORKER_2}" \
  DECK_PREPARE_TEMPLATE_PATH="${ROOT_DIR}/test/vagrant/scenario-templates/offline-multinode-prepare.yaml" \
    "${PREPARE_CACHE_HELPER}" "${ROOT_DIR}" "${ART_DIR_ABS}" "offline-multinode-agent"
  load_state_env
}

step_up_vms() {
  pushd "${VAGRANT_DIR}" >/dev/null
  IN_VAGRANT_DIR=1
  if [[ "${DECK_VAGRANT_SKIP_CLEANUP}" != "1" ]]; then
    DECK_VAGRANT_BOX="${DECK_VAGRANT_BOX}" DECK_VAGRANT_PROVIDER="${DECK_VAGRANT_PROVIDER}" DECK_VAGRANT_VM_PREFIX="${DECK_VAGRANT_VM_PREFIX}" vagrant destroy -f || true
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
  run_vagrant_ssh "control-plane" "ART_DIR_REL=${ART_DIR_REL} SERVER_URL=${SERVER_URL} DECK_KUBEADM_ADVERTISE_ADDRESS=${SERVER_IP} DECK_OFFLINE_RELEASE_CONTROL_PLANE=ubuntu2204 DECK_OFFLINE_RELEASE_WORKER=ubuntu2404 DECK_OFFLINE_RELEASE_WORKER_2=rocky9 DECK_PREPARED_BUNDLE_REL=${DECK_PREPARED_BUNDLE_REL:-} DECK_PREPARE_CACHE_STATUS=${DECK_PREPARE_CACHE_STATUS:-none} bash /workspace/test/vagrant/scripts/run-offline-multinode-agent-vm.sh control-plane ${action}"
}

step_start_agents() {
  load_state_env
  run_vagrant_ssh "worker" "ART_DIR_REL=${ART_DIR_REL} SERVER_URL=${SERVER_URL} DECK_OFFLINE_RELEASE=ubuntu2404 bash /workspace/test/vagrant/scripts/run-offline-multinode-agent-vm.sh worker start-agent"
  run_vagrant_ssh "worker-2" "ART_DIR_REL=${ART_DIR_REL} SERVER_URL=${SERVER_URL} DECK_OFFLINE_RELEASE=rocky9 bash /workspace/test/vagrant/scripts/run-offline-multinode-agent-vm.sh worker-2 start-agent"
  control_plane_action "start-agent"
}

step_start_server() { control_plane_action "start-server"; }
step_enqueue_install() { control_plane_action "apply-control-plane"; }
step_wait_install() { control_plane_action "verify-install"; }
step_enqueue_join() {
  load_state_env
  run_vagrant_ssh "worker" "ART_DIR_REL=${ART_DIR_REL} SERVER_URL=${SERVER_URL} DECK_OFFLINE_RELEASE=ubuntu2404 bash /workspace/test/vagrant/scripts/run-offline-multinode-agent-vm.sh worker apply-worker"
  run_vagrant_ssh "worker-2" "ART_DIR_REL=${ART_DIR_REL} SERVER_URL=${SERVER_URL} DECK_OFFLINE_RELEASE=rocky9 bash /workspace/test/vagrant/scripts/run-offline-multinode-agent-vm.sh worker-2 apply-worker"
}
step_wait_join() {
  load_state_env
  run_vagrant_ssh "worker" "ART_DIR_REL=${ART_DIR_REL} SERVER_URL=${SERVER_URL} DECK_OFFLINE_RELEASE=ubuntu2404 bash /workspace/test/vagrant/scripts/run-offline-multinode-agent-vm.sh worker verify-worker"
  run_vagrant_ssh "worker-2" "ART_DIR_REL=${ART_DIR_REL} SERVER_URL=${SERVER_URL} DECK_OFFLINE_RELEASE=rocky9 bash /workspace/test/vagrant/scripts/run-offline-multinode-agent-vm.sh worker-2 verify-worker"
}
step_assert_cluster() { control_plane_action "assert-cluster"; }

step_collect() {
  pushd "${VAGRANT_DIR}" >/dev/null
  IN_VAGRANT_DIR=1
  fetch_vm_artifacts "control-plane"
  fetch_vm_artifacts "worker"
  fetch_vm_artifacts "worker-2"
  popd >/dev/null
  IN_VAGRANT_DIR=0

  if [[ ! -f "${ART_DIR_ABS}/offline-multinode-agent-pass.txt" ]]; then
    echo "[deck] PASS marker missing: ${ART_DIR_ABS}/offline-multinode-agent-pass.txt"
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
  if (( idx < STEP_FROM_INDEX || idx > STEP_TO_INDEX )); then
    return 0
  fi
  if [[ ${RESUME} -eq 1 && -f "${done_marker}" ]]; then
    echo "[deck] step=${step_name} skip(resume)"
    return 0
  fi
  echo "[deck] step=${step_name} start"
  rm -f "${err_file}"
  if ! "step_${step_name//-/_}" > >(tee -a "${ART_DIR_ABS}/step-${step_name}.log") 2> >(tee -a "${err_file}" >&2); then
    echo "[deck] step failed: ${step_name}"
    echo "last_completed=$(ls "${CHECKPOINT_DIR}"/*.done 2>/dev/null | sed 's#.*/##; s#.done$##' | tr '\n' ',' | sed 's/,$//')" >> "${ART_DIR_ABS}/run-summary.txt"
    exit 1
  fi
  mark_done "${step_name}"
  echo "[deck] step=${step_name} done"
}

for p in "${VAGRANT_DIR}/Vagrantfile" "${VM_SCENARIO_SCRIPT}" "${LIBVIRT_POOL_HELPER}" "${ENSURE_BINARIES_HELPER}" "${PREPARE_CACHE_HELPER}"; do
  if [[ ! -e "${p}" ]]; then
    echo "[deck] missing required path: ${p}"
    exit 1
  fi
done

validate_box_provider
check_provider_available
mkdir -p "${ART_DIR_ABS}" "${CHECKPOINT_DIR}"

run_step prepare-host
run_step up-vms
run_step start-agents
run_step start-server
run_step enqueue-install
run_step wait-install
run_step enqueue-join
run_step wait-join
run_step assert-cluster
run_step collect
run_step cleanup

trap - EXIT INT TERM
echo "[deck] offline multi-node agent artifacts: ${ART_DIR_ABS}"
