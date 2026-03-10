#!/usr/bin/env bash
set -euo pipefail

ROLE="${1:?role required}"
ACTION="${2:?action required}"
ART_DIR_REL="${ART_DIR_REL:?ART_DIR_REL is required}"
ART_DIR="/workspace/${ART_DIR_REL}"
CASE_DIR="${ART_DIR}/cases"
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
PREPARED_BUNDLE_REL="${DECK_PREPARED_BUNDLE_REL:-}"
SERVER_ROOT="/tmp/deck/server-root"
DECK_BIN="/tmp/deck/deck"
SERVER_PID=""
REPO_TYPE="apt-flat"
OFFLINE_GUARD_ACTIVE=0
TEMPLATE_DIR="/workspace/test/vagrant/workflows"
KEEP_PROCESSES=0
SERVER_PID_FILE="/tmp/deck/offline-server.pid"

mkdir -p "${ART_DIR}" "${CASE_DIR}" /tmp/deck
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

if [[ ! -f "/workspace/test/artifacts/bin/deck-linux-${ARCH}" ]]; then
  echo "[deck] missing host-built deck binary: /workspace/test/artifacts/bin/deck-linux-${ARCH}"
  exit 1
fi

if [[ ! -x "${DECK_BIN}" ]]; then
  cp "/workspace/test/artifacts/bin/deck-linux-${ARCH}" "${DECK_BIN}"
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

render_prepare_fragments() {
	local target_dir="$1"
	rm -rf "${target_dir}"
	mkdir -p "${target_dir}"
	cp -a "${TEMPLATE_DIR}/offline-multinode/." "${target_dir}/"
}

render_apply_fragments() {
	local target_dir="$1"
	rm -rf "${target_dir}"
	mkdir -p "${target_dir}"
	cp -a "${TEMPLATE_DIR}/offline-multinode/." "${target_dir}/"
}

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

write_prepare_workflows() {
	local root="/tmp/deck/offline-pack"
	local wf_dir="${root}/workflows"
	local fragment_dir="${wf_dir}/offline-multinode"
	rm -rf "${root}"
	mkdir -p "${wf_dir}"
	render_prepare_fragments "${fragment_dir}"
	cp "${TEMPLATE_DIR}/offline-multinode/profile/prepare.yaml" "${wf_dir}/pack.yaml"
  cat > "${wf_dir}/apply.yaml" <<'EOF'
role: apply
version: v1alpha1
phases:
  - name: install
    steps:
      - id: noop
        apiVersion: deck/v1alpha1
        kind: RunCommand
        spec:
          command: ["sh", "-c", "true"]
EOF
  printf '{}\n' > "${wf_dir}/vars.yaml"
}

prepare_server_bundle() {
  if [[ -n "${PREPARED_BUNDLE_REL}" ]] && [[ -f "/workspace/${PREPARED_BUNDLE_REL}/.deck/manifest.json" ]]; then
    sudo -n rm -rf "${SERVER_ROOT}"
    mkdir -p "${SERVER_ROOT}"
    cp -a "/workspace/${PREPARED_BUNDLE_REL}/." "${SERVER_ROOT}/"
    printf 'prepared-bundle=%s\n' "${PREPARED_BUNDLE_REL}" > "${CASE_DIR}/01-prepare.log"
    return 0
  fi

  write_prepare_workflows
  local out_tar="/tmp/deck/offline-pack-bundle.tar"
  (cd /tmp/deck/offline-pack && sudo -n "${DECK_BIN}" pack --out "${out_tar}" \
    --var "kubernetesVersion=${KUBERNETES_VERSION}" \
    --var "arch=${ARCH}" \
    --var "backendRuntime=docker") > "${CASE_DIR}/01-prepare.log" 2>&1
  sudo -n rm -rf "${SERVER_ROOT}"
  mkdir -p "${SERVER_ROOT}"
  tar -xf "${out_tar}" -C "${SERVER_ROOT}" --strip-components=1
  if [[ ! -f "${SERVER_ROOT}/.deck/manifest.json" ]]; then
    echo "[deck] prepared bundle manifest missing: ${SERVER_ROOT}/.deck/manifest.json"
    exit 1
  fi
}

write_runtime_workflows() {
	local workflow_dir="${SERVER_ROOT}/files/workflows"
	local fragment_dir="${workflow_dir}/offline-multinode"
	mkdir -p "${workflow_dir}"
	ensure_advertise_address
	render_apply_fragments "${fragment_dir}"
	cp "${TEMPLATE_DIR}/offline-multinode/profile/control-plane.yaml" "${workflow_dir}/offline-pull-control-plane.yaml"
	cp "${TEMPLATE_DIR}/offline-multinode/profile/worker.yaml" "${workflow_dir}/offline-pull-worker.yaml"
	cp "${TEMPLATE_DIR}/offline-multinode/profile/worker.yaml" "${workflow_dir}/offline-pull-worker-2.yaml"
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
  sudo -n bash -c "nohup \"${DECK_BIN}\" serve --root \"${SERVER_ROOT}\" --addr \"${SERVER_BIND_ADDR}\" > \"${CASE_DIR}/02-server.log\" 2>&1 < /dev/null & echo \$! > \"${SERVER_PID_FILE}\""
  SERVER_PID="$(cat "${SERVER_PID_FILE}")"
  if ! sudo -n kill -0 "${SERVER_PID}" >/dev/null 2>&1; then
    echo "[deck] server failed to stay running after start"
    exit 1
  fi
}

wait_for_join_file() {
  local i
  for ((i=1; i<=120; i++)); do
    if [[ -s "${SERVER_ROOT}/files/cluster/join.txt" ]]; then
      cp "${SERVER_ROOT}/files/cluster/join.txt" "${ART_DIR}/join.txt"
      return 0
    fi
    sleep 2
  done
  return 1
}

apply_control_plane_workflow() {
  local server_no_scheme="${SERVER_URL#http://}"
  server_no_scheme="${server_no_scheme#https://}"
  sudo -n "${DECK_BIN}" apply --file "${SERVER_URL}/files/workflows/offline-pull-control-plane.yaml" --phase install \
    --var "serverURL=${server_no_scheme}" \
    --var "registryHost=${server_no_scheme}" \
    --var "release=${OFFLINE_RELEASE_CONTROL_PLANE}" \
    --var "kubernetesVersion=${KUBERNETES_VERSION}" > "${CASE_DIR}/04-apply-control-plane.log" 2>&1
}

apply_worker_workflow() {
  local workflow_url="${SERVER_URL}/files/workflows/offline-pull-worker.yaml"
  local release="${OFFLINE_RELEASE_WORKER}"
  local server_no_scheme="${SERVER_URL#http://}"
  server_no_scheme="${server_no_scheme#https://}"
  if [[ "${ROLE}" == "worker-2" ]]; then
    workflow_url="${SERVER_URL}/files/workflows/offline-pull-worker-2.yaml"
    release="${OFFLINE_RELEASE_WORKER_2}"
  fi
  sudo -n "${DECK_BIN}" apply --file "${workflow_url}" --phase install \
    --var "serverURL=${server_no_scheme}" \
    --var "registryHost=${server_no_scheme}" \
    --var "release=${release}" \
    --var "joinFile=/tmp/deck/join.txt" > "${CASE_DIR}/05-apply-${ROLE}.log" 2>&1
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

if [[ "${ACTION}" != "orchestrate" && "${ACTION}" != "cleanup" ]]; then
  KEEP_PROCESSES=1
fi

if [[ "${ACTION}" == "apply-worker" ]]; then
  apply_worker_workflow
  printf '%s\n' "ok" > "${ART_DIR}/${ROLE}-apply-done.txt"
  echo "[deck] ${ROLE} apply completed"
  exit 0
fi

if [[ "${ROLE}" != "control-plane" ]]; then
  echo "[deck] unsupported role/action: role=${ROLE} action=${ACTION}"
  exit 1
fi

if [[ "${ACTION}" == "cleanup" ]]; then
  KEEP_PROCESSES=0
  cleanup
  exit 0
fi

case "${ACTION}" in
  start-server)
    prepare_server_bundle
    write_runtime_workflows
    start_server_background
    if ! wait_server_health; then
      echo "[deck] server health check failed" | tee "${CASE_DIR}/06-assertions.log"
      exit 1
    fi
    apply_offline_guard
    ;;
  apply-control-plane)
    apply_control_plane_workflow
    ;;
  verify-install)
    if ! wait_for_join_file; then
      echo "[deck] control-plane join file was not published" | tee "${CASE_DIR}/06-assertions.log"
      exit 1
    fi
    ;;
  assert-cluster)
    if ! wait_for_three_ready_nodes; then
      exit 1
    fi

    CTR_PULL_LOG="${ART_DIR}/ctr-pull-pause.log"
    if ! sudo -n timeout 180s ctr images pull --hosts-dir /etc/containerd/certs.d registry.k8s.io/pause:3.9 > "${CTR_PULL_LOG}" 2>&1; then
      echo "[deck] ctr pull failed: registry.k8s.io/pause:3.9" | tee -a "${CASE_DIR}/06-assertions.log"
      cat "${CTR_PULL_LOG}" >> "${CASE_DIR}/06-assertions.log" || true
      exit 1
    fi

    cat > "${ART_DIR}/offline-multinode-result.txt" <<EOF
scenario=offline-multinode
result=PASS
jobs=offline-cp-install,offline-worker-join,offline-worker-2-join
server=${SERVER_URL}
EOF
    printf '%s\n' "PASS" > "${ART_DIR}/offline-multinode-pass.txt"
    echo "[deck] offline multi-node workflow-driven scenario passed"
    ;;
  collect)
    mkdir -p "${ART_DIR}"
    ;;
  orchestrate)
    KEEP_PROCESSES=0
    prepare_server_bundle
    write_runtime_workflows
    start_server_background
    if ! wait_server_health; then
      echo "[deck] server health check failed" | tee "${CASE_DIR}/06-assertions.log"
      exit 1
    fi
    apply_offline_guard
    apply_control_plane_workflow
    if ! wait_for_join_file; then
      echo "[deck] control-plane join file was not published" | tee "${CASE_DIR}/06-assertions.log"
      exit 1
    fi
    if ! wait_for_three_ready_nodes; then
      exit 1
    fi
    ;;
  *)
    echo "[deck] unsupported role/action: role=${ROLE} action=${ACTION}"
    exit 1
    ;;
esac
