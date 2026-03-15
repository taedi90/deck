#!/usr/bin/env bash
set -euo pipefail

if [[ "${1:-}" == "--help" || "${1:-}" == "-h" ]]; then
  cat <<'EOF'
Usage: test/e2e/vagrant/render-workflows.sh <root-dir> <target-dir>

Copy the canonical workflow tree from test/workflows/ into a target workspace.
EOF
  exit 0
fi

ROOT_DIR="${1:?root dir required}"
TARGET_DIR="${2:?target dir required}"

CANONICAL_ROOT="${ROOT_DIR}/test/workflows"

mkdir -p "${TARGET_DIR}"

if [[ -d "${CANONICAL_ROOT}" ]]; then
  cp -a "${CANONICAL_ROOT}/." "${TARGET_DIR}/"
fi
