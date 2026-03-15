#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="${1:?root dir required}"
TARGET_DIR="${2:?target dir required}"

CANONICAL_ROOT="${ROOT_DIR}/test/workflows"

mkdir -p "${TARGET_DIR}"

if [[ -d "${CANONICAL_ROOT}" ]]; then
  cp -a "${CANONICAL_ROOT}/." "${TARGET_DIR}/"
fi
