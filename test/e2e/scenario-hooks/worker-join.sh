#!/usr/bin/env bash

worker_join_apply_once() {
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

worker_join_reconcile_cni() {
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
}

worker_join_ensure_stable() {
  local log_path="$1"
  if [[ ! -s /var/lib/kubelet/config.yaml ]]; then
    [[ -s /tmp/deck/join.txt ]] || { echo "[deck] missing join file for worker join fallback" | tee -a "${log_path}"; exit 1; }
    local raw_join_cmd
    raw_join_cmd="$(tr -d '\r' < /tmp/deck/join.txt)"
    [[ -n "${raw_join_cmd}" ]] || { echo "[deck] empty join command for worker join fallback" | tee -a "${log_path}"; exit 1; }
    sudo -n bash -o pipefail -c "${raw_join_cmd} --cri-socket unix:///run/containerd/containerd.sock --ignore-preflight-errors=Swap,FileExisting-crictl,FileExisting-conntrack,FileExisting-socat" >> "${log_path}" 2>&1
  fi
  worker_join_reconcile_cni >> "${log_path}" 2>&1
  sudo -n systemctl enable --now kubelet
  local i
  for i in $(seq 1 12); do
    if [[ -s /var/lib/kubelet/config.yaml ]] && sudo -n systemctl is-active --quiet kubelet; then
      return 0
    fi
    sleep 5
  done
  echo "[deck] worker kubelet did not stabilize after join" | tee -a "${log_path}"
  ls -l /var/lib/kubelet | tee -a "${log_path}" || true
  sudo -n systemctl status kubelet --no-pager | tee -a "${log_path}" || true
  sudo -n journalctl -u kubelet -n 80 --no-pager | tee -a "${log_path}" || true
  exit 1
}

worker_join_wait_for_three_ready_nodes() {
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
with open(path, 'r', encoding='utf-8') as fp:
    for line in fp:
        line = line.strip()
        if not line or line.startswith('NAME'):
            continue
        parts = line.split()
        if len(parts) < 2:
            continue
        total += 1
        nodes.add(parts[0])
        if parts[1] == 'Ready':
            ready += 1
raise SystemExit(0 if total == 3 and ready == 3 and {'control-plane','worker','worker-2'} <= nodes else 1)
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
  cp "${pods_file}" "${REPORT_DIR}/kube-system-pods.txt" || true
  [[ "${ok}" == "1" ]] || { echo "[deck] expected 3 Ready nodes but cluster did not converge" | tee -a "${CASE_DIR}/06-assertions.log"; return 1; }
}

worker_join_prepare() {
  apply_offline_guard
}

worker_join_apply() {
  local workflow_url="${SERVER_URL}/workflows/scenarios/worker-join.yaml"
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
  worker_join_apply_once "${workflow_url}" "${release}" "${os_family}" "${server_no_scheme}" "${CASE_DIR}/05-apply-${ROLE}.log"
  printf '%s\n' "ok" > "${ART_DIR}/${ROLE}-apply-done.txt"
  echo "[deck] ${ROLE} apply completed"
}

worker_join_verify() {
  local stage="$1"
  case "${stage}" in
    cluster|join)
      worker_join_wait_for_three_ready_nodes || exit 1
      verify_cluster_contract
      finalize_result_contract
      echo "[deck] scenario workflow-driven run passed"
      ;;
    *)
      echo "[deck] unsupported verify stage for worker-join scenario: ${stage}"
      exit 1
      ;;
  esac
}
