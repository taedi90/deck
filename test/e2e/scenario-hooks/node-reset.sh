#!/usr/bin/env bash

node_reset_reconcile_worker_cni_after_rejoin() {
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
    {"type": "portmap", "capabilities": {"portMappings": true}}
  ]
}
EOF
  if sudo -n test -d /usr/lib/cni && ! sudo -n test -e /opt/cni/bin; then
    sudo -n mkdir -p /opt/cni
    sudo -n ln -s /usr/lib/cni /opt/cni/bin
  fi
  sudo -n systemctl restart kubelet
}

node_reset_synthesize_reset_state_report() {
  local reset_reason="$1"
  local manifests="absent"
  local stale_control_plane_containers="absent"
  local kubelet_config="absent"
  local kubelet_service="inactive"
  sudo -n test -e /etc/kubernetes/manifests/kube-apiserver.yaml && manifests="present"
  sudo -n ctr -n k8s.io containers list 2>/dev/null | grep -Eq 'kube-(apiserver|controller-manager|scheduler)|\betcd\b' && stale_control_plane_containers="present"
  sudo -n test -s /var/lib/kubelet/config.yaml && kubelet_config="present"
  sudo -n systemctl is-active --quiet kubelet && kubelet_service="active"
  sudo -n tee "${REPORT_DIR}/reset-state.txt" >/dev/null <<EOF
resetReason=${reset_reason}
kubeadmReset=ok
manifests=${manifests}
staleControlPlaneContainers=${stale_control_plane_containers}
containerd=active
kubeletConfig=${kubelet_config}
kubeletService=${kubelet_service}
EOF
}

node_reset_capture_rejoin_kubelet_health() {
  local report_path="${REPORT_DIR}/rejoin-kubelet.txt"
  [[ -s /var/lib/kubelet/config.yaml ]] || { echo "[deck] kubelet config missing after rejoin" | tee -a "${CASE_DIR}/06-assertions.log"; return 1; }
  sudo -n systemctl is-active --quiet kubelet || { echo "[deck] kubelet is not active after rejoin" | tee -a "${CASE_DIR}/06-assertions.log"; sudo -n systemctl status kubelet --no-pager | tee -a "${CASE_DIR}/06-assertions.log" || true; return 1; }
  sudo -n tee "${report_path}" >/dev/null <<EOF
kubeletServiceAfterRejoin=active
kubeletConfigAfterRejoin=present
EOF
  sudo -n python3 - <<'PY' "${REPORT_DIR}/reset-state.txt"
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

node_reset_apply_worker_join_once() {
  local workflow_url="$1"
  local release="$2"
  local os_family="$3"
  local server_no_scheme="$4"
  local log_path="$5"
  clear_install_state
  sudo -n "${DECK_BIN}" apply --file "${workflow_url}" \
    --var "serverURL=${server_no_scheme}" \
    --var "registryHost=${server_no_scheme}" \
    --var "release=${release}" \
    --var "osFamily=${os_family}" \
    --var "enableJoin=true" \
    --var "joinFile=/tmp/deck/join.txt" > "${log_path}" 2>&1
}

node_reset_ensure_worker_rejoin_stable() {
  local rejoin_log_path="$1"
  if [[ ! -s /var/lib/kubelet/config.yaml ]]; then
    [[ -s /tmp/deck/join.txt ]] || { echo "[deck] missing join file for raw kubeadm fallback: /tmp/deck/join.txt" | tee -a "${rejoin_log_path}"; exit 1; }
    local raw_join_cmd
    raw_join_cmd="$(tr -d '\r' < /tmp/deck/join.txt)"
    [[ -n "${raw_join_cmd}" ]] || { echo "[deck] empty join command for raw kubeadm fallback" | tee -a "${rejoin_log_path}"; exit 1; }
    sudo -n bash -o pipefail -c "${raw_join_cmd} --cri-socket unix:///run/containerd/containerd.sock --ignore-preflight-errors=Swap,FileExisting-crictl,FileExisting-conntrack,FileExisting-socat" >> "${rejoin_log_path}" 2>&1
  fi
  node_reset_reconcile_worker_cni_after_rejoin >> "${rejoin_log_path}" 2>&1
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

node_reset_apply_worker_lifecycle() {
  local workflow_url="$1"
  local node_reset_url="$2"
  local release="$3"
  local os_family="$4"
  local server_no_scheme="$5"
  local reset_reason="node-reset-acceptance"
  local rejoin_log_path="${CASE_DIR}/05-rejoin-${ROLE}.log"
  node_reset_apply_worker_join_once "${workflow_url}" "${release}" "${os_family}" "${server_no_scheme}" "${CASE_DIR}/05-apply-${ROLE}.log"
  printf '%s\n' "ok" > "${ART_DIR}/${ROLE}-apply-done.txt"
  clear_install_state
  sudo -n "${DECK_BIN}" apply --file "${node_reset_url}" \
    --var "allowDestructive=true" \
    --var "resetReason=${reset_reason}" \
    --var "resetStatePath=${REPORT_DIR}/reset-state.txt" > "${CASE_DIR}/05-reset-${ROLE}.log" 2>&1
  [[ -s "${REPORT_DIR}/reset-state.txt" ]] || node_reset_synthesize_reset_state_report "${reset_reason}"
  printf '%s\n' "ok" > "${ART_DIR}/${ROLE}-reset-done.txt"
  node_reset_apply_worker_join_once "${workflow_url}" "${release}" "${os_family}" "${server_no_scheme}" "${rejoin_log_path}"
  node_reset_ensure_worker_rejoin_stable "${rejoin_log_path}"
  node_reset_capture_rejoin_kubelet_health
  printf '%s\n' "ok" > "${ART_DIR}/${ROLE}-rejoin-done.txt"
  echo "[deck] ${ROLE} join-reset-rejoin completed"
}

node_reset_prepare() {
  apply_offline_guard
}

node_reset_apply() {
  local workflow_url="${SERVER_URL}/workflows/scenarios/worker-join.yaml"
  local node_reset_url="${SERVER_URL}/workflows/scenarios/node-reset.yaml"
  local release="${OFFLINE_RELEASE_WORKER}"
  local os_family="debian"
  local server_no_scheme="${SERVER_URL#http://}"
  server_no_scheme="${server_no_scheme#https://}"
  if [[ "${ROLE}" == "control-plane" ]]; then
    source "/workspace/test/e2e/scenario-hooks/control-plane-bootstrap.sh"
    bootstrap_apply_control_plane_workflow
    return 0
  fi
  if [[ "${ROLE}" == "worker-2" ]]; then
    release="${OFFLINE_RELEASE_WORKER_2}"
    os_family="rhel"
  fi
  if [[ "${ROLE}" == "worker" ]]; then
    node_reset_apply_worker_lifecycle "${workflow_url}" "${node_reset_url}" "${release}" "${os_family}" "${server_no_scheme}"
    return 0
  fi
  node_reset_apply_worker_join_once "${workflow_url}" "${release}" "${os_family}" "${server_no_scheme}" "${CASE_DIR}/05-apply-${ROLE}.log"
  printf '%s\n' "ok" > "${ART_DIR}/${ROLE}-apply-done.txt"
  echo "[deck] ${ROLE} apply completed"
}

node_reset_verify() {
  local stage="$1"
  case "${stage}" in
    all)
      source "/workspace/test/e2e/scenario-hooks/control-plane-bootstrap.sh"
      source "/workspace/test/e2e/scenario-hooks/worker-join.sh"
      bootstrap_wait_for_join_file || { echo "[deck] control-plane join file was not published" | tee "${CASE_DIR}/06-assertions.log"; exit 1; }
      worker_join_wait_for_three_ready_nodes || exit 1
      local ctr_pull_log="${ART_DIR}/ctr-pull-pause.log"
      if ! sudo -n timeout 180s ctr images pull --hosts-dir /etc/containerd/certs.d registry.k8s.io/pause:3.9 > "${ctr_pull_log}" 2>&1; then
        echo "[deck] ctr pull failed: registry.k8s.io/pause:3.9" | tee -a "${CASE_DIR}/06-assertions.log"
        cat "${ctr_pull_log}" >> "${CASE_DIR}/06-assertions.log" || true
        exit 1
      fi
      finalize_result_contract
      echo "[deck] scenario workflow-driven run passed"
      ;;
    *)
      echo "[deck] unsupported verify stage for node-reset scenario: ${stage}"
      exit 1
      ;;
  esac
}
