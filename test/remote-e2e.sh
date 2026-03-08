#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MODE="${MODE:-offline-multinode}"
REF="${REF:-}"
ENV_FILE="${DECK_REMOTE_ENV_FILE:-}"
SYNC_MODE="${DECK_REMOTE_SYNC_MODE:-auto}"
EFFECTIVE_SYNC_MODE="${SYNC_MODE}"
DECK_VAGRANT_SKIP_CLEANUP_PRESET=0
DECK_VAGRANT_SKIP_CLEANUP_PRESET_VALUE="0"
OUT_DIR_REL="test/artifacts/remote-e2e-$(date +%Y%m%d-%H%M%S)"
OUT_DIR_ABS="${ROOT_DIR}/${OUT_DIR_REL}"

usage() {
  cat <<'EOF'
Run deck Vagrant E2E on remote host.

Usage:
  test/remote-e2e.sh --env-file <path> [--mode offline-multinode] [--ref <git-ref>] [--sync auto|git|upload] [--skip-cleanup]

Required env file keys:
  DECK_REMOTE_HOST
  DECK_REMOTE_USER

Optional env file keys:
  DECK_REMOTE_PORT=22
  DECK_REMOTE_WORKDIR=/backup/deck/workspace/deck-remote
  DECK_REMOTE_SSH_KEY_PATH=~/.ssh/id_ed25519
  DECK_REMOTE_KNOWN_HOSTS_PATH=~/.ssh/known_hosts
  DECK_REMOTE_SYNC_MODE=auto
  DECK_VAGRANT_SKIP_CLEANUP=0

EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --env-file)
      [[ $# -ge 2 ]] || { echo "--env-file requires value"; exit 1; }
      ENV_FILE="$2"
      shift 2
      ;;
    --mode)
      [[ $# -ge 2 ]] || { echo "--mode requires value"; exit 1; }
      MODE="$2"
      shift 2
      ;;
    --ref)
      [[ $# -ge 2 ]] || { echo "--ref requires value"; exit 1; }
      REF="$2"
      shift 2
      ;;
    --sync)
      [[ $# -ge 2 ]] || { echo "--sync requires value"; exit 1; }
      SYNC_MODE="$2"
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
      echo "unknown argument: $1"
      usage
      exit 1
      ;;
  esac
done

EFFECTIVE_SYNC_MODE="${SYNC_MODE}"

[[ -n "${ENV_FILE}" ]] || { echo "DECK_REMOTE_ENV_FILE or --env-file is required"; exit 1; }
[[ -f "${ENV_FILE}" ]] || { echo "env file not found: ${ENV_FILE}"; exit 1; }

# shellcheck disable=SC1090
if [[ "${DECK_VAGRANT_SKIP_CLEANUP+x}" == "x" ]]; then
  DECK_VAGRANT_SKIP_CLEANUP_PRESET=1
  DECK_VAGRANT_SKIP_CLEANUP_PRESET_VALUE="${DECK_VAGRANT_SKIP_CLEANUP}"
fi
source "${ENV_FILE}"
if [[ "${DECK_VAGRANT_SKIP_CLEANUP_PRESET}" == "1" ]]; then
  DECK_VAGRANT_SKIP_CLEANUP="${DECK_VAGRANT_SKIP_CLEANUP_PRESET_VALUE}"
fi

DECK_REMOTE_PORT="${DECK_REMOTE_PORT:-22}"
DECK_REMOTE_WORKDIR="${DECK_REMOTE_WORKDIR:-/backup/deck/workspace/deck-remote}"
DECK_REMOTE_SSH_KEY_PATH="${DECK_REMOTE_SSH_KEY_PATH:-${HOME}/.ssh/id_ed25519}"
DECK_REMOTE_KNOWN_HOSTS_PATH="${DECK_REMOTE_KNOWN_HOSTS_PATH:-${HOME}/.ssh/known_hosts}"
DECK_VAGRANT_SKIP_CLEANUP="${DECK_VAGRANT_SKIP_CLEANUP:-0}"

[[ -n "${DECK_REMOTE_HOST:-}" ]] || { echo "DECK_REMOTE_HOST is required"; exit 1; }
[[ -n "${DECK_REMOTE_USER:-}" ]] || { echo "DECK_REMOTE_USER is required"; exit 1; }
[[ -f "${DECK_REMOTE_SSH_KEY_PATH}" ]] || { echo "ssh key file not found: ${DECK_REMOTE_SSH_KEY_PATH}"; exit 1; }

if [[ -z "${REF}" ]]; then
  REF="$(git -C "${ROOT_DIR}" rev-parse --abbrev-ref HEAD 2>/dev/null || true)"
  [[ -n "${REF}" ]] || REF="main"
fi

case "${MODE}" in
  offline-multinode)
    REMOTE_CMD="DECK_PREPARE_FORCE_REDOWNLOAD=${DECK_PREPARE_FORCE_REDOWNLOAD:-0} DECK_VAGRANT_PROVIDER=libvirt test/vagrant/run-offline-multinode-agent.sh"
    REMOTE_GLOB="test/artifacts/offline-multinode-*"
    ;;
  *)
    echo "MODE must be one of: offline-multinode"
    exit 1
    ;;
esac

case "${SYNC_MODE}" in
  auto|git|upload)
    ;;
  *)
    echo "SYNC_MODE must be one of: auto, git, upload"
    exit 1
    ;;
esac

is_repo_clean() {
  if [[ -n "$(git -C "${ROOT_DIR}" status --porcelain 2>/dev/null || true)" ]]; then
    return 1
  fi
  return 0
}

archive_working_tree() {
  local out_file="$1"
  tar -czf "${out_file}" \
    --exclude='.git' \
    --exclude='test/artifacts' \
    --exclude='test/cache' \
    --exclude='.vagrant' \
    --exclude='tmp' \
    -C "${ROOT_DIR}" .
}

if [[ "${SYNC_MODE}" == "auto" ]] && ! is_repo_clean; then
  EFFECTIVE_SYNC_MODE="upload"
fi

mkdir -p "${OUT_DIR_ABS}"
REMOTE_ARCHIVE="/tmp/deck-remote-${MODE}-$$.tgz"
REMOTE_SOURCE_ARCHIVE="/tmp/deck-source-${MODE}-$$.tgz"
LOCAL_SOURCE_ARCHIVE=""
FORWARD_DECK_VAGRANT_BOX="${DECK_VAGRANT_BOX:-}"
FORWARD_DECK_VAGRANT_BOXES="${DECK_VAGRANT_BOXES:-}"
FORWARD_DECK_NIGHTLY_BOX_FILE="${DECK_NIGHTLY_BOX_FILE:-}"
FORWARD_DECK_VAGRANT_SYNC_TYPE="${DECK_VAGRANT_SYNC_TYPE:-}"
FORWARD_DECK_VAGRANT_MGMT_DEVICE="${DECK_VAGRANT_MGMT_DEVICE:-}"
FORWARD_DECK_VAGRANT_BOOT_TIMEOUT="${DECK_VAGRANT_BOOT_TIMEOUT:-}"
FORWARD_DECK_VAGRANT_LIBVIRT_DRIVER="${DECK_VAGRANT_LIBVIRT_DRIVER:-}"
FORWARD_DECK_VAGRANT_QEMU_USE_AGENT="${DECK_VAGRANT_QEMU_USE_AGENT:-}"
FORWARD_DECK_VAGRANT_QEMU_USE_SESSION="${DECK_VAGRANT_QEMU_USE_SESSION:-0}"
FORWARD_DECK_VAGRANT_MGMT_ATTACH="${DECK_VAGRANT_MGMT_ATTACH:-1}"
FORWARD_DECK_VAGRANT_ENABLE_PRIVATE_NETWORK="${DECK_VAGRANT_ENABLE_PRIVATE_NETWORK:-0}"
FORWARD_DECK_VAGRANT_PRIVATE_NETWORK_NAME="${DECK_VAGRANT_PRIVATE_NETWORK_NAME:-deck-vagrant}"
FORWARD_DECK_VAGRANT_MANAGEMENT_NETWORK_NAME="${DECK_VAGRANT_MANAGEMENT_NETWORK_NAME:-default}"
FORWARD_DECK_VAGRANT_MANAGEMENT_NETWORK_ADDRESS="${DECK_VAGRANT_MANAGEMENT_NETWORK_ADDRESS:-192.168.122.0/24}"
FORWARD_DECK_VAGRANT_IP_ADDRESS_TIMEOUT="${DECK_VAGRANT_IP_ADDRESS_TIMEOUT:-300}"
FORWARD_DECK_VAGRANT_LIBVIRT_IP_COMMAND="${DECK_VAGRANT_LIBVIRT_IP_COMMAND:-}"
FORWARD_DECK_BACKUP_ROOT="${DECK_BACKUP_ROOT:-/backup/deck}"
FORWARD_DECK_LIBVIRT_URI="${DECK_LIBVIRT_URI:-qemu:///system}"
FORWARD_DECK_LIBVIRT_USE_SESSION="${DECK_LIBVIRT_USE_SESSION:-}"
FORWARD_DECK_LIBVIRT_POOL_NAME="${DECK_LIBVIRT_POOL_NAME:-deck}"
FORWARD_DECK_LIBVIRT_POOL_PATH="${DECK_LIBVIRT_POOL_PATH:-${FORWARD_DECK_BACKUP_ROOT}/libvirt/pool}"
FORWARD_DECK_VAGRANT_HOME="${DECK_VAGRANT_HOME:-${FORWARD_DECK_BACKUP_ROOT}/vagrant/home}"
FORWARD_DECK_VAGRANT_DOTFILE_PATH="${DECK_VAGRANT_DOTFILE_PATH:-${FORWARD_DECK_BACKUP_ROOT}/vagrant/dotfiles}"
FORWARD_DECK_VM_SSH_BOX_FILE="${DECK_VM_SSH_BOX_FILE:-}"
FORWARD_DECK_VM_SSH_TEMPLATE_ROOT="${DECK_VM_SSH_TEMPLATE_ROOT:-${FORWARD_DECK_BACKUP_ROOT}/vagrant/templates}"
FORWARD_DECK_VM_SSH_VM_PREFIX_BASE="${DECK_VM_SSH_VM_PREFIX_BASE:-}"
FORWARD_DECK_VM_SSH_FORCE_FRESH="${DECK_VM_SSH_FORCE_FRESH:-}"
FORWARD_DECK_VAGRANT_SKIP_CLEANUP="${DECK_VAGRANT_SKIP_CLEANUP:-0}"

ssh_opts=(-i "${DECK_REMOTE_SSH_KEY_PATH}" -p "${DECK_REMOTE_PORT}" -o BatchMode=yes)
scp_opts=(-i "${DECK_REMOTE_SSH_KEY_PATH}" -P "${DECK_REMOTE_PORT}" -o BatchMode=yes)
if [[ -f "${DECK_REMOTE_KNOWN_HOSTS_PATH}" ]]; then
  ssh_opts+=( -o StrictHostKeyChecking=yes -o UserKnownHostsFile="${DECK_REMOTE_KNOWN_HOSTS_PATH}" )
  scp_opts+=( -o StrictHostKeyChecking=yes -o UserKnownHostsFile="${DECK_REMOTE_KNOWN_HOSTS_PATH}" )
else
  ssh_opts+=( -o StrictHostKeyChecking=accept-new )
  scp_opts+=( -o StrictHostKeyChecking=accept-new )
fi

cleanup() {
  if [[ -n "${LOCAL_SOURCE_ARCHIVE}" && -f "${LOCAL_SOURCE_ARCHIVE}" ]]; then
    rm -f "${LOCAL_SOURCE_ARCHIVE}"
  fi
}

trap cleanup EXIT INT TERM

if [[ "${EFFECTIVE_SYNC_MODE}" == "auto" || "${EFFECTIVE_SYNC_MODE}" == "upload" ]]; then
  LOCAL_SOURCE_ARCHIVE="$(mktemp "${TMPDIR:-/tmp}/deck-source.XXXXXX.tgz")"
  if [[ "${EFFECTIVE_SYNC_MODE}" == "upload" ]]; then
    archive_working_tree "${LOCAL_SOURCE_ARCHIVE}"
  else
    if git -C "${ROOT_DIR}" rev-parse --verify "${REF}^{commit}" >/dev/null 2>&1; then
      git -C "${ROOT_DIR}" archive --format=tar.gz --output "${LOCAL_SOURCE_ARCHIVE}" "${REF}"
    else
      archive_working_tree "${LOCAL_SOURCE_ARCHIVE}"
    fi
  fi
  scp "${scp_opts[@]}" "${LOCAL_SOURCE_ARCHIVE}" "${DECK_REMOTE_USER}@${DECK_REMOTE_HOST}:${REMOTE_SOURCE_ARCHIVE}"
fi

ssh "${ssh_opts[@]}" "${DECK_REMOTE_USER}@${DECK_REMOTE_HOST}" \
  "MODE='${MODE}' DECK_REMOTE_WORKDIR='${DECK_REMOTE_WORKDIR}' REF='${REF}' REMOTE_ARCHIVE='${REMOTE_ARCHIVE}' REMOTE_SOURCE_ARCHIVE='${REMOTE_SOURCE_ARCHIVE}' REMOTE_CMD='${REMOTE_CMD}' REMOTE_GLOB='${REMOTE_GLOB}' REPOSITORY='taedi90/deck' SYNC_MODE='${EFFECTIVE_SYNC_MODE}' DECK_VAGRANT_BOX='${FORWARD_DECK_VAGRANT_BOX}' DECK_VAGRANT_BOXES='${FORWARD_DECK_VAGRANT_BOXES}' DECK_NIGHTLY_BOX_FILE='${FORWARD_DECK_NIGHTLY_BOX_FILE}' DECK_VAGRANT_SYNC_TYPE='${FORWARD_DECK_VAGRANT_SYNC_TYPE}' DECK_VAGRANT_MGMT_DEVICE='${FORWARD_DECK_VAGRANT_MGMT_DEVICE}' DECK_VAGRANT_BOOT_TIMEOUT='${FORWARD_DECK_VAGRANT_BOOT_TIMEOUT}' DECK_VAGRANT_LIBVIRT_DRIVER='${FORWARD_DECK_VAGRANT_LIBVIRT_DRIVER}' DECK_VAGRANT_QEMU_USE_AGENT='${FORWARD_DECK_VAGRANT_QEMU_USE_AGENT}' DECK_VAGRANT_QEMU_USE_SESSION='${FORWARD_DECK_VAGRANT_QEMU_USE_SESSION}' DECK_VAGRANT_MGMT_ATTACH='${FORWARD_DECK_VAGRANT_MGMT_ATTACH}' DECK_VAGRANT_ENABLE_PRIVATE_NETWORK='${FORWARD_DECK_VAGRANT_ENABLE_PRIVATE_NETWORK}' DECK_VAGRANT_PRIVATE_NETWORK_NAME='${FORWARD_DECK_VAGRANT_PRIVATE_NETWORK_NAME}' DECK_VAGRANT_MANAGEMENT_NETWORK_NAME='${FORWARD_DECK_VAGRANT_MANAGEMENT_NETWORK_NAME}' DECK_VAGRANT_MANAGEMENT_NETWORK_ADDRESS='${FORWARD_DECK_VAGRANT_MANAGEMENT_NETWORK_ADDRESS}' DECK_VAGRANT_IP_ADDRESS_TIMEOUT='${FORWARD_DECK_VAGRANT_IP_ADDRESS_TIMEOUT}' DECK_VAGRANT_LIBVIRT_IP_COMMAND='${FORWARD_DECK_VAGRANT_LIBVIRT_IP_COMMAND}' DECK_BACKUP_ROOT='${FORWARD_DECK_BACKUP_ROOT}' DECK_LIBVIRT_URI='${FORWARD_DECK_LIBVIRT_URI}' DECK_LIBVIRT_USE_SESSION='${FORWARD_DECK_LIBVIRT_USE_SESSION}' DECK_LIBVIRT_POOL_NAME='${FORWARD_DECK_LIBVIRT_POOL_NAME}' DECK_LIBVIRT_POOL_PATH='${FORWARD_DECK_LIBVIRT_POOL_PATH}' DECK_VAGRANT_HOME='${FORWARD_DECK_VAGRANT_HOME}' DECK_VAGRANT_DOTFILE_PATH='${FORWARD_DECK_VAGRANT_DOTFILE_PATH}' DECK_VM_SSH_BOX_FILE='${FORWARD_DECK_VM_SSH_BOX_FILE}' DECK_VM_SSH_TEMPLATE_ROOT='${FORWARD_DECK_VM_SSH_TEMPLATE_ROOT}' DECK_VM_SSH_VM_PREFIX_BASE='${FORWARD_DECK_VM_SSH_VM_PREFIX_BASE}' DECK_VM_SSH_FORCE_FRESH='${FORWARD_DECK_VM_SSH_FORCE_FRESH}' DECK_VAGRANT_SKIP_CLEANUP='${FORWARD_DECK_VAGRANT_SKIP_CLEANUP}' bash -s" <<'REMOTE'
set -euo pipefail

sync_from_upload() {
  if [[ ! -f "${REMOTE_SOURCE_ARCHIVE}" ]]; then
    return 1
  fi
  if [[ -z "${DECK_REMOTE_WORKDIR}" || "${DECK_REMOTE_WORKDIR}" == "/" || "${DECK_REMOTE_WORKDIR}" == "." ]]; then
    echo "[deck] unsafe DECK_REMOTE_WORKDIR for upload sync: '${DECK_REMOTE_WORKDIR}'"
    return 1
  fi
  mkdir -p "${DECK_REMOTE_WORKDIR}"
  tar -xzf "${REMOTE_SOURCE_ARCHIVE}" -C "${DECK_REMOTE_WORKDIR}"
  return 0
}

sync_from_git() {
  if [[ ! -d "${DECK_REMOTE_WORKDIR}/.git" ]]; then
    if [[ -d "${DECK_REMOTE_WORKDIR}" ]] && [[ -n "$(ls -A "${DECK_REMOTE_WORKDIR}" 2>/dev/null || true)" ]]; then
      return 1
    fi
    git clone "git@github.com:${REPOSITORY}.git" "${DECK_REMOTE_WORKDIR}" || return 1
  fi
  cd "${DECK_REMOTE_WORKDIR}" || return 1
  git fetch --prune origin || return 1
  git fetch origin "${REF}" --depth 1 || return 1
  git checkout -f FETCH_HEAD || return 1
  return 0
}

if [[ "${SYNC_MODE}" == "git" ]]; then
  sync_from_git
elif [[ "${SYNC_MODE}" == "upload" ]]; then
  sync_from_upload
else
  if ! sync_from_git; then
    sync_from_upload
  fi
fi

cd "${DECK_REMOTE_WORKDIR}"
if [[ "${MODE}" == "offline-multinode" ]]; then
  bash test/vagrant/build-deck-binaries.sh "$(pwd)"
fi
bash -lc "${REMOTE_CMD}"
REMOTE

latest_dir="$(ssh "${ssh_opts[@]}" "${DECK_REMOTE_USER}@${DECK_REMOTE_HOST}" "set -euo pipefail; cd '${DECK_REMOTE_WORKDIR}'; ls -dt ${REMOTE_GLOB} | head -n1")"
[[ -n "${latest_dir}" ]] || { echo "[deck] failed to detect latest artifacts with glob: ${REMOTE_GLOB}"; exit 1; }
ssh "${ssh_opts[@]}" "${DECK_REMOTE_USER}@${DECK_REMOTE_HOST}" "set -euo pipefail; cd '${DECK_REMOTE_WORKDIR}'; tar -czf '${REMOTE_ARCHIVE}' '${latest_dir}'"

scp "${scp_opts[@]}" "${DECK_REMOTE_USER}@${DECK_REMOTE_HOST}:${REMOTE_ARCHIVE}" "${OUT_DIR_ABS}/result.tgz"
ssh "${ssh_opts[@]}" "${DECK_REMOTE_USER}@${DECK_REMOTE_HOST}" "rm -f '${REMOTE_ARCHIVE}' '${REMOTE_SOURCE_ARCHIVE}'" || true

echo "[deck] remote e2e mode=${MODE} ref=${REF} artifacts=${OUT_DIR_ABS}/result.tgz"
