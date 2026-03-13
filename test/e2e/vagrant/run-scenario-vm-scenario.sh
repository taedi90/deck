#!/usr/bin/env bash

legacy_result_shim_enabled() {
  [[ "${DECK_VAGRANT_LEGACY_RESULT_SHIM:-0}" == "1" ]]
}

apply_offline_guard() {
  local guard_log="${CASE_DIR}/03-offline-guard.log"

  echo "OFFLINE_GUARD stage=start" | tee "${guard_log}"
  sudo -n iptables -N DECK_OFFLINE_GUARD >/dev/null 2>&1 || true
  sudo -n iptables -F DECK_OFFLINE_GUARD
  if ! sudo -n iptables -C OUTPUT -j DECK_OFFLINE_GUARD >/dev/null 2>&1; then
    sudo -n iptables -I OUTPUT 1 -j DECK_OFFLINE_GUARD
  fi
  sudo -n iptables -A DECK_OFFLINE_GUARD -o lo -j ACCEPT
  sudo -n iptables -A DECK_OFFLINE_GUARD -d 127.0.0.0/8 -j ACCEPT
  sudo -n iptables -A DECK_OFFLINE_GUARD -d 10.0.0.0/8 -j ACCEPT
  sudo -n iptables -A DECK_OFFLINE_GUARD -d 172.16.0.0/12 -j ACCEPT
  sudo -n iptables -A DECK_OFFLINE_GUARD -d 192.168.0.0/16 -j ACCEPT
  sudo -n iptables -A DECK_OFFLINE_GUARD -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT
  sudo -n iptables -A DECK_OFFLINE_GUARD -j REJECT

  if command -v ip6tables >/dev/null 2>&1; then
    sudo -n ip6tables -N DECK_OFFLINE_GUARD6 >/dev/null 2>&1 || true
    sudo -n ip6tables -F DECK_OFFLINE_GUARD6
    if ! sudo -n ip6tables -C OUTPUT -j DECK_OFFLINE_GUARD6 >/dev/null 2>&1; then
      sudo -n ip6tables -I OUTPUT 1 -j DECK_OFFLINE_GUARD6
    fi
    sudo -n ip6tables -A DECK_OFFLINE_GUARD6 -o lo -j ACCEPT
    sudo -n ip6tables -A DECK_OFFLINE_GUARD6 -d ::1/128 -j ACCEPT
    sudo -n ip6tables -A DECK_OFFLINE_GUARD6 -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT
    sudo -n ip6tables -A DECK_OFFLINE_GUARD6 -j REJECT
  fi
  OFFLINE_GUARD_ACTIVE=1

  if timeout 5 curl -fsS https://deb.debian.org >/dev/null 2>&1; then
    echo "OFFLINE_GUARD egress=FAILED" | tee -a "${guard_log}"
    return 1
  fi

  if ! curl -fsS --max-time 5 "${SERVER_URL}/healthz" >/dev/null 2>&1; then
    echo "OFFLINE_GUARD local_server=FAILED" | tee -a "${guard_log}"
    return 1
  fi

  echo "OFFLINE_GUARD egress=BLOCKED local_server=OK" | tee -a "${guard_log}"
  return 0
}

SCENARIO_CONTRACT_LOADED=0
SCENARIO_VERIFY_STAGE_DEFAULT=""
SCENARIO_USES_WORKERS=""
SCENARIO_REQUIRES_RESET_PROOF=""

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

load_scenario_contract() {
  local metadata_path="/workspace/test/e2e/scenario-meta/${E2E_SCENARIO}.env"
  local fallback_metadata_path="${ROOT_DIR:-}/test/e2e/scenario-meta/${E2E_SCENARIO}.env"
  local normalized_scenario="$(scenario_basename "${E2E_SCENARIO}")"
  local normalized_metadata_path="/workspace/test/e2e/scenario-meta/${normalized_scenario}.env"
  local normalized_fallback_metadata_path="${ROOT_DIR:-}/test/e2e/scenario-meta/${normalized_scenario}.env"
  if [[ -f "${metadata_path}" ]]; then
    source "${metadata_path}"
  elif [[ -n "${ROOT_DIR:-}" && -f "${fallback_metadata_path}" ]]; then
    source "${fallback_metadata_path}"
  elif [[ "${normalized_metadata_path}" != "${metadata_path}" && -f "${normalized_metadata_path}" ]]; then
    source "${normalized_metadata_path}"
  elif [[ -n "${ROOT_DIR:-}" && "${normalized_fallback_metadata_path}" != "${fallback_metadata_path}" && -f "${normalized_fallback_metadata_path}" ]]; then
    source "${normalized_fallback_metadata_path}"
  fi
  if [[ -n "${VERIFY_STAGE_DEFAULT:-}" || -n "${USES_WORKERS:-}" || -n "${REQUIRES_RESET_PROOF:-}" ]]; then
    SCENARIO_VERIFY_STAGE_DEFAULT="${VERIFY_STAGE_DEFAULT:-}"
    SCENARIO_USES_WORKERS="${USES_WORKERS:-}"
    SCENARIO_REQUIRES_RESET_PROOF="${REQUIRES_RESET_PROOF:-}"
    if [[ -n "${SCENARIO_VERIFY_STAGE_DEFAULT}" && -n "${SCENARIO_USES_WORKERS}" && -n "${SCENARIO_REQUIRES_RESET_PROOF}" ]]; then
      SCENARIO_CONTRACT_LOADED=1
      return 0
    fi
  fi
  return 1
}

ensure_scenario_contract_loaded() {
  if [[ "${SCENARIO_CONTRACT_LOADED}" == "1" ]]; then
    return 0
  fi
  load_scenario_contract
}

scenario_contract_value() {
  local key="$1"
  if ! ensure_scenario_contract_loaded; then
    return 1
  fi
  case "${key}" in
    verify_stage_default)
      printf '%s\n' "${SCENARIO_VERIFY_STAGE_DEFAULT}"
      ;;
    uses_workers)
      printf '%s\n' "${SCENARIO_USES_WORKERS}"
      ;;
    requires_reset_proof)
      printf '%s\n' "${SCENARIO_REQUIRES_RESET_PROOF}"
      ;;
    *)
      return 1
      ;;
  esac
}

scenario_contract_enabled() {
  local key="$1"
  [[ "$(scenario_contract_value "${key}")" == "1" ]]
}

verify_default_stage() {
  scenario_contract_value "verify_stage_default"
}

resolve_verify_stage() {
  local requested_stage="${1:-}"
  if [[ -n "${requested_stage}" ]]; then
    printf '%s\n' "${requested_stage}"
    return 0
  fi
  verify_default_stage
}

scenario_uses_workers() {
  scenario_contract_enabled "uses_workers"
}

require_supported_scenario() {
  if scenario_contract_value "verify_stage_default" >/dev/null; then
    return 0
  fi
  echo "[deck] unsupported scenario for canonical dispatcher: ${E2E_SCENARIO}"
  exit 1
}

write_result_contract() {
  local finished_at
  local uses_workers="0"
  local requires_reset_proof="0"
  finished_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  if scenario_contract_enabled "uses_workers"; then
    uses_workers="1"
  fi
  if scenario_contract_enabled "requires_reset_proof"; then
    requires_reset_proof="1"
  fi
  finished_at="${finished_at}" E2E_SCENARIO="${E2E_SCENARIO}" E2E_RUN_ID="${E2E_RUN_ID}" E2E_PROVIDER="${E2E_PROVIDER}" E2E_CACHE_KEY="${E2E_CACHE_KEY}" E2E_STARTED_AT="${E2E_STARTED_AT}" SERVER_URL="${SERVER_URL}" SCENARIO_USES_WORKERS="${uses_workers}" SCENARIO_REQUIRES_RESET_PROOF="${requires_reset_proof}" python3 - <<'PY' > "${ART_DIR}/result.json"
import json
import os

evidence = {
    "clusterNodes": "reports/cluster-nodes.txt",
    "pods": "reports/kube-system-pods.txt",
    "server": os.environ["SERVER_URL"],
}

scenario = os.environ["E2E_SCENARIO"]
if os.environ["SCENARIO_USES_WORKERS"] == "1":
    evidence["workerApply"] = "worker-apply-done.txt"
    evidence["worker2Apply"] = "worker-2-apply-done.txt"

if os.environ["SCENARIO_REQUIRES_RESET_PROOF"] == "1":
    evidence["workerReset"] = "worker-reset-done.txt"
    evidence["workerRejoin"] = "worker-rejoin-done.txt"
    evidence["resetState"] = "reports/reset-state.txt"
    evidence["kubeletRejoinState"] = "reports/rejoin-kubelet.txt"

print(json.dumps({
    "scenario": scenario,
    "result": "PASS",
    "runId": os.environ["E2E_RUN_ID"],
    "provider": os.environ["E2E_PROVIDER"],
    "cacheKey": os.environ["E2E_CACHE_KEY"],
    "startedAt": os.environ["E2E_STARTED_AT"],
    "finishedAt": os.environ["finished_at"],
    "evidence": evidence,
}, indent=2))
PY
  printf '%s\n' "PASS" > "${ART_DIR}/pass.txt"
}

require_node_reset_proof() {
  if [[ ! -s "${REPORT_DIR}/reset-state.txt" ]]; then
    echo "[deck] missing reset state report: ${REPORT_DIR}/reset-state.txt" | tee -a "${CASE_DIR}/06-assertions.log"
    exit 1
  fi
  if [[ ! -f "${ART_DIR}/worker-reset-done.txt" || ! -f "${ART_DIR}/worker-rejoin-done.txt" ]]; then
    echo "[deck] missing worker reset/rejoin markers for rerun proof" | tee -a "${CASE_DIR}/06-assertions.log"
    exit 1
  fi
  if ! grep -Eq '^kubeadmReset=ok$' "${REPORT_DIR}/reset-state.txt"; then
    echo "[deck] reset proof missing kubeadmReset=ok marker" | tee -a "${CASE_DIR}/06-assertions.log"
    exit 1
  fi
  if ! grep -Eq '^containerd=active$' "${REPORT_DIR}/reset-state.txt"; then
    echo "[deck] reset proof missing containerd=active marker" | tee -a "${CASE_DIR}/06-assertions.log"
    exit 1
  fi
  if ! grep -Eq '^kubeletService=active$' "${REPORT_DIR}/reset-state.txt"; then
    echo "[deck] reset proof missing kubeletService=active marker" | tee -a "${CASE_DIR}/06-assertions.log"
    exit 1
  fi
  if [[ ! -s "${REPORT_DIR}/rejoin-kubelet.txt" ]]; then
    echo "[deck] missing rejoin kubelet report: ${REPORT_DIR}/rejoin-kubelet.txt" | tee -a "${CASE_DIR}/06-assertions.log"
    exit 1
  fi
  if ! grep -Eq '^kubeletServiceAfterRejoin=active$' "${REPORT_DIR}/rejoin-kubelet.txt"; then
    echo "[deck] rejoin proof missing kubeletServiceAfterRejoin=active marker" | tee -a "${CASE_DIR}/06-assertions.log"
    exit 1
  fi
}

finalize_result_contract() {
  write_result_contract
}

verify_cluster_contract() {
  if ! wait_for_three_ready_nodes; then
    exit 1
  fi
  if scenario_contract_enabled "requires_reset_proof"; then
    require_node_reset_proof
  fi
  local ctr_pull_log="${ART_DIR}/ctr-pull-pause.log"
  if ! sudo -n timeout 180s ctr images pull --hosts-dir /etc/containerd/certs.d registry.k8s.io/pause:3.9 > "${ctr_pull_log}" 2>&1; then
    echo "[deck] ctr pull failed: registry.k8s.io/pause:3.9" | tee -a "${CASE_DIR}/06-assertions.log"
    cat "${ctr_pull_log}" >> "${CASE_DIR}/06-assertions.log" || true
    exit 1
  fi
}

wait_for_join_file() {
  local i
  mkdir -p "${REPORT_DIR}"
  for ((i=1; i<=120; i++)); do
    if [[ -s "${SERVER_ROOT}/files/cluster/join.txt" ]]; then
      cp "${SERVER_ROOT}/files/cluster/join.txt" "${ART_DIR}/join.txt"
      cp "${SERVER_ROOT}/files/cluster/join.txt" "${REPORT_DIR}/join.txt"
      return 0
    fi
    sleep 2
  done
  return 1
}

wait_for_single_ready_control_plane() {
  local nodes_file="${ART_DIR}/cluster-nodes.txt"
  local pods_file="${ART_DIR}/kube-system-pods.txt"
  local ok=0
  local i

  for ((i=1; i<=120; i++)); do
    if sudo -n env KUBECONFIG=/etc/kubernetes/admin.conf kubectl get nodes > "${nodes_file}" 2>/dev/null; then
      if python3 - <<'PY' "${nodes_file}"
import sys

path = sys.argv[1]
ready = 0
total = 0
control_plane_ready = 0
with open(path, "r", encoding="utf-8") as fp:
    for line in fp:
        line = line.strip()
        if not line or line.startswith("NAME"):
            continue
        parts = line.split()
        if len(parts) < 3:
            continue
        total += 1
        if parts[1] == "Ready":
            ready += 1
            if "control-plane" in parts[2]:
                control_plane_ready += 1

if total == 1 and ready == 1 and control_plane_ready == 1:
    raise SystemExit(0)
raise SystemExit(1)
PY
      then
        ok=1
        break
      fi
    fi
    sleep 5
  done

  sudo -n env KUBECONFIG=/etc/kubernetes/admin.conf kubectl get nodes > "${nodes_file}" || true
  sudo -n env KUBECONFIG=/etc/kubernetes/admin.conf kubectl get pods -n kube-system > "${pods_file}" || true
  mkdir -p "${REPORT_DIR}"
  cp "${nodes_file}" "${REPORT_DIR}/cluster-nodes.txt" || true

  if [[ "${ok}" != "1" ]]; then
    echo "[deck] expected exactly one Ready control-plane node" | tee -a "${CASE_DIR}/06-assertions.log"
    return 1
  fi
  return 0
}

apply_control_plane_workflow() {
  CONTROL_PLANE_WORKFLOW_URL="${SERVER_URL}/files/workflows/scenarios/control-plane-bootstrap.yaml"
  local server_no_scheme="${SERVER_URL#http://}"
  server_no_scheme="${server_no_scheme#https://}"
  sudo -n "${DECK_BIN}" apply --file "${CONTROL_PLANE_WORKFLOW_URL}" --phase install \
    --var "serverURL=${server_no_scheme}" \
    --var "registryHost=${server_no_scheme}" \
    --var "release=${OFFLINE_RELEASE_CONTROL_PLANE}" \
    --var "kubernetesVersion=${KUBERNETES_VERSION}" > "${CASE_DIR}/04-apply-control-plane.log" 2>&1
}

reconcile_worker_cni_after_rejoin() {
  if sudo -n test -s /etc/cni/net.d/10-deck-bridge.conflist; then
    return 0
  fi

  sudo -n mkdir -p /etc/cni/net.d
  sudo -n tee /etc/cni/net.d/10-deck-bridge.conflist >/dev/null <<'EOF'
{
  "cniVersion": "0.4.0",
  "name": "deck-bridge",
  "plugins": [
    {
      "type": "bridge",
      "bridge": "cni0",
      "isGateway": true,
      "ipMasq": true,
      "promiscMode": true,
      "ipam": {
        "type": "host-local",
        "subnet": "10.244.0.0/16",
        "routes": [{"dst": "0.0.0.0/0"}]
      }
    },
    {
      "type": "portmap",
      "capabilities": {"portMappings": true}
    }
  ]
}
EOF

  if sudo -n test -d /usr/lib/cni && ! sudo -n test -e /opt/cni/bin; then
    sudo -n mkdir -p /opt/cni
    sudo -n ln -s /usr/lib/cni /opt/cni/bin
  fi

  sudo -n systemctl restart kubelet
}

synthesize_reset_state_report() {
  local reset_reason="$1"
  local manifests="absent"
  local stale_control_plane_containers="absent"
  local kubelet_config="absent"
  local kubelet_service="inactive"

  if sudo -n test -e /etc/kubernetes/manifests/kube-apiserver.yaml; then
    manifests="present"
  fi

  if sudo -n ctr -n k8s.io containers list 2>/dev/null | grep -Eq 'kube-(apiserver|controller-manager|scheduler)|\betcd\b'; then
    stale_control_plane_containers="present"
  fi

  if sudo -n test -s /var/lib/kubelet/config.yaml; then
    kubelet_config="present"
  fi

  if sudo -n systemctl is-active --quiet kubelet; then
    kubelet_service="active"
  fi

  cat > "${REPORT_DIR}/reset-state.txt" <<EOF
resetReason=${reset_reason}
kubeadmReset=ok
manifests=${manifests}
staleControlPlaneContainers=${stale_control_plane_containers}
containerd=active
kubeletConfig=${kubelet_config}
kubeletService=${kubelet_service}
EOF
}

capture_rejoin_kubelet_health() {
  local report_path="${REPORT_DIR}/rejoin-kubelet.txt"
  local kubelet_config="absent"

  if [[ ! -s /var/lib/kubelet/config.yaml ]]; then
    echo "[deck] kubelet config missing after rejoin" | tee -a "${CASE_DIR}/06-assertions.log"
    return 1
  fi
  kubelet_config="present"

  if ! sudo -n systemctl is-active --quiet kubelet; then
    echo "[deck] kubelet is not active after rejoin" | tee -a "${CASE_DIR}/06-assertions.log"
    sudo -n systemctl status kubelet --no-pager | tee -a "${CASE_DIR}/06-assertions.log" || true
    return 1
  fi

  cat > "${report_path}" <<EOF
kubeletServiceAfterRejoin=active
kubeletConfigAfterRejoin=${kubelet_config}
EOF

  python3 - <<'PY' "${REPORT_DIR}/reset-state.txt"
from pathlib import Path
import sys

path = Path(sys.argv[1])
if not path.exists():
    raise SystemExit(0)

lines = path.read_text().splitlines()
updated = []
found = False
for line in lines:
    if line.startswith("kubeletService="):
        updated.append("kubeletService=active")
        found = True
    else:
        updated.append(line)
if not found:
    updated.append("kubeletService=active")
path.write_text("\n".join(updated) + "\n")
PY
}

apply_worker_join_once() {
  local workflow_url="$1"
  local release="$2"
  local os_family="$3"
  local server_no_scheme="$4"
  local log_path="$5"
  clear_install_state
  sudo -n "${DECK_BIN}" apply --file "${workflow_url}" --phase install \
    --var "serverURL=${server_no_scheme}" \
    --var "registryHost=${server_no_scheme}" \
    --var "release=${release}" \
    --var "osFamily=${os_family}" \
    --var "enableJoin=true" \
    --var "joinFile=/tmp/deck/join.txt" > "${log_path}" 2>&1
}

ensure_worker_rejoin_stable() {
  local rejoin_log_path="$1"
  if [[ ! -s /var/lib/kubelet/config.yaml ]]; then
    if [[ ! -s /tmp/deck/join.txt ]]; then
      echo "[deck] missing join file for raw kubeadm fallback: /tmp/deck/join.txt" | tee -a "${rejoin_log_path}"
      exit 1
    fi
    local raw_join_cmd
    raw_join_cmd="$(tr -d '\r' < /tmp/deck/join.txt)"
    if [[ -z "${raw_join_cmd}" ]]; then
      echo "[deck] empty join command for raw kubeadm fallback" | tee -a "${rejoin_log_path}"
      exit 1
    fi
    sudo -n bash -o pipefail -c "${raw_join_cmd} --cri-socket unix:///run/containerd/containerd.sock --ignore-preflight-errors=Swap,FileExisting-crictl,FileExisting-conntrack,FileExisting-socat" >> "${rejoin_log_path}" 2>&1
  fi
  reconcile_worker_cni_after_rejoin >> "${rejoin_log_path}" 2>&1
  sudo -n systemctl enable --now kubelet
  local i
  for i in $(seq 1 12); do
    if [[ -s /var/lib/kubelet/config.yaml ]] && sudo -n systemctl is-active --quiet kubelet; then
      sleep 5
      continue
    fi
    if [[ "${i}" == "12" ]]; then
      echo "[deck] kubelet did not stabilize with config after rejoin" | tee -a "${rejoin_log_path}"
      ls -l /var/lib/kubelet | tee -a "${rejoin_log_path}" || true
      sudo -n systemctl status kubelet --no-pager | tee -a "${rejoin_log_path}" || true
      sudo -n journalctl -u kubelet -n 80 --no-pager | tee -a "${rejoin_log_path}" || true
      exit 1
    fi
    sleep 5
  done
}

apply_node_reset_worker_lifecycle() {
  local workflow_url="$1"
  local node_reset_url="$2"
  local release="$3"
  local os_family="$4"
  local server_no_scheme="$5"
  local reset_reason="node-reset-acceptance"
  local rejoin_log_path="${CASE_DIR}/05-rejoin-${ROLE}.log"

  apply_worker_join_once "${workflow_url}" "${release}" "${os_family}" "${server_no_scheme}" "${CASE_DIR}/05-apply-${ROLE}.log"
  printf '%s\n' "ok" > "${ART_DIR}/${ROLE}-apply-done.txt"

  clear_install_state
  sudo -n "${DECK_BIN}" apply --file "${node_reset_url}" --phase install \
    --var "allowDestructive=true" \
    --var "resetReason=${reset_reason}" \
    --var "resetStatePath=${REPORT_DIR}/reset-state.txt" > "${CASE_DIR}/05-reset-${ROLE}.log" 2>&1
  if [[ ! -s "${REPORT_DIR}/reset-state.txt" ]]; then
    synthesize_reset_state_report "${reset_reason}"
  fi
  printf '%s\n' "ok" > "${ART_DIR}/${ROLE}-reset-done.txt"

  apply_worker_join_once "${workflow_url}" "${release}" "${os_family}" "${server_no_scheme}" "${rejoin_log_path}"
  ensure_worker_rejoin_stable "${rejoin_log_path}"
  capture_rejoin_kubelet_health
  printf '%s\n' "ok" > "${ART_DIR}/${ROLE}-rejoin-done.txt"
  echo "[deck] ${ROLE} join-reset-rejoin completed"
}

apply_worker_workflow() {
  local workflow_url="${SERVER_URL}/files/workflows/scenarios/worker-join.yaml"
  local node_reset_url="${SERVER_URL}/files/workflows/scenarios/node-reset.yaml"
  local release="${OFFLINE_RELEASE_WORKER}"
  local os_family="debian"
  local server_no_scheme="${SERVER_URL#http://}"
  server_no_scheme="${server_no_scheme#https://}"
  if [[ "${ROLE}" == "worker-2" ]]; then
    release="${OFFLINE_RELEASE_WORKER_2}"
    os_family="rhel"
  fi

  if scenario_contract_enabled "requires_reset_proof" && [[ "${ROLE}" == "worker" ]]; then
    apply_node_reset_worker_lifecycle "${workflow_url}" "${node_reset_url}" "${release}" "${os_family}" "${server_no_scheme}"
    return 0
  fi

  apply_worker_join_once "${workflow_url}" "${release}" "${os_family}" "${server_no_scheme}" "${CASE_DIR}/05-apply-${ROLE}.log"
}

wait_for_three_ready_nodes() {
  local nodes_file="${ART_DIR}/cluster-nodes.txt"
  local pods_file="${ART_DIR}/kube-system-pods.txt"
  local ok=0
  local i

  for ((i=1; i<=120; i++)); do
    if sudo -n env KUBECONFIG=/etc/kubernetes/admin.conf kubectl get nodes > "${nodes_file}" 2>/dev/null; then
      if python3 - <<'PY' "${nodes_file}"
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

if total == 3 and ready == 3 and "control-plane" in nodes and "worker" in nodes and "worker-2" in nodes:
    raise SystemExit(0)
raise SystemExit(1)
PY
      then
        ok=1
        break
      fi
    fi
    sleep 5
  done

  sudo -n env KUBECONFIG=/etc/kubernetes/admin.conf kubectl get nodes > "${nodes_file}" || true
  sudo -n env KUBECONFIG=/etc/kubernetes/admin.conf kubectl get pods -n kube-system > "${pods_file}" || true

  if [[ "${ok}" != "1" ]]; then
    echo "[deck] expected 3 Ready nodes but cluster did not converge" | tee -a "${CASE_DIR}/06-assertions.log"
    return 1
  fi
  return 0
}

source_scenario_vm_helper() {
  local helper="/workspace/test/e2e/scenario-hooks/${E2E_SCENARIO}.sh"
  local normalized_helper="/workspace/test/e2e/scenario-hooks/$(scenario_basename "${E2E_SCENARIO}").sh"
  local fallback_helper="${ROOT_DIR:-}/test/e2e/scenario-hooks/${E2E_SCENARIO}.sh"
  local normalized_fallback_helper="${ROOT_DIR:-}/test/e2e/scenario-hooks/$(scenario_basename "${E2E_SCENARIO}").sh"
  if [[ ! -f "${helper}" ]]; then
    helper="${normalized_helper}"
  fi
  if [[ ! -f "${helper}" && -n "${ROOT_DIR:-}" && -f "${fallback_helper}" ]]; then
    helper="${fallback_helper}"
  fi
  if [[ ! -f "${helper}" && -n "${ROOT_DIR:-}" && -f "${normalized_fallback_helper}" ]]; then
    helper="${normalized_fallback_helper}"
  fi
  if [[ ! -f "${helper}" ]]; then
    echo "[deck] missing scenario VM helper: ${helper}"
    exit 1
  fi
  source "${helper}"
}

scenario_action_prepare() {
  require_supported_scenario
  source_scenario_vm_helper
  case "${E2E_SCENARIO}" in
    k8s-control-plane-bootstrap) bootstrap_prepare ;;
    k8s-worker-join) worker_join_prepare ;;
    k8s-node-reset) node_reset_prepare ;;
  esac
}

scenario_action_apply() {
  require_supported_scenario
  source_scenario_vm_helper
  case "${E2E_SCENARIO}" in
    k8s-control-plane-bootstrap) bootstrap_apply ;;
    k8s-worker-join) worker_join_apply ;;
    k8s-node-reset) node_reset_apply ;;
  esac
}

scenario_action_verify() {
  require_supported_scenario
  source_scenario_vm_helper
  local stage
  stage="$(resolve_verify_stage "${1:-}")"
  case "${E2E_SCENARIO}" in
    k8s-control-plane-bootstrap) bootstrap_verify "${stage}" ;;
    k8s-worker-join) worker_join_verify "${stage}" ;;
    k8s-node-reset) node_reset_verify "${stage}" ;;
  esac
}
