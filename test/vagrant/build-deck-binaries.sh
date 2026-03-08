#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="${1:?root directory path required}"

BIN_DIR="${ROOT_DIR}/test/artifacts/bin"
DECK_BIN_AMD64="${BIN_DIR}/deck-linux-amd64"
DECK_BIN_ARM64="${BIN_DIR}/deck-linux-arm64"
DECK_HOST_BIN="${BIN_DIR}/deck-host"
STAMP_FILE="${BIN_DIR}/deck-build.stamp"

mkdir -p "${BIN_DIR}"

desired_stamp=""
if [[ -d "${ROOT_DIR}/.git" ]]; then
  desired_stamp="git:$(git -C "${ROOT_DIR}" rev-parse HEAD 2>/dev/null || true)"
fi
if [[ -z "${desired_stamp}" ]]; then
  desired_stamp="time:$(date +%Y%m%d%H%M%S)"
fi

current_stamp=""
if [[ -f "${STAMP_FILE}" ]]; then
  current_stamp="$(cat "${STAMP_FILE}" 2>/dev/null || true)"
fi

needs_build=0
for f in "${DECK_BIN_AMD64}" "${DECK_BIN_ARM64}" "${DECK_HOST_BIN}"; do
  if [[ ! -x "${f}" ]]; then
    needs_build=1
  fi
done
if [[ "${current_stamp}" != "${desired_stamp}" ]]; then
  needs_build=1
fi

if [[ "${needs_build}" == "1" ]]; then
  echo "[deck] building deck binaries on remote host"
  GOOS=linux GOARCH=amd64 go build -o "${DECK_BIN_AMD64}" ./cmd/deck
  GOOS=linux GOARCH=arm64 go build -o "${DECK_BIN_ARM64}" ./cmd/deck
  go build -o "${DECK_HOST_BIN}" ./cmd/deck
  printf '%s\n' "${desired_stamp}" > "${STAMP_FILE}"
else
  echo "[deck] reusing existing deck binaries"
fi
