#!/usr/bin/env bash
set -euo pipefail

DISPATCHER="/workspace/test/e2e/vagrant/run-scenario-vm.sh"

usage() {
  cat <<'EOF'
Usage: test/vagrant/run-offline-multinode-vm.sh <role> <action>

Compatibility shim for offline-multinode VM entrypoint.
Delegates to test/e2e/vagrant/run-scenario-vm.sh.

Legacy actions:
  start-server
  apply-control-plane
  apply-worker
  verify-install
  assert-cluster
  collect
  cleanup
  orchestrate
EOF
}

if [[ "${1:-}" == "--help" || "${1:-}" == "-h" ]]; then
  usage
  exit 0
fi

ROLE="${1:?role required}"
ACTION="${2:?action required}"

if [[ ! -x "${DISPATCHER}" ]]; then
  echo "[deck] missing dispatcher script: ${DISPATCHER}"
  exit 1
fi

map_action() {
  case "${ACTION}" in
    start-server)
      printf '%s\n' "prepare-bundle"
      ;;
    apply-control-plane|apply-worker)
      printf '%s\n' "apply-scenario"
      ;;
    verify-install|assert-cluster)
      printf '%s\n' "verify-scenario"
      ;;
    collect|cleanup)
      printf '%s\n' "${ACTION}"
      ;;
    orchestrate)
      printf '%s\n' "orchestrate"
      ;;
    *)
      return 1
      ;;
  esac
}

map_stage() {
  case "${ACTION}" in
    verify-install)
      printf '%s\n' "bootstrap"
      ;;
    assert-cluster)
      printf '%s\n' "cluster"
      ;;
  esac
}

default_scenario() {
  case "${ACTION}" in
    start-server|apply-control-plane|verify-install)
      printf '%s\n' "k8s-control-plane-bootstrap"
      ;;
    apply-worker|assert-cluster|orchestrate)
      printf '%s\n' "k8s-worker-join"
      ;;
    collect|cleanup)
      printf '%s\n' "k8s-worker-join"
      ;;
    *)
      printf '%s\n' "k8s-worker-join"
      ;;
  esac
}

if [[ "${ACTION}" == "orchestrate" ]]; then
  scenario="${DECK_E2E_SCENARIO:-$(default_scenario)}"
  DECK_E2E_SCENARIO="${scenario}" DECK_VAGRANT_LEGACY_RESULT_SHIM=1 bash "${DISPATCHER}" "${ROLE}" prepare-bundle
  DECK_E2E_SCENARIO="${scenario}" DECK_VAGRANT_LEGACY_RESULT_SHIM=1 bash "${DISPATCHER}" "${ROLE}" apply-scenario
  DECK_E2E_SCENARIO="${scenario}" DECK_VAGRANT_LEGACY_RESULT_SHIM=1 bash "${DISPATCHER}" "${ROLE}" verify-scenario bootstrap
  DECK_E2E_SCENARIO="${scenario}" DECK_VAGRANT_LEGACY_RESULT_SHIM=1 bash "${DISPATCHER}" "${ROLE}" verify-scenario cluster
  exit 0
fi

NEW_ACTION="$(map_action)" || {
  echo "[deck] unsupported role/action: role=${ROLE} action=${ACTION}"
  exit 1
}

STAGE="$(map_stage || true)"
SCENARIO="${DECK_E2E_SCENARIO:-$(default_scenario)}"
if [[ -n "${STAGE}" ]]; then
  DECK_E2E_SCENARIO="${SCENARIO}" bash "${DISPATCHER}" "${ROLE}" "${NEW_ACTION}" "${STAGE}"
else
  DECK_E2E_SCENARIO="${SCENARIO}" bash "${DISPATCHER}" "${ROLE}" "${NEW_ACTION}"
fi

if [[ "${ACTION}" == "verify-install" || "${ACTION}" == "assert-cluster" || "${ACTION}" == "orchestrate" ]]; then
  cat > "/workspace/${ART_DIR_REL}/offline-multinode-result.txt" <<EOF
scenario=offline-multinode
result=PASS
jobs=offline-cp-install,offline-worker-join,offline-worker-2-join
server=${SERVER_URL}
EOF
  cp "/workspace/${ART_DIR_REL}/pass.txt" "/workspace/${ART_DIR_REL}/offline-multinode-pass.txt"
fi
