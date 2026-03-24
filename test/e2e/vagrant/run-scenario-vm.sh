#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'EOF'
Usage: test/e2e/vagrant/run-scenario-vm.sh <role> <action> [stage]

Actions:
  prepare-bundle   control-plane only; prepare bundle, render workflows, start server, apply offline guard
  apply-scenario   apply control-plane bootstrap or worker join based on role
  verify-scenario  control-plane only; verify stage=bootstrap|cluster|all (default: cluster)
  collect          ensure artifact directory exists
  cleanup          stop server and offline guard, restore ownership

Roles:
  control-plane | worker | worker-2

Examples:
  bash test/e2e/vagrant/run-scenario-vm.sh control-plane prepare-bundle
  bash test/e2e/vagrant/run-scenario-vm.sh control-plane apply-scenario
  bash test/e2e/vagrant/run-scenario-vm.sh worker apply-scenario
  bash test/e2e/vagrant/run-scenario-vm.sh control-plane verify-scenario bootstrap
  bash test/e2e/vagrant/run-scenario-vm.sh control-plane verify-scenario cluster
EOF
}

if [[ "${1:-}" == "--help" || "${1:-}" == "-h" ]]; then
  usage
  exit 0
fi

ROLE="${1:?role required}"
ACTION="${2:?action required}"
SCENARIO_STAGE="${3:-}"
ART_DIR_REL="${ART_DIR_REL:?ART_DIR_REL is required}"
ART_DIR="/workspace/${ART_DIR_REL}"
CASE_DIR="${ART_DIR}/cases"
REPORT_DIR="${ART_DIR}/reports"
RENDERED_WORKFLOWS_DIR="${ART_DIR}/rendered-workflows"
SERVER_URL="${SERVER_URL:?SERVER_URL is required}"
SERVER_BIND_ADDR="${DECK_SERVER_BIND_ADDR:-0.0.0.0:18080}"
SERVER_ENDPOINT="${SERVER_URL#http://}"
SERVER_ENDPOINT="${SERVER_ENDPOINT#https://}"
SERVER_HOST="${SERVER_ENDPOINT%%:*}"
KUBEADM_ADVERTISE_ADDRESS="${DECK_KUBEADM_ADVERTISE_ADDRESS:-${SERVER_HOST}}"
OFFLINE_RELEASE="${DECK_OFFLINE_RELEASE:-ubuntu2204}"
OFFLINE_RELEASE_CONTROL_PLANE="${DECK_OFFLINE_RELEASE_CONTROL_PLANE:-ubuntu2204}"
OFFLINE_RELEASE_WORKER="${DECK_OFFLINE_RELEASE_WORKER:-ubuntu2404}"
OFFLINE_RELEASE_WORKER_2="${DECK_OFFLINE_RELEASE_WORKER_2:-rocky9}"
KUBERNETES_VERSION="${DECK_KUBERNETES_VERSION:-v1.30.1}"
KUBERNETES_UPGRADE_VERSION="${DECK_KUBERNETES_UPGRADE_VERSION:-}"
PREPARED_BUNDLE_REL="${DECK_PREPARED_BUNDLE_REL:-}"
E2E_SCENARIO="${DECK_E2E_SCENARIO:-k8s-worker-join}"
E2E_RUN_ID="${DECK_E2E_RUN_ID:-local}"
E2E_PROVIDER="${DECK_E2E_PROVIDER:-libvirt}"
E2E_CACHE_KEY="${DECK_E2E_CACHE_KEY:-compat}"
E2E_STARTED_AT="${DECK_E2E_STARTED_AT:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"
SERVER_ROOT="/tmp/deck/server-root"
DECK_BIN="/tmp/deck/deck"
SERVER_PID=""
REPO_TYPE="apt-flat"
OFFLINE_GUARD_ACTIVE=0
KEEP_PROCESSES=0
SERVER_PID_FILE="/tmp/deck/offline-server.pid"
CONTROL_PLANE_WORKFLOW_URL="${SERVER_URL}/workflows/scenarios/control-plane-bootstrap.yaml"
SCENARIO_HELPERS="/workspace/test/e2e/vagrant/run-scenario-vm-scenario.sh"

if [[ ! -f "${SCENARIO_HELPERS}" ]]; then
  echo "[deck] missing scenario helper script: ${SCENARIO_HELPERS}"
  exit 1
fi
source "${SCENARIO_HELPERS}"

if [[ "${ROLE}" == "control-plane" && "${ACTION}" == "prepare-bundle" ]]; then
  rm -rf "${ART_DIR}"
fi

mkdir -p "${ART_DIR}" "${CASE_DIR}" "${REPORT_DIR}" "${RENDERED_WORKFLOWS_DIR}" /tmp/deck
mkdir -p "${SERVER_ROOT}"

ARCH_RAW="$(uname -m)"
ARCH=""
case "${ARCH_RAW}" in
  x86_64)
    ARCH="amd64"
    ;;
  aarch64|arm64)
    ARCH="arm64"
    ;;
  *)
    echo "[deck] unsupported VM architecture: ${ARCH_RAW}"
    exit 1
    ;;
esac

if [[ ! -f "/workspace/${PREPARED_BUNDLE_REL}/outputs/bin/linux/${ARCH}/deck" ]]; then
  echo "[deck] missing prepared bundle runtime binary: /workspace/${PREPARED_BUNDLE_REL}/outputs/bin/linux/${ARCH}/deck"
  exit 1
fi

if [[ ! -x "${DECK_BIN}" ]]; then
  cp "/workspace/${PREPARED_BUNDLE_REL}/outputs/bin/linux/${ARCH}/deck" "${DECK_BIN}"
  chmod +x "${DECK_BIN}"
fi

cd /workspace

if [[ -d /etc/yum.repos.d ]]; then
  REPO_TYPE="yum"
fi

cleanup() {
  set +e
  if [[ "${KEEP_PROCESSES}" == "1" ]]; then
    chown -R vagrant:vagrant "${ART_DIR}" /tmp/deck >/dev/null 2>&1 || true
    set -e
    return
  fi
  if [[ -n "${SERVER_PID}" ]]; then
    sudo -n kill "${SERVER_PID}" >/dev/null 2>&1 || true
    SERVER_PID=""
  fi
  if [[ -f "${SERVER_PID_FILE}" ]]; then
    sudo -n kill "$(cat "${SERVER_PID_FILE}")" >/dev/null 2>&1 || true
    rm -f "${SERVER_PID_FILE}"
  fi
  if [[ "${OFFLINE_GUARD_ACTIVE}" == "1" ]]; then
    sudo -n iptables -D OUTPUT -j DECK_OFFLINE_GUARD >/dev/null 2>&1 || true
    sudo -n iptables -F DECK_OFFLINE_GUARD >/dev/null 2>&1 || true
    sudo -n iptables -X DECK_OFFLINE_GUARD >/dev/null 2>&1 || true
    if command -v ip6tables >/dev/null 2>&1; then
      sudo -n ip6tables -D OUTPUT -j DECK_OFFLINE_GUARD6 >/dev/null 2>&1 || true
      sudo -n ip6tables -F DECK_OFFLINE_GUARD6 >/dev/null 2>&1 || true
      sudo -n ip6tables -X DECK_OFFLINE_GUARD6 >/dev/null 2>&1 || true
    fi
  fi
  chown -R vagrant:vagrant "${ART_DIR}" /tmp/deck >/dev/null 2>&1 || true
  set -e
}

trap cleanup EXIT INT TERM

wait_server_health() {
  local i
  for ((i=1; i<=180; i++)); do
    if curl -fsS --max-time 5 "${SERVER_URL}/healthz" > "${ART_DIR}/server-health.json" 2>/dev/null; then
      return 0
    fi
    sleep 2
  done
  return 1
}

detect_local_ipv4() {
  local candidate=""
  candidate="$(ip -4 route get 1.1.1.1 2>/dev/null | awk '{for (i=1; i<=NF; i++) if ($i=="src") {print $(i+1); exit}}')"
  if [[ -n "${candidate}" ]]; then
    printf '%s\n' "${candidate}"
    return 0
  fi
  candidate="$(ip -4 -o addr show scope global 2>/dev/null | awk '{print $4}' | cut -d/ -f1 | head -n1)"
  if [[ -n "${candidate}" ]]; then
    printf '%s\n' "${candidate}"
    return 0
  fi
  return 1
}

ensure_advertise_address() {
  if ip -4 -o addr show scope global 2>/dev/null | awk '{print $4}' | cut -d/ -f1 | grep -Fx "${KUBEADM_ADVERTISE_ADDRESS}" >/dev/null 2>&1; then
    return 0
  fi
  local detected=""
  detected="$(detect_local_ipv4 || true)"
  if [[ -n "${detected}" ]]; then
    KUBEADM_ADVERTISE_ADDRESS="${detected}"
  fi
}

prepare_server_bundle() {
  if [[ -n "${PREPARED_BUNDLE_REL}" ]] && [[ -f "/workspace/${PREPARED_BUNDLE_REL}/.deck/manifest.json" ]]; then
    sudo -n rm -rf "${SERVER_ROOT}"
    mkdir -p "${SERVER_ROOT}"
    cp -a "/workspace/${PREPARED_BUNDLE_REL}/." "${SERVER_ROOT}/"
    printf 'prepared-bundle=%s\n' "${PREPARED_BUNDLE_REL}" > "${CASE_DIR}/01-prepare.log"
    return 0
  fi

  echo "[deck] prepared bundle missing; host step prepare-host must publish DECK_PREPARED_BUNDLE_REL" | tee -a "${CASE_DIR}/01-prepare.log"
  exit 1
}

write_runtime_workflows() {
  local workflow_dir="${SERVER_ROOT}/workflows"
  rm -rf "${workflow_dir}"
  mkdir -p "${workflow_dir}"
  ensure_advertise_address
  cp -a "/workspace/test/workflows/." "${workflow_dir}/"
  CONTROL_PLANE_WORKFLOW_URL="${SERVER_URL}/workflows/scenarios/control-plane-bootstrap.yaml"
  rm -rf "${RENDERED_WORKFLOWS_DIR}"
  mkdir -p "${RENDERED_WORKFLOWS_DIR}"
  cp -a "${workflow_dir}/." "${RENDERED_WORKFLOWS_DIR}/"
}

start_server_background() {
  if [[ -f "${SERVER_PID_FILE}" ]]; then
    local existing
    existing="$(cat "${SERVER_PID_FILE}")"
    if sudo -n kill -0 "${existing}" >/dev/null 2>&1; then
      SERVER_PID="${existing}"
      return 0
    fi
  fi
  echo "[deck] starting server ${SERVER_BIND_ADDR}"
  rm -f "${SERVER_PID_FILE}"
  sudo -n bash -c "nohup \"${DECK_BIN}\" server up --root \"${SERVER_ROOT}\" --addr \"${SERVER_BIND_ADDR}\" > \"${CASE_DIR}/02-server.log\" 2>&1 < /dev/null & echo \$! > \"${SERVER_PID_FILE}\""
  SERVER_PID="$(cat "${SERVER_PID_FILE}")"
  if ! sudo -n kill -0 "${SERVER_PID}" >/dev/null 2>&1; then
    echo "[deck] server failed to stay running after start"
    exit 1
  fi
}

clear_install_state() {
  sudo -n rm -f /root/.deck/state/*.json
}

require_control_plane() {
  if [[ "${ROLE}" != "control-plane" ]]; then
    echo "[deck] unsupported role/action: role=${ROLE} action=${ACTION}"
    exit 1
  fi
}

action_prepare_bundle() {
  require_control_plane
  prepare_server_bundle
  write_runtime_workflows
  start_server_background
  if ! wait_server_health; then
    echo "[deck] server health check failed" | tee "${CASE_DIR}/06-assertions.log"
    exit 1
  fi
  scenario_action_prepare
}

action_apply_scenario() {
  scenario_action_apply
}

action_verify_scenario() {
  require_control_plane
  scenario_action_verify "${SCENARIO_STAGE:-}"
}

if [[ "${ACTION}" != "cleanup" ]]; then
  KEEP_PROCESSES=1
fi

case "${ACTION}" in
  prepare-bundle)
    action_prepare_bundle
    ;;
  apply-scenario)
    action_apply_scenario
    ;;
  verify-scenario)
    action_verify_scenario
    ;;
  collect)
    mkdir -p "${ART_DIR}"
    ;;
  cleanup)
    KEEP_PROCESSES=0
    cleanup
    exit 0
    ;;
  *)
    echo "[deck] unsupported role/action: role=${ROLE} action=${ACTION}"
    exit 1
    ;;
esac
