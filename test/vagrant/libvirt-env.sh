#!/usr/bin/env bash
set -euo pipefail

DECK_BACKUP_ROOT="${DECK_BACKUP_ROOT:-}"
DECK_LIBVIRT_POOL_NAME_EXPLICIT="${DECK_LIBVIRT_POOL_NAME:-}"
DECK_LIBVIRT_POOL_NAME="${DECK_LIBVIRT_POOL_NAME:-deck}"
DECK_LIBVIRT_URI="${DECK_LIBVIRT_URI:-qemu:///system}"
DECK_LIBVIRT_USE_SESSION="${DECK_LIBVIRT_USE_SESSION:-}"
DECK_VAGRANT_LIBVIRT_DRIVER="${DECK_VAGRANT_LIBVIRT_DRIVER:-}"
DECK_VAGRANT_QEMU_USE_SESSION="${DECK_VAGRANT_QEMU_USE_SESSION:-}"
DECK_VAGRANT_BOOT_TIMEOUT="${DECK_VAGRANT_BOOT_TIMEOUT:-}"
if [[ -z "${DECK_BACKUP_ROOT}" ]]; then
  DECK_BACKUP_ROOT="/backup/deck"
  if ! mkdir -p "${DECK_BACKUP_ROOT}" >/dev/null 2>&1; then
    if [[ -n "${HOME:-}" ]] && mkdir -p "${HOME}/.cache/deck" >/dev/null 2>&1; then
      DECK_BACKUP_ROOT="${HOME}/.cache/deck"
    else
      DECK_BACKUP_ROOT="/tmp/deck-backup"
      mkdir -p "${DECK_BACKUP_ROOT}"
    fi
  fi
fi
DECK_LIBVIRT_POOL_PATH="${DECK_LIBVIRT_POOL_PATH:-${DECK_BACKUP_ROOT}/libvirt/pool}"
DECK_VAGRANT_HOME="${DECK_VAGRANT_HOME:-${DECK_BACKUP_ROOT}/vagrant/home}"
DECK_VAGRANT_DOTFILE_PATH="${DECK_VAGRANT_DOTFILE_PATH:-${DECK_BACKUP_ROOT}/vagrant/dotfiles}"

prepare_libvirt_environment() {
  local uri="${DECK_LIBVIRT_URI}"
  local -a virsh_cmd=(virsh -c "${uri}")

  if [[ -z "${DECK_LIBVIRT_USE_SESSION}" ]]; then
    if [[ "${uri}" == *"/session" ]]; then
      DECK_LIBVIRT_USE_SESSION="1"
    else
      DECK_LIBVIRT_USE_SESSION="0"
    fi
  fi

  if [[ -z "${DECK_VAGRANT_QEMU_USE_SESSION}" ]]; then
    DECK_VAGRANT_QEMU_USE_SESSION="${DECK_LIBVIRT_USE_SESSION}"
  fi

  if [[ -z "${DECK_VAGRANT_LIBVIRT_DRIVER}" ]]; then
    if command -v qemu-system-x86_64 >/dev/null 2>&1; then
      kvm_test_pid="$(mktemp "${TMPDIR:-/tmp}/deck-kvm-test-pid.XXXXXX")"
      set +e
      qemu-system-x86_64 -accel kvm -machine none -display none -nodefaults -daemonize -pidfile "${kvm_test_pid}" >/dev/null 2>&1
      kvm_test_rc=$?
      set -e
      if [[ ${kvm_test_rc} -eq 0 ]]; then
        if [[ -f "${kvm_test_pid}" ]]; then
          kill "$(cat "${kvm_test_pid}")" >/dev/null 2>&1 || true
        fi
        DECK_VAGRANT_LIBVIRT_DRIVER="kvm"
      else
        DECK_VAGRANT_LIBVIRT_DRIVER="qemu"
      fi
      rm -f "${kvm_test_pid}"
    else
      DECK_VAGRANT_LIBVIRT_DRIVER="qemu"
    fi
  fi

  if [[ -z "${DECK_VAGRANT_BOOT_TIMEOUT}" ]]; then
    if [[ "${DECK_VAGRANT_LIBVIRT_DRIVER}" == "qemu" ]]; then
      DECK_VAGRANT_BOOT_TIMEOUT="1200"
    else
      DECK_VAGRANT_BOOT_TIMEOUT="300"
    fi
  fi

  mkdir -p "${DECK_BACKUP_ROOT}" "${DECK_BACKUP_ROOT}/libvirt" "${DECK_LIBVIRT_POOL_PATH}" "${DECK_VAGRANT_HOME}" "${DECK_VAGRANT_DOTFILE_PATH}"
  chmod 711 "${DECK_BACKUP_ROOT}" || true
  chmod 755 "${DECK_BACKUP_ROOT}/libvirt" || true
  chmod 777 "${DECK_LIBVIRT_POOL_PATH}" || true
  if command -v chcon >/dev/null 2>&1; then
    chcon -R -t virt_image_t "${DECK_LIBVIRT_POOL_PATH}" >/dev/null 2>&1 || true
  fi

  if ! command -v virsh >/dev/null 2>&1; then
    echo "[deck] virsh command not found"
    exit 1
  fi

  pool_name_default=0
  if [[ -z "${DECK_LIBVIRT_POOL_NAME_EXPLICIT}" && "${DECK_LIBVIRT_POOL_NAME}" == "deck" ]]; then
    pool_name_default=1
  fi

  for _pool_try in 1 2; do
    if ! "${virsh_cmd[@]}" pool-info "${DECK_LIBVIRT_POOL_NAME}" >/dev/null 2>&1; then
      xml_path="$(mktemp "${TMPDIR:-/tmp}/deck-libvirt-pool.XXXXXX.xml")"
      cat >"${xml_path}" <<EOF
<pool type='dir'>
  <name>${DECK_LIBVIRT_POOL_NAME}</name>
  <target>
    <path>${DECK_LIBVIRT_POOL_PATH}</path>
  </target>
</pool>
EOF
      "${virsh_cmd[@]}" pool-define "${xml_path}" >/dev/null
      rm -f "${xml_path}"
    fi

    pool_xml="$("${virsh_cmd[@]}" pool-dumpxml "${DECK_LIBVIRT_POOL_NAME}")"
    case "${pool_xml}" in
      *"<path>${DECK_LIBVIRT_POOL_PATH}</path>"*)
        break
        ;;
    esac

    if [[ ${_pool_try} -eq 1 && ${pool_name_default} -eq 1 ]]; then
      pool_suffix=""
      if command -v sha256sum >/dev/null 2>&1; then
        pool_suffix="$(printf '%s' "${DECK_LIBVIRT_POOL_PATH}" | sha256sum | awk '{print substr($1,1,8)}')"
      elif command -v shasum >/dev/null 2>&1; then
        pool_suffix="$(printf '%s' "${DECK_LIBVIRT_POOL_PATH}" | shasum -a 256 | awk '{print substr($1,1,8)}')"
      else
        pool_suffix="$(python3 - <<'PY' "${DECK_LIBVIRT_POOL_PATH}"
import hashlib
import sys
print(hashlib.sha256(sys.argv[1].encode('utf-8')).hexdigest()[:8])
PY
)"
      fi

      DECK_LIBVIRT_POOL_NAME="deck-${pool_suffix}"
      pool_name_default=0
      echo "[deck] libvirt pool path mismatch for 'deck'; switching to pool '${DECK_LIBVIRT_POOL_NAME}'"
      continue
    fi

    echo "[deck] libvirt pool path mismatch: name=${DECK_LIBVIRT_POOL_NAME} expected=${DECK_LIBVIRT_POOL_PATH}"
    exit 1
  done

  "${virsh_cmd[@]}" pool-build "${DECK_LIBVIRT_POOL_NAME}" >/dev/null 2>&1 || true
  "${virsh_cmd[@]}" pool-start "${DECK_LIBVIRT_POOL_NAME}" >/dev/null 2>&1 || true
  "${virsh_cmd[@]}" pool-autostart "${DECK_LIBVIRT_POOL_NAME}" >/dev/null 2>&1 || true

  if [[ "${DECK_LIBVIRT_USE_SESSION}" != "1" ]]; then
    if ! "${virsh_cmd[@]}" net-info default >/dev/null 2>&1; then
      net_xml="$(mktemp "${TMPDIR:-/tmp}/deck-libvirt-default-net.XXXXXX.xml")"
      cat >"${net_xml}" <<EOF
<network>
  <name>default</name>
  <forward mode='nat'/>
  <bridge name='virbr0' stp='on' delay='0'/>
  <ip address='192.168.122.1' netmask='255.255.255.0'>
    <dhcp>
      <range start='192.168.122.2' end='192.168.122.254'/>
    </dhcp>
  </ip>
</network>
EOF
      "${virsh_cmd[@]}" net-define "${net_xml}" >/dev/null
      rm -f "${net_xml}"
    fi
    "${virsh_cmd[@]}" net-start default >/dev/null 2>&1 || true
    "${virsh_cmd[@]}" net-autostart default >/dev/null 2>&1 || true

    deck_net_xml="$(mktemp "${TMPDIR:-/tmp}/deck-libvirt-deck-net.XXXXXX.xml")"
    cat >"${deck_net_xml}" <<EOF
<network>
  <name>deck-vagrant</name>
  <forward mode='nat'/>
  <bridge name='virbr57' stp='on' delay='0'/>
  <ip address='192.168.57.1' netmask='255.255.255.0'/>
</network>
EOF
    redefine_deck_network=0
    if ! "${virsh_cmd[@]}" net-info deck-vagrant >/dev/null 2>&1; then
      redefine_deck_network=1
    else
      deck_xml_current="$("${virsh_cmd[@]}" net-dumpxml deck-vagrant 2>/dev/null || true)"
      case "${deck_xml_current}" in
        *"<dhcp>"*)
          redefine_deck_network=1
          ;;
      esac
    fi

    if [[ ${redefine_deck_network} -eq 1 ]]; then
      "${virsh_cmd[@]}" net-destroy deck-vagrant >/dev/null 2>&1 || true
      "${virsh_cmd[@]}" net-undefine deck-vagrant >/dev/null 2>&1 || true
      "${virsh_cmd[@]}" net-define "${deck_net_xml}" >/dev/null
    fi

    rm -f "${deck_net_xml}"
    "${virsh_cmd[@]}" net-start deck-vagrant >/dev/null 2>&1 || true
    "${virsh_cmd[@]}" net-autostart deck-vagrant >/dev/null 2>&1 || true
  fi

  export VAGRANT_HOME="${DECK_VAGRANT_HOME}"
  export VAGRANT_DOTFILE_PATH="${DECK_VAGRANT_DOTFILE_PATH}"

  if ! command -v vagrant >/dev/null 2>&1; then
    echo "[deck] vagrant command not found"
    exit 1
  fi

  if ! vagrant plugin list | grep -q '^vagrant-libvirt '; then
    vagrant plugin install vagrant-libvirt
  fi

  export DECK_BACKUP_ROOT
  export DECK_LIBVIRT_POOL_NAME
  export DECK_LIBVIRT_POOL_PATH
  export DECK_LIBVIRT_URI
  export DECK_LIBVIRT_USE_SESSION
  export DECK_VAGRANT_LIBVIRT_DRIVER
  export DECK_VAGRANT_QEMU_USE_SESSION
  export DECK_VAGRANT_BOOT_TIMEOUT
  export DECK_VAGRANT_HOME
  export DECK_VAGRANT_DOTFILE_PATH
  export LIBVIRT_DEFAULT_STORAGE_POOL="${DECK_LIBVIRT_POOL_NAME}"
  export LIBVIRT_DEFAULT_URI="${DECK_LIBVIRT_URI}"
}
