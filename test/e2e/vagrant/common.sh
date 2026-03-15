#!/usr/bin/env bash
set -euo pipefail

COMMON_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="${ROOT_DIR:-$(cd "${COMMON_DIR}/../../.." && pwd)}"
VAGRANT_DIR="${ROOT_DIR}/test/vagrant"
LIBVIRT_ENV_HELPER="${ROOT_DIR}/test/vagrant/libvirt-env.sh"
BUILD_BINARIES_HELPER="${ROOT_DIR}/test/vagrant/build-deck-binaries.sh"

TS="$(date +%Y%m%d-%H%M%S)"
SCENARIO_ID="${DECK_VAGRANT_SCENARIO:-k8s-worker-join}"
RUN_ID="${DECK_VAGRANT_RUN_ID:-local}"
CACHE_KEY="${DECK_VAGRANT_CACHE_KEY:-compat}"
RUN_STARTED_AT="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
SCENARIO_ID_SANITIZED="${SCENARIO_ID//\//-}"
RUN_ID_SANITIZED="${RUN_ID//\//-}"
ART_DIR_REL=""
ART_DIR_ABS=""
CHECKPOINT_DIR=""
RUN_LOG_DIR=""
RUN_REPORT_DIR=""
RUN_RENDERED_WORKFLOWS_DIR=""
RUN_BUNDLE_SOURCE_FILE=""
CACHE_BUNDLES_ROOT_REL=""
PREPARED_BUNDLE_REL=""
PREPARED_BUNDLE_ABS=""
PREPARED_BUNDLE_STAMP=""
PREPARED_BUNDLE_WORK_REL=""
PREPARED_BUNDLE_WORK_ABS=""
PREPARED_BUNDLE_PACK_ROOT=""
PREPARED_BUNDLE_WORKFLOW_DIR=""
PREPARED_BUNDLE_FRAGMENT_DIR=""
PREPARED_BUNDLE_TAR=""
PREPARED_BUNDLE_STAGE_ABS=""
RSYNC_STAGE_REL=""
RSYNC_STAGE_ABS=""
RSYNC_STAGE_STAGE_ABS=""
RSYNC_STAGE_STAMP=""

DECK_VAGRANT_PROVIDER="libvirt"
DECK_VAGRANT_SYNC_TYPE="${DECK_VAGRANT_SYNC_TYPE:-rsync}"
DECK_VAGRANT_BOX_CONTROL_PLANE="${DECK_VAGRANT_BOX_CONTROL_PLANE:-${DECK_VAGRANT_BOX:-generic/ubuntu2204}}"
DECK_VAGRANT_BOX_WORKER="${DECK_VAGRANT_BOX_WORKER:-bento/ubuntu-24.04}"
DECK_VAGRANT_BOX_WORKER_2="${DECK_VAGRANT_BOX_WORKER_2:-generic/rocky9}"
DECK_VAGRANT_BOX="${DECK_VAGRANT_BOX:-${DECK_VAGRANT_BOX_CONTROL_PLANE}}"
DECK_VAGRANT_VM_PREFIX_FROM_ENV=0
if [[ -n "${DECK_VAGRANT_VM_PREFIX:-}" ]]; then
  DECK_VAGRANT_VM_PREFIX_FROM_ENV=1
fi
DECK_VAGRANT_VM_PREFIX="${DECK_VAGRANT_VM_PREFIX:-deck-${SCENARIO_ID_SANITIZED}-${RUN_ID_SANITIZED}}"
DECK_VAGRANT_SKIP_CLEANUP="${DECK_VAGRANT_SKIP_CLEANUP:-1}"
DECK_VAGRANT_SKIP_COLLECT="${DECK_VAGRANT_SKIP_COLLECT:-0}"
DECK_VAGRANT_COLLECT_PARALLEL="${DECK_VAGRANT_COLLECT_PARALLEL:-3}"
DECK_VAGRANT_HELPER_ROOT_REL="${DECK_VAGRANT_HELPER_ROOT_REL:-test/vagrant}"

STEP=""
FROM_STEP=""
TO_STEP=""
RESUME="${DECK_VAGRANT_RESUME:-1}"
IN_VAGRANT_DIR=0
LIBVIRT_ENV_INITIALIZED=0
FRESH=0
FRESH_CACHE=0

HOST_BIN=""
HOST_BACKEND_RUNTIME=""
HOST_ARCH=""
SERVER_IP=""
SERVER_URL=""
STATE_ENV_PATH=""
STEP_FROM_INDEX=0
STEP_TO_INDEX=0
SCENARIO_METADATA_LOADED=0
SCENARIO_METADATA_NODES=""
SCENARIO_METADATA_USES_WORKERS=""

scenario_basename() {
  local scenario_id="${1:-}"
  case "${scenario_id}" in
    k8s-*)
      printf '%s\n' "${scenario_id#k8s-}"
      ;;
    *)
      printf '%s\n' "${scenario_id}"
      ;;
  esac
}

load_scenario_metadata() {
  local metadata_path="${ROOT_DIR}/test/e2e/scenario-meta/${SCENARIO_ID}.env"
  local normalized_metadata_path="${ROOT_DIR}/test/e2e/scenario-meta/$(scenario_basename "${SCENARIO_ID}").env"
  SCENARIO_METADATA_LOADED=0
  SCENARIO_METADATA_NODES=""
  SCENARIO_METADATA_USES_WORKERS=""
  if [[ -f "${metadata_path}" ]]; then
    source "${metadata_path}"
  elif [[ "${normalized_metadata_path}" != "${metadata_path}" && -f "${normalized_metadata_path}" ]]; then
    source "${normalized_metadata_path}"
  fi
  if [[ -n "${NODES:-}" || -n "${USES_WORKERS:-}" ]]; then
    SCENARIO_METADATA_NODES="${NODES:-}"
    SCENARIO_METADATA_USES_WORKERS="${USES_WORKERS:-}"
    if [[ -n "${SCENARIO_METADATA_NODES}" && -n "${SCENARIO_METADATA_USES_WORKERS}" ]]; then
      SCENARIO_METADATA_LOADED=1
      return 0
    fi
  fi
  return 1
}

ensure_scenario_metadata_loaded() {
  if [[ "${SCENARIO_METADATA_LOADED}" == "1" ]]; then
    return 0
  fi
  load_scenario_metadata
}

scenario_nodes() {
  if ! ensure_scenario_metadata_loaded; then
    return 1
  fi
  local node
  for node in ${SCENARIO_METADATA_NODES}; do
    printf '%s\n' "${node}"
  done
}

scenario_requires_workers() {
  if ! ensure_scenario_metadata_loaded; then
    return 1
  fi
  [[ "${SCENARIO_METADATA_USES_WORKERS}" == "1" ]]
}

active_nodes() {
  scenario_nodes
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

deck_vagrant_usage() {
  local entrypoint="${DECK_VAGRANT_ENTRYPOINT:-test/e2e/vagrant/run-scenario.sh}"
  cat <<EOF
Usage: ${entrypoint} [options]

Options:
  --scenario <name>    Override the scenario cache/artifact namespace.
  --step <name>       Run only one step.
  --from-step <name>  Start from step.
  --to-step <name>    End at step.
  --resume            Skip completed checkpoints and continue.
  --fresh             Recreate VMs and rerun from a clean local state.
  --fresh-cache       Remove run artifacts and scenario cache, then rerun.
  --art-dir <path>    Reuse artifact directory (absolute or workspace-relative).
  --skip-cleanup      Keep VMs after scenario for debugging.
  --cleanup           Destroy VMs at the end of the run.
  --skip-collect      Do not fetch artifacts back from VMs.

Steps:
  $(IFS=,; echo "${STEPS[*]}")
EOF
}

refresh_layout_contracts() {
  SCENARIO_ID_SANITIZED="${SCENARIO_ID//\//-}"
  RUN_ID_SANITIZED="${RUN_ID//\//-}"
  ART_DIR_REL="${DECK_VAGRANT_ART_DIR:-test/artifacts/runs/${SCENARIO_ID}/${RUN_ID}}"
  ART_DIR_ABS="${ROOT_DIR}/${ART_DIR_REL}"
  CACHE_BUNDLES_ROOT_REL="test/artifacts/cache/bundles/${SCENARIO_ID}/${CACHE_KEY}"
  PREPARED_BUNDLE_REL="${CACHE_BUNDLES_ROOT_REL}"
  PREPARED_BUNDLE_ABS="${ROOT_DIR}/${PREPARED_BUNDLE_REL}"
  PREPARED_BUNDLE_STAMP="${PREPARED_BUNDLE_ABS}/.deck-cache-key"
  PREPARED_BUNDLE_WORK_REL="test/artifacts/cache/staging/${SCENARIO_ID}/${CACHE_KEY}"
  PREPARED_BUNDLE_WORK_ABS="${ROOT_DIR}/${PREPARED_BUNDLE_WORK_REL}"
  PREPARED_BUNDLE_PACK_ROOT="${PREPARED_BUNDLE_WORK_ABS}/host-pack"
  PREPARED_BUNDLE_WORKFLOW_DIR="${PREPARED_BUNDLE_PACK_ROOT}/workflows"
  PREPARED_BUNDLE_FRAGMENT_DIR="${PREPARED_BUNDLE_WORKFLOW_DIR}/scenarios"
  PREPARED_BUNDLE_TAR="${PREPARED_BUNDLE_WORK_ABS}/prepared-bundle.tar"
  PREPARED_BUNDLE_STAGE_ABS="${PREPARED_BUNDLE_WORK_ABS}/prepared-bundle.stage"
  RSYNC_STAGE_REL="test/artifacts/cache/vagrant/${SCENARIO_ID}/rsync-root"
  RSYNC_STAGE_ABS="${ROOT_DIR}/${RSYNC_STAGE_REL}"
  RSYNC_STAGE_STAGE_ABS="${ROOT_DIR}/test/artifacts/cache/vagrant/${SCENARIO_ID}/rsync-root.stage"
  RSYNC_STAGE_STAMP="${RSYNC_STAGE_ABS}/.deck-rsync-key"
  if [[ "${DECK_VAGRANT_VM_PREFIX_FROM_ENV}" != "1" ]]; then
    DECK_VAGRANT_VM_PREFIX="deck-${SCENARIO_ID_SANITIZED}-${RUN_ID_SANITIZED}"
  fi
  load_scenario_metadata || true
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
  export DECK_VAGRANT_ART_DIR="${ART_DIR_REL}"
}

parse_args() {
  refresh_layout_contracts
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --scenario)
        SCENARIO_ID="${2:?--scenario requires value}"
        export DECK_VAGRANT_SCENARIO="${SCENARIO_ID}"
        refresh_layout_contracts
        shift 2
        ;;
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
      --fresh-cache)
        FRESH=1
        FRESH_CACHE=1
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
        deck_vagrant_usage
        exit 0
        ;;
      *)
        echo "[deck] unknown argument: $1"
        deck_vagrant_usage
        exit 1
        ;;
    esac
  done

  if [[ -n "${STEP}" ]]; then
    FROM_STEP="${STEP}"
    TO_STEP="${STEP}"
  fi

  CHECKPOINT_DIR="${ART_DIR_ABS}/checkpoints"
  RUN_LOG_DIR="${ART_DIR_ABS}/logs"
  RUN_REPORT_DIR="${ART_DIR_ABS}/reports"
  RUN_RENDERED_WORKFLOWS_DIR="${ART_DIR_ABS}/rendered-workflows"
  RUN_BUNDLE_SOURCE_FILE="${ART_DIR_ABS}/bundle-source.txt"
  STATE_ENV_PATH="${CHECKPOINT_DIR}/state.env"

  if [[ ${FRESH} -eq 0 && -f "${STATE_ENV_PATH}" ]]; then
    sync_type_live="${DECK_VAGRANT_SYNC_TYPE}"
    source "${STATE_ENV_PATH}"
    DECK_VAGRANT_SYNC_TYPE="${sync_type_live}"
  fi
}

prepare_local_run_state() {
  if [[ ${FRESH} -eq 1 ]]; then
    rm -rf "${ART_DIR_ABS}"
    SERVER_IP=""
    SERVER_URL=""
    if [[ ${FRESH_CACHE} -eq 1 ]]; then
      rm -rf "${ROOT_DIR}/test/artifacts/cache/bundles/${SCENARIO_ID}"
      rm -rf "${ROOT_DIR}/test/artifacts/cache/staging/${SCENARIO_ID}"
      rm -rf "${ROOT_DIR}/test/artifacts/cache/vagrant/${SCENARIO_ID}"
    fi
    return 0
  fi
  if [[ ${RESUME} -eq 1 && -z "${STEP}" && -z "${FROM_STEP}" && -z "${TO_STEP}" && -f "${CHECKPOINT_DIR}/cleanup.done" ]]; then
    local step_name=""
    for step_name in prepare-bundle apply-scenario verify-scenario collect cleanup; do
      rm -f "${CHECKPOINT_DIR}/${step_name}.done"
    done
    FROM_STEP="prepare-bundle"
  fi
}

initialize_run_contract() {
  mkdir -p "${ART_DIR_ABS}" "${CHECKPOINT_DIR}" "${RUN_LOG_DIR}" "${RUN_REPORT_DIR}" "${RUN_RENDERED_WORKFLOWS_DIR}"
  if [[ ! -f "${RUN_BUNDLE_SOURCE_FILE}" ]]; then
    printf '%s\n' "pending" > "${RUN_BUNDLE_SOURCE_FILE}"
  fi
}

resolve_step_range() {
  local from_idx=0
  local to_idx
  to_idx=$((${#STEPS[@]} - 1))
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

ensure_libvirt_environment() {
  if [[ "${LIBVIRT_ENV_INITIALIZED}" == "1" ]]; then
    return 0
  fi
  source "${LIBVIRT_ENV_HELPER}"
  prepare_libvirt_environment
  LIBVIRT_ENV_INITIALIZED=1
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

compute_prepared_bundle_cache_key() {
  local host_bin="$1"
  local workflow_root="$2"
  local helper_root="$3"
  local backend_runtime="$4"
  local arch="$5"
  local include_legacy_workflows="$6"
  local vm_scenario_script="${7:-}"
  local vm_dispatcher_script="${8:-}"
  python3 - <<'PY' "${ROOT_DIR}" "${host_bin}" "${workflow_root}" "${helper_root}" "${backend_runtime}" "${arch}" "${include_legacy_workflows}" "${vm_scenario_script}" "${vm_dispatcher_script}"
import hashlib
from pathlib import Path
import sys

root_dir = Path(sys.argv[1])
host_bin = Path(sys.argv[2])
workflow_root = Path(sys.argv[3])
helper_root = Path(sys.argv[4])
backend_runtime = sys.argv[5]
arch = sys.argv[6]
include_legacy_workflows = sys.argv[7] == "1"
vm_scenario_script = Path(sys.argv[8]) if sys.argv[8] else None
vm_dispatcher_script = Path(sys.argv[9]) if sys.argv[9] else None

paths = [host_bin]
paths.extend(sorted(p for p in workflow_root.rglob('*') if p.is_file()))
for candidate in sorted(p for p in helper_root.rglob('*') if p.is_file()):
    if not include_legacy_workflows and candidate.is_relative_to(root_dir / 'test/vagrant/workflows'):
        continue
    paths.append(candidate)

for extra_root in (root_dir / 'test/workflows',):
    if extra_root.exists():
        paths.extend(sorted(p for p in extra_root.rglob('*') if p.is_file()))

for candidate in (
    root_dir / 'test/e2e/vagrant/common.sh',
    root_dir / 'test/e2e/vagrant/run-scenario.sh',
    root_dir / 'test/e2e/vagrant/run-scenario-vm.sh',
    root_dir / 'test/e2e/vagrant/run-scenario-vm-scenario.sh',
    vm_scenario_script,
    vm_dispatcher_script,
):
    if candidate and candidate.is_file():
        paths.append(candidate)

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
  local workflow_root_abs="${ROOT_DIR}/${DECK_VAGRANT_WORKFLOW_ROOT_REL}"
  local helper_root_abs="${ROOT_DIR}/${DECK_VAGRANT_HELPER_ROOT_REL}"
  local cache_key=""

  cache_key="$(compute_prepared_bundle_cache_key "${host_bin}" "${workflow_root_abs}" "${helper_root_abs}" "${backend_runtime}" "${arch}" "0" "${DECK_VAGRANT_VM_SCENARIO_SCRIPT:-}" "${DECK_VAGRANT_VM_DISPATCHER_SCRIPT:-}")"
  CACHE_KEY="${cache_key}"
  refresh_layout_contracts
  if [[ -f "${PREPARED_BUNDLE_STAMP}" ]] && [[ -f "${PREPARED_BUNDLE_ABS}/.deck/manifest.json" ]] && [[ "$(cat "${PREPARED_BUNDLE_STAMP}" 2>/dev/null || true)" == "${cache_key}" ]]; then
    echo "[deck] reusing shared prepared bundle cache"
    printf '%s\n' "cache-hit:${PREPARED_BUNDLE_REL}" > "${RUN_BUNDLE_SOURCE_FILE}"
    return 0
  fi

  echo "[deck] rebuilding shared prepared bundle cache"
  rm -rf "${PREPARED_BUNDLE_WORK_ABS}" "${PREPARED_BUNDLE_STAGE_ABS}"
  mkdir -p "${PREPARED_BUNDLE_WORKFLOW_DIR}" "${PREPARED_BUNDLE_FRAGMENT_DIR}"
  deck_vagrant_prepare_workflow_bundle
  (cd "${PREPARED_BUNDLE_PACK_ROOT}" && "${host_bin}" prepare --out "${PREPARED_BUNDLE_TAR}" \
    --var "kubernetesVersion=v1.30.1" \
    --var "arch=${arch}" \
    --var "backendRuntime=${backend_runtime}")

  mkdir -p "${PREPARED_BUNDLE_STAGE_ABS}"
  tar -xf "${PREPARED_BUNDLE_TAR}" -C "${PREPARED_BUNDLE_STAGE_ABS}" --strip-components=1
  printf '%s\n' "${cache_key}" > "${PREPARED_BUNDLE_STAGE_ABS}/.deck-cache-key"

  rm -rf "${PREPARED_BUNDLE_ABS}"
  mkdir -p "$(dirname "${PREPARED_BUNDLE_ABS}")"
  mv "${PREPARED_BUNDLE_STAGE_ABS}" "${PREPARED_BUNDLE_ABS}"
  printf '%s\n' "cache-rebuild:${PREPARED_BUNDLE_REL}" > "${RUN_BUNDLE_SOURCE_FILE}"
}

prepare_rsync_stage_root() {
  local deck_bin_source="${ROOT_DIR}/test/artifacts/bin/deck-linux-${HOST_ARCH}"
  local vm_stage_path="${DECK_VAGRANT_VM_STAGED_PATH:-test/e2e/vagrant/run-scenario-vm.sh}"
  local dispatcher_source="${ROOT_DIR}/test/e2e/vagrant/run-scenario-vm.sh"
  local dispatcher_stage_path="${DECK_VAGRANT_VM_DISPATCHER_STAGED_PATH:-test/e2e/vagrant/run-scenario-vm.sh}"
  local dispatcher_scenario_helper_source="${ROOT_DIR}/test/e2e/vagrant/run-scenario-vm-scenario.sh"
  local dispatcher_scenario_helper_stage_path="test/e2e/vagrant/run-scenario-vm-scenario.sh"
  local rsync_key=""

  rsync_key="$(python3 - <<'PY' "${ROOT_DIR}" "${deck_bin_source}" "${DECK_VAGRANT_VM_SCENARIO_SCRIPT}" "${dispatcher_source}" "${dispatcher_scenario_helper_source}" "${PREPARED_BUNDLE_ABS}"
import hashlib
from pathlib import Path
import sys

root = Path(sys.argv[1])
deck_bin = Path(sys.argv[2])
scenario_script = Path(sys.argv[3])
dispatcher_script = Path(sys.argv[4]) if sys.argv[4] else None
dispatcher_scenario_helper = Path(sys.argv[5]) if sys.argv[5] else None
prepared_bundle = Path(sys.argv[6])

paths = [scenario_script, deck_bin]
if dispatcher_script:
    paths.append(dispatcher_script)
if dispatcher_scenario_helper and dispatcher_scenario_helper.is_file():
    paths.append(dispatcher_scenario_helper)
for base in (root / "test/workflows", root / "test/e2e/scenario-meta", root / "test/e2e/scenario-hooks"):
    if base.exists():
        paths.extend(sorted(p for p in base.rglob("*") if p.is_file()))

bundle_stamp = prepared_bundle / ".deck-cache-key"
if bundle_stamp.is_file():
    paths.append(bundle_stamp)

digest = hashlib.sha256()
for path in paths:
    digest.update(path.relative_to(root).as_posix().encode())
    digest.update(b"\0")
    digest.update(path.read_bytes())
    digest.update(b"\0")
print(digest.hexdigest())
PY
)"

  if [[ -f "${RSYNC_STAGE_STAMP}" ]] && [[ "$(cat "${RSYNC_STAGE_STAMP}" 2>/dev/null || true)" == "${rsync_key}" ]]; then
    echo "[deck] reusing rsync stage cache"
    return 0
  fi

  rm -rf "${RSYNC_STAGE_STAGE_ABS}" "${RSYNC_STAGE_ABS}"
  mkdir -p "${RSYNC_STAGE_STAGE_ABS}/$(dirname "${vm_stage_path}")" "${RSYNC_STAGE_STAGE_ABS}/test/artifacts/bin"
  cp "${DECK_VAGRANT_VM_SCENARIO_SCRIPT}" "${RSYNC_STAGE_STAGE_ABS}/${vm_stage_path}"
  if [[ -n "${dispatcher_source}" ]] && [[ -n "${dispatcher_stage_path}" ]]; then
    mkdir -p "${RSYNC_STAGE_STAGE_ABS}/$(dirname "${dispatcher_stage_path}")"
    cp "${dispatcher_source}" "${RSYNC_STAGE_STAGE_ABS}/${dispatcher_stage_path}"
    if ! cmp -s "${dispatcher_source}" "${RSYNC_STAGE_STAGE_ABS}/${dispatcher_stage_path}"; then
      echo "[deck] staged dispatcher mismatch: ${dispatcher_source} != ${RSYNC_STAGE_STAGE_ABS}/${dispatcher_stage_path}"
      exit 1
    fi
  fi
  if [[ -f "${dispatcher_scenario_helper_source}" ]]; then
    mkdir -p "${RSYNC_STAGE_STAGE_ABS}/$(dirname "${dispatcher_scenario_helper_stage_path}")"
    cp "${dispatcher_scenario_helper_source}" "${RSYNC_STAGE_STAGE_ABS}/${dispatcher_scenario_helper_stage_path}"
  fi
  if [[ "${include_legacy_workflows}" == "1" ]]; then
    cp -a "${ROOT_DIR}/test/vagrant/workflows" "${RSYNC_STAGE_STAGE_ABS}/test/vagrant/"
  fi
  cp -a "${ROOT_DIR}/test/workflows" "${RSYNC_STAGE_STAGE_ABS}/test/"
  mkdir -p "${RSYNC_STAGE_STAGE_ABS}/test/e2e"
  cp -a "${ROOT_DIR}/test/e2e/scenario-meta" "${RSYNC_STAGE_STAGE_ABS}/test/e2e/"
  cp -a "${ROOT_DIR}/test/e2e/scenario-hooks" "${RSYNC_STAGE_STAGE_ABS}/test/e2e/"
  cp "${deck_bin_source}" "${RSYNC_STAGE_STAGE_ABS}/test/artifacts/bin/deck-linux-${HOST_ARCH}"
  if [[ -f "${ROOT_DIR}/test/artifacts/bin/deck-linux-amd64" && "${HOST_ARCH}" != "amd64" ]]; then
    cp "${ROOT_DIR}/test/artifacts/bin/deck-linux-amd64" "${RSYNC_STAGE_STAGE_ABS}/test/artifacts/bin/deck-linux-amd64"
  fi
  if [[ -f "${ROOT_DIR}/test/artifacts/bin/deck-linux-arm64" && "${HOST_ARCH}" != "arm64" ]]; then
    cp "${ROOT_DIR}/test/artifacts/bin/deck-linux-arm64" "${RSYNC_STAGE_STAGE_ABS}/test/artifacts/bin/deck-linux-arm64"
  fi
  if [[ -d "${PREPARED_BUNDLE_ABS}" ]]; then
    mkdir -p "${RSYNC_STAGE_STAGE_ABS}/${PREPARED_BUNDLE_REL}"
    cp -a "${PREPARED_BUNDLE_ABS}/." "${RSYNC_STAGE_STAGE_ABS}/${PREPARED_BUNDLE_REL}"
  fi
  printf '%s\n' "${rsync_key}" > "${RSYNC_STAGE_STAGE_ABS}/.deck-rsync-key"
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
  if [[ -f "${ART_DIR_ABS}/pass.txt" && -f "${ART_DIR_ABS}/reports/cluster-nodes.txt" && "${DECK_VAGRANT_SYNC_TYPE}" != "rsync" ]]; then
    echo "[deck] artifacts already visible on host via shared workspace; skipping VM fetch"
    return 1
  fi
  return 0
}

fetch_vm_artifacts_parallel() {
  local -a nodes=()
  local -a pids=()
  local node=""
  local pid=""
  local rc=0
  mapfile -t nodes < <(active_nodes)
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
  local -a nodes=()
  local node=""
  mapfile -t nodes < <(active_nodes)
  for node in "${nodes[@]}"; do
    fetch_vm_artifacts "${node}"
  done
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
SCENARIO_ID=${SCENARIO_ID:-k8s-worker-join}
RUN_ID=${RUN_ID:-local}
CACHE_KEY=${CACHE_KEY:-compat}
RUN_STARTED_AT=${RUN_STARTED_AT:-}
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
  prepare_rsync_stage_root
  load_state_env
  save_state_env
}

step_up_vms() {
  local up_rc=0
  local sync_source_env="${DECK_VAGRANT_SYNC_SOURCE:-${ROOT_DIR}}"
  local -a nodes=()
  local node=""
  mapfile -t nodes < <(active_nodes)
  ensure_libvirt_environment
  if [[ "${DECK_VAGRANT_SYNC_TYPE}" == "rsync" ]]; then
    ensure_rsync_sync_source
    sync_source_env="${RSYNC_STAGE_ABS}"
  fi
  pushd "${VAGRANT_DIR}" >/dev/null
  IN_VAGRANT_DIR=1
  if [[ "${DECK_VAGRANT_SKIP_CLEANUP}" != "1" ]]; then
    DECK_VAGRANT_BOX="${DECK_VAGRANT_BOX}" DECK_VAGRANT_PROVIDER="${DECK_VAGRANT_PROVIDER}" DECK_VAGRANT_VM_PREFIX="${DECK_VAGRANT_VM_PREFIX}" vagrant destroy -f || true
    for node in "${nodes[@]}"; do
      delete_stale_volume "${node}"
    done
  fi
  set +e
  DECK_VAGRANT_BOX="${DECK_VAGRANT_BOX}" DECK_VAGRANT_PROVIDER="${DECK_VAGRANT_PROVIDER}" DECK_VAGRANT_VM_PREFIX="${DECK_VAGRANT_VM_PREFIX}" DECK_VAGRANT_SYNC_TYPE="${DECK_VAGRANT_SYNC_TYPE}" DECK_VAGRANT_SYNC_SOURCE="${sync_source_env}" vagrant up "${nodes[@]}" --provider "${DECK_VAGRANT_PROVIDER}"
  up_rc=$?
  set -e
  if [[ ${up_rc} -ne 0 && "${DECK_VAGRANT_SYNC_TYPE}" == "9p" ]]; then
    echo "[deck] 9p shared folders are unavailable on this host; retrying with rsync"
    DECK_VAGRANT_SYNC_TYPE="rsync"
    export DECK_VAGRANT_SYNC_TYPE
    ensure_rsync_sync_source
    DECK_VAGRANT_BOX="${DECK_VAGRANT_BOX}" DECK_VAGRANT_PROVIDER="${DECK_VAGRANT_PROVIDER}" DECK_VAGRANT_VM_PREFIX="${DECK_VAGRANT_VM_PREFIX}" DECK_VAGRANT_SYNC_TYPE="${DECK_VAGRANT_SYNC_TYPE}" vagrant destroy -f >/dev/null 2>&1 || true
    for node in "${nodes[@]}"; do
      delete_stale_volume "${node}"
    done
    DECK_VAGRANT_BOX="${DECK_VAGRANT_BOX}" DECK_VAGRANT_PROVIDER="${DECK_VAGRANT_PROVIDER}" DECK_VAGRANT_VM_PREFIX="${DECK_VAGRANT_VM_PREFIX}" DECK_VAGRANT_SYNC_TYPE="${DECK_VAGRANT_SYNC_TYPE}" DECK_VAGRANT_SYNC_SOURCE="${RSYNC_STAGE_ABS}" vagrant up "${nodes[@]}" --provider "${DECK_VAGRANT_PROVIDER}"
  elif [[ ${up_rc} -ne 0 ]]; then
    exit ${up_rc}
  fi
  local ip_try=""
  local ip_attempt=0
  for ((ip_attempt=1; ip_attempt<=30; ip_attempt++)); do
    ip_try="$(DECK_VAGRANT_BOX="${DECK_VAGRANT_BOX}" DECK_VAGRANT_PROVIDER="${DECK_VAGRANT_PROVIDER}" DECK_VAGRANT_VM_PREFIX="${DECK_VAGRANT_VM_PREFIX}" vagrant ssh-config control-plane 2>/dev/null | awk '/^[[:space:]]*HostName[[:space:]]+/ {print $2; exit}')"
    if [[ -n "${ip_try}" ]]; then
      break
    fi
    sleep 2
  done
  if [[ -z "${ip_try}" ]]; then
    ip_try="$(virsh -c "${DECK_LIBVIRT_URI}" domifaddr "${DECK_VAGRANT_VM_PREFIX}control-plane" --source lease 2>/dev/null | awk '/ipv4/ {print $4; exit}' | cut -d/ -f1)"
  fi
  SERVER_IP="${ip_try}"
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

step_collect() {
  if should_fetch_vm_artifacts; then
    pushd "${VAGRANT_DIR}" >/dev/null
    IN_VAGRANT_DIR=1
    if [[ "${DECK_VAGRANT_COLLECT_PARALLEL}" -gt 1 ]]; then
      fetch_vm_artifacts_parallel || true
    fi
  if [[ ! -f "${ART_DIR_ABS}/pass.txt" || ! -f "${ART_DIR_ABS}/reports/cluster-nodes.txt" ]]; then
    fetch_vm_artifacts_serial
  fi
    popd >/dev/null
    IN_VAGRANT_DIR=0
  else
    echo "[deck] collect fetch skipped"
  fi

  if [[ ! -f "${ART_DIR_ABS}/pass.txt" ]]; then
    echo "[deck] PASS marker missing: ${ART_DIR_ABS}/pass.txt"
    exit 1
  fi
  if [[ ! -f "${ART_DIR_ABS}/reports/cluster-nodes.txt" ]]; then
    echo "[deck] missing nodes report: ${ART_DIR_ABS}/reports/cluster-nodes.txt"
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
  local idx
  idx="$(step_index "${step_name}")"
  local done_marker="${CHECKPOINT_DIR}/${step_name}.done"
  local err_file="${RUN_LOG_DIR}/error-${step_name}.log"
  local step_log="${RUN_LOG_DIR}/step-${step_name}.log"
  local err_file_legacy="${ART_DIR_ABS}/error-${step_name}.log"
  local step_log_legacy="${ART_DIR_ABS}/step-${step_name}.log"
  if (( idx < STEP_FROM_INDEX || idx > STEP_TO_INDEX )); then
    return 0
  fi
  if [[ ${RESUME} -eq 1 && -f "${done_marker}" ]]; then
    echo "[deck] step=${step_name} skip(resume)"
    return 0
  fi
  echo "[deck] step=${step_name} start"
  rm -f "${step_log}" "${err_file}" "${step_log_legacy}" "${err_file_legacy}"
  if ! "step_${step_name//-/_}" > >(tee "${step_log}") 2> >(tee "${err_file}" >&2); then
    echo "[deck] step failed: ${step_name}"
    cp "${step_log}" "${step_log_legacy}" 2>/dev/null || true
    cp "${err_file}" "${err_file_legacy}" 2>/dev/null || true
    echo "last_completed=$(ls "${CHECKPOINT_DIR}"/*.done 2>/dev/null | sed 's#.*/##; s#.done$##' | tr '\n' ',' | sed 's/,$//')" >> "${RUN_REPORT_DIR}/run-summary.txt"
    cp "${RUN_REPORT_DIR}/run-summary.txt" "${ART_DIR_ABS}/run-summary.txt" 2>/dev/null || true
    exit 1
  fi
  cp "${step_log}" "${step_log_legacy}" 2>/dev/null || true
  cp "${err_file}" "${err_file_legacy}" 2>/dev/null || true
  mark_done "${step_name}"
  echo "[deck] step=${step_name} done"
}

deck_vagrant_main() {
  parse_args "$@"
  prepare_local_run_state
  resolve_step_range

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

  for p in "${VAGRANT_DIR}/Vagrantfile" "${DECK_VAGRANT_VM_SCENARIO_SCRIPT}" "${LIBVIRT_ENV_HELPER}" "${BUILD_BINARIES_HELPER}"; do
    if [[ ! -e "${p}" ]]; then
      echo "[deck] missing required path: ${p}"
      exit 1
    fi
  done
  if [[ -n "${DECK_VAGRANT_VM_DISPATCHER_SCRIPT:-}" ]] && [[ ! -e "${DECK_VAGRANT_VM_DISPATCHER_SCRIPT}" ]]; then
    echo "[deck] missing required path: ${DECK_VAGRANT_VM_DISPATCHER_SCRIPT}"
    exit 1
  fi

  validate_box_provider
  ensure_libvirt_environment
  check_provider_available
  initialize_run_contract

  local step_name
  for step_name in "${STEPS[@]}"; do
    run_step "${step_name}"
  done

  trap - EXIT INT TERM
  echo "[deck] ${SCENARIO_ID} artifacts: ${ART_DIR_ABS}"
}
