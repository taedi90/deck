#!/usr/bin/env bash

bootstrap_wait_for_join_file() {
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

bootstrap_wait_for_single_ready_control_plane() {
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
with open(path, 'r', encoding='utf-8') as fp:
    for line in fp:
        line = line.strip()
        if not line or line.startswith('NAME'):
            continue
        parts = line.split()
        if len(parts) < 3:
            continue
        total += 1
        if parts[1] == 'Ready':
            ready += 1
            if 'control-plane' in parts[2]:
                control_plane_ready += 1
raise SystemExit(0 if total == 1 and ready == 1 and control_plane_ready == 1 else 1)
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

bootstrap_apply_control_plane_workflow() {
  CONTROL_PLANE_WORKFLOW_URL="${SERVER_URL}/files/workflows/scenarios/control-plane-bootstrap.yaml"
  local server_no_scheme="${SERVER_URL#http://}"
  server_no_scheme="${server_no_scheme#https://}"
  sudo -n "${DECK_BIN}" apply --file "${CONTROL_PLANE_WORKFLOW_URL}" --phase install \
    --var "serverURL=${server_no_scheme}" \
    --var "registryHost=${server_no_scheme}" \
    --var "release=${OFFLINE_RELEASE_CONTROL_PLANE}" \
    --var "kubernetesVersion=${KUBERNETES_VERSION}" > "${CASE_DIR}/04-apply-control-plane.log" 2>&1
}

bootstrap_prepare() {
  apply_offline_guard
}

bootstrap_apply() {
  bootstrap_apply_control_plane_workflow
}

bootstrap_verify() {
  local stage="$1"
  case "${stage}" in
    bootstrap|install)
      if ! bootstrap_wait_for_join_file; then
        echo "[deck] control-plane join file was not published" | tee "${CASE_DIR}/06-assertions.log"
        exit 1
      fi
      bootstrap_wait_for_single_ready_control_plane || exit 1
      finalize_result_contract
      ;;
    *)
      echo "[deck] unsupported verify stage for bootstrap scenario: ${stage}"
      exit 1
      ;;
  esac
}
