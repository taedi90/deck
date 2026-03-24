#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
source "${ROOT_DIR}/test/e2e/vagrant/common.sh"

CONTRACT_WORKFLOW_COMPONENTS_ROOT="test/workflows/components"
CONTRACT_WORKFLOW_SCENARIOS_ROOT="test/workflows/scenarios"
CONTRACT_E2E_VAGRANT_ROOT="test/e2e/vagrant"
DECK_VAGRANT_WORKFLOW_ROOT_REL="test/workflows"
DECK_VAGRANT_VM_SCENARIO_SCRIPT="${ROOT_DIR}/test/e2e/vagrant/run-scenario-vm.sh"
DECK_VAGRANT_VM_STAGED_PATH="test/e2e/vagrant/run-scenario-vm.sh"
DECK_VAGRANT_VM_DISPATCHER_SCRIPT="${ROOT_DIR}/test/e2e/vagrant/run-scenario-vm.sh"
DECK_VAGRANT_VM_DISPATCHER_STAGED_PATH="test/e2e/vagrant/run-scenario-vm.sh"

STEPS=(
  prepare-host
  up-vms
  prepare-bundle
  apply-scenario
  verify-scenario
  collect
  cleanup
)

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
  prepare-host, up-vms, prepare-bundle, apply-scenario,
  verify-scenario, collect, cleanup
EOF
}

deck_vagrant_prepare_workflow_bundle() {
  "${ROOT_DIR}/test/e2e/vagrant/render-workflows.sh" "${ROOT_DIR}" "${PREPARED_BUNDLE_WORKFLOW_DIR}"
}

guest_vm_action_command() {
  local role="$1"
  local action="$2"
  local stage="${3:-}"
  local cmd="set -euo pipefail; exec bash /workspace/${DECK_VAGRANT_VM_STAGED_PATH} ${role} ${action}"
  if [[ -n "${stage}" ]]; then
    cmd+=" ${stage}"
  fi
  printf '%s' "bash -lc '${cmd}'"
}

control_plane_action() {
  run_role_action "control-plane" "$@"
}

step_prepare_bundle() { control_plane_action "prepare-bundle"; }
role_release() {
  local role="$1"
  case "${role}" in
    worker-2)
      printf '%s\n' "rocky9"
      ;;
    *)
      printf '%s\n' "ubuntu2404"
      ;;
  esac
}

run_role_action() {
  local role="$1"
  local action="$2"
  local stage="${3:-}"
  local role_env=""
  local worker_release=""
  local kubernetes_version=""
  local upgrade_kubernetes_version=""

  load_state_env
  kubernetes_version="$(scenario_kubernetes_version || printf '%s' 'v1.30.1')"
  upgrade_kubernetes_version="$(scenario_upgrade_kubernetes_version || true)"
  case "${role}" in
    control-plane)
      role_env="DECK_KUBEADM_ADVERTISE_ADDRESS=${SERVER_IP} DECK_OFFLINE_RELEASE_CONTROL_PLANE=ubuntu2204 DECK_OFFLINE_RELEASE_WORKER=ubuntu2404 DECK_OFFLINE_RELEASE_WORKER_2=rocky9 DECK_PREPARED_BUNDLE_REL=${PREPARED_BUNDLE_REL:-} DECK_KUBERNETES_VERSION=${kubernetes_version} DECK_KUBERNETES_UPGRADE_VERSION=${upgrade_kubernetes_version}"
      ;;
    worker|worker-2)
      worker_release="$(role_release "${role}")"
      role_env="DECK_OFFLINE_RELEASE=${worker_release} DECK_PREPARED_BUNDLE_REL=${PREPARED_BUNDLE_REL:-} DECK_KUBERNETES_VERSION=${kubernetes_version} DECK_KUBERNETES_UPGRADE_VERSION=${upgrade_kubernetes_version}"
      ;;
  esac

  echo "[deck] role=${role} action=${action} scenario=${SCENARIO_ID}"
  run_vagrant_ssh "${role}" "ART_DIR_REL=${ART_DIR_REL} SERVER_URL=${SERVER_URL} ${role_env} DECK_E2E_SCENARIO=${SCENARIO_ID} DECK_E2E_RUN_ID=${RUN_ID} DECK_E2E_PROVIDER=${DECK_VAGRANT_PROVIDER} DECK_E2E_CACHE_KEY=${CACHE_KEY} DECK_E2E_STARTED_AT=${RUN_STARTED_AT} $(guest_vm_action_command "${role}" "${action}" "${stage}")"
}

step_apply_scenario() {
  local -a nodes=()
  local node=""
  mapfile -t nodes < <(active_nodes)
  for node in "${nodes[@]}"; do
    run_role_action "${node}" "apply-scenario"
  done
}

step_verify_scenario() {
  control_plane_action "verify-scenario"
}

deck_vagrant_main "$@"
